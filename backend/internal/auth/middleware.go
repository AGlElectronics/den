package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// TokenVerifier verifies a bearer token from the web app and returns its
// claims. *OIDCVerifier is the production implementation.
type TokenVerifier interface {
	Verify(ctx context.Context, bearer string) (*OIDCClaims, error)
}

// Middleware authenticates a human request. In dev mode, X-Dev-User (or the
// default from DEV_DEFAULT_USER) is accepted and the user is provisioned on
// the fly. In OIDC mode, the Authorization Bearer ID token is verified
// against the configured issuer and the user is JIT-provisioned. Disabled
// accounts (users.is_disabled) get 403.
func Middleware(cfg *config.Config, resolver *Resolver, oidcVerifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			switch cfg.AuthMode {
			case config.AuthModeDev:
				name := strings.TrimSpace(r.Header.Get("X-Dev-User"))
				if name == "" {
					name = cfg.DevDefaultUser
				}
				user, err := resolver.LoadOrProvisionDevUser(ctx, name)
				if errors.Is(err, ErrUserDisabled) {
					httperror.Render(w, r, httperror.Forbidden("this account is disabled"))
					return
				}
				if err != nil {
					log.Ctx(ctx).Error().Err(err).Msg("dev auth failed")
					httperror.Render(w, r, httperror.Internal(err))
					return
				}
				ctx = WithUser(ctx, user)

			case config.AuthModeOIDC:
				if oidcVerifier == nil {
					httperror.Render(w, r, httperror.Internal(errors.New("oidc verifier not initialised")))
					return
				}
				bearer := bearerFromHeader(r.Header.Get("Authorization"))
				if bearer == "" {
					httperror.Render(w, r, httperror.Unauthorized("missing bearer token"))
					return
				}
				claims, err := oidcVerifier.Verify(ctx, bearer)
				if err != nil {
					log.Ctx(ctx).Debug().Err(err).Msg("oidc verify failed")
					httperror.Render(w, r, httperror.Unauthorized("invalid token"))
					return
				}
				user, err := resolver.LoadOrProvisionOIDCUser(ctx, claims)
				if errors.Is(err, ErrUserDisabled) {
					httperror.Render(w, r, httperror.Forbidden("this account is disabled"))
					return
				}
				if err != nil {
					log.Ctx(ctx).Error().Err(err).Msg("oidc resolver failed")
					httperror.Render(w, r, httperror.Internal(err))
					return
				}
				ctx = WithUser(ctx, user)

			default:
				httperror.Render(w, r, httperror.Internal(errors.New("unknown auth mode")))
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerFromHeader(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// --- Integration (module) auth -----------------------------------------------

// ModuleResolver abstracts the lookup the bearer middleware needs. We keep
// it as an interface so tests can swap in a fake instead of a real DB pool.
type ModuleResolver interface {
	ResolveModuleCredential(ctx context.Context, rawCredential string) (*ModulePrincipal, error)
}

// ModuleBearerMiddleware authenticates server-to-server calls from mated
// modules against /api/v1/integrations/*. It expects
// `Authorization: Bearer <raw-credential>`, hashes it, looks up the
// non-revoked credential, and attaches a ModulePrincipal to the context.
func ModuleBearerMiddleware(resolver ModuleResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := bearerFromHeader(r.Header.Get("Authorization"))
			if bearer == "" {
				httperror.Render(w, r, httperror.Unauthorized("missing bearer token"))
				return
			}
			principal, err := resolver.ResolveModuleCredential(r.Context(), bearer)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					httperror.Render(w, r, httperror.Unauthorized("invalid or revoked credential"))
					return
				}
				httperror.Render(w, r, httperror.Internal(err))
				return
			}
			ctx := WithModule(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// EnsureModule returns a 401 if the request doesn't carry a module
// principal. Handlers use it rather than panicking on nil.
func EnsureModule(w http.ResponseWriter, r *http.Request) (*ModulePrincipal, bool) {
	m, ok := ModuleFromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("module authentication required"))
		return nil, false
	}
	return m, true
}

// --- Convenience assertions ---------------------------------------------------

// RequireRole wraps a handler so the caller must be a member of the given
// organization with any of the listed roles. Used for admin-only mutations.
func RequireRole(orgID uuid.UUID, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := FromContext(r.Context())
			if !ok {
				httperror.Render(w, r, httperror.Unauthorized("authentication required"))
				return
			}
			if !u.HasRoleIn(orgID, roles...) {
				httperror.Render(w, r, httperror.Forbidden("insufficient role"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}