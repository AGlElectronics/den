package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// Organisation settings are one JSON document per organisation. Den is the
// source of truth; paired modules read it through the integration API and
// show these fields as "Managed in Den". Known keys are validated, unknown
// keys are kept so a module can add its own without a Den release.
//
// Known keys:
//
//	organization_name   — shown in emails and on login screens
//	org_email_domain    — e.g. "example.org"
//	username_template   — login names for new accounts, e.g. "{nickname}"
//	org_email_template  — organisation email names, e.g. "{first}.{last}"
//	events_public_schedule — Events serves its published schedule publicly (bool)
//	wiki_public_links   — the Wiki may create public share links (bool)

type orgSettingsHandler struct {
	pool *pgxpool.Pool
	// onChange runs after a successful update (e.g. to resync Keycloak).
	onChange func()
}

type orgSettingsDTO struct {
	Data      map[string]any `json:"data"`
	UpdatedAt *string        `json:"updated_at"`
}

var (
	settingsDomainRE   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	settingsTemplateRE = regexp.MustCompile(`^[A-Za-z0-9._{}-]+$`)
	settingsTokenRE    = regexp.MustCompile(`\{[^}]*\}`)
	settingsTokens     = map[string]bool{"{first}": true, "{last}": true, "{nickname}": true}
)

func (h *orgSettingsHandler) load(ctx context.Context, orgID uuid.UUID) (*orgSettingsDTO, error) {
	if h.pool == nil {
		return nil, httperror.Internal(errors.New("database not configured"))
	}
	var (
		raw     []byte
		updated time.Time
	)
	err := h.pool.QueryRow(ctx,
		`SELECT data, updated_at FROM organization_settings WHERE organization_id = $1`, orgID).Scan(&raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return &orgSettingsDTO{Data: map[string]any{}}, nil
	}
	if err != nil {
		return nil, httperror.Internal(err)
	}
	out := &orgSettingsDTO{Data: map[string]any{}}
	if err := json.Unmarshal(raw, &out.Data); err != nil {
		return nil, httperror.Internal(err)
	}
	ts := updated.Format(time.RFC3339)
	out.UpdatedAt = &ts
	return out, nil
}

func (h *orgSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	s, err := h.load(r.Context(), defaultOrgID)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *orgSettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if in == nil {
		in = map[string]any{}
	}
	if fields := normalizeOrgSettings(in); len(fields) > 0 {
		httperror.Render(w, r, httperror.Validation("validation failed", fields))
		return
	}
	buf, err := json.Marshal(in)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	actor := ""
	if id := auth.ActorID(r.Context()); id != nil {
		actor = id.String()
	}
	if _, err := h.pool.Exec(r.Context(), `
		INSERT INTO organization_settings (organization_id, data, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (organization_id) DO UPDATE
		   SET data = EXCLUDED.data, updated_at = now(), updated_by = EXCLUDED.updated_by
	`, defaultOrgID, buf, actor); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if h.onChange != nil {
		h.onChange()
	}
	h.get(w, r)
}

// normalizeOrgSettings trims known keys in place and returns per-key errors.
func normalizeOrgSettings(in map[string]any) map[string]string {
	fields := map[string]string{}
	str := func(key string) (string, bool) {
		v, ok := in[key]
		if !ok || v == nil {
			delete(in, key)
			return "", false
		}
		s, isStr := v.(string)
		if !isStr {
			fields[key] = "must be a string"
			return "", false
		}
		s = strings.TrimSpace(s)
		in[key] = s
		return s, true
	}
	str("organization_name")
	if d, ok := str("org_email_domain"); ok && d != "" {
		d = strings.ToLower(strings.TrimPrefix(d, "@"))
		in["org_email_domain"] = d
		if !settingsDomainRE.MatchString(d) {
			fields["org_email_domain"] = "must be a domain like example.org"
		}
	}
	for _, key := range []string{"username_template", "org_email_template"} {
		if t, ok := str(key); ok && t != "" {
			if msg := validateNameTemplate(t); msg != "" {
				fields[key] = msg
			}
		}
	}
	for _, key := range []string{"events_public_schedule", "wiki_public_links"} {
		if v, ok := in[key]; ok {
			if v == nil {
				delete(in, key)
			} else if _, isBool := v.(bool); !isBool {
				fields[key] = "must be true or false"
			}
		}
	}
	return fields
}

func validateNameTemplate(tpl string) string {
	if !settingsTemplateRE.MatchString(tpl) {
		return "may only contain letters, digits, . - _ and {first} {last} {nickname}"
	}
	tokens := settingsTokenRE.FindAllString(tpl, -1)
	if len(tokens) == 0 {
		return "must use at least one of {first} {last} {nickname}"
	}
	for _, t := range tokens {
		if !settingsTokens[t] {
			return "unknown placeholder " + t + " (use {first} {last} {nickname})"
		}
	}
	return ""
}

// integrationGet is GET /api/v1/integrations/settings for paired modules.
func (h *orgSettingsHandler) integrationGet(w http.ResponseWriter, r *http.Request) {
	m, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	s, err := h.load(r.Context(), m.OrganizationID)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}
