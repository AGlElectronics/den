package http

import "net/http"

// roleCatalogHandler serves GET /api/role-catalog (public, read-only). Den
// roles come from the IdP: the first admin group → convention admin, the
// first member group → member (on by default for new onboardings). Without a
// member group every signed-in user is a member.
func roleCatalogHandler(adminGroups, memberGroups []string) http.HandlerFunc {
	adminGroup, memberGroup := "", ""
	if len(adminGroups) > 0 {
		adminGroup = adminGroups[0]
	}
	if len(memberGroups) > 0 {
		memberGroup = memberGroups[0]
	}
	member := map[string]any{
		"key":         "member",
		"label":       "Member",
		"description": "Sees the portal home and every usable service.",
		"group":       memberGroup,
		"assignable":  memberGroup != "",
		"default":     memberGroup != "",
		"scope":       "organization",
		"permissions": []string{"portal.view"},
	}
	if memberGroup == "" {
		member["note"] = "Automatic for every signed-in user."
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
				member,
			},
		})
	}
}
