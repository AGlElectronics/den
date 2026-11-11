package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// integrationModule is one row of GET /api/v1/integrations/modules.
// role_catalog_url is the full address HR fetches (the module's
// internal_url when that is set, otherwise base_url, plus
// /api/role-catalog). It is empty when Den has no address for the module.
// Every module in the suite publishes that path; HR skips an empty URL.
type integrationModule struct {
	Slug           string `json:"slug"`
	DisplayName    string `json:"display_name"`
	BaseURL        string `json:"base_url"`
	InternalURL    string `json:"internal_url"`
	RoleCatalogURL string `json:"role_catalog_url"`
}

// roleCatalogURL builds the catalog address HR should fetch. internalURL
// wins when it is set. A missing address yields an empty URL.
func roleCatalogURL(internalURL, baseURL string) string {
	addr := strings.TrimRight(strings.TrimSpace(internalURL), "/")
	if addr == "" {
		addr = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	if addr == "" {
		return ""
	}
	return addr + "/api/role-catalog"
}

// integrationModules is GET /api/v1/integrations/modules. The caller is a
// paired module (its credential). The list is the organisation's enabled
// modules that are paired or degraded and still have a credential that
// has not been revoked. Disabled, unpaired and fully revoked modules are
// left out. The body is a handful of addresses, well under the 64 KiB
// denlink refuses.
func (h *moduleHandler) integrationModules(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.EnsureModule(w, r)
	if !ok {
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT slug, display_name, base_url, internal_url
		  FROM modules m
		 WHERE m.organization_id = $1
		   AND m.enabled = true
		   AND m.status IN ('paired', 'degraded')
		   AND EXISTS (
		       SELECT 1 FROM module_credentials c
		        WHERE c.module_id = m.id AND c.revoked_at IS NULL)
		 ORDER BY m.slug, m.environment`, principal.OrganizationID)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (integrationModule, error) {
		var m integrationModule
		if err := row.Scan(&m.Slug, &m.DisplayName, &m.BaseURL, &m.InternalURL); err != nil {
			return integrationModule{}, err
		}
		m.RoleCatalogURL = roleCatalogURL(m.InternalURL, m.BaseURL)
		return m, nil
	})
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if list == nil {
		list = []integrationModule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": list})
}
