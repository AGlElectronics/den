package http

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/files"
	"github.com/AGlElectronics/den/backend/internal/keycloak"
)

// Den orchestrates the Keycloak login look the same way it brands paired
// modules: it pushes the brand into the realm (login theme, display name and
// the conservicesBrand attribute, which the conservices login theme renders).
// Keycloak never needs to know where Den lives.

const (
	loginThemeName      = "conservices"
	loginBrandAttribute = "conservicesBrand"
	keycloakResync      = 5 * time.Minute
	keycloakRetry       = 15 * time.Second
)

// loginBrand is what the login theme reads from the realm attribute.
type loginBrand struct {
	Revision         int             `json:"revision"`
	OrganizationName string          `json:"organization_name"`
	Tagline          string          `json:"tagline,omitempty"`
	Theme            json.RawMessage `json:"theme"`
	LogoURL          string          `json:"logo_url,omitempty"`
	FaviconURL       string          `json:"favicon_url,omitempty"`
}

type keycloakSyncStatus struct {
	Enabled   bool       `json:"enabled"`
	Realm     string     `json:"realm,omitempty"`
	Revision  int        `json:"revision,omitempty"`
	SyncedAt  *time.Time `json:"synced_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

// KeycloakSync keeps the realm's login branding in line with Den.
type KeycloakSync struct {
	kc        *keycloak.Client // nil = not configured
	brand     *brandingHandler
	settings  *orgSettingsHandler
	publicURL string
	log       zerolog.Logger
	kick      chan struct{}

	mu     sync.Mutex
	status keycloakSyncStatus
}

// NewKeycloakSync returns a syncer; with an unconfigured client it is a no-op
// that reports enabled=false. publicURL is Den's browser-facing base URL.
func NewKeycloakSync(pool *pgxpool.Pool, filesClient *files.Client, cfg keycloak.Config, publicURL string, log zerolog.Logger) *KeycloakSync {
	s := &KeycloakSync{
		brand:     newBrandingHandler(pool, filesClient),
		settings:  &orgSettingsHandler{pool: pool},
		publicURL: strings.TrimRight(publicURL, "/"),
		log:       log,
		kick:      make(chan struct{}, 1),
	}
	if cfg.Enabled() {
		s.kc = keycloak.New(cfg)
		s.status = keycloakSyncStatus{Enabled: true, Realm: cfg.Realm}
	}
	return s
}

// Run syncs at start, after every Trigger, and every few minutes (which also
// repairs manual edits in the Keycloak console). Failures retry sooner.
func (s *KeycloakSync) Run(ctx context.Context) {
	if s == nil || s.kc == nil {
		return
	}
	for {
		wait := keycloakResync
		if err := s.Sync(ctx); err != nil {
			wait = keycloakRetry
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.kick:
			t.Stop()
		case <-t.C:
		}
	}
}

// Trigger asks Run to sync now without blocking the caller.
func (s *KeycloakSync) Trigger() {
	if s == nil || s.kc == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *KeycloakSync) Status() keycloakSyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Sync pushes the current brand to the realm once.
func (s *KeycloakSync) Sync(ctx context.Context) error {
	if s.kc == nil {
		return nil
	}
	revision, err := s.push(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
		s.log.Warn().Err(err).Str("realm", s.kc.Realm()).Msg("keycloak brand sync failed")
		return err
	}
	now := time.Now().UTC()
	s.status.SyncedAt, s.status.Revision, s.status.LastError = &now, revision, ""
	return nil
}

func (s *KeycloakSync) push(ctx context.Context) (int, error) {
	st, err := s.settings.load(ctx, defaultOrgID)
	if err != nil {
		return 0, err
	}
	b, err := s.brand.bundle(ctx, defaultOrgID, st.Data)
	if err != nil {
		return 0, err
	}
	lb := loginBrand{Revision: b.Revision, OrganizationName: b.OrganizationName, Theme: b.Theme}
	var copyMap map[string]any
	_ = json.Unmarshal(b.Copy, &copyMap)
	if t, ok := copyMap["tagline"].(string); ok {
		lb.Tagline = strings.TrimSpace(t)
	}
	asset := func(role string) string {
		id, ok := b.Assets[role]
		if !ok {
			return ""
		}
		return s.publicURL + "/api/v1/public/brand/assets/" + role + "?v=" + id
	}
	lb.LogoURL, lb.FaviconURL = asset("logo"), asset("favicon")
	attr, err := json.Marshal(lb)
	if err != nil {
		return 0, err
	}
	rep := map[string]any{
		"loginTheme": loginThemeName,
		"attributes": map[string]string{loginBrandAttribute: string(attr)},
	}
	if b.OrganizationName != "" {
		rep["displayName"] = b.OrganizationName
		rep["displayNameHtml"] = html.EscapeString(b.OrganizationName)
	}
	return b.Revision, s.kc.UpdateRealm(ctx, rep)
}

func (s *KeycloakSync) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Status())
}

// syncNow is POST /keycloak-sync: sync synchronously and report the result.
func (s *KeycloakSync) syncNow(w http.ResponseWriter, r *http.Request) {
	_ = s.Sync(r.Context())
	writeJSON(w, http.StatusOK, s.Status())
}
