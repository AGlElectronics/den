package http_test

import (
	"net/http"
	"testing"
	"time"

	denhttp "github.com/AGlElectronics/den/backend/internal/http"
	"github.com/AGlElectronics/den/backend/internal/ratelimit"
)

func TestPublicRoutesAreRateLimited(t *testing.T) {
	e := newEnv(t, nil)
	for i := 0; i < 10; i++ {
		e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token=x", "", nil), http.StatusUnauthorized)
	}
	r := e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token=x", "", nil), http.StatusTooManyRequests)
	if r.Hdr.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}
	// A forged X-Forwarded-For from an untrusted peer does not reset the budget.
	e.expect(e.do(http.MethodPost, "/api/v1/integrations/pairing/redeem?token=x", "", nil, "X-Forwarded-For", "203.0.113.9"), http.StatusTooManyRequests)

	for i := 0; i < 10; i++ {
		e.expect(e.do(http.MethodGet, "/api/v1/backup-downloads/nope", "", nil), http.StatusNotFound)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/backup-downloads/nope", "", nil), http.StatusTooManyRequests)
}

type denyAll struct{}

func (denyAll) Allow(string) (bool, time.Duration) { return false, 30 * time.Second }

func TestLimiterCanBeReplaced(t *testing.T) {
	routes := map[string]bool{}
	e := newEnv(t, nil, func(d *denhttp.Deps) {
		d.NewLimiter = func(route string, _ int, _ time.Duration) ratelimit.Limiter {
			routes[route] = true
			return denyAll{}
		}
	})
	r := e.expect(e.do(http.MethodGet, "/api/v1/public/brand", "", nil), http.StatusTooManyRequests)
	if r.Hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After %q", r.Hdr.Get("Retry-After"))
	}
	for _, want := range []string{"pairing-redeem", "public-brand", "backup-download", "integration-modules"} {
		if !routes[want] {
			t.Errorf("limiter for %s not requested", want)
		}
	}
}
