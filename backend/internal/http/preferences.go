package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// User preferences are stored in Den and shared by every app, so picking dark
// mode in Stash also applies in Den, HR and Rally. People use these endpoints
// in Den; paired modules read and write them for their signed-in user through
// the integration API, limited to people who may use that module.
//
//	GET/PUT /api/v1/me/preferences
//	GET     /api/v1/integrations/user-preferences?issuer=…&subject=…
//	PUT     /api/v1/integrations/user-preferences   {issuer, subject, theme}

type preferencesHandler struct {
	pool   *pgxpool.Pool
	caller *moduleCaller
	// devMode skips the module scope check (AUTH_MODE=dev, everyone is an admin).
	devMode bool
}

// errNotModuleUser hides whether Den knows the person at all.
var errNotModuleUser = httperror.NotFound("person for this module")

// checkModuleUser lets a module read or write only the preferences of its
// own users: people who signed in to Den at least once, are not disabled and
// may use the module (portal_visibility everyone, or one of their groups is
// in the module's role catalog).
func (h *preferencesHandler) checkModuleUser(ctx context.Context, m *auth.ModulePrincipal, issuer, subject string) error {
	if h.devMode {
		return nil
	}
	var (
		groups   []string
		disabled bool
	)
	err := h.pool.QueryRow(ctx, `
		SELECT u.groups, u.is_disabled
		  FROM external_identities ei JOIN users u ON u.id = ei.user_id
		 WHERE ei.issuer = $1 AND ei.subject = $2`, issuer, subject).Scan(&groups, &disabled)
	if errors.Is(err, pgx.ErrNoRows) || disabled {
		return errNotModuleUser
	}
	if err != nil {
		return httperror.Internal(err)
	}
	var pm portalModule
	err = h.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, base_url, internal_url, environment, status, enabled, portal_visibility
		  FROM modules WHERE id = $1 AND organization_id = $2`, m.ModuleID, defaultOrgID).
		Scan(&pm.ID, &pm.Slug, &pm.DisplayName, &pm.BaseURL, &pm.InternalURL, &pm.Environment, &pm.Status, &pm.Enabled, &pm.Visibility)
	if err != nil {
		return httperror.Internal(err)
	}
	if !h.caller.canUse(ctx, groups, pm) {
		return errNotModuleUser
	}
	return nil
}

type preferencesDTO struct {
	Theme     string  `json:"theme"`
	UpdatedAt *string `json:"updated_at"`
}

var validThemes = map[string]bool{"system": true, "light": true, "dark": true}

func (h *preferencesHandler) load(ctx context.Context, issuer, subject string) (*preferencesDTO, error) {
	var (
		theme   string
		updated time.Time
	)
	err := h.pool.QueryRow(ctx,
		`SELECT theme, updated_at FROM user_preferences WHERE issuer = $1 AND subject = $2`,
		issuer, subject).Scan(&theme, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return &preferencesDTO{Theme: "system"}, nil
	}
	if err != nil {
		return nil, httperror.Internal(err)
	}
	ts := updated.Format(time.RFC3339)
	return &preferencesDTO{Theme: theme, UpdatedAt: &ts}, nil
}

func (h *preferencesHandler) save(ctx context.Context, issuer, subject, theme string) (*preferencesDTO, error) {
	theme = strings.TrimSpace(theme)
	if !validThemes[theme] {
		return nil, httperror.Validation("validation failed", map[string]string{"theme": "must be system, light or dark"})
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO user_preferences (issuer, subject, theme, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (issuer, subject) DO UPDATE SET theme = EXCLUDED.theme, updated_at = now()
	`, issuer, subject, theme); err != nil {
		return nil, httperror.Internal(err)
	}
	return h.load(ctx, issuer, subject)
}

func (h *preferencesHandler) getMine(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("not signed in"))
		return
	}
	p, err := h.load(r.Context(), u.Issuer, u.Subject)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *preferencesHandler) putMine(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("not signed in"))
		return
	}
	var in struct {
		Theme string `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	p, err := h.save(r.Context(), u.Issuer, u.Subject, in.Theme)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func identityMissing() error {
	return httperror.Validation("validation failed", map[string]string{"subject": "issuer and subject are required"})
}

func (h *preferencesHandler) integrationGet(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	issuer, subject := r.URL.Query().Get("issuer"), r.URL.Query().Get("subject")
	if issuer == "" || subject == "" {
		httperror.Render(w, r, identityMissing())
		return
	}
	if err := h.checkModuleUser(r.Context(), m, issuer, subject); err != nil {
		httperror.Render(w, r, err)
		return
	}
	p, err := h.load(r.Context(), issuer, subject)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *preferencesHandler) integrationPut(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	var in struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
		Theme   string `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if in.Issuer == "" || in.Subject == "" {
		httperror.Render(w, r, identityMissing())
		return
	}
	if err := h.checkModuleUser(r.Context(), m, in.Issuer, in.Subject); err != nil {
		httperror.Render(w, r, err)
		return
	}
	p, err := h.save(r.Context(), in.Issuer, in.Subject, in.Theme)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
