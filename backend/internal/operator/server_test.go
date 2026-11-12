package operator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func testOperator(t *testing.T, pool *pgxpool.Pool, managed bool) *Operator {
	t.Helper()
	cfg := &Config{
		Token:            "op-secret",
		ManagedDatabases: managed,
		Protected:        map[string]bool{"postgres": true, "template0": true, "template1": true, "den": true, "keycloak": true},
		ModuleDBHost:     "postgres:5432",
		ModuleDBParams:   "sslmode=disable",
		Location:         time.UTC,
	}
	return &Operator{cfg: cfg, pg: &pg{pool: pool, cfg: cfg}, log: zerolog.Nop()}
}

func call(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTokenIsRequired(t *testing.T) {
	h := testOperator(t, nil, true).Router()
	if rec := call(h, http.MethodGet, "/livez", "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("livez must not need the token: %d", rec.Code)
	}
	for _, p := range []struct{ method, path string }{
		{http.MethodGet, "/v1/status"},
		{http.MethodPut, "/v1/settings"},
		{http.MethodGet, "/v1/backups"},
		{http.MethodPost, "/v1/backups"},
		{http.MethodGet, "/v1/backups/download?key=x"},
		{http.MethodPost, "/v1/databases"},
		{http.MethodDelete, "/v1/databases/stash"},
	} {
		for _, tok := range []string{"", "wrong", "op-secret-and-more"} {
			if rec := call(h, p.method, p.path, tok, "{}"); rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with token %q: %d", p.method, p.path, tok, rec.Code)
			}
		}
	}
	// "Bearer" is required, not just the raw token.
	req := httptest.NewRequest(http.MethodGet, "/v1/backups", nil)
	req.Header.Set("Authorization", "op-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("raw token without Bearer: %d", rec.Code)
	}
	// Backups off: the list is empty, not an error.
	if rec := call(h, http.MethodGet, "/v1/backups", "op-secret", ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("list with backups off: %d %s", rec.Code, rec.Body)
	}
}

func TestDropProtection(t *testing.T) {
	h := testOperator(t, nil, true).Router()
	for _, name := range []string{"den", "keycloak", "postgres", "template1", "Den", "a", "x;drop", "1abc"} {
		rec := call(h, http.MethodDelete, "/v1/databases/"+name, "op-secret", "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("drop %q: want 403, got %d %s", name, rec.Code, rec.Body)
		}
	}
	off := testOperator(t, nil, false).Router()
	if rec := call(off, http.MethodDelete, "/v1/databases/stash", "op-secret", ""); rec.Code != http.StatusConflict {
		t.Fatalf("drop with managed databases off: %d", rec.Code)
	}
}

func TestCreateRefusesProtectedAndBadNames(t *testing.T) {
	h := testOperator(t, nil, true).Router()
	for _, name := range []string{"den", "keycloak", "postgres", "Bad-Name", "", strings.Repeat("a", 42)} {
		rec := call(h, http.MethodPost, "/v1/databases", "op-secret", `{"name":"`+name+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %q: want 400, got %d", name, rec.Code)
		}
	}
	if rec := call(h, http.MethodPost, "/v1/databases", "op-secret", `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: %d", rec.Code)
	}
	off := testOperator(t, nil, false).Router()
	if rec := call(off, http.MethodPost, "/v1/databases", "op-secret", `{"name":"stash"}`); rec.Code != http.StatusConflict {
		t.Fatalf("create with managed databases off: %d", rec.Code)
	}
}

func TestSettingsValid(t *testing.T) {
	if !DefaultSettings.valid() {
		t.Fatal("defaults must be valid")
	}
	for _, s := range []Settings{
		{Hour: 24, KeepDaily: 7}, {Hour: -1, KeepDaily: 7}, {Hour: 3, KeepDaily: 0}, {Hour: 3, KeepDaily: 7, KeepWeekly: -1},
		{Hour: 3, KeepDaily: 366}, {Hour: 3, KeepDaily: 7, KeepMonthly: 241},
	} {
		if s.valid() {
			t.Errorf("%+v should be invalid", s)
		}
	}
}

func TestStoreOpenRejectsForeignKeys(t *testing.T) {
	s := &store{}
	for _, key := range []string{"state/settings.json", "../etc/passwd", "backups/notastamp-x.tar.age", "other/20261111T000000Z.tar.age"} {
		if _, _, err := s.open(context.Background(), key); err != ErrNotFound {
			t.Errorf("open %q: want ErrNotFound, got %v", key, err)
		}
	}
}

// Creates and drops a real database. Needs a superuser connection in
// DEN_TEST_PG_ADMIN_URL (local Docker Postgres: postgres/postgres, dev-only).
func TestCreateAndDropDatabase(t *testing.T) {
	dsn := os.Getenv("DEN_TEST_PG_ADMIN_URL")
	if dsn == "" {
		t.Skip("DEN_TEST_PG_ADMIN_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Close after the drop cleanup. A defer would run first and the drop
	// would then execute on a closed pool, leaving the database and role.
	t.Cleanup(pool.Close)
	o := testOperator(t, pool, true)
	h := o.Router()
	name := "optest_" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `DROP ROLE IF EXISTS `+name); err != nil {
			t.Errorf("drop role %s: %v", name, err)
		}
	})

	rec := call(h, http.MethodPost, "/v1/databases", "op-secret", `{"name":"`+name+`"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "postgres://"+name+":") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := call(h, http.MethodPost, "/v1/databases", "op-secret", `{"name":"`+name+`"}`); rec.Code != http.StatusConflict {
		t.Fatalf("second create: %d", rec.Code)
	}
	if rec := call(h, http.MethodDelete, "/v1/databases/"+name, "op-secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("drop: %d %s", rec.Code, rec.Body)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_roles WHERE rolname = $1`, name).Scan(&n)
	if n != 0 {
		t.Fatal("the database's own login must be dropped too")
	}
	if rec := call(h, http.MethodDelete, "/v1/databases/"+name, "op-secret", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("drop missing: %d", rec.Code)
	}
}
