package http_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestRolesFromGroups(t *testing.T) {
	e := newEnv(t, nil)
	cases := []struct {
		token string
		want  []string
	}{
		{memberTok(), []string{"member"}},
		{adminTok(), []string{"admin"}},
		{opsTok(), []string{"admin", "ops"}},
		{"nobody-" + strings.Repeat("x", 4) + "|", []string{}},
	}
	for _, c := range cases {
		var me struct {
			Roles []string `json:"roles"`
		}
		e.expect(e.do(http.MethodGet, "/api/v1/me", c.token, nil), http.StatusOK).json(t, &me)
		if !slices.Equal(me.Roles, c.want) {
			t.Errorf("%s: roles %v, want %v", c.token, me.Roles, c.want)
		}
	}
}

func TestOperationsNeedOpsRole(t *testing.T) {
	e := newEnv(t, nil)
	admin, ops, member := adminTok(), opsTok(), memberTok()

	for _, p := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/operations/backups"},
		{http.MethodPost, "/api/v1/operations/backups"},
		{http.MethodPut, "/api/v1/operations/settings"},
		{http.MethodPost, "/api/v1/operations/backups/download-link"},
	} {
		e.expect(e.do(p.method, p.path, admin, map[string]any{}), http.StatusForbidden)
		e.expect(e.do(p.method, p.path, member, map[string]any{}), http.StatusForbidden)
	}
	// Without an operator: list is empty, mutations say it is not set up.
	e.expect(e.do(http.MethodGet, "/api/v1/operations/backups", ops, nil), http.StatusOK)
	e.expect(e.do(http.MethodPost, "/api/v1/operations/backups", ops, nil), http.StatusConflict)

	// The read-only status is for admins too, not for members.
	e.expect(e.do(http.MethodGet, "/api/v1/operations/status", admin, nil), http.StatusOK)
	e.expect(e.do(http.MethodGet, "/api/v1/operations/status", ops, nil), http.StatusOK)
	e.expect(e.do(http.MethodGet, "/api/v1/operations/status", member, nil), http.StatusForbidden)
}

func TestDropDatabaseNeedsOps(t *testing.T) {
	e := newEnv(t, nil)
	id, slug := e.newModule("https://drop.example.test")
	e.expect(e.do(http.MethodDelete, "/api/v1/modules/"+id.String()+"?database=drop&confirm="+slug, adminTok(), nil), http.StatusForbidden)
}

func TestRoleCatalogPublishesOps(t *testing.T) {
	e := newEnv(t, nil)
	var cat struct {
		Roles []struct {
			Key   string `json:"key"`
			Group string `json:"group"`
		} `json:"roles"`
	}
	e.expect(e.do(http.MethodGet, "/api/role-catalog", "", nil), http.StatusOK).json(t, &cat)
	got := map[string]string{}
	for _, r := range cat.Roles {
		got[r.Key] = r.Group
	}
	if got["ops"] != "den-ops" || got["admin"] != "den-admin" || got["member"] != "den-member" {
		t.Fatalf("catalog groups: %v", got)
	}
}

func TestDisabledUserIsRefused(t *testing.T) {
	e := newEnv(t, nil)
	tok := adminTok()
	e.expect(e.do(http.MethodGet, "/api/v1/me", tok, nil), http.StatusOK)
	sub := strings.SplitN(tok, "|", 2)[0]
	if _, err := e.pool.Exec(context.Background(), `
		UPDATE users SET is_disabled = true
		 WHERE id = (SELECT user_id FROM external_identities WHERE issuer = $1 AND subject = $2)`, testIssuer, sub); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/me", tok, nil), http.StatusForbidden)
}

func TestUserGroupsAreStored(t *testing.T) {
	e := newEnv(t, nil)
	tok := "grp-" + strings.Repeat("y", 4) + "|den-member,stash-admin"
	e.expect(e.do(http.MethodGet, "/api/v1/me", tok, nil), http.StatusOK)
	var groups []string
	if err := e.pool.QueryRow(context.Background(), `
		SELECT u.groups FROM users u JOIN external_identities ei ON ei.user_id = u.id
		 WHERE ei.issuer = $1 AND ei.subject = 'grp-yyyy'`, testIssuer).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(groups, []string{"den-member", "stash-admin"}) {
		t.Fatalf("groups %v", groups)
	}
}

func TestSeedOwnerAndRoleAssignmentsAreGone(t *testing.T) {
	pool := testPool(t)
	var owners, tables int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE id = '00000000-0000-0000-0000-00000000d0a1'`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'role_assignments'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if owners != 0 || tables != 0 {
		t.Fatalf("seed owner rows=%d role_assignments tables=%d", owners, tables)
	}
}
