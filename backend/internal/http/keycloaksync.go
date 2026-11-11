package http

import (
	"context"
	"html"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/keycloak"
)

// The Keycloak sign-in page PULLS its look from Den: the conservices login
// theme fetches GET /api/v1/public/brand in the browser (see
// docs/adr/0008-keycloak-branding-pull.md), so Den needs no write access to
// the realm for it. The one thing a browser cannot fetch is the realm's
// display name, which Keycloak puts into its own e-mails and page titles.
// When a Keycloak admin client is configured, Den keeps that in line with the
// organisation name (realm-management manage-realm; Keycloak has no smaller
// role that allows it). In identity mode "existing" the realm belongs to the
// convention and Den never writes to it.

const (
	keycloakResync = 5 * time.Minute
	keycloakRetry  = 15 * time.Second
)

type keycloakSyncStatus struct {
	Enabled bool `json:"enabled"`
	// Mode is how the sign-in page gets the brand: always "pull".
	Mode string `json:"mode"`
	// Message explains why the display-name sync is off, when it is.
	Message     string     `json:"message,omitempty"`
	Realm       string     `json:"realm,omitempty"`
	Revision    int        `json:"revision,omitempty"`
	DisplayName string     `json:"display_name,omitempty"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

// KeycloakSync keeps the realm's display name in line with Den.
type KeycloakSync struct {
	kc       *keycloak.Client // nil = not configured or not allowed
	pool     *pgxpool.Pool
	brand    *brandingHandler
	settings *orgSettingsHandler
	log      zerolog.Logger
	kick     chan struct{}

	mu       sync.Mutex
	status   keycloakSyncStatus
	lastName string
	lastOK   *bool
}

// NewKeycloakSync returns a syncer. Without a configured client, or in
// identity mode existing, it is a no-op that reports enabled=false with the
// reason.
func NewKeycloakSync(pool *pgxpool.Pool, cfg keycloak.Config, identityMode string, log zerolog.Logger) *KeycloakSync {
	s := &KeycloakSync{
		pool:     pool,
		brand:    newBrandingHandler(pool, nil),
		settings: &orgSettingsHandler{pool: pool},
		log:      log,
		kick:     make(chan struct{}, 1),
		status:   keycloakSyncStatus{Mode: "pull"},
	}
	switch {
	case identityMode == config.IdentityExisting:
		s.status.Message = "Off because IDENTITY_STAFF_MODE is existing: the realm belongs to your convention and Den never changes it. " +
			"Its sign-in page shows Den's brand only if the conservices login theme is installed there."
	case !cfg.Enabled():
		s.status.Message = "The sign-in page fetches the brand from Den by itself. Set KEYCLOAK_ADMIN_URL and KEYCLOAK_CLIENT_SECRET " +
			"only if Keycloak's e-mails should also use the organisation name."
	default:
		s.kc = keycloak.New(cfg)
		s.status.Enabled, s.status.Realm = true, cfg.Realm
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
		if err := s.Sync(ctx, false); err != nil {
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

// Sync pushes the display name once. manual marks an admin's "Sync now",
// which is always audited; background syncs are audited when the pushed
// name or the outcome changes.
func (s *KeycloakSync) Sync(ctx context.Context, manual bool) error {
	if s.kc == nil {
		return nil
	}
	revision, name, err := s.push(ctx)
	s.mu.Lock()
	ok := err == nil
	changed := s.lastOK == nil || *s.lastOK != ok || (ok && name != s.lastName)
	s.lastOK = &ok
	if err != nil {
		s.status.LastError = err.Error()
		s.log.Warn().Err(err).Str("realm", s.kc.Realm()).Msg("keycloak display name sync failed")
	} else {
		now := time.Now().UTC()
		s.status.SyncedAt, s.status.Revision, s.status.DisplayName, s.status.LastError = &now, revision, name, ""
		s.lastName = name
	}
	s.mu.Unlock()

	if (manual || changed) && s.pool != nil {
		ev := audit.Event{
			OrgID: orgRef(), Action: "keycloak.sync", EntityType: "organization", EntityID: defaultOrgID,
			After: map[string]any{"realm": s.kc.Realm(), "display_name": name, "ok": ok, "manual": manual},
		}
		if err != nil {
			ev.Note = err.Error()
		}
		if aerr := audit.Write(context.WithoutCancel(ctx), s.pool, ev); aerr != nil {
			s.log.Warn().Err(aerr).Msg("keycloak sync audit event failed")
		}
	}
	return err
}

func (s *KeycloakSync) push(ctx context.Context) (int, string, error) {
	st, err := s.settings.load(ctx, defaultOrgID)
	if err != nil {
		return 0, "", err
	}
	b, err := s.brand.bundle(ctx, defaultOrgID, st.Data)
	if err != nil {
		return 0, "", err
	}
	if b.OrganizationName == "" {
		return b.Revision, "", nil
	}
	rep := map[string]any{
		"displayName":     b.OrganizationName,
		"displayNameHtml": html.EscapeString(b.OrganizationName),
	}
	return b.Revision, b.OrganizationName, s.kc.UpdateRealm(ctx, rep)
}

func (s *KeycloakSync) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Status())
}

// syncNow is POST /keycloak-sync: sync synchronously and report the result.
func (s *KeycloakSync) syncNow(w http.ResponseWriter, r *http.Request) {
	if s.kc != nil {
		_ = s.Sync(r.Context(), true)
	}
	writeJSON(w, http.StatusOK, s.Status())
}
