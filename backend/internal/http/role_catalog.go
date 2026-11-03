package http

import "net/http"

// roleCatalogHandler serves GET /api/role-catalog (public, read-only). Den
// roles come from the IdP: the first admin group → convention admin; every
// signed-in user is a member automatically.
func roleCatalogHandler(adminGroups []string) http.HandlerFunc {
	adminGroup := ""
	if len(adminGroups) > 0 {
		adminGroup = adminGroups[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"module":       "den",
			"display_name": "Den",
			"description":  "Convention portal: home, modules, branding.",
			"roles": []map[string]any{
				{
					"key":         "admin",
					"label":       "Convention admin",
					"description": "Manage modules, pairing, branding and portal settings.",
					"group":       adminGroup,
					"assignable":  adminGroup != "",
					"scope":       "organization",
					"permissions": []string{"modules.manage", "branding.manage", "settings.manage"},
				},
				{
					"key":         "member",
					"label":       "Member",
					"description": "Sees the portal home and every usable service.",
					"group":       "",
					"assignable":  false,
					"scope":       "organization",
					"permissions": []string{"portal.view"},
					"note":        "Automatic for every signed-in user.",
				},
			},
		})
	}
}
