package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// branding/* routes serve two distinct audiences:
//
//  1. The HUMAN UI (operator in Den's SPA). Authenticated with X-Dev-User /
//     OIDC. Routes live under /api/v1/brand. Can read + mutate.
//
//  2. The MATED MODULES (Stash, Events, HR …). Authenticated with a module
//     bearer credential. Routes live under /api/v1/integrations/branding.
//     Read-only. Streams asset bytes from Den's bucket so the module never
//     needs S3 credentials (see docs/adr/0004-one-bucket-per-module.md).

type brandingHandler struct {
	pool  *pgxpool.Pool
	files AssetStore
	// onChange runs after the profile or an asset changes (e.g. to resync Keycloak).
	onChange func()
}

func (h *brandingHandler) changed() {
	if h.onChange != nil {
		h.onChange()
	}
}

func newBrandingHandler(pool *pgxpool.Pool, filesClient AssetStore) *brandingHandler {
	return &brandingHandler{pool: pool, files: filesClient}
}

// ----------------------------------------------------------------------------
// Shared loaders
// ----------------------------------------------------------------------------

type brandingProfileDTO struct {
	Revision   int             `json:"revision"`
	Theme      json.RawMessage `json:"theme"`
	Typography json.RawMessage `json:"typography"`
	Copy       json.RawMessage `json:"copy"`
	UpdatedAt  string          `json:"updated_at"`
}

func (h *brandingHandler) loadLatestProfile(ctx context.Context, orgID uuid.UUID) (*brandingProfileDTO, error) {
	if h.pool == nil {
		return nil, httperror.Internal(errors.New("database not configured"))
	}
	var (
		rev        int
		theme      []byte
		typography []byte
		copyJSON   []byte
		created    time.Time
	)
	err := h.pool.QueryRow(ctx, `
		SELECT revision, theme, typography, copy, created_at
		  FROM branding_profiles
		 WHERE organization_id = $1
		 ORDER BY revision DESC
		 LIMIT 1
	`, orgID).Scan(&rev, &theme, &typography, &copyJSON, &created)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httperror.NotFound("branding profile")
		}
		return nil, httperror.Internal(err)
	}
	return &brandingProfileDTO{
		Revision:   rev,
		Theme:      ensureJSONObject(theme),
		Typography: ensureJSONObject(typography),
		Copy:       ensureJSONObject(copyJSON),
		UpdatedAt:  created.Format(time.RFC3339),
	}, nil
}

type brandingAssetDTO struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	URL         string `json:"url"`
	CreatedAt   string `json:"created_at"`
}

// listAssets returns asset metadata for the org. URLs point at Den's own
// stream endpoint — never at the bucket directly, so switching bucket backends
// later is invisible to clients.
func (h *brandingHandler) listAssets(r *http.Request, orgID uuid.UUID, urlBuilder func(uuid.UUID) string) ([]brandingAssetDTO, error) {
	if h.pool == nil {
		return nil, httperror.Internal(errors.New("database not configured"))
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, role, content_type, size_bytes, created_at
		  FROM branding_assets
		 WHERE organization_id = $1
		 ORDER BY role, created_at DESC
	`, orgID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()

	out := make([]brandingAssetDTO, 0)
	for rows.Next() {
		var (
			id          uuid.UUID
			role        string
			contentType string
			sizeBytes   int64
			created     time.Time
		)
		if err := rows.Scan(&id, &role, &contentType, &sizeBytes, &created); err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, brandingAssetDTO{
			ID:          id.String(),
			Role:        role,
			ContentType: contentType,
			SizeBytes:   sizeBytes,
			URL:         urlBuilder(id),
			CreatedAt:   created.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (h *brandingHandler) streamAsset(w http.ResponseWriter, r *http.Request, assetID uuid.UUID, orgID uuid.UUID) {
	if h.pool == nil || h.files == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("storage not configured")))
		return
	}
	var (
		rowOrgID    uuid.UUID
		bucket      string
		key         string
		contentType string
		sizeBytes   int64
	)
	err := h.pool.QueryRow(r.Context(), `
		SELECT organization_id, storage_bucket, storage_key, content_type, size_bytes
		  FROM branding_assets WHERE id = $1
	`, assetID).Scan(&rowOrgID, &bucket, &key, &contentType, &sizeBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httperror.Render(w, r, httperror.NotFound("asset"))
			return
		}
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	// Enforce the asset belongs to the caller's org, so a module paired to
	// org A cannot fetch org B's branding by guessing UUIDs.
	if rowOrgID != orgID {
		httperror.Render(w, r, httperror.NotFound("asset"))
		return
	}

	rc, err := h.files.GetObject(r.Context(), key)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	defer func() { _ = rc.Close() }()

	setAssetHeaders(w, contentType)
	if sizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sizeBytes))
	}
	// Cache aggressively; the asset id is immutable per-upload.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, rc)
}

// ensureJSONObject returns `{}` when the input is empty so clients never
// have to defend against a null theme.
func ensureJSONObject(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage([]byte("{}"))
	}
	return json.RawMessage(b)
}

// ----------------------------------------------------------------------------
// Human UI: /api/v1/brand/...
// ----------------------------------------------------------------------------

func (h *brandingHandler) humanRoutes(r chi.Router) {
	r.Get("/profile", h.humanGetProfile)
	r.Put("/profile", h.humanPutProfile)
	r.Get("/assets", h.humanListAssets)
	r.Get("/assets/{id}", h.humanGetAsset)
	r.Post("/assets", h.humanUploadAsset)
	r.Delete("/assets/{id}", h.humanDeleteAsset)
}

func (h *brandingHandler) humanGetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.loadLatestProfile(r.Context(), defaultOrgID)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

type putProfileInput struct {
	Theme      json.RawMessage `json:"theme"`
	Typography json.RawMessage `json:"typography"`
	Copy       json.RawMessage `json:"copy"`
}

func (h *brandingHandler) humanPutProfile(w http.ResponseWriter, r *http.Request) {
	var in putProfileInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	theme := []byte(ensureJSONObject(in.Theme))
	typography := []byte(ensureJSONObject(in.Typography))
	copyJSON := []byte(ensureJSONObject(in.Copy))

	var nextRevision int
	err := auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		// New revision = max(revision)+1; the lock serialises concurrent saves.
		if _, err := tx.Exec(r.Context(), `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`, defaultOrgID); err != nil {
			return err
		}
		var prev int
		if err := tx.QueryRow(r.Context(),
			`SELECT COALESCE(MAX(revision), 0) FROM branding_profiles WHERE organization_id = $1`,
			defaultOrgID).Scan(&prev); err != nil {
			return err
		}
		nextRevision = prev + 1
		var id uuid.UUID
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO branding_profiles (organization_id, revision, theme, typography, copy, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, defaultOrgID, nextRevision, theme, typography, copyJSON, auth.ActorID(r.Context())).Scan(&id); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "branding.profile.update", EntityType: "branding_profile", EntityID: id,
			Before: map[string]any{"revision": prev},
			After:  map[string]any{"revision": nextRevision, "theme": in.Theme, "typography": in.Typography, "copy": in.Copy},
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}

	h.changed()
	writeJSON(w, http.StatusCreated, map[string]any{"revision": nextRevision})
}

func (h *brandingHandler) humanListAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := h.listAssets(r, defaultOrgID, func(id uuid.UUID) string {
		return "/api/v1/brand/assets/" + id.String()
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (h *brandingHandler) humanGetAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	h.streamAsset(w, r, id, defaultOrgID)
}

const maxBrandingAssetBytes = 10 * 1024 * 1024

func (h *brandingHandler) humanUploadAsset(w http.ResponseWriter, r *http.Request) {
	if h.files == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("storage not configured")))
		return
	}
	if err := r.ParseMultipartForm(maxBrandingAssetBytes); err != nil {
		httperror.Render(w, r, httperror.BadRequest("invalid multipart form: "+err.Error()))
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	if !assetRoles[role] {
		httperror.Render(w, r, httperror.Validation("validation failed", map[string]string{"role": "must be logo, favicon or login-background"}))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httperror.Render(w, r, httperror.Validation("file is required", nil))
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > maxBrandingAssetBytes {
		httperror.Render(w, r, httperror.Validation("validation failed", map[string]string{"file": "must be at most 10 MB"}))
		return
	}

	head := make([]byte, 1024)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		httperror.Render(w, r, httperror.BadRequest("could not read the file"))
		return
	}
	head = head[:n]
	contentType := detectAssetType(head, header.Filename, header.Header.Get("Content-Type"))
	if contentType == "" {
		httperror.Render(w, r, httperror.Validation("validation failed", map[string]string{"file": "must be a PNG, JPEG, WebP, ICO or SVG image"}))
		return
	}

	storageKey := fmt.Sprintf("branding/%s/%s/%s%s", defaultOrgID, role, uuid.NewString(), assetExtension[contentType])

	// Hard cap mirror of ParseMultipartForm's limit so a lying header
	// cannot smuggle more bytes in.
	body := io.LimitReader(io.MultiReader(bytes.NewReader(head), file), maxBrandingAssetBytes+1)
	if err := h.files.PutObject(r.Context(), storageKey, body, header.Size, contentType); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	var id uuid.UUID
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO branding_assets (organization_id, role, storage_bucket, storage_key, content_type, size_bytes, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`, defaultOrgID, role, h.files.Bucket(), storageKey, contentType, header.Size, auth.ActorID(r.Context())).Scan(&id); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "branding.asset.create", EntityType: "branding_asset", EntityID: id,
			After: map[string]any{"role": role, "content_type": contentType, "size_bytes": header.Size, "filename": header.Filename},
		})
	})
	if err != nil {
		// best-effort rollback of the uploaded bytes
		_ = h.files.Remove(r.Context(), storageKey)
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	h.changed()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

func (h *brandingHandler) humanDeleteAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	var key string
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(r.Context(),
			`DELETE FROM branding_assets WHERE id = $1 AND organization_id = $2 RETURNING storage_key, role`,
			id, defaultOrgID).Scan(&key, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return httperror.NotFound("asset")
		}
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "branding.asset.delete", EntityType: "branding_asset", EntityID: id,
			Before: map[string]any{"role": role},
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.files != nil {
		_ = h.files.Remove(r.Context(), key)
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Integration (mated module) API: /api/v1/integrations/branding/...
// ----------------------------------------------------------------------------

func (h *brandingHandler) integrationRoutes(r chi.Router) {
	r.Get("/profile", h.integrationGetProfile)
	r.Get("/assets", h.integrationListAssets)
	r.Get("/assets/{id}", h.integrationGetAsset)
}

func (h *brandingHandler) integrationGetProfile(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	profile, err := h.loadLatestProfile(r.Context(), m.OrganizationID)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *brandingHandler) integrationListAssets(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	assets, err := h.listAssets(r, m.OrganizationID, func(id uuid.UUID) string {
		return "/api/v1/integrations/branding/assets/" + id.String()
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (h *brandingHandler) integrationGetAsset(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	h.streamAsset(w, r, id, m.OrganizationID)
}
