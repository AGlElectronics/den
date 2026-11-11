package keycloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigEnabled(t *testing.T) {
	full := Config{BaseURL: "http://kc", Realm: "r", ClientID: "c", ClientSecret: "s"}
	if !full.Enabled() {
		t.Fatal("full config should be enabled")
	}
	for _, c := range []Config{{}, {BaseURL: "http://kc", Realm: "r", ClientID: "c"}, {Realm: "r", ClientID: "c", ClientSecret: "s"}} {
		if c.Enabled() {
			t.Fatalf("%+v should be disabled", c)
		}
	}
}

func TestUpdateRealm(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/conservices/protocol/openid-connect/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "s3cret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		case "/admin/realms/conservices":
			if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL + "/", Realm: "conservices", ClientID: "den-keycloak", ClientSecret: "s3cret"})
	if err := c.UpdateRealm(context.Background(), map[string]any{"displayName": "Example Con"}); err != nil {
		t.Fatal(err)
	}
	if got["displayName"] != "Example Con" {
		t.Fatalf("realm update body: %v", got)
	}

	bad := New(Config{BaseURL: srv.URL, Realm: "conservices", ClientID: "den-keycloak", ClientSecret: "wrong"})
	err := bad.UpdateRealm(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("want token error with status, got %v", err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate(strings.Repeat("x", 400)); len(got) != 300+len("…") {
		t.Fatalf("truncate length %d", len(got))
	}
}
