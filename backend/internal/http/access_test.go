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

const testCredentialKey = "0123456789abcdef0123456789abcdef"

// fakeModule serves a role catalog with one group and, when todos is set,
// /api/portal/todos for requests carrying the expected credential.
type fakeModule struct {
	group      string
	todos      bool
	mu         sync.Mutex
	credential string
	seen       []http.Header
}

func (f *fakeModule) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/role-catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{"module": "t", "roles": []map[string]any{{"key": "user", "group": f.group}}})
		case "/api/portal/todos":
			if !f.todos {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.mu.Lock()
			f.seen = append(f.seen, r.Header.Clone())
			want := f.credential
			f.mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer "+want {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"items":[
				{"id":"b","title":"Later","due_at":"2026-12-01T10:00:00Z","url":"https://m.example.test/b","severity":"info"},
				{"id":"a","title":"Soon","due_at":"2026-11-12T10:00:00Z","url":"https://m.example.test/a","severity":"danger"},
				{"id":"x","title":"Bad link","url":"javascript:alert(1)","severity":"info"},
				{"id":"c","title":"No date","due_at":null,"url":"https://m.example.test/c","severity":"weird"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withCredentialKey(c *config.Config) { c.CredentialKey = []byte(testCredentialKey) }

func (e *testEnv) setVisibility(id uuid.UUID, v string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE modules SET portal_visibility = $2 WHERE id = $1`, id, v); err != nil {
		e.t.Fatal(err)
	}
}

func dashboardSlugs(e *testEnv, tok string) (map[string]bool, map[string]any) {
	e.t.Helper()
	var d map[string]any
	e.expect(e.do(http.MethodGet, "/api/v1/dashboard", tok, nil), http.StatusOK).json(e.t, &d)
	slugs := map[string]bool{}
	for _, m := range d["usable_modules"].([]any) {
		slugs[m.(map[string]any)["slug"].(string)] = true
	}
	return slugs, d
}

func TestMembersSeeOnlyTheirModules(t *testing.T) {
	group := "tmod-" + uuid.NewString()[:6]
	mod := &fakeModule{group: group}
	srv := mod.start(t)
	e := newEnv(t, nil)
	restricted, rSlug := e.newModule(srv.URL)
	open, oSlug := e.newModule(srv.URL)
	e.pair(restricted, nil)
	e.pair(open, nil)
	e.setVisibility(open, "everyone")

	withGroup := "m1-" + uuid.NewString()[:6] + "|den-member," + group
	slugs, d := dashboardSlugs(e, withGroup)
	if !slugs[rSlug] || !slugs[oSlug] {
		t.Fatalf("member with the module's group should see both: %v", slugs)
	}
	for _, k := range []string{"users", "modules", "paired_modules", "unpaired_modules"} {
		if _, ok := d[k]; ok {
			t.Errorf("members must not get org-wide count %q", k)
		}
	}

	slugs, _ = dashboardSlugs(e, memberTok())
	if slugs[rSlug] || !slugs[oSlug] {
		t.Fatalf("member without the group should only see the open module: %v", slugs)
	}

	slugs, d = dashboardSlugs(e, adminTok())
	if !slugs[rSlug] || !slugs[oSlug] {
		t.Fatalf("admins see every usable module: %v", slugs)
	}
	if _, ok := d["paired_modules"]; !ok {
		t.Fatal("admins get the counts")
	}
}

func TestIntegrationPreferencesAreScoped(t *testing.T) {
	group := "tpref-" + uuid.NewString()[:6]
	mod := &fakeModule{group: group}
	srv := mod.start(t)
	e := newEnv(t, nil)
	id, _ := e.newModule(srv.URL)
	cred := e.pair(id, nil)

	user := "pu-" + uuid.NewString()[:6]
	outsider := "po-" + uuid.NewString()[:6]
	e.expect(e.do(http.MethodGet, "/api/v1/me", user+"|den-member,"+group, nil), http.StatusOK)
	e.expect(e.do(http.MethodGet, "/api/v1/me", outsider+"|den-member", nil), http.StatusOK)

	q := "/api/v1/integrations/user-preferences?issuer=" + testIssuer + "&subject="
	e.expect(e.do(http.MethodPut, "/api/v1/integrations/user-preferences", cred, map[string]string{"issuer": testIssuer, "subject": user, "theme": "dark"}), http.StatusOK)
	var p struct {
		Theme string `json:"theme"`
	}
	e.expect(e.do(http.MethodGet, q+user, cred, nil), http.StatusOK).json(t, &p)
	if p.Theme != "dark" {
		t.Fatalf("theme %q", p.Theme)
	}
	e.expect(e.do(http.MethodGet, q+outsider, cred, nil), http.StatusNotFound)
	e.expect(e.do(http.MethodPut, "/api/v1/integrations/user-preferences", cred, map[string]string{"issuer": testIssuer, "subject": outsider, "theme": "dark"}), http.StatusNotFound)
	e.expect(e.do(http.MethodGet, q+"never-signed-in", cred, nil), http.StatusNotFound)

	e.setVisibility(id, "everyone")
	e.expect(e.do(http.MethodGet, q+outsider, cred, nil), http.StatusOK)
}

func TestPortalTodosServerToServer(t *testing.T) {
	group := "ttodo-" + uuid.NewString()[:6]
	mod := &fakeModule{group: group, todos: true}
	srv := mod.start(t)
	quiet := &fakeModule{group: group}
	quietSrv := quiet.start(t)

	e := newEnv(t, withCredentialKey)
	id, slug := e.newModule(srv.URL)
	mod.credential = e.pair(id, nil)
	qid, qSlug := e.newModule(quietSrv.URL)
	e.pair(qid, nil)

	sub := "tu-" + uuid.NewString()[:6]
	var out struct {
		Enabled bool `json:"enabled"`
		Items   []struct {
			ID, Severity, ModuleSlug string
			DueAt                    *string `json:"due_at"`
		} `json:"items"`
		Modules []struct {
			Slug, Status string
		} `json:"modules"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/portal/todos", sub+"|den-member,"+group, nil), http.StatusOK).json(t, &out)
	if !out.Enabled {
		t.Fatal("to-dos should be enabled with a credential key")
	}
	var ids []string
	for _, it := range out.Items {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("items should be sorted by due date, invalid ones dropped: %v", ids)
	}
	if out.Items[2].Severity != "info" {
		t.Fatalf("unknown severity becomes info: %+v", out.Items[2])
	}
	status := map[string]string{}
	for _, m := range out.Modules {
		status[m.Slug] = m.Status
	}
	if status[slug] != "ok" || status[qSlug] != "unsupported" {
		t.Fatalf("module statuses: %v", status)
	}
	mod.mu.Lock()
	h := mod.seen[len(mod.seen)-1]
	mod.mu.Unlock()
	if h.Get("X-Den-User-Issuer") != testIssuer || h.Get("X-Den-User-Subject") != sub {
		t.Fatalf("Den must send the person's issuer and subject: %v", h)
	}
	if strings.Contains(h.Get("Authorization"), "|") {
		t.Fatal("the user's token must never be forwarded")
	}

	// Without the group the module is not asked at all.
	before := len(mod.seen)
	e.expect(e.do(http.MethodGet, "/api/v1/portal/todos", memberTok(), nil), http.StatusOK)
	if len(mod.seen) != before {
		t.Fatal("a module the person cannot use must not be asked")
	}

	off := newEnv(t, nil)
	off.expect(off.do(http.MethodGet, "/api/v1/portal/todos", memberTok(), nil), http.StatusOK).json(t, &out)
	if out.Enabled || len(out.Items) != 0 {
		t.Fatalf("without DEN_CREDENTIAL_KEY to-dos are off: %+v", out)
	}
}
