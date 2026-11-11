package http_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestIntegrationModules(t *testing.T) {
	e := newEnv(t, nil)
	path := "/api/v1/integrations/modules"

	noAuth := e.expect(e.do(http.MethodGet, path, "", nil), http.StatusUnauthorized)
	var missing struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	noAuth.json(t, &missing)
	if missing.Error.Code != "unauthorized" || missing.Error.Message != "missing bearer token" {
		t.Fatalf("missing credential: %+v", missing.Error)
	}
	bad := e.expect(e.do(http.MethodGet, path, "not-a-real-credential", nil), http.StatusUnauthorized)
	bad.json(t, &missing)
	if missing.Error.Code != "unauthorized" || missing.Error.Message != "invalid or revoked credential" {
		t.Fatalf("rejected credential: %+v", missing.Error)
	}
	// A person's access token is not a module credential.
	e.expect(e.do(http.MethodGet, path, adminTok(), nil), http.StatusUnauthorized)

	deskID, deskSlug := e.newModule("https://desk.con.example.org")
	setModule(t, e, deskID, "Desk", "https://desk.con.example.org",
		"https://desk-backend.conservices-desk.svc.cluster.local:8080/")
	deskCred := e.pair(deskID, nil)

	wikiID, wikiSlug := e.newModule("https://wiki.con.example.org")
	setModule(t, e, wikiID, "Wiki", "https://wiki.con.example.org", "")
	wikiCred := e.pair(wikiID, nil)

	blankID, blankSlug := e.newModule("https://blank.con.example.org")
	setModule(t, e, blankID, "Blank", "", "")
	e.pair(blankID, nil)

	_, unpairedSlug := e.newModule("https://learn.con.example.org")

	disabledID, disabledSlug := e.newModule("https://stash.con.example.org")
	setModule(t, e, disabledID, "Stash", "https://stash.con.example.org", "")
	e.pair(disabledID, nil)
	_, err := e.pool.Exec(context.Background(), `UPDATE modules SET enabled = false WHERE id = $1`, disabledID)
	if err != nil {
		t.Fatal(err)
	}

	revokedID, revokedSlug := e.newModule("https://finance.con.example.org")
	setModule(t, e, revokedID, "Finance", "https://finance.con.example.org", "")
	revokedCred := e.pair(revokedID, nil)
	e.expect(e.do(http.MethodDelete, "/api/v1/modules/"+revokedID.String()+"/credentials", adminTok(), nil), http.StatusOK)

	revokedResp := e.expect(e.do(http.MethodGet, path, revokedCred, nil), http.StatusUnauthorized)
	revokedResp.json(t, &missing)
	if missing.Error.Code != "unauthorized" || missing.Error.Message != "invalid or revoked credential" {
		t.Fatalf("revoked credential: %+v", missing.Error)
	}

	var body struct {
		Modules []struct {
			Slug           string `json:"slug"`
			DisplayName    string `json:"display_name"`
			BaseURL        string `json:"base_url"`
			InternalURL    string `json:"internal_url"`
			RoleCatalogURL string `json:"role_catalog_url"`
		} `json:"modules"`
	}
	raw := e.expect(e.do(http.MethodGet, path, deskCred, nil), http.StatusOK)
	raw.json(t, &body)
	if ct := raw.Hdr.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}

	got := map[string]struct {
		name, base, internal, catalog string
	}{}
	for _, m := range body.Modules {
		got[m.Slug] = struct {
			name, base, internal, catalog string
		}{m.DisplayName, m.BaseURL, m.InternalURL, m.RoleCatalogURL}
	}
	desk := got[deskSlug]
	if desk.name != "Desk" || desk.base != "https://desk.con.example.org" ||
		desk.internal != "https://desk-backend.conservices-desk.svc.cluster.local:8080/" ||
		desk.catalog != "https://desk-backend.conservices-desk.svc.cluster.local:8080/api/role-catalog" {
		t.Fatalf("desk: %+v", desk)
	}
	wiki := got[wikiSlug]
	if wiki.name != "Wiki" || wiki.internal != "" ||
		wiki.catalog != "https://wiki.con.example.org/api/role-catalog" {
		t.Fatalf("wiki: %+v", wiki)
	}
	if blank := got[blankSlug]; blank.catalog != "" || blank.base != "" {
		t.Fatalf("blank address: %+v", blank)
	}
	for _, slug := range []string{unpairedSlug, disabledSlug, revokedSlug} {
		if _, ok := got[slug]; ok {
			t.Fatalf("%s should not be listed", slug)
		}
	}

	// Degraded is still a paired module with a live credential.
	if _, err := e.pool.Exec(context.Background(), `UPDATE modules SET status = 'degraded' WHERE id = $1`, wikiID); err != nil {
		t.Fatal(err)
	}
	var degraded struct {
		Modules []struct {
			Slug string `json:"slug"`
		} `json:"modules"`
	}
	e.expect(e.do(http.MethodGet, path, deskCred, nil), http.StatusOK).json(t, &degraded)
	seenWiki := false
	for _, m := range degraded.Modules {
		if m.Slug == wikiSlug {
			seenWiki = true
		}
	}
	if !seenWiki {
		t.Fatal("degraded module was left out")
	}

	// Another paired module sees the same organisation list.
	again := e.expect(e.do(http.MethodGet, path, wikiCred, nil), http.StatusOK)
	var body2 struct {
		Modules []struct {
			Slug string `json:"slug"`
		} `json:"modules"`
	}
	again.json(t, &body2)
	if len(body2.Modules) != len(body.Modules) {
		t.Fatalf("caller %s saw %d modules, desk saw %d", wikiSlug, len(body2.Modules), len(body.Modules))
	}
}

func TestIntegrationModulesRateLimit(t *testing.T) {
	e := newEnv(t, nil)
	path := "/api/v1/integrations/modules"
	// The bucket allows 120 at once and refills while the test runs, so the
	// 429 lands a little after 120. It must land well before an unlimited route would.
	var limited resp
	for i := 0; i < 200; i++ {
		r := e.do(http.MethodGet, path, "", nil)
		if r.Code == http.StatusTooManyRequests {
			limited = r
			break
		}
		if r.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: want 401 or 429, got %d: %s", i, r.Code, r.Body)
		}
	}
	if limited.Code != http.StatusTooManyRequests {
		t.Fatal("module list was not rate limited")
	}
	if limited.Hdr.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	limited.json(t, &body)
	if body.Error.Code != "rate_limited" {
		t.Fatalf("code %q", body.Error.Code)
	}
}

func setModule(t *testing.T, e *testEnv, id uuid.UUID, name, baseURL, internalURL string) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(), `
		UPDATE modules SET display_name = $2, base_url = $3, internal_url = $4 WHERE id = $1`,
		id, name, baseURL, internalURL)
	if err != nil {
		t.Fatal(err)
	}
}
