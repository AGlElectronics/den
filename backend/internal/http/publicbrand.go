package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// Brand bundle: everything a login screen or a paired module needs to look
// like the organisation. Branding is public by nature (login pages show it
// before anyone signs in), so /api/v1/public/brand needs no auth. Paired
// modules use /api/v1/integrations/bundle, which adds the org settings.

type brandBundle struct {
	Revision         int             `json:"revision"`
	UpdatedAt        string          `json:"updated_at,omitempty"`
	OrganizationName string          `json:"organization_name"`
	Theme            json.RawMessage `json:"theme"`
	Typography       json.RawMessage `json:"typography"`
	Copy             json.RawMessage `json:"copy"`
	// Assets maps a role ("logo", "favicon", …) to the newest asset id.
	Assets map[string]string `json:"assets"`
	// LogoURL, FaviconURL and LoginBackgroundURL are relative to Den
	// (public endpoint).
	LogoURL            string `json:"logo_url,omitempty"`
	FaviconURL         string `json:"favicon_url,omitempty"`
	LoginBackgroundURL string `json:"login_background_url,omitempty"`
}

// assetLoginBackground is the picture behind every sign-in screen. Its
// layout and caption are copy.login_layout ("panel" | "centered") and
// copy.login_caption.
const assetLoginBackground = "login-background"

func (h *brandingHandler) bundle(ctx context.Context, orgID uuid.UUID, settings map[string]any) (*brandBundle, error) {
	out := &brandBundle{Theme: json.RawMessage("{}"), Typography: json.RawMessage("{}"), Copy: json.RawMessage("{}"), Assets: map[string]string{}}
	profile, err := h.loadLatestProfile(ctx, orgID)
	var he *httperror.Error
	switch {
	case err == nil:
		out.Revision, out.UpdatedAt = profile.Revision, profile.UpdatedAt
		out.Theme, out.Typography, out.Copy = profile.Theme, profile.Typography, profile.Copy
	case errors.As(err, &he) && he.Code == httperror.CodeNotFound:
	default:
		return nil, err
	}

	rows, err := h.pool.Query(ctx, `
		SELECT DISTINCT ON (role) role, id FROM branding_assets
		 WHERE organization_id = $1 ORDER BY role, created_at DESC`, orgID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			role string
			id   uuid.UUID
		)
		if err := rows.Scan(&role, &id); err != nil {
			return nil, httperror.Internal(err)
		}
		out.Assets[role] = id.String()
	}
	if _, ok := out.Assets["logo"]; ok {
		out.LogoURL = "/api/v1/public/brand/assets/logo"
	}
	if id, ok := out.Assets["favicon"]; ok {
		out.FaviconURL = "/api/v1/public/brand/assets/favicon?v=" + id
	}
	if id, ok := out.Assets[assetLoginBackground]; ok {
		out.LoginBackgroundURL = "/api/v1/public/brand/assets/" + assetLoginBackground + "?v=" + id
	}

	if name, _ := settings["organization_name"].(string); strings.TrimSpace(name) != "" {
		out.OrganizationName = strings.TrimSpace(name)
	} else {
		var copyMap map[string]any
		_ = json.Unmarshal(out.Copy, &copyMap)
		if name, _ := copyMap["convention_name"].(string); strings.TrimSpace(name) != "" {
			out.OrganizationName = strings.TrimSpace(name)
		} else if err := h.pool.QueryRow(ctx,
			`SELECT display_name FROM organizations WHERE id = $1`, orgID).Scan(&out.OrganizationName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, httperror.Internal(err)
		}
	}
	return out, nil
}

// allowAnyOrigin lets any web page read the public brand without
// credentials: the Keycloak login theme fetches it from another origin. An
// origin from CORS_ALLOWED_ORIGINS keeps the credentialed answer the CORS
// middleware already set.
func allowAnyOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("Access-Control-Allow-Origin") == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		next.ServeHTTP(w, r)
	})
}

func (h *brandingHandler) publicRoutes(settings *orgSettingsHandler) func(chi.Router) {
	return func(r chi.Router) {
		r.Use(allowAnyOrigin)
		r.Get("/brand", func(w http.ResponseWriter, req *http.Request) {
			s, err := settings.load(req.Context(), defaultOrgID)
			if err != nil {
				httperror.Render(w, req, err)
				return
			}
			b, err := h.bundle(req.Context(), defaultOrgID, s.Data)
			if err != nil {
				httperror.Render(w, req, err)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=60")
			writeJSON(w, http.StatusOK, b)
		})
		r.Get("/brand/assets/{role}", func(w http.ResponseWriter, req *http.Request) {
			if !assetRoles[chi.URLParam(req, "role")] {
				httperror.Render(w, req, httperror.NotFound("asset"))
				return
			}
			var id uuid.UUID
			err := h.pool.QueryRow(req.Context(), `
				SELECT id FROM branding_assets WHERE organization_id = $1 AND role = $2
				 ORDER BY created_at DESC LIMIT 1`, defaultOrgID, chi.URLParam(req, "role")).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				httperror.Render(w, req, httperror.NotFound("asset"))
				return
			}
			if err != nil {
				httperror.Render(w, req, httperror.Internal(err))
				return
			}
			h.streamAsset(w, req, id, defaultOrgID)
		})
	}
}

// integrationBundle is GET /api/v1/integrations/bundle: brand + org settings
// in one call, so a module syncs everything it needs at once.
// module_settings holds the Den → Module settings meant for the calling
// module (see modulesettings.go).
func (h *brandingHandler) integrationBundle(settings *orgSettingsHandler, modSettings *moduleSettingsHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := auth.EnsureModule(w, r)
		if !ok {
			return
		}
		s, err := settings.load(r.Context(), m.OrganizationID)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		b, err := h.bundle(r.Context(), m.OrganizationID, s.Data)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		ms, err := modSettings.forModule(r.Context(), m.Slug)
		if err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		b.LogoURL, b.FaviconURL, b.LoginBackgroundURL = "", "", ""
		writeJSON(w, http.StatusOK, map[string]any{"brand": b, "settings": s, "module_settings": ms})
	}
}
