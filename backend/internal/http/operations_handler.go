package http

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// downloadLinkTTL is how long a one-time backup download link works.
const downloadLinkTTL = 5 * time.Minute

// operationsHandler is the Den admin "Backups" page API. Every change is
// written to the audit log; the operator itself keeps no audit trail.
type operationsHandler struct {
	ops  *opsClient
	pool *pgxpool.Pool
}

// routes are the ops-only routes; /status is mounted by the router.
func (h *operationsHandler) routes(r chi.Router) {
	r.Put("/settings", h.settings)
	r.Get("/backups", h.list)
	r.Post("/backups", h.backupNow)
	r.Post("/backups/download-link", h.downloadLink)
}

func (h *operationsHandler) status(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	var st map[string]any
	if err := h.ops.do(r.Context(), http.MethodGet, "/v1/status", nil, &st); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "reachable": false, "error": err.Error()})
		return
	}
	st["configured"], st["reachable"] = true, true
	writeJSON(w, http.StatusOK, st)
}

// recordOp writes an audit row for an operator action that already
// happened. The operator holds no Den state, so there is no transaction to
// join; a failed write is reported, not hidden.
func (h *operationsHandler) recordOp(r *http.Request, ev audit.Event) error {
	if h.pool == nil {
		return nil
	}
	ev.OrgID, ev.EntityType, ev.EntityID = orgRef(), "backup", defaultOrgID
	return audit.Write(r.Context(), h.pool, ev)
}

func (h *operationsHandler) settings(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	var in map[string]int
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	var before struct {
		Settings map[string]int `json:"settings"`
	}
	_ = h.ops.do(r.Context(), http.MethodGet, "/v1/status", nil, &before)
	var out map[string]int
	if err := h.ops.do(r.Context(), http.MethodPut, "/v1/settings", in, &out); err != nil {
		renderOps(w, r, err)
		return
	}
	if err := h.recordOp(r, audit.Event{Action: "operations.schedule.update", Before: before.Settings, After: out}); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *operationsHandler) list(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	var out []map[string]any
	if err := h.ops.do(r.Context(), http.MethodGet, "/v1/backups", nil, &out); err != nil {
		renderOps(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *operationsHandler) backupNow(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	if err := h.ops.do(r.Context(), http.MethodPost, "/v1/backups", nil, nil); err != nil {
		renderOps(w, r, err)
		return
	}
	if err := h.recordOp(r, audit.Event{Action: "operations.backup.start", Note: "manual backup started"}); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// downloadLink mints a one-time URL valid for five minutes so the browser
// can stream a (possibly large) backup without holding it in memory. The
// link is stored (hashed) in the database, so any replica can serve it.
func (h *operationsHandler) downloadLink(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	if in.Key == "" {
		httperror.Render(w, r, httperror.Validation("validation failed", map[string]string{"key": "is required"}))
		return
	}
	token, err := randomToken(24)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	expires := time.Now().Add(downloadLinkTTL)
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM backup_download_links WHERE expires_at < now() - interval '1 day'`); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO backup_download_links (token_hash, backup_key, expires_at, created_by)
			VALUES ($1, $2, $3, $4)`, sha256Hex(token), in.Key, expires, auth.ActorID(r.Context())); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "operations.download_link.create", EntityType: "backup", EntityID: defaultOrgID,
			After: map[string]any{"key": in.Key, "expires_at": expires.UTC().Format(time.RFC3339)}, Note: in.Key,
		})
	})
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/api/v1/backup-downloads/" + token})
}

// download is public: the one-time token is the credential. The link is
// used up before streaming starts, so it works exactly once.
func (h *operationsHandler) download(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if h.ops == nil || h.pool == nil || token == "" {
		httperror.Render(w, r, httperror.NotFound("download link"))
		return
	}
	var key string
	err := auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var createdBy *uuid.UUID
		err := tx.QueryRow(r.Context(), `
			UPDATE backup_download_links SET used_at = now()
			 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			 RETURNING backup_key, created_by`, sha256Hex(token)).Scan(&key, &createdBy)
		if errors.Is(err, pgx.ErrNoRows) {
			return httperror.NotFound("download link")
		}
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Actor: createdBy, Action: "operations.backup.download", EntityType: "backup", EntityID: defaultOrgID,
			Note: key,
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.ops.open(r.Context(), key)
	if err != nil {
		renderOps(w, r, err)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", res.Header.Get("Content-Length"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(key)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, res.Body)
}
