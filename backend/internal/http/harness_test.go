package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/db"
	denhttp "github.com/AGlElectronics/den/backend/internal/http"
)

const testIssuer = "https://auth.test/realms/conservices"

var testOrgID = uuid.MustParse("00000000-0000-0000-0000-00000000d001")

// fakeVerifier accepts tokens of the form "<subject>|<group>,<group>".
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, bearer string) (*auth.OIDCClaims, error) {
	sub, groups, ok := strings.Cut(bearer, "|")
	if !ok || sub == "" {
		return nil, errors.New("bad test token")
	}
	c := &auth.OIDCClaims{Issuer: testIssuer, Subject: sub, Name: sub, Email: sub + "@example.test"}
	if groups != "" {
		c.Groups = strings.Split(groups, ",")
	}
	return c, nil
}

var migrateOnce sync.Once

// testPool connects to DEN_TEST_DATABASE_URL and applies the migrations once
// per test binary. Tests skip without it. Every test that talks to that
// database must use testPool (or newEnv). A pool opened on its own never
// migrates, so the test fails on an empty database when it runs first.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DEN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DEN_TEST_DATABASE_URL not set")
	}
	var migErr error
	migrateOnce.Do(func() { migErr = db.MigrateUp(context.Background(), dsn) })
	if migErr != nil {
		t.Fatalf("migrate: %v", migErr)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type testEnv struct {
	t      *testing.T
	pool   *pgxpool.Pool
	cfg    *config.Config
	router http.Handler
}

// newEnv builds the router in OIDC mode with the fake verifier. mutate may
// adjust the config before the router is built.
func newEnv(t *testing.T, mutate func(*config.Config), deps ...func(*denhttp.Deps)) *testEnv {
	t.Helper()
	pool := testPool(t)
	cfg := &config.Config{
		AppEnv:       "development",
		AppBaseURL:   "http://localhost:8090",
		HTTPAddr:     ":0",
		AuthMode:     config.AuthModeOIDC,
		AdminGroups:  []string{"den-admin"},
		MemberGroups: []string{"den-member"},
		OpsGroups:    []string{"den-ops"},
	}
	if mutate != nil {
		mutate(cfg)
	}
	d := denhttp.Deps{Config: cfg, Pool: pool, Logger: zerolog.Nop(), OIDCVerifier: fakeVerifier{}}
	for _, f := range deps {
		f(&d)
	}
	return &testEnv{t: t, pool: pool, cfg: cfg, router: denhttp.NewRouter(d, "test")}
}

// Tokens for the fake verifier.
func adminTok() string  { return "admin-" + uuid.NewString()[:8] + "|den-admin" }
func opsTok() string    { return "ops-" + uuid.NewString()[:8] + "|den-admin,den-ops" }
func memberTok() string { return "member-" + uuid.NewString()[:8] + "|den-member" }

type resp struct {
	Code int
	Body []byte
	Hdr  http.Header
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

// do sends a request; token "" sends none, body may be nil, a string or any
// JSON-marshallable value.
func (e *testEnv) do(method, path, token string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		rd = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return resp{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

func (e *testEnv) expect(r resp, code int) resp {
	e.t.Helper()
	if r.Code != code {
		e.t.Fatalf("want %d, got %d: %s", code, r.Code, r.Body)
	}
	return r
}

// newModule inserts an unpaired module and removes it after the test.
func (e *testEnv) newModule(baseURL string) (uuid.UUID, string) {
	e.t.Helper()
	slug := "t-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO modules (organization_id, slug, display_name, base_url, environment)
		VALUES ($1, $2, 'Test Module', $3, 'test') RETURNING id`, testOrgID, slug, baseURL).Scan(&id); err != nil {
		e.t.Fatalf("insert module: %v", err)
	}
	e.t.Cleanup(func() { _, _ = e.pool.Exec(context.Background(), `DELETE FROM modules WHERE id = $1`, id) })
	return id, slug
}

// pair mints a pairing token as an admin and redeems it.
func (e *testEnv) pair(moduleID uuid.UUID, body any) string {
	e.t.Helper()
	var mint struct {
		Token string `json:"token"`
	}
	e.expect(e.do(http.MethodPost, "/api/v1/modules/"+moduleID.String()+"/pairing-tokens", adminTok(), map[string]any{}), http.StatusCreated).json(e.t, &mint)
	var red struct {
		Credential string `json:"credential"`
	}
	e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token="+mint.Token, "", body), http.StatusOK).json(e.t, &red)
	return red.Credential
}

// auditCount counts audit rows with action for entity.
func (e *testEnv) auditCount(action string, entity uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = $1 AND entity_id = $2`, action, entity).Scan(&n); err != nil {
		e.t.Fatalf("count audit: %v", err)
	}
	return n
}
