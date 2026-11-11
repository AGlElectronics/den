package http

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
)

// dashboardHandler returns the home-portal payload: org/convention identity,
// the usable (enabled + paired) modules the person may use (admins see all),
// and, for admins only, organisation-wide counts.
func dashboardHandler(pool *pgxpool.Pool, caller *moduleCaller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u, _ := auth.FromContext(ctx)
		isAdmin := u.HasRoleIn(defaultOrgID, "admin", "owner")

		type orgDTO struct {
			ID          string `json:"id"`
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
		}
		type moduleDTO struct {
			ID          string `json:"id"`
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			BaseURL     string `json:"base_url"`
			Environment string `json:"environment"`
			Status      string `json:"status"`
			Enabled     bool   `json:"enabled"`
		}
		type copyDTO struct {
			ConventionName string `json:"convention_name,omitempty"`
			Tagline        string `json:"tagline,omitempty"`
			Welcome        string `json:"welcome,omitempty"`
		}

		out := map[string]any{
			"organization":   nil,
			"copy":           copyDTO{},
			"usable_modules": []moduleDTO{},
		}

		if pool == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}

		var (
			orgID = defaultOrgID
			slug  string
			name  string
		)
		if err := pool.QueryRow(ctx, `SELECT slug, display_name FROM organizations WHERE id = $1`, orgID).Scan(&slug, &name); err == nil {
			out["organization"] = orgDTO{ID: orgID.String(), Slug: slug, DisplayName: name}
		}

		if isAdmin {
			var modules, paired, unpaired, users int64
			_ = pool.QueryRow(ctx, `
				SELECT count(*), count(*) FILTER (WHERE status = 'paired'), count(*) FILTER (WHERE status = 'unpaired')
				  FROM modules WHERE organization_id = $1`, orgID).Scan(&modules, &paired, &unpaired)
			_ = pool.QueryRow(ctx, `
				SELECT count(DISTINCT user_id) FROM organization_memberships WHERE organization_id = $1`, orgID).Scan(&users)
			out["modules"] = modules
			out["paired_modules"] = paired
			out["unpaired_modules"] = unpaired
			out["users"] = users
		}

		var copyRaw, themeRaw, typoRaw []byte
		err := pool.QueryRow(ctx, `
			SELECT copy, theme, typography FROM branding_profiles
			 WHERE organization_id = $1
			 ORDER BY revision DESC
			 LIMIT 1
		`, orgID).Scan(&copyRaw, &themeRaw, &typoRaw)
		if err == nil {
			// Raw brand JSON so every signed-in user's portal can apply it.
			out["brand"] = map[string]json.RawMessage{
				"theme":      nonEmptyJSON(themeRaw),
				"typography": nonEmptyJSON(typoRaw),
				"copy":       nonEmptyJSON(copyRaw),
			}
		}
		if err == nil && len(copyRaw) > 0 {
			var raw map[string]any
			if json.Unmarshal(copyRaw, &raw) == nil {
				c := copyDTO{}
				if v, ok := raw["convention_name"].(string); ok {
					c.ConventionName = v
				}
				if v, ok := raw["event_name"].(string); ok && c.ConventionName == "" {
					c.ConventionName = v
				}
				if v, ok := raw["tagline"].(string); ok {
					c.Tagline = v
				}
				if v, ok := raw["welcome"].(string); ok {
					c.Welcome = v
				}
				out["copy"] = c
			}
		}

		if u != nil {
			if mods, err := caller.visibleModules(ctx, u); err == nil {
				list := make([]moduleDTO, 0, len(mods))
				for _, m := range mods {
					list = append(list, moduleDTO{
						ID: m.ID.String(), Slug: m.Slug, DisplayName: m.DisplayName, BaseURL: m.BaseURL,
						Environment: m.Environment, Status: m.Status, Enabled: m.Enabled,
					})
				}
				out["usable_modules"] = list
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func nonEmptyJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(b)
}
