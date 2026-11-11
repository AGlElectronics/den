package operator

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/authbase"
)

// StaleAfter is how old the last good backup may get before Den warns.
const StaleAfter = 26 * time.Hour

func New(ctx context.Context, cfg *Config, log zerolog.Logger) (*Operator, error) {
	pool, err := pgxpool.New(ctx, cfg.PGAdminURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	o := &Operator{cfg: cfg, pg: &pg{pool: pool, cfg: cfg}, log: log}
	if cfg.BackupsEnabled {
		mc, err := newMinio(cfg.Backup)
		if err != nil {
			return nil, fmt.Errorf("backup storage: %w", err)
		}
		o.store = &store{mc: mc, bucket: cfg.BackupBucket, region: cfg.Backup.Region}
		if err := o.store.ensureBucket(ctx); err != nil {
			return nil, fmt.Errorf("backup bucket %s: %w", cfg.BackupBucket, err)
		}
		if len(cfg.FileBuckets) > 0 {
			if o.files, err = newMinio(cfg.Files); err != nil {
				return nil, fmt.Errorf("file storage: %w", err)
			}
		}
	}
	return o, nil
}

func (o *Operator) BackupsEnabled() bool { return o.store != nil }

func (o *Operator) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Group(func(r chi.Router) {
		r.Use(o.requireToken)
		r.Get("/v1/status", o.handleStatus)
		r.Put("/v1/settings", o.handleSettings)
		r.Get("/v1/backups", o.handleList)
		r.Post("/v1/backups", o.handleBackupNow)
		r.Get("/v1/backups/download", o.handleDownload)
		r.Post("/v1/databases", o.handleCreateDB)
		r.Delete("/v1/databases/{name}", o.handleDropDB)
	})
	return r
}

func (o *Operator) requireToken(next http.Handler) http.Handler {
	want := []byte("Bearer " + o.cfg.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusDTO struct {
	ManagedDatabases bool       `json:"managed_databases"`
	BackupsEnabled   bool       `json:"backups_enabled"`
	Target           string     `json:"target,omitempty"`
	Recipients       []string   `json:"recipients,omitempty"`
	FileBuckets      []string   `json:"file_buckets"`
	Timezone         string     `json:"timezone"`
	Settings         *Settings  `json:"settings,omitempty"`
	Last             *Run       `json:"last,omitempty"`
	LastSuccess      *Run       `json:"last_success,omitempty"`
	NextRun          *time.Time `json:"next_run,omitempty"`
	Running          string     `json:"running,omitempty"`
	Problem          string     `json:"problem,omitempty"`
	// SameStorageAsData and Warning flag backups kept on the live data's S3
	// service: allowed, but not a real copy.
	SameStorageAsData bool       `json:"same_storage_as_data"`
	Warning           string     `json:"warning,omitempty"`
	Databases         []Database `json:"databases"`
}

// SameStorageWarning is the status warning for BACKUP_SAME_STORAGE_AS_DATA.
const SameStorageWarning = "Backups are stored on the same S3 service as the live data. " +
	"If that service is lost, the data and its backups are lost together; move backups to another provider or location."

func (o *Operator) handleStatus(w http.ResponseWriter, r *http.Request) {
	dbs, err := o.pg.list(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "can't reach the database server: "+err.Error())
		return
	}
	out := statusDTO{
		ManagedDatabases: o.cfg.ManagedDatabases,
		BackupsEnabled:   o.store != nil,
		FileBuckets:      o.cfg.FileBuckets,
		Timezone:         o.cfg.Location.String(),
		Running:          o.Running(),
		Databases:        dbs,
	}
	if out.FileBuckets == nil {
		out.FileBuckets = []string{}
	}
	if o.store != nil {
		out.Target = o.cfg.Backup.Endpoint + "/" + o.cfg.BackupBucket
		out.Recipients = o.cfg.RecipientText
		if o.cfg.SameStorageAsData {
			out.SameStorageAsData, out.Warning = true, SameStorageWarning
		}
		set, err := o.store.settings(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, "can't read backup storage: "+err.Error())
			return
		}
		st, err := o.store.status(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, "can't read backup storage: "+err.Error())
			return
		}
		next := o.NextRun(set, st.LastScheduledAttempt)
		out.Settings, out.Last, out.LastSuccess, out.NextRun = &set, st.Last, st.LastSuccess, &next
		switch {
		case st.Last != nil && !st.Last.OK:
			out.Problem = "The last backup failed: " + st.Last.Error
		case st.LastSuccess == nil && out.Running == "":
			out.Problem = "No backup has been made yet."
		case st.LastSuccess != nil && time.Since(st.LastSuccess.FinishedAt) > StaleAfter:
			out.Problem = "The last good backup is more than a day old."
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (o *Operator) handleSettings(w http.ResponseWriter, r *http.Request) {
	if o.store == nil {
		writeErr(w, http.StatusConflict, "backups are turned off")
		return
	}
	var in Settings
	if err := authbase.DecodeJSON(w, r, &in, authbase.DecodeOptions{MaxBytes: 64 << 10}); err != nil || !in.valid() {
		writeErr(w, http.StatusBadRequest, "hour must be 0–23, keep at least 1 daily backup")
		return
	}
	if err := o.store.writeJSON(r.Context(), keySettings, in); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (o *Operator) handleList(w http.ResponseWriter, r *http.Request) {
	if o.store == nil {
		writeJSON(w, http.StatusOK, []BackupObject{})
		return
	}
	list, err := o.store.list(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if list == nil {
		list = []BackupObject{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (o *Operator) handleBackupNow(w http.ResponseWriter, _ *http.Request) {
	if o.store == nil {
		writeErr(w, http.StatusConflict, "backups are turned off")
		return
	}
	if !o.lock("backup") {
		writeErr(w, http.StatusConflict, "a backup or database removal is already running")
		return
	}
	go o.RunBackup(context.Background(), "manual")
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func (o *Operator) handleDownload(w http.ResponseWriter, r *http.Request) {
	if o.store == nil {
		writeErr(w, http.StatusNotFound, "backups are turned off")
		return
	}
	obj, info, err := o.store.open(r.Context(), r.URL.Query().Get("key"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no such backup")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	_, _ = io.Copy(w, obj)
}

func (o *Operator) handleCreateDB(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := authbase.DecodeJSON(w, r, &in, authbase.DecodeOptions{MaxBytes: 64 << 10}); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	db, err := o.pg.create(r.Context(), in.Name)
	switch {
	case errors.Is(err, ErrDisabled):
		writeErr(w, http.StatusConflict, "Den-managed databases are turned off")
	case errors.Is(err, ErrBadName):
		writeErr(w, http.StatusBadRequest, "database names use lowercase letters, digits and _ and must not be a protected database")
	case errors.Is(err, ErrExists):
		writeErr(w, http.StatusConflict, "a database with this name already exists")
	case err != nil:
		writeErr(w, http.StatusBadGateway, err.Error())
	default:
		o.log.Info().Str("database", db.Name).Msg("database created")
		writeJSON(w, http.StatusCreated, db)
	}
}

// handleDropDB backs the database up (when backups are on) and drops it.
// The backup must succeed before anything is dropped.
func (o *Operator) handleDropDB(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !o.cfg.ManagedDatabases {
		writeErr(w, http.StatusConflict, "Den-managed databases are turned off")
		return
	}
	if o.cfg.Protected[name] || !dbNameRE.MatchString(name) {
		writeErr(w, http.StatusForbidden, "this database is protected")
		return
	}
	if ok, err := o.pg.exists(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	} else if !ok {
		writeErr(w, http.StatusNotFound, "no such database")
		return
	}
	if !o.lock("drop " + name) {
		writeErr(w, http.StatusConflict, "a backup or database removal is already running")
		return
	}
	defer o.unlock()
	ctx := context.WithoutCancel(r.Context())

	out := map[string]any{"name": name}
	if o.store != nil {
		run := o.backup(ctx, "pre-drop", name)
		if !run.OK {
			writeErr(w, http.StatusBadGateway, "backup before removal failed, nothing was deleted: "+run.Error)
			return
		}
		out["backup_key"] = run.Key
	}
	if err := o.pg.drop(ctx, name); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	o.log.Warn().Str("database", name).Any("backup", out["backup_key"]).Msg("database dropped")
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
