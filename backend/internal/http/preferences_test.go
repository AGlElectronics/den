package http_test

import (
	"net/http"
	"testing"
)

func TestMyPreferences(t *testing.T) {
	e := newEnv(t, nil)
	tok := memberTok()
	var p struct {
		Theme     string  `json:"theme"`
		UpdatedAt *string `json:"updated_at"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/me/preferences", tok, nil), http.StatusOK).json(t, &p)
	if p.Theme != "system" || p.UpdatedAt != nil {
		t.Fatalf("default: %+v", p)
	}
	e.expect(e.do(http.MethodPut, "/api/v1/me/preferences", tok, map[string]string{"theme": "neon"}), http.StatusUnprocessableEntity)
	e.expect(e.do(http.MethodPut, "/api/v1/me/preferences", tok, map[string]string{"theme": "dark"}), http.StatusOK).json(t, &p)
	if p.Theme != "dark" || p.UpdatedAt == nil {
		t.Fatalf("saved: %+v", p)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/me/preferences", "", nil), http.StatusUnauthorized)
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/user-preferences?issuer=x&subject=y", "", nil), http.StatusUnauthorized)
}
