// Package http builds Den's chi router and wires every handler package to
// its concrete dependencies (DB pool, files client, auth middleware).
//
// Route layout:
//
//	GET  /livez                                        — process liveness, never touches DB
//	GET  /readyz                                       — ready for traffic (requires DB ping)
//	GET  /api/health                                   — human-friendly health, like Stash's
//
//	Authenticated (human UI, dev-header or OIDC):
//	GET    /api/v1/me
//	GET    /api/v1/me/preferences
//	PUT    /api/v1/me/preferences                — theme: system | light | dark
//	GET    /api/v1/dashboard
//	GET    /api/v1/modules
//	POST   /api/v1/modules
//	GET    /api/v1/modules/{id}
//	PATCH  /api/v1/modules/{id}
//	DELETE /api/v1/modules/{id}
//	POST   /api/v1/modules/{id}/pairing-tokens   — mints a one-shot pairing URL
//	GET    /api/v1/brand/profile
//	PUT    /api/v1/brand/profile
//	GET    /api/v1/brand/assets
//	POST   /api/v1/brand/assets
//	GET    /api/v1/brand/assets/{id}
//	DELETE /api/v1/brand/assets/{id}
//	GET    /api/v1/org-settings
//	PUT    /api/v1/org-settings
//	GET    /api/v1/keycloak-sync                 — login branding push status
//	POST   /api/v1/keycloak-sync                 — push branding to Keycloak now
//	GET    /api/v1/operations/status             — databases + backup status (operator)
//	PUT    /api/v1/operations/settings           — backup hour + retention
//	GET    /api/v1/operations/backups
//	POST   /api/v1/operations/backups            — back up now
//	POST   /api/v1/operations/backups/download-link
//
//	DELETE /api/v1/modules/{id}?database=drop&confirm=<slug> also drops the
//	module's database (backed up first); without it the data is kept.
//
//	Public:
//	POST /api/v1/integrations/pairing/redeem?token=... — one-shot token → credential
//	GET  /api/v1/public/brand                         — brand bundle for login screens
//	GET  /api/v1/public/brand/assets/{role}           — newest asset of a role (logo, …)
//	GET  /api/v1/backup-downloads/{token}             — one-time backup download link
//
//	Module-bearer auth (Authorization: Bearer <module credential>):
//	GET /api/v1/integrations/branding/profile
//	GET /api/v1/integrations/branding/assets
//	GET /api/v1/integrations/branding/assets/{id}
//	GET /api/v1/integrations/settings
//	GET /api/v1/integrations/bundle                   — brand + settings in one call
//	GET/PUT /api/v1/integrations/user-preferences     — a person's theme choice
package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/clientip"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/httperror"
	"github.com/AGlElectronics/den/backend/internal/keycloak"
	"github.com/AGlElectronics/den/backend/internal/ratelimit"
)

// Deps groups the runtime dependencies the HTTP layer needs. The server
// bootstrap (internal/app) builds this struct once and hands it to
// NewRouter.
type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Logger       zerolog.Logger
	Files        AssetStore
	OIDCVerifier auth.TokenVerifier
	// KeycloakSync pushes branding into the realm; nil disables it.
	KeycloakSync *KeycloakSync
	// NewLimiter builds the limiter of one rate-limited route; nil uses an
	// in-process ratelimit.Memory.
	NewLimiter func(route string, limit int, window time.Duration) ratelimit.Limiter
}

// Rate limits per client IP on the routes that need no sign-in.
var rateLimits = map[string]struct {
	limit  int
	window time.Duration
}{
	"pairing-redeem":  {10, time.Minute},
	"public-brand":    {120, time.Minute},
	"backup-download": {10, time.Minute},
}

// limitRoute is the rate-limit hook of one route.
func limitRoute(deps Deps, route string) func(http.Handler) http.Handler {
	rl := rateLimits[route]
	var l ratelimit.Limiter
	if deps.NewLimiter != nil {
		l = deps.NewLimiter(route, rl.limit, rl.window)
	} else {
		l = ratelimit.NewMemory(rl.limit, rl.window)
	}
	return ratelimit.Middleware(l,
		func(r *http.Request) string { return clientip.FromRequest(r) },
		func(w http.ResponseWriter, r *http.Request) { httperror.Render(w, r, httperror.RateLimited()) })
}

// NewRouter wires the chi router with middleware, CORS and routes.
// `version` is baked in via -ldflags at build time and surfaced on
// /livez and /api/health.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(clientip.Middleware(deps.Config.TrustedProxyCIDRs))
	r.Use(chimiddleware.Recoverer)
	r.Use(timeoutExceptLongRunning(60 * time.Second))

	r.Use(hlog.NewHandler(deps.Logger))
	r.Use(hlog.AccessHandler(func(req *http.Request, status, size int, duration time.Duration) {
		hlog.FromRequest(req).Info().
			Str("method", req.Method).
			Str("path", req.URL.Path).
			Int("status", status).
			Int("size", size).
			Dur("duration", duration).
			Msg("http")
	}))
	r.Use(hlog.RequestIDHandler("request_id", "X-Request-ID"))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Dev-User", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// --- Public health probes -------------------------------------------------

	health := newHealthHandler(deps.Pool, version)
	// K8s-style probes live at the root (not under /api) so a readiness
	// gate can be configured without worrying about the API prefix.
	r.Get("/livez", health.livez)
	r.Get("/readyz", health.readyz)

	// --- Handlers -------------------------------------------------------------

	resolver := auth.NewResolver(deps.Pool, auth.GroupConfig{
		Admin:  deps.Config.AdminGroups,
		Member: deps.Config.MemberGroups,
		Ops:    deps.Config.OpsGroups,
	})
	authMW := auth.Middleware(deps.Config, resolver, deps.OIDCVerifier)

	ops := newOpsClient(deps.Config.OperatorURL, deps.Config.OperatorToken)
	modH := newModuleHandler(deps.Pool, ops, deps.Config.IsDevelopment())
	opsH := &operationsHandler{ops: ops, pool: deps.Pool}
	brandH := newBrandingHandler(deps.Pool, deps.Files)
	pairH := newPairingHandler(deps.Pool, deps.Config.AppBaseURL, deps.Config.CredentialKey)
	settingsH := &orgSettingsHandler{pool: deps.Pool}
	prefsH := &preferencesHandler{pool: deps.Pool}
	auditH := &auditHandler{pool: deps.Pool}
	kcSync := deps.KeycloakSync
	if kcSync == nil {
		kcSync = NewKeycloakSync(deps.Pool, keycloak.Config{}, deps.Config.IdentityStaffMode, deps.Logger)
	}
	brandH.onChange = kcSync.Trigger
	settingsH.onChange = kcSync.Trigger

	// Module bearer middleware: hashes the Authorization bearer, resolves
	// to a module_credential row, and attaches a ModulePrincipal to the
	// request context. The pairing redeem endpoint itself cannot use this —
	// that is where the module gets its first credential.
	moduleAuth := auth.ModuleBearerMiddleware(NewModuleResolver(deps.Pool))

	// --- API --------------------------------------------------------------
	r.Route("/api", func(api chi.Router) {
		api.Get("/health", health.health)
		api.Get("/role-catalog", roleCatalogHandler(deps.Config.AdminGroups, deps.Config.MemberGroups, deps.Config.OpsGroups))

		api.Route("/v1", func(v1 chi.Router) {
			// --- Integration (server-to-server) -------------------------
			// `pairing/redeem` is intentionally PUBLIC: the module doesn't
			// have a credential yet and is authenticating via the raw
			// pairing token in the query string. Everything else under
			// /integrations/* is bearer-authenticated.
			v1.Route("/integrations", func(integ chi.Router) {
				integ.With(limitRoute(deps, "pairing-redeem")).Post("/pairing/redeem", pairH.redeem)

				integ.Group(func(bearer chi.Router) {
					bearer.Use(moduleAuth)
					bearer.Route("/branding", brandH.integrationRoutes)
					bearer.Get("/settings", settingsH.integrationGet)
					bearer.Get("/bundle", brandH.integrationBundle(settingsH))
					bearer.Get("/user-preferences", prefsH.integrationGet)
					bearer.Put("/user-preferences", prefsH.integrationPut)
				})
			})

			// --- Public: login screens need the brand before sign-in ----
			v1.With(limitRoute(deps, "public-brand")).Route("/public", brandH.publicRoutes(settingsH))
			// One-time backup download links minted by an admin.
			v1.With(limitRoute(deps, "backup-download")).Get("/backup-downloads/{token}", opsH.download)

			// --- Human UI (dev header or OIDC) --------------------------
			v1.Group(func(secure chi.Router) {
				secure.Use(authMW)

				secure.Get("/me", meHandler)
				secure.Get("/me/preferences", prefsH.getMine)
				secure.Put("/me/preferences", prefsH.putMine)
				secure.With(auth.RequireRole(defaultOrgID, "member", "admin", "owner")).Get("/dashboard", dashboardHandler(deps.Pool))

				// Admin console APIs: convention admins / owners only.
				secure.Group(func(admin chi.Router) {
					admin.Use(auth.RequireRole(defaultOrgID, "admin", "owner"))
					admin.Route("/modules", func(sub chi.Router) {
						modH.routes(sub)
						sub.Post("/{id}/pairing-tokens", pairH.mintToken)
						sub.Route("/{id}/credentials", pairH.credentialRoutes)
					})
					admin.Route("/brand", brandH.humanRoutes)
					admin.Get("/org-settings", settingsH.get)
					admin.Put("/org-settings", settingsH.put)
					admin.Get("/keycloak-sync", kcSync.getStatus)
					admin.Post("/keycloak-sync", kcSync.syncNow)
					admin.Get("/audit-events", auditH.list)
				})

				// Backups, downloads, the schedule and database drops:
				// the ops role only, admin alone is not enough. The
				// read-only status (backup health, database list) is also
				// shown to admins.
				secure.Route("/operations", func(op chi.Router) {
					op.With(auth.RequireRole(defaultOrgID, "admin", "owner", auth.RoleOps)).Get("/status", opsH.status)
					op.Group(func(ops chi.Router) {
						ops.Use(auth.RequireRole(defaultOrgID, auth.RoleOps))
						opsH.routes(ops)
					})
				})
			})
		})
	})

	return r
}

// timeoutExceptLongRunning applies the request timeout everywhere except
// backup downloads and module removal (which backs the database up first).
func timeoutExceptLongRunning(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		limited := chimiddleware.Timeout(d)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if strings.HasPrefix(p, "/api/v1/backup-downloads/") ||
				(r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/v1/modules/")) {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}
