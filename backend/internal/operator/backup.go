package operator

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog"
)

// Manifest is written into every archive as manifest.json.
type Manifest struct {
	Kind      string           `json:"kind"`
	CreatedAt time.Time        `json:"created_at"`
	Databases []ManifestDB     `json:"databases"`
	Buckets   []ManifestBucket `json:"buckets"`
	Restore   string           `json:"restore"`
}

type ManifestDB struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	File  string `json:"file"`
}

type ManifestBucket struct {
	Name    string `json:"name"`
	Objects int    `json:"objects"`
}

type Operator struct {
	cfg   *Config
	pg    *pg
	store *store // nil when backups are off
	files *minio.Client
	log   zerolog.Logger

	mu      sync.Mutex
	running string
}

// lock marks a backup or drop as running; only one runs at a time.
func (o *Operator) lock(what string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running != "" {
		return false
	}
	o.running = what
	return true
}

func (o *Operator) unlock() {
	o.mu.Lock()
	o.running = ""
	o.mu.Unlock()
}

func (o *Operator) Running() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

// backup archives one database (only) or all of them, plus the server's
// roles and, for full backups, the module file buckets. Callers hold the
// lock.
func (o *Operator) backup(ctx context.Context, kind string, only string) (run Run) {
	run = Run{Kind: kind, StartedAt: time.Now().UTC()}
	defer func() {
		run.FinishedAt = time.Now().UTC()
		ev := o.log.Info()
		if !run.OK {
			ev = o.log.Error().Str("error", run.Error)
		}
		ev.Str("kind", kind).Str("key", run.Key).Int64("size", run.SizeBytes).
			Dur("took", run.FinishedAt.Sub(run.StartedAt)).Msg("backup")
	}()
	fail := func(err error) Run {
		run.Error = err.Error()
		return run
	}
	if o.store == nil {
		return fail(ErrDisabled)
	}

	all, err := o.pg.list(ctx)
	if err != nil {
		return fail(fmt.Errorf("list databases: %w", err))
	}
	var dbs []Database
	for _, d := range all {
		if only == "" || d.Name == only {
			dbs = append(dbs, d)
		}
	}
	if only != "" && len(dbs) == 0 {
		return fail(ErrNotFound)
	}

	dir, err := os.MkdirTemp("", "backup-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(dir)

	man := Manifest{
		Kind:      kind,
		CreatedAt: run.StartedAt,
		Restore:   "See docs/operations.md, section Restore.",
	}
	if err := o.dump(ctx, "pg_dumpall", filepath.Join(dir, "globals.sql"),
		"--globals-only", "--dbname="+o.cfg.PGAdminURL); err != nil {
		return fail(err)
	}
	for _, d := range dbs {
		file := "databases/" + d.Name + ".dump"
		if err := os.MkdirAll(filepath.Join(dir, "databases"), 0o700); err != nil {
			return fail(err)
		}
		if err := o.dump(ctx, "pg_dump", filepath.Join(dir, file),
			"--format=custom", "--dbname="+o.pg.dbURL(d.Name)); err != nil {
			return fail(err)
		}
		man.Databases = append(man.Databases, ManifestDB{Name: d.Name, Owner: d.Owner, File: file})
		run.Databases = append(run.Databases, d.Name)
	}

	buckets := o.cfg.FileBuckets
	if only != "" {
		buckets = nil
	}

	stamp := run.StartedAt.Format(stampLayout)
	if kind == "pre-drop" {
		run.Key = prefixPreDrop + only + "/" + stamp + ".tar.age"
	} else {
		run.Key = prefixBackups + stamp + "-" + kind + ".tar.age"
	}

	pr, pw := io.Pipe()
	writeErr := make(chan error, 1)
	go func() {
		err := o.writeArchive(ctx, pw, dir, &man, buckets)
		_ = pw.CloseWithError(err)
		writeErr <- err
	}()
	info, err := o.store.mc.PutObject(ctx, o.store.bucket, run.Key, pr, -1, minio.PutObjectOptions{
		ContentType:  "application/octet-stream",
		UserMetadata: map[string]string{"kind": kind},
		PartSize:     16 << 20,
	})
	_ = pr.CloseWithError(err)
	if werr := <-writeErr; werr != nil {
		_ = o.store.mc.RemoveObject(ctx, o.store.bucket, run.Key, minio.RemoveObjectOptions{})
		return fail(werr)
	}
	if err != nil {
		return fail(fmt.Errorf("upload: %w", err))
	}
	for _, b := range man.Buckets {
		run.Buckets = append(run.Buckets, b.Name)
	}
	run.SizeBytes = info.Size
	run.OK = true
	return run
}

func (o *Operator) dump(ctx context.Context, bin, out string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, append(args, "--file="+out)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", bin, err, strings.TrimSpace(string(b)))
	}
	return nil
}

// writeArchive streams tar → age encryption → w.
func (o *Operator) writeArchive(ctx context.Context, w io.Writer, dir string, man *Manifest, buckets []string) error {
	enc, err := age.Encrypt(w, o.cfg.Recipients...)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)

	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return writeEntry(tw, filepath.ToSlash(rel), fi.Size(), fi.ModTime(), f)
	})
	if err != nil {
		return err
	}

	for _, b := range buckets {
		n := 0
		for obj := range o.files.ListObjects(ctx, b, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				return fmt.Errorf("list bucket %s: %w", b, obj.Err)
			}
			r, err := o.files.GetObject(ctx, b, obj.Key, minio.GetObjectOptions{})
			if err != nil {
				return fmt.Errorf("read %s/%s: %w", b, obj.Key, err)
			}
			err = writeEntry(tw, "files/"+b+"/"+obj.Key, obj.Size, obj.LastModified, r)
			_ = r.Close()
			if err != nil {
				return fmt.Errorf("archive %s/%s: %w", b, obj.Key, err)
			}
			n++
		}
		man.Buckets = append(man.Buckets, ManifestBucket{Name: b, Objects: n})
	}

	mb, _ := json.MarshalIndent(man, "", "  ")
	if err := writeEntry(tw, "manifest.json", int64(len(mb)), time.Now(), strings.NewReader(string(mb))); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}

func writeEntry(tw *tar.Writer, name string, size int64, mod time.Time, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: mod, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.CopyN(tw, r, size)
	return err
}

// RunBackup takes a full backup and records it; on success old backups are
// pruned.
func (o *Operator) RunBackup(ctx context.Context, kind string) Run {
	defer o.unlock()
	run := o.backup(ctx, kind, "")
	st, err := o.store.status(ctx)
	if err != nil {
		o.log.Error().Err(err).Msg("read backup status")
	}
	st.Last = &run
	if run.OK {
		st.LastSuccess = &run
	}
	if kind == "scheduled" {
		st.LastScheduledAttempt = run.StartedAt
	}
	if err := o.store.writeJSON(ctx, keyStatus, st); err != nil {
		o.log.Error().Err(err).Msg("write backup status")
	}
	if run.OK {
		if err := o.prune(ctx); err != nil {
			o.log.Error().Err(err).Msg("prune backups")
		}
	}
	return run
}

// prune thins out scheduled backups to the newest of each of the last N
// days, ISO weeks and months; manual backups are kept for N days.
// Pre-drop backups are never pruned.
func (o *Operator) prune(ctx context.Context) error {
	set, err := o.store.settings(ctx)
	if err != nil {
		return err
	}
	all, err := o.store.list(ctx)
	if err != nil {
		return err
	}
	var regular []BackupObject
	for _, b := range all {
		if strings.HasPrefix(b.Key, prefixBackups) {
			regular = append(regular, b)
		}
	}
	keep := retained(regular, set, o.cfg.Location)
	for _, b := range regular {
		if keep[b.Key] {
			continue
		}
		if err := o.store.mc.RemoveObject(ctx, o.store.bucket, b.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
		o.log.Info().Str("key", b.Key).Msg("backup pruned")
	}
	return nil
}

func retained(backups []BackupObject, set Settings, loc *time.Location) map[string]bool {
	keep := map[string]bool{}
	var sorted []BackupObject
	cutoff := time.Now().AddDate(0, 0, -set.KeepDaily)
	for _, b := range backups {
		if b.Kind == "manual" {
			if b.CreatedAt.After(cutoff) {
				keep[b.Key] = true
			}
			continue
		}
		sorted = append(sorted, b)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CreatedAt.After(sorted[j].CreatedAt) })
	if len(sorted) > 0 {
		keep[sorted[0].Key] = true
	}
	bucket := func(n int, period func(time.Time) string) {
		seen := map[string]bool{}
		for _, b := range sorted {
			p := period(b.CreatedAt.In(loc))
			if seen[p] {
				continue
			}
			if len(seen) >= n {
				return
			}
			seen[p] = true
			keep[b.Key] = true
		}
	}
	bucket(set.KeepDaily, func(t time.Time) string { return t.Format("2006-01-02") })
	bucket(set.KeepWeekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-W%02d", y, w) })
	bucket(set.KeepMonthly, func(t time.Time) string { return t.Format("2006-01") })
	return keep
}

// NextRun is the next scheduled backup time.
func (o *Operator) NextRun(set Settings, lastAttempt time.Time) time.Time {
	now := time.Now().In(o.cfg.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), set.Hour, 0, 0, 0, o.cfg.Location)
	if lastAttempt.Before(today) {
		return today
	}
	return today.AddDate(0, 0, 1)
}

// Schedule runs the daily backup. A backup missed while the operator was
// down runs as soon as it is back.
func (o *Operator) Schedule(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		o.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (o *Operator) tick(ctx context.Context) {
	set, err := o.store.settings(ctx)
	if err != nil {
		o.log.Error().Err(err).Msg("read backup settings")
		return
	}
	st, err := o.store.status(ctx)
	if err != nil {
		o.log.Error().Err(err).Msg("read backup status")
		return
	}
	if time.Now().Before(o.NextRun(set, st.LastScheduledAttempt)) {
		return
	}
	if !o.lock("backup") {
		return
	}
	o.RunBackup(ctx, "scheduled")
}
