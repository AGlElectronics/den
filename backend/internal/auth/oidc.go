package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/groupmatch"
)

// OIDCVerifier wraps go-oidc. Den only verifies tokens — it never issues them.
// See docs/adr/0001-idp-first-ecosystem.md.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	issuer   string
	groups   groupmatch.Config
}

// OIDCClaims is the subset of the access token Den cares about during
// provisioning. Groups feeds org role grants (e.g. den-admin → org admin).
type OIDCClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Nickname          string `json:"nickname"`
	// Type is Keycloak's "typ" claim: "Bearer" for access tokens, "ID" for
	// ID tokens.
	Type string `json:"typ"`
	// Groups come only from the claim named by OIDC_GROUPS_CLAIM.
	Groups []string `json:"-"`
}

// NewOIDCVerifier runs OIDC discovery against the configured issuer and
// returns a verifier pinned to Den's API audience (OIDC_AUDIENCE, default
// den-api).
func NewOIDCVerifier(ctx context.Context, cfg *config.Config) (*OIDCVerifier, error) {
	if cfg.OIDCIssuerURL == "" || cfg.OIDCAudience == "" {
		return nil, errors.New("OIDC verifier requires OIDC_ISSUER_URL and OIDC_AUDIENCE")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return newOIDCVerifier(provider.Verifier(&oidc.Config{ClientID: cfg.OIDCAudience}), cfg.OIDCIssuerURL, cfg.GroupMatch), nil
}

func newOIDCVerifier(v *oidc.IDTokenVerifier, issuer string, groups groupmatch.Config) *OIDCVerifier {
	return &OIDCVerifier{verifier: v, issuer: issuer, groups: groups}
}

// Verify parses and validates a bearer access token: signature, issuer,
// audience, expiry and subject. Only tokens with typ "Bearer" are accepted:
// ID tokens and other token types are refused even when their audience
// matches.
func (v *OIDCVerifier) Verify(ctx context.Context, bearer string) (*OIDCClaims, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return nil, errors.New("missing bearer token")
	}
	tok, err := v.verifier.Verify(ctx, bearer)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var c OIDCClaims
	if err := tok.Claims(&c); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if !strings.EqualFold(c.Type, "Bearer") {
		return nil, fmt.Errorf("token typ %q is not Bearer; Den accepts access tokens only", c.Type)
	}
	if c.Subject == "" {
		return nil, errors.New("token missing sub claim")
	}
	var raw map[string]any
	if err := tok.Claims(&raw); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	c.Groups = v.groups.Groups(raw)
	// Trust the configured issuer, not a client-asserted value.
	c.Issuer = v.issuer
	return &c, nil
}

func displayNameFromClaims(c *OIDCClaims) string {
	for _, candidate := range []string{c.Name, c.PreferredUsername, c.Nickname, c.Email} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return c.Subject
}
