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
// the integration API.
//
//	GET/PUT /api/v1/me/preferences
//	GET     /api/v1/integrations/user-preferences?issuer=…&subject=…
//	PUT     /api/v1/integrations/user-preferences   {issuer, subject, theme}

type preferencesHandler struct {
	pool *pgxpool.Pool
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
	if _, ok := auth.EnsureModule(w, r); !ok {
		return
	}
	issuer, subject := r.URL.Query().Get("issuer"), r.URL.Query().Get("subject")
	if issuer == "" || subject == "" {
		httperror.Render(w, r, identityMissing())
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
	if _, ok := auth.EnsureModule(w, r); !ok {
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
	p, err := h.save(r.Context(), in.Issuer, in.Subject, in.Theme)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
