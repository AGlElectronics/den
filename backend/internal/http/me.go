package http

import (
	"net/http"

	"github.com/AGlElectronics/den/backend/internal/auth"
)

// meHandler returns the authed principal to the SPA so it can decide what
// to render. Shape mirrors Stash's /api/me for familiarity.
func meHandler(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	memberships := make([]map[string]any, 0, len(u.Memberships))
	for _, m := range u.Memberships {
		memberships = append(memberships, map[string]any{
			"organization_id": m.OrganizationID.String(),
			"role":            m.Role,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           u.ID.String(),
		"display_name": u.DisplayName,
		"email":        u.Email,
		"issuer":       u.Issuer,
		"subject":      u.Subject,
		"memberships":  memberships,
		"roles":        u.Roles(),
	})
}
