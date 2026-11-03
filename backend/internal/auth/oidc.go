package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/AGlElectronics/den/backend/internal/config"
)

// OIDCVerifier wraps go-oidc. Den only verifies tokens — it never issues them.
// See docs/adr/0001-idp-first-ecosystem.md.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	issuer   string
}

// OIDCClaims is the subset of the ID token Den cares about during
// provisioning. Groups feeds org role grants (e.g. den-admin → org admin).
type OIDCClaims struct {
	Issuer            string   `json:"iss"`
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Nickname          string   `json:"nickname"`
	Groups            []string `json:"groups"`
	Roles             []string `json:"roles"`
}

// NewOIDCVerifier runs OIDC discovery against the configured issuer and
// returns a verifier pinned to Den's audience (client id by default).
func NewOIDCVerifier(ctx context.Context, cfg *config.Config) (*OIDCVerifier, error) {
	if cfg.OIDCIssuerURL == "" || cfg.OIDCClientID == "" {
		return nil, errors.New("OIDC verifier requires OIDC_ISSUER_URL and OIDC_CLIENT_ID")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	audience := cfg.OIDCAudience
	if audience == "" {
		audience = cfg.OIDCClientID
	}
	return &OIDCVerifier{
		verifier: provider.Verifier(&oidc.Config{ClientID: audience}),
		issuer:   cfg.OIDCIssuerURL,
	}, nil
}

// Verify parses and validates a bearer ID token.
func (v *OIDCVerifier) Verify(ctx context.Context, bearer string) (*OIDCClaims, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return nil, errors.New("missing bearer token")
	}
	idToken, err := v.verifier.Verify(ctx, bearer)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var c OIDCClaims
	if err := idToken.Claims(&c); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if c.Subject == "" {
		return nil, errors.New("token missing sub claim")
	}
	// Trust the configured issuer, not a client-asserted value.
	c.Issuer = v.issuer
	c.Groups = mergeUnique(c.Groups, c.Roles)
	return &c, nil
}

func mergeUnique(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, v := range append(append([]string{}, a...), b...) {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func displayNameFromClaims(c *OIDCClaims) string {
	for _, candidate := range []string{c.Name, c.PreferredUsername, c.Nickname, c.Email} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return c.Subject
}
