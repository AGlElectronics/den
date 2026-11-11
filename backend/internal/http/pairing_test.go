package http_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func mintToken(e *testEnv, moduleID uuid.UUID, tok string) string {
	e.t.Helper()
	var mint struct {
		Token string `json:"token"`
	}
	e.expect(e.do(http.MethodPost, "/api/v1/modules/"+moduleID.String()+"/pairing-tokens", tok, map[string]any{}), http.StatusCreated).json(e.t, &mint)
	return mint.Token
}

func TestRedeemIsAtomic(t *testing.T) {
	e := newEnv(t, nil)
	id, _ := e.newModule("https://atomic.example.test")
	token := mintToken(e, id, adminTok())

	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token="+token, "", nil).Code
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusUnauthorized:
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	var creds int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM module_credentials WHERE module_id = $1`, id).Scan(&creds); err != nil {
		t.Fatal(err)
	}
	if ok != 1 || creds != 1 {
		t.Fatalf("want exactly one successful redeem and one credential, got %d / %d", ok, creds)
	}
}

func TestRedeemValidatesModule(t *testing.T) {
	e := newEnv(t, nil)
	id, slug := e.newModule("https://validate.example.test")
	token := mintToken(e, id, adminTok())
	url := "/api/v1/integrations/pairing/redeem?token=" + token

	e.expect(e.do(http.MethodPost, url, "", map[string]any{"module_id": uuid.NewString()}), http.StatusConflict)
	e.expect(e.do(http.MethodPost, url, "", map[string]any{"module_slug": "something-else"}), http.StatusConflict)
	e.expect(e.do(http.MethodPost, url, "", `{"module_version":`), http.StatusBadRequest)
	e.expect(e.do(http.MethodPost, url, "", map[string]any{"unknown": 1}), http.StatusBadRequest)
	// None of the refusals consumed the token.
	e.expect(e.do(http.MethodPost, url, "", map[string]any{"module_id": id.String(), "module_slug": slug, "module_version": "1.0.0"}), http.StatusOK)
	e.expect(e.do(http.MethodPost, url, "", nil), http.StatusUnauthorized)
}

func TestRedeemExpiredToken(t *testing.T) {
	e := newEnv(t, nil)
	id, _ := e.newModule("https://expired.example.test")
	token := mintToken(e, id, adminTok())
	if _, err := e.pool.Exec(context.Background(), `UPDATE pairing_tokens SET expires_at = now() - interval '1 minute' WHERE module_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token="+token, "", nil), http.StatusUnauthorized)
	e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token=nope", "", nil), http.StatusUnauthorized)
}

func TestRepairRevokesOlderCredentials(t *testing.T) {
	e := newEnv(t, nil)
	id, _ := e.newModule("https://repair.example.test")
	first := e.pair(id, nil)
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/settings", first, nil), http.StatusOK)
	second := e.pair(id, nil)
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/settings", first, nil), http.StatusUnauthorized)
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/settings", second, nil), http.StatusOK)

	var page struct {
		Items []struct {
			Action string `json:"action"`
			Actor  *struct {
				ID string `json:"id"`
			} `json:"actor"`
		} `json:"items"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?entity_id="+id.String()+"&action=pairing.token.redeem", adminTok(), nil), http.StatusOK).json(t, &page)
	if len(page.Items) != 2 || page.Items[0].Actor == nil {
		t.Fatalf("want two redeem events with the minting admin as actor, got %+v", page.Items)
	}
	if e.auditCount("pairing.token.create", id) != 2 {
		t.Fatal("want two pairing.token.create events")
	}
}

func TestCredentialRevokeAPI(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	id, _ := e.newModule("https://revoke.example.test")
	cred := e.pair(id, nil)
	base := "/api/v1/modules/" + id.String() + "/credentials"

	var list []struct {
		ID        string  `json:"id"`
		RevokedAt *string `json:"revoked_at"`
	}
	e.expect(e.do(http.MethodGet, base, admin, nil), http.StatusOK).json(t, &list)
	if len(list) != 1 || list[0].RevokedAt != nil {
		t.Fatalf("list: %+v", list)
	}
	e.expect(e.do(http.MethodGet, base, memberTok(), nil), http.StatusForbidden)
	e.expect(e.do(http.MethodDelete, base+"/"+uuid.NewString(), admin, nil), http.StatusNotFound)

	var res struct {
		Revoked      int    `json:"revoked"`
		ModuleStatus string `json:"module_status"`
	}
	e.expect(e.do(http.MethodDelete, base+"/"+list[0].ID, admin, nil), http.StatusOK).json(t, &res)
	if res.Revoked != 1 || res.ModuleStatus != "unpaired" {
		t.Fatalf("revoke: %+v", res)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/settings", cred, nil), http.StatusUnauthorized)
	if e.auditCount("module.credential.revoke", id) != 1 {
		t.Fatal("missing revoke audit event")
	}

	e.pair(id, nil)
	e.expect(e.do(http.MethodDelete, base, admin, nil), http.StatusOK).json(t, &res)
	if res.Revoked != 1 || res.ModuleStatus != "unpaired" {
		t.Fatalf("revoke all: %+v", res)
	}
	if e.auditCount("module.credentials.revoke_all", id) != 1 {
		t.Fatal("missing revoke_all audit event")
	}
}
