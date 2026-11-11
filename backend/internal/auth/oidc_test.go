package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/AGlElectronics/den/backend/internal/groupmatch"
)

const testIssuer = "https://auth.test/realms/conservices"

type testSigner struct {
	t      *testing.T
	signer jose.Signer
	keys   *oidc.StaticKeySet
}

func newTestSigner(t *testing.T) *testSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &testSigner{t: t, signer: signer, keys: &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}}
}

func (s *testSigner) token(claims map[string]any) string {
	s.t.Helper()
	base := map[string]any{
		"iss": testIssuer, "sub": "sub-1", "aud": []string{"den-api", "account"}, "typ": "Bearer",
		"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "name": "Alice",
	}
	for k, v := range claims {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	payload, err := json.Marshal(base)
	if err != nil {
		s.t.Fatal(err)
	}
	obj, err := s.signer.Sign(payload)
	if err != nil {
		s.t.Fatal(err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		s.t.Fatal(err)
	}
	return raw
}

func (s *testSigner) verifier(rule groupmatch.Config) *OIDCVerifier {
	v := oidc.NewVerifier(testIssuer, s.keys, &oidc.Config{ClientID: "den-api"})
	return newOIDCVerifier(v, testIssuer, rule)
}

func TestVerifyAccessToken(t *testing.T) {
	s := newTestSigner(t)
	v := s.verifier(groupmatch.Config{})
	c, err := v.Verify(context.Background(), s.token(map[string]any{
		"groups": []string{"/den-admin", "den-ops"},
		"roles":  []string{"den-member"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "sub-1" || c.Issuer != testIssuer {
		t.Fatalf("claims %+v", c)
	}
	if !slices.Equal(c.Groups, []string{"/den-admin", "den-ops"}) {
		t.Fatalf("groups %v: the roles claim must not be merged in", c.Groups)
	}
}

func TestVerifyRefusals(t *testing.T) {
	s := newTestSigner(t)
	v := s.verifier(groupmatch.Config{})
	cases := map[string]struct {
		claims map[string]any
		want   string
	}{
		"id token":       {map[string]any{"typ": "ID"}, "access tokens only"},
		"no typ":         {map[string]any{"typ": nil}, "access tokens only"},
		"refresh token":  {map[string]any{"typ": "Refresh"}, "access tokens only"},
		"other audience": {map[string]any{"aud": "den-spa"}, "audience"},
		"expired":        {map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}, "expired"},
		"other issuer":   {map[string]any{"iss": "https://evil.test/realms/conservices"}, "different provider"},
		"no subject":     {map[string]any{"sub": nil}, "sub"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), s.token(c.claims))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
	if _, err := v.Verify(context.Background(), "  "); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestVerifyGroupsClaim(t *testing.T) {
	s := newTestSigner(t)
	v := s.verifier(groupmatch.Config{Claim: "realm_access.roles"})
	c, err := v.Verify(context.Background(), s.token(map[string]any{
		"groups":       []string{"den-member"},
		"realm_access": map[string]any{"roles": []string{"den-admin"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Groups, []string{"den-admin"}) {
		t.Fatalf("groups %v, want only the configured claim", c.Groups)
	}
}

func TestDisplayNameFromClaims(t *testing.T) {
	c := &OIDCClaims{Subject: "sub-1", PreferredUsername: "alice", Email: "a@x"}
	if got := displayNameFromClaims(c); got != "alice" {
		t.Fatalf("got %q", got)
	}
	c = &OIDCClaims{Subject: "sub-2"}
	if got := displayNameFromClaims(c); got != "sub-2" {
		t.Fatalf("got %q", got)
	}
}
