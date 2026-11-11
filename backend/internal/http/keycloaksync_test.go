package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
	denhttp "github.com/AGlElectronics/den/backend/internal/http"
	"github.com/AGlElectronics/den/backend/internal/keycloak"
)

type fakeKeycloak struct {
	mu      sync.Mutex
	updates []map[string]any
}

func (f *fakeKeycloak) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
			return
		}
		var rep map[string]any
		_ = json.NewDecoder(r.Body).Decode(&rep)
		f.mu.Lock()
		f.updates = append(f.updates, rep)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type syncStatus struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	Message     string `json:"message"`
	DisplayName string `json:"display_name"`
	LastError   string `json:"last_error"`
}

func TestKeycloakSyncPushesOnlyDisplayName(t *testing.T) {
	kc := &fakeKeycloak{}
	srv := kc.start(t)
	pool := testPool(t)
	s := denhttp.NewKeycloakSync(pool, keycloak.Config{BaseURL: srv.URL, Realm: "conservices", ClientID: "den-keycloak", ClientSecret: "x"},
		config.IdentityOwn, zerolog.Nop())
	e := newEnv(t, nil, func(d *denhttp.Deps) { d.KeycloakSync = s })
	admin := adminTok()
	start := time.Now().Add(-time.Second)

	e.expect(e.do(http.MethodPut, "/api/v1/org-settings", admin, map[string]any{"organization_name": "Sync Con"}), http.StatusOK)
	var st syncStatus
	e.expect(e.do(http.MethodPost, "/api/v1/keycloak-sync", admin, nil), http.StatusOK).json(t, &st)
	if !st.Enabled || st.Mode != "pull" || st.DisplayName != "Sync Con" || st.LastError != "" {
		t.Fatalf("status: %+v", st)
	}
	kc.mu.Lock()
	last := kc.updates[len(kc.updates)-1]
	kc.mu.Unlock()
	if last["displayName"] != "Sync Con" || len(last) != 2 {
		t.Fatalf("only displayName and displayNameHtml may be written, got %v", last)
	}
	if countAudit(e, "keycloak.sync", start) < 1 {
		t.Fatal("a manual sync must be audited")
	}
}

func TestKeycloakSyncOffInExistingMode(t *testing.T) {
	kc := &fakeKeycloak{}
	srv := kc.start(t)
	s := denhttp.NewKeycloakSync(testPool(t), keycloak.Config{BaseURL: srv.URL, Realm: "theirs", ClientID: "c", ClientSecret: "x"},
		config.IdentityExisting, zerolog.Nop())
	e := newEnv(t, func(c *config.Config) { c.IdentityStaffMode = config.IdentityExisting }, func(d *denhttp.Deps) { d.KeycloakSync = s })
	var st syncStatus
	e.expect(e.do(http.MethodPost, "/api/v1/keycloak-sync", adminTok(), nil), http.StatusOK).json(t, &st)
	if st.Enabled || !strings.Contains(st.Message, "existing") {
		t.Fatalf("status: %+v", st)
	}
	if len(kc.updates) != 0 {
		t.Fatal("Den must never write to the realm in existing mode")
	}
}

func TestPublicBrandAllowsAnyOrigin(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.CORSAllowedOrigins = []string{"http://localhost:3001"} })
	r := e.expect(e.do(http.MethodGet, "/api/v1/public/brand", "", nil, "Origin", "https://auth.example.org"), http.StatusOK)
	if r.Hdr.Get("Access-Control-Allow-Origin") != "*" || r.Hdr.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("login theme origin: %v", r.Hdr)
	}
	r = e.expect(e.do(http.MethodGet, "/api/v1/public/brand", "", nil, "Origin", "http://localhost:3001"), http.StatusOK)
	if r.Hdr.Get("Access-Control-Allow-Origin") != "http://localhost:3001" {
		t.Fatalf("SPA origin keeps the credentialed answer: %v", r.Hdr)
	}
	// Den's own admin API stays closed to other origins.
	r = e.do(http.MethodGet, "/api/v1/me", adminTok(), nil, "Origin", "https://auth.example.org")
	if r.Hdr.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("admin API must not allow other origins: %v", r.Hdr)
	}
}
