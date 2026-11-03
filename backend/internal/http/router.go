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
//
//	Public (mated-module server-to-server; the module holds the credential itself):
//	POST /api/v1/integrations/pairing/redeem?token=... — one-shot token → credential
//
//	Module-bearer auth (Authorization: Bearer <module credential>):
//	GET /api/v1/integrations/branding/profile
//	GET /api/v1/integrations/branding/assets
//	GET /api/v1/integrations/branding/assets/{id}
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/files"
)

// Deps groups the runtime dependencies the HTTP layer needs. The server
// bootstrap (internal/app) builds this struct once and hands it to
// NewRouter.
type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Logger       zerolog.Logger
	Files        *files.Client
	OIDCVerifier *auth.OIDCVerifier
}

// NewRouter wires the chi router with middleware, CORS and routes.
// `version` is baked in via -ldflags at build time and surfaced on
// /livez and /api/health.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

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

	resolver := auth.NewResolver(deps.Pool, deps.Config.AdminGroups)
	authMW := auth.Middleware(deps.Config, resolver, deps.OIDCVerifier)

	modH := newModuleHandler(deps.Pool)
	brandH := newBrandingHandler(deps.Pool, deps.Files)
	pairH := newPairingHandler(deps.Pool, deps.Config.AppBaseURL)

	// Module bearer middleware: hashes the Authorization bearer, resolves
	// to a module_credential row, and attaches a ModulePrincipal to the
	// request context. The pairing redeem endpoint itself cannot use this —
	// that is where the module gets its first credential.
	moduleAuth := auth.ModuleBearerMiddleware(NewModuleResolver(deps.Pool))

	// --- API --------------------------------------------------------------
	r.Route("/api", func(api chi.Router) {
		api.Get("/health", health.health)
		api.Get("/role-catalog", roleCatalogHandler(deps.Config.AdminGroups))

		api.Route("/v1", func(v1 chi.Router) {
			// --- Integration (server-to-server) -------------------------
			// `pairing/redeem` is intentionally PUBLIC: the module doesn't
			// have a credential yet and is authenticating via the raw
			// pairing token in the query string. Everything else under
			// /integrations/* is bearer-authenticated.
			v1.Route("/integrations", func(integ chi.Router) {
				integ.Post("/pairing/redeem", pairH.redeem)

				integ.Group(func(bearer chi.Router) {
					bearer.Use(moduleAuth)
					bearer.Route("/branding", brandH.integrationRoutes)
				})
			})

			// --- Human UI (dev header or OIDC) --------------------------
			v1.Group(func(secure chi.Router) {
				secure.Use(authMW)

				secure.Get("/me", meHandler)
				secure.Get("/dashboard", dashboardHandler(deps.Pool))

				// Admin console APIs: convention admins / owners only.
				secure.Group(func(admin chi.Router) {
					admin.Use(auth.RequireRole(defaultOrgID, "admin", "owner"))
					admin.Route("/modules", func(sub chi.Router) {
						modH.routes(sub)
						sub.Post("/{id}/pairing-tokens", pairH.mintToken)
					})
					admin.Route("/brand", brandH.humanRoutes)
				})
			})
		})
	})

	return r
}
