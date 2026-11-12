package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/groupmatch"
)

// moduleCaller is how Den calls paired modules server to server: their
// role catalogs (who may use a module) and their portal to-dos. It calls the
// module's internal_url, else its base_url, over https (http only with
// APP_ENV=development).

const (
	moduleCallTimeout = 5 * time.Second
	catalogTTL        = 5 * time.Minute
	maxModuleBody     = 1 << 20
)

type moduleCaller struct {
	pool      *pgxpool.Pool
	hc        *http.Client
	allowHTTP bool
	key       []byte
	log       zerolog.Logger
	// groups is the shared group rule for matching a person's groups
	// against a module's role catalog.
	groups groupmatch.Config

	mu       sync.Mutex
	catalogs map[uuid.UUID]cachedCatalog
}

type cachedCatalog struct {
	addr    string
	groups  []string
	fetched time.Time
}

func newModuleCaller(pool *pgxpool.Pool, allowHTTP bool, key []byte, log zerolog.Logger) *moduleCaller {
	return &moduleCaller{
		pool:      pool,
		hc:        &http.Client{Timeout: moduleCallTimeout},
		allowHTTP: allowHTTP,
		key:       key,
		log:       log,
		catalogs:  map[uuid.UUID]cachedCatalog{},
	}
}

// portalModule is a module as the access checks and calls need it.
type portalModule struct {
	ID          uuid.UUID
	Slug        string
	DisplayName string
	BaseURL     string
	InternalURL string
	Environment string
	Status      string
	Enabled     bool
	Visibility  string
}

// callAddr is the address Den calls, or an error when it may not call it.
func (c *moduleCaller) callAddr(m portalModule) (string, error) {
	addr := m.InternalURL
	if addr == "" {
		addr = m.BaseURL
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return "", errors.New("module has no address")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !c.allowHTTP) {
		return "", errors.New("module address is not https")
	}
	return strings.TrimRight(addr, "/"), nil
}

func (c *moduleCaller) getJSON(ctx context.Context, addr string, header http.Header, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, moduleCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return res.StatusCode, json.NewDecoder(io.LimitReader(res.Body, maxModuleBody)).Decode(out)
}

// catalogGroups returns the IdP groups in the module's role catalog. A
// failed fetch keeps the last good answer; with none, ok is false.
func (c *moduleCaller) catalogGroups(ctx context.Context, m portalModule) ([]string, bool) {
	addr, err := c.callAddr(m)
	if err != nil {
		return nil, false
	}
	c.mu.Lock()
	cached, have := c.catalogs[m.ID]
	c.mu.Unlock()
	if have && cached.addr == addr && time.Since(cached.fetched) < catalogTTL {
		return cached.groups, true
	}
	var cat struct {
		Roles []struct {
			Group string `json:"group"`
		} `json:"roles"`
	}
	if _, err := c.getJSON(ctx, addr+"/api/role-catalog", nil, &cat); err != nil {
		c.log.Debug().Err(err).Str("module", m.Slug).Msg("role catalog fetch failed")
		if have && cached.addr == addr {
			return cached.groups, true
		}
		return nil, false
	}
	groups := make([]string, 0, len(cat.Roles))
	for _, r := range cat.Roles {
		if g := strings.TrimSpace(r.Group); g != "" {
			groups = append(groups, g)
		}
	}
	c.mu.Lock()
	c.catalogs[m.ID] = cachedCatalog{addr: addr, groups: groups, fetched: time.Now()}
	c.mu.Unlock()
	return groups, true
}

// canUse reports whether a person with groups may use m: everyone for
// portal_visibility "everyone", else one of the groups in its role catalog.
func (c *moduleCaller) canUse(ctx context.Context, groups []string, m portalModule) bool {
	if m.Visibility == "everyone" {
		return true
	}
	catalog, ok := c.catalogGroups(ctx, m)
	return ok && auth.InAnyGroup(c.groups, groups, catalog)
}

// usableModules lists enabled, paired modules with an address.
func (c *moduleCaller) usableModules(ctx context.Context) ([]portalModule, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT id, slug, display_name, base_url, internal_url, environment, status, enabled, portal_visibility
		  FROM modules
		 WHERE organization_id = $1
		   AND enabled = true
		   AND status IN ('paired', 'degraded')
		   AND base_url <> ''
		 ORDER BY display_name`, defaultOrgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (portalModule, error) {
		var m portalModule
		err := r.Scan(&m.ID, &m.Slug, &m.DisplayName, &m.BaseURL, &m.InternalURL, &m.Environment, &m.Status, &m.Enabled, &m.Visibility)
		return m, err
	})
}

// visibleModules filters usable modules for a user: admins see all, others
// only those they can use.
func (c *moduleCaller) visibleModules(ctx context.Context, u *auth.User) ([]portalModule, error) {
	mods, err := c.usableModules(ctx)
	if err != nil || u.HasRoleIn(defaultOrgID, "admin", "owner") {
		return mods, err
	}
	out := make([]portalModule, 0, len(mods))
	var wg sync.WaitGroup
	keep := make([]bool, len(mods))
	for i, m := range mods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keep[i] = c.canUse(ctx, u.Groups, m)
		}()
	}
	wg.Wait()
	for i, m := range mods {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out, nil
}

// credential recomputes the module's newest active credential, or "" when
// Den cannot (no DEN_CREDENTIAL_KEY, or paired before the key was set).
func (c *moduleCaller) credential(ctx context.Context, moduleID uuid.UUID) (string, error) {
	if len(c.key) == 0 {
		return "", nil
	}
	var (
		id   uuid.UUID
		hash string
	)
	err := c.pool.QueryRow(ctx, `
		SELECT id, credential_hash FROM module_credentials
		 WHERE module_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`, moduleID).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	cred := deriveCredential(c.key, id)
	if sha256Hex(cred) != hash {
		return "", nil
	}
	return cred, nil
}
