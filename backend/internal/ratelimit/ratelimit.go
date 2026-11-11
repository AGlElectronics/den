// Package ratelimit limits how often one client may call a route. Den uses
// it on its unauthenticated routes (pairing redeem, public brand, backup
// downloads), keyed by client IP, and on the signed-in API, keyed by person.
// The Limiter interface is the seam; New returns the suite-wide token bucket
// from the shared authbase package.
package ratelimit

import (
	"net/http"
	"strconv"
	"time"

	"github.com/AGlElectronics/den/backend/internal/authbase"
)

// Limiter decides whether one more request for key is allowed. When it is
// not, retryAfter says how long the client should wait.
type Limiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// New allows limit requests per key per window on average, and up to limit
// at once (a token bucket of size limit, refilled at limit/window). It is
// kept in process memory: with several replicas each counts on its own, so
// the effective limit is limit times the number of replicas.
func New(limit int, window time.Duration) Limiter {
	return authbase.NewRateLimiter(authbase.RateLimitOptions{
		Rate:  float64(limit) / window.Seconds(),
		Burst: limit,
	})
}

// ByClientIP keys a request by its client address: an IPv4 address on its
// own, an IPv6 address by its /64. It must run after
// authbase.TrustedProxies.Middleware, which replaces RemoteAddr with the
// real client behind TRUSTED_PROXY_CIDRS.
func ByClientIP(r *http.Request) string { return authbase.ClientIP(r) }

// BySubject keys a request by the signed-in person that user returns, and by
// client IP when there is none.
func BySubject(user func(*http.Request) (authbase.UserKey, bool)) func(*http.Request) string {
	return authbase.BySubject(user)
}

// Middleware refuses requests over the limit with 429 and Retry-After.
// keyOf returns the client key; an empty key is not limited. reject writes
// the error body.
func Middleware(l Limiter, keyOf func(*http.Request) string, reject func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyOf(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if ok, wait := l.Allow(key); !ok {
				secs := int((wait + time.Second - 1) / time.Second)
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
