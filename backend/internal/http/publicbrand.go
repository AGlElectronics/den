package http

import (
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
	// LogoURL is relative to Den (public endpoint).
	LogoURL string `json:"logo_url,omitempty"`
}

func (h *brandingHandler) bundle(r *http.Request, orgID uuid.UUID, settings map[string]any) (*brandBundle, error) {
	out := &brandBundle{Theme: json.RawMessage("{}"), Typography: json.RawMessage("{}"), Copy: json.RawMessage("{}"), Assets: map[string]string{}}
	profile, err := h.loadLatestProfile(r, orgID)
	var he *httperror.Error
	switch {
	case err == nil:
		out.Revision, out.UpdatedAt = profile.Revision, profile.UpdatedAt
		out.Theme, out.Typography, out.Copy = profile.Theme, profile.Typography, profile.Copy
	case errors.As(err, &he) && he.Code == httperror.CodeNotFound:
	default:
		return nil, err
	}

	rows, err := h.pool.Query(r.Context(), `
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

	if name, _ := settings["organization_name"].(string); strings.TrimSpace(name) != "" {
		out.OrganizationName = strings.TrimSpace(name)
	} else {
		var copyMap map[string]any
		_ = json.Unmarshal(out.Copy, &copyMap)
		if name, _ := copyMap["convention_name"].(string); strings.TrimSpace(name) != "" {
			out.OrganizationName = strings.TrimSpace(name)
		} else if err := h.pool.QueryRow(r.Context(),
			`SELECT display_name FROM organizations WHERE id = $1`, orgID).Scan(&out.OrganizationName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, httperror.Internal(err)
		}
	}
	return out, nil
}

func (h *brandingHandler) publicRoutes(settings *orgSettingsHandler) func(chi.Router) {
	return func(r chi.Router) {
		// Public and read-only, so any origin may read it (the Keycloak login
		// page fetches it from its own origin).
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				next.ServeHTTP(w, req)
			})
		})
		r.Get("/brand", func(w http.ResponseWriter, req *http.Request) {
			s, err := settings.load(req, defaultOrgID)
			if err != nil {
				httperror.Render(w, req, err)
				return
			}
			b, err := h.bundle(req, defaultOrgID, s.Data)
			if err != nil {
				httperror.Render(w, req, err)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=60")
			writeJSON(w, http.StatusOK, b)
		})
		r.Get("/brand/assets/{role}", func(w http.ResponseWriter, req *http.Request) {
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
func (h *brandingHandler) integrationBundle(settings *orgSettingsHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := auth.EnsureModule(w, r)
		if !ok {
			return
		}
		s, err := settings.load(r, m.OrganizationID)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		b, err := h.bundle(r, m.OrganizationID, s.Data)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		b.LogoURL = ""
		writeJSON(w, http.StatusOK, map[string]any{"brand": b, "settings": s})
	}
}
