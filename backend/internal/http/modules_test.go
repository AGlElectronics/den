package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/AGlElectronics/den/backend/internal/config"
)

// fakeOperator answers the operator calls Den makes and records them.
type fakeOperator struct {
	mu       sync.Mutex
	calls    []string
	failNext bool
}

func (f *fakeOperator) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		fail := f.failNext
		f.failNext = false
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer op-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if fail {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"a database with this name already exists"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/databases":
			var in struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"name": in.Name, "username": in.Name, "password": "pw", "database_url": "postgres://" + in.Name})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/databases/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"backup_key": "pre-drop/x/20261111T000000Z.tar.age"})
		case r.URL.Path == "/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"managed_databases": true, "backups_enabled": true, "databases": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/backups":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"started":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/settings":
			_, _ = w.Write([]byte(`{"hour":4,"keep_daily":7,"keep_weekly":4,"keep_monthly":6}`))
		case r.URL.Path == "/v1/backups":
			_, _ = w.Write([]byte(`[{"key":"backups/20261111T030000Z-scheduled.tar.age","kind":"scheduled","size_bytes":3}]`))
		case r.URL.Path == "/v1/backups/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "3")
			_, _ = w.Write([]byte("age"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeOperator) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func withOperator(t *testing.T, op *fakeOperator) func(*config.Config) {
	srv := op.start(t)
	return func(c *config.Config) {
		c.OperatorURL, c.OperatorToken = srv.URL, "op-token"
	}
}

func cleanupSlug(e *testEnv, slug string) {
	e.t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM modules WHERE organization_id = $1 AND slug = $2`, testOrgID, slug)
	})
}

func TestModuleCreateValidation(t *testing.T) {
	prod := newEnv(t, func(c *config.Config) { c.AppEnv = "production" })
	admin := adminTok()
	slug := "v-" + uuid.NewString()[:8]
	cleanupSlug(prod, slug)

	for name, body := range map[string]map[string]any{
		"bad slug":       {"slug": "Not A Slug", "display_name": "X"},
		"no name":        {"slug": slug},
		"http in prod":   {"slug": slug, "display_name": "X", "base_url": "http://x.example.test"},
		"bad internal":   {"slug": slug, "display_name": "X", "internal_url": "ftp://x"},
		"bad visibility": {"slug": slug, "display_name": "X", "portal_visibility": "all"},
		"query in url":   {"slug": slug, "display_name": "X", "base_url": "https://x.example.test/?a=1"},
	} {
		if r := prod.do(http.MethodPost, "/api/v1/modules", admin, body); r.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: want 422, got %d %s", name, r.Code, r.Body)
		}
	}
	prod.expect(prod.do(http.MethodPost, "/api/v1/modules", admin, map[string]any{
		"slug": slug, "display_name": "X", "base_url": "https://x.example.test/", "internal_url": "https://x-backend.ns.svc:8080",
	}), http.StatusCreated)
	prod.expect(prod.do(http.MethodPost, "/api/v1/modules", admin, map[string]any{"slug": slug, "display_name": "X"}), http.StatusConflict)
	prod.expect(prod.do(http.MethodPost, "/api/v1/modules", memberTok(), map[string]any{"slug": "m", "display_name": "X"}), http.StatusForbidden)

	var list []struct {
		Slug             string `json:"slug"`
		BaseURL          string `json:"base_url"`
		PortalVisibility string `json:"portal_visibility"`
	}
	prod.expect(prod.do(http.MethodGet, "/api/v1/modules", admin, nil), http.StatusOK).json(t, &list)
	found := false
	for _, m := range list {
		if m.Slug == slug {
			found = m.BaseURL == "https://x.example.test" && m.PortalVisibility == "role_holders"
		}
	}
	if !found {
		t.Fatalf("created module missing or wrong: %+v", list)
	}

	dev := newEnv(t, nil)
	devSlug := "d-" + uuid.NewString()[:8]
	cleanupSlug(dev, devSlug)
	dev.expect(dev.do(http.MethodPost, "/api/v1/modules", admin, map[string]any{"slug": devSlug, "display_name": "X", "base_url": "http://localhost:8091"}), http.StatusCreated)
}

func TestModuleCreateWithDatabase(t *testing.T) {
	op := &fakeOperator{}
	e := newEnv(t, withOperator(t, op))
	admin := adminTok()
	slug := "db-" + uuid.NewString()[:8]
	cleanupSlug(e, slug)

	op.failNext = true
	e.expect(e.do(http.MethodPost, "/api/v1/modules", admin, map[string]any{"slug": slug, "display_name": "DB", "create_database": true}), http.StatusConflict)
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM modules WHERE slug = $1`, slug).Scan(&n)
	if n != 0 {
		t.Fatal("a failed database must not leave a module row")
	}

	var created struct {
		ID       string `json:"id"`
		Database struct {
			Name string `json:"name"`
		} `json:"database"`
	}
	e.expect(e.do(http.MethodPost, "/api/v1/modules", admin, map[string]any{"slug": slug, "display_name": "DB", "create_database": true}), http.StatusCreated).json(t, &created)
	if created.Database.Name != strings.ReplaceAll(slug, "-", "_") {
		t.Fatalf("database: %+v", created)
	}
	var m struct {
		DatabaseCreatedByDen bool `json:"database_created_by_den"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/modules/"+created.ID, admin, nil), http.StatusOK).json(t, &m)
	if !m.DatabaseCreatedByDen {
		t.Fatal("database_created_by_den should be true")
	}
	if e.auditCount("module.create", uuid.MustParse(created.ID)) != 1 {
		t.Fatal("missing module.create audit event")
	}

	// Dropping needs the ops role and the typed slug, then calls the operator.
	path := "/api/v1/modules/" + created.ID + "?database=drop&confirm=" + slug
	e.expect(e.do(http.MethodDelete, "/api/v1/modules/"+created.ID+"?database=drop&confirm=wrong", opsTok(), nil), http.StatusUnprocessableEntity)
	var res struct {
		Dropped   bool   `json:"database_dropped"`
		BackupKey string `json:"backup_key"`
	}
	e.expect(e.do(http.MethodDelete, path, opsTok(), nil), http.StatusOK).json(t, &res)
	if !res.Dropped || res.BackupKey == "" || !op.called("DELETE /v1/databases/") {
		t.Fatalf("drop: %+v calls=%v", res, op.calls)
	}
	if e.auditCount("module.delete", uuid.MustParse(created.ID)) != 1 {
		t.Fatal("missing module.delete audit event")
	}
}

func TestModuleDropOnlyDenCreatedDatabases(t *testing.T) {
	op := &fakeOperator{}
	e := newEnv(t, withOperator(t, op))
	id, slug := e.newModule("https://legacy.example.test")
	e.expect(e.do(http.MethodDelete, "/api/v1/modules/"+id.String()+"?database=drop&confirm="+slug, opsTok(), nil), http.StatusConflict)
	if op.called("DELETE") {
		t.Fatal("the operator must not be asked to drop a database Den did not create")
	}
	var res struct {
		Dropped bool `json:"database_dropped"`
	}
	e.expect(e.do(http.MethodDelete, "/api/v1/modules/"+id.String(), adminTok(), nil), http.StatusOK).json(t, &res)
	if res.Dropped {
		t.Fatal("plain removal must keep the data")
	}
}

func TestModuleUpdateAuditAndOrgScope(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	id, _ := e.newModule("https://upd.example.test")

	e.expect(e.do(http.MethodPatch, "/api/v1/modules/"+id.String(), admin, map[string]any{"base_url": "ftp://nope"}), http.StatusUnprocessableEntity)
	e.expect(e.do(http.MethodPatch, "/api/v1/modules/"+id.String(), admin, map[string]any{"display_name": "Renamed", "portal_visibility": "everyone"}), http.StatusOK)
	var page struct {
		Items []struct {
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		} `json:"items"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?action=module.update&entity_id="+id.String(), admin, nil), http.StatusOK).json(t, &page)
	if len(page.Items) != 1 || page.Items[0].Before["display_name"] != "Test Module" || page.Items[0].After["display_name"] != "Renamed" ||
		page.Items[0].After["portal_visibility"] != "everyone" {
		t.Fatalf("update audit: %+v", page.Items)
	}

	// A module of another organisation is invisible.
	ctx := context.Background()
	other := uuid.New()
	if _, err := e.pool.Exec(ctx, `INSERT INTO organizations (id, slug, display_name) VALUES ($1, $2, 'Other')`, other, "o-"+other.String()[:8]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = e.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, other) })
	var foreign uuid.UUID
	if err := e.pool.QueryRow(ctx, `INSERT INTO modules (organization_id, slug, display_name) VALUES ($1, 'foreign', 'F') RETURNING id`, other).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	p := "/api/v1/modules/" + foreign.String()
	e.expect(e.do(http.MethodGet, p, admin, nil), http.StatusNotFound)
	e.expect(e.do(http.MethodPatch, p, admin, map[string]any{"display_name": "Mine"}), http.StatusNotFound)
	e.expect(e.do(http.MethodDelete, p, admin, nil), http.StatusNotFound)
	e.expect(e.do(http.MethodPost, p+"/pairing-tokens", admin, map[string]any{}), http.StatusNotFound)
}
