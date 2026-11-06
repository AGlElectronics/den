package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	prefixBackups = "backups/"
	prefixPreDrop = "pre-drop/"
	keySettings   = "state/settings.json"
	keyStatus     = "state/status.json"
	stampLayout   = "20060102T150405Z"
)

// Settings are changed from Den admin and kept in the backup bucket, so the
// operator needs no database of its own.
type Settings struct {
	Hour        int `json:"hour"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
}

var DefaultSettings = Settings{Hour: 3, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6}

func (s Settings) valid() bool {
	return s.Hour >= 0 && s.Hour <= 23 &&
		s.KeepDaily >= 1 && s.KeepDaily <= 365 &&
		s.KeepWeekly >= 0 && s.KeepWeekly <= 520 &&
		s.KeepMonthly >= 0 && s.KeepMonthly <= 240
}

// Run is one backup attempt.
type Run struct {
	Kind       string    `json:"kind"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
	Key        string    `json:"key,omitempty"`
	SizeBytes  int64     `json:"size_bytes,omitempty"`
	Databases  []string  `json:"databases,omitempty"`
	Buckets    []string  `json:"buckets,omitempty"`
}

type Status struct {
	Last                 *Run      `json:"last,omitempty"`
	LastSuccess          *Run      `json:"last_success,omitempty"`
	LastScheduledAttempt time.Time `json:"last_scheduled_attempt"`
}

type BackupObject struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	Database  string    `json:"database,omitempty"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func newMinio(t S3Target) (*minio.Client, error) {
	u, err := url.Parse(t.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint %q: %w", t.Endpoint, err)
	}
	host := u.Host
	if host == "" {
		host = u.Path
	}
	lookup := minio.BucketLookupAuto
	if t.UsePathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(t.AccessKey, t.SecretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       t.Region,
		BucketLookup: lookup,
	})
}

type store struct {
	mc     *minio.Client
	bucket string
	region string
}

func (s *store) ensureBucket(ctx context.Context) error {
	ok, err := s.mc.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return s.mc.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region})
}

func (s *store) readJSON(ctx context.Context, key string, v any) (bool, error) {
	obj, err := s.mc.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return false, err
	}
	defer obj.Close()
	b, err := io.ReadAll(obj)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, json.Unmarshal(b, v)
}

func (s *store) writeJSON(ctx context.Context, key string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.mc.PutObject(ctx, s.bucket, key, bytes.NewReader(b), int64(len(b)),
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

func (s *store) settings(ctx context.Context) (Settings, error) {
	st := DefaultSettings
	_, err := s.readJSON(ctx, keySettings, &st)
	return st, err
}

func (s *store) status(ctx context.Context) (Status, error) {
	var st Status
	_, err := s.readJSON(ctx, keyStatus, &st)
	return st, err
}

// list returns every backup, newest first.
func (s *store) list(ctx context.Context) ([]BackupObject, error) {
	var out []BackupObject
	for _, prefix := range []string{prefixBackups, prefixPreDrop} {
		for obj := range s.mc.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if obj.Err != nil {
				return nil, obj.Err
			}
			b, ok := parseKey(obj.Key)
			if !ok {
				continue
			}
			b.SizeBytes = obj.Size
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// parseKey understands "backups/<stamp>-<kind>.tar.age" and
// "pre-drop/<db>/<stamp>.tar.age".
func parseKey(key string) (BackupObject, bool) {
	name, ok := strings.CutSuffix(key, ".tar.age")
	if !ok {
		return BackupObject{}, false
	}
	b := BackupObject{Key: key}
	switch {
	case strings.HasPrefix(name, prefixBackups):
		stamp, kind, ok := strings.Cut(strings.TrimPrefix(name, prefixBackups), "-")
		if !ok {
			return b, false
		}
		b.Kind = kind
		name = stamp
	case strings.HasPrefix(name, prefixPreDrop):
		db, stamp, ok := strings.Cut(strings.TrimPrefix(name, prefixPreDrop), "/")
		if !ok {
			return b, false
		}
		b.Kind, b.Database = "pre-drop", db
		name = stamp
	default:
		return b, false
	}
	t, err := time.Parse(stampLayout, name)
	if err != nil {
		return b, false
	}
	b.CreatedAt = t
	return b, true
}

func (s *store) open(ctx context.Context, key string) (*minio.Object, minio.ObjectInfo, error) {
	if _, ok := parseKey(key); !ok {
		return nil, minio.ObjectInfo{}, ErrNotFound
	}
	obj, err := s.mc.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, minio.ObjectInfo{}, err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, minio.ObjectInfo{}, ErrNotFound
		}
		return nil, minio.ObjectInfo{}, err
	}
	return obj, info, nil
}
