package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dashboardHandler returns the home-portal payload: org/convention identity,
// usable (enabled + paired) modules for everyone, and light admin counts.
func dashboardHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

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
			"organization":     nil,
			"copy":             copyDTO{},
			"usable_modules":   []moduleDTO{},
			"organizations":    0,
			"users":            0,
			"modules":          0,
			"paired_modules":   0,
			"unpaired_modules": 0,
		}

		if pool == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}

		var (
			orgID    uuid.UUID
			slug     string
			name     string
			orgs     int64
			modules  int64
			paired   int64
			unpaired int64
			users    int64
		)
		_ = pool.QueryRow(ctx, `
			SELECT id, slug, display_name FROM organizations
			 ORDER BY slug ASC
			 LIMIT 1
		`).Scan(&orgID, &slug, &name)
		if orgID != uuid.Nil {
			out["organization"] = orgDTO{
				ID:          orgID.String(),
				Slug:        slug,
				DisplayName: name,
			}
		}

		_ = pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM modules`).Scan(&modules)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM modules WHERE status = 'paired'`).Scan(&paired)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM modules WHERE status = 'unpaired'`).Scan(&unpaired)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users)
		out["organizations"] = orgs
		out["modules"] = modules
		out["paired_modules"] = paired
		out["unpaired_modules"] = unpaired
		out["users"] = users

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

		rows, err := pool.Query(ctx, `
			SELECT id, slug, display_name, COALESCE(base_url, ''), environment, status, enabled
			  FROM modules
			 WHERE enabled = true
			   AND status IN ('paired', 'degraded')
			   AND COALESCE(base_url, '') <> ''
			 ORDER BY display_name
		`)
		if err == nil {
			defer rows.Close()
			list := make([]moduleDTO, 0)
			for rows.Next() {
				var m moduleDTO
				var id uuid.UUID
				if err := rows.Scan(&id, &m.Slug, &m.DisplayName, &m.BaseURL, &m.Environment, &m.Status, &m.Enabled); err != nil {
					continue
				}
				m.ID = id.String()
				list = append(list, m)
			}
			out["usable_modules"] = list
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
