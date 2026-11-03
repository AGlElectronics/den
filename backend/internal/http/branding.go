package http

import (
	"encoding/json"
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

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/files"
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
	files *files.Client
}

func newBrandingHandler(pool *pgxpool.Pool, filesClient *files.Client) *brandingHandler {
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

func (h *brandingHandler) loadLatestProfile(r *http.Request, orgID uuid.UUID) (*brandingProfileDTO, error) {
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
	err := h.pool.QueryRow(r.Context(), `
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
// stream endpoint — never at MinIO directly, so switching bucket backends
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

	w.Header().Set("Content-Type", contentType)
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
	profile, err := h.loadLatestProfile(r, defaultOrgID)
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

	// New revision = max(revision)+1.
	var nextRevision int
	if err := h.pool.QueryRow(r.Context(),
		`SELECT COALESCE(MAX(revision), 0) + 1 FROM branding_profiles WHERE organization_id = $1`,
		defaultOrgID).Scan(&nextRevision); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO branding_profiles (organization_id, revision, theme, typography, copy, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, defaultOrgID, nextRevision, theme, typography, copyJSON, auth.ActorID(r.Context()))
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

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
	if role == "" {
		httperror.Render(w, r, httperror.Validation("role is required", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httperror.Render(w, r, httperror.Validation("file is required", nil))
		return
	}
	defer func() { _ = file.Close() }()

	ext := strings.ToLower(path.Ext(header.Filename))
	storageKey := fmt.Sprintf("branding/%s/%s/%s%s", defaultOrgID, role, uuid.NewString(), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Hard cap mirror of ParseMultipartForm's limit so a lying header
	// cannot smuggle more bytes in.
	body := io.LimitReader(file, maxBrandingAssetBytes+1)
	if err := h.files.PutObject(r.Context(), storageKey, body, header.Size, contentType); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	var id uuid.UUID
	err = h.pool.QueryRow(r.Context(), `
		INSERT INTO branding_assets (organization_id, role, storage_bucket, storage_key, content_type, size_bytes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, defaultOrgID, role, h.files.Bucket(), storageKey, contentType, header.Size, auth.ActorID(r.Context())).Scan(&id)
	if err != nil {
		// best-effort rollback of the uploaded bytes
		_ = h.files.Remove(r.Context(), storageKey)
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
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
	err = h.pool.QueryRow(r.Context(),
		`SELECT storage_key FROM branding_assets WHERE id = $1 AND organization_id = $2`,
		id, defaultOrgID).Scan(&key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httperror.Render(w, r, httperror.NotFound("asset"))
			return
		}
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if _, err := h.pool.Exec(r.Context(), `DELETE FROM branding_assets WHERE id = $1`, id); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if h.files != nil {
		_ = h.files.Remove(r.Context(), key)
	}
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
	profile, err := h.loadLatestProfile(r, m.OrganizationID)
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
