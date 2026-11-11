package http

import "net/http"

// roleCatalogHandler serves GET /api/role-catalog (public, read-only). Den
// roles come from the IdP: the first admin group → convention admin, the
// first ops group → operations, the first member group → member (on by
// default for new onboardings). Without a member group every signed-in user
// is a member.
func roleCatalogHandler(adminGroups, memberGroups, opsGroups []string) http.HandlerFunc {
	first := func(groups []string) string {
		if len(groups) > 0 {
			return groups[0]
		}
		return ""
	}
	adminGroup, memberGroup, opsGroup := first(adminGroups), first(memberGroups), first(opsGroups)
	member := map[string]any{
		"key":         "member",
		"label":       "Member",
		"description": "Sees the portal home and the services they have a role in.",
		"group":       memberGroup,
		"assignable":  memberGroup != "",
		"default":     memberGroup != "",
		"scope":       "organization",
		"permissions": []string{"portal.view"},
	}
	if memberGroup == "" {
		member["note"] = "Automatic for every signed-in user."
	}
	ops := map[string]any{
		"key":         "ops",
		"label":       "Operations",
		"description": "Runs and downloads backups, changes the backup schedule and deletes module databases.",
		"group":       opsGroup,
		"assignable":  opsGroup != "",
		"scope":       "organization",
		"permissions": []string{"backups.manage", "databases.drop"},
	}
	if opsGroup == "" {
		ops["note"] = "No group is configured (DEN_OPS_GROUPS), so nobody can run backups."
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
					"description": "Manage modules, pairing, branding, settings and the audit log.",
					"group":       adminGroup,
					"assignable":  adminGroup != "",
					"scope":       "organization",
					"permissions": []string{"modules.manage", "branding.manage", "settings.manage", "audit.view"},
				},
				ops,
				member,
			},
		})
	}
}
