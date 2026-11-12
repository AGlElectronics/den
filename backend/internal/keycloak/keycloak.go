// Package keycloak is Den's minimal Keycloak admin client. Den only uses it
// to keep the realm's display name equal to the organisation name (the login
// theme pulls the rest of the brand from Den itself). Authenticates with a
// confidential client's service account (realm-management: manage-realm).
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	// BaseURL is how Den reaches Keycloak (e.g. http://keycloak:8080).
	BaseURL      string
	Realm        string
	ClientID     string
	ClientSecret string
}

func (c Config) Enabled() bool {
	return c.BaseURL != "" && c.Realm != "" && c.ClientID != "" && c.ClientSecret != ""
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Realm() string { return c.cfg.Realm }

func (c *Client) token(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	endpoint := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", c.cfg.BaseURL, url.PathEscape(c.cfg.Realm))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("keycloak token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("keycloak token: status %d: %s", res.StatusCode, truncate(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("keycloak token: no access_token in response")
	}
	return tok.AccessToken, nil
}

// UpdateRealm applies a partial realm representation. Keycloak only touches
// the fields present; listed attributes are set, others are left alone.
func (c *Client) UpdateRealm(ctx context.Context, rep map[string]any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	buf, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/admin/realms/%s", c.cfg.BaseURL, url.PathEscape(c.cfg.Realm))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("keycloak update realm: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		return fmt.Errorf("keycloak update realm: status %d: %s", res.StatusCode, truncate(string(body)))
	}
	return nil
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
