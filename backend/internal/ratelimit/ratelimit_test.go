package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AGlElectronics/den/backend/internal/authbase"
)

func TestNewAllowsBurstThenRefuses(t *testing.T) {
	l := New(2, time.Minute)
	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 || wait > 30*time.Second {
		t.Fatalf("third request: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another key has its own budget")
	}
}

func TestByClientIPGroupsIPv6Per64(t *testing.T) {
	a := httptest.NewRequest(http.MethodGet, "/", nil)
	a.RemoteAddr = "2001:db8:1:2::1"
	b := httptest.NewRequest(http.MethodGet, "/", nil)
	b.RemoteAddr = "[2001:db8:1:2:ffff::9]:443"
	c := httptest.NewRequest(http.MethodGet, "/", nil)
	c.RemoteAddr = "203.0.113.7"
	if ByClientIP(a) != ByClientIP(b) {
		t.Fatalf("same /64 must share a key: %q %q", ByClientIP(a), ByClientIP(b))
	}
	if ByClientIP(c) != "ip:203.0.113.7" {
		t.Fatalf("ipv4 key %q", ByClientIP(c))
	}
}

func TestBySubject(t *testing.T) {
	key := BySubject(func(r *http.Request) (authbase.UserKey, bool) {
		if s := r.Header.Get("X-Test-Sub"); s != "" {
			return authbase.UserKey{Issuer: "https://auth.test", Subject: s}, true
		}
		return authbase.UserKey{}, false
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:1234"
	if key(r) != "ip:203.0.113.7" {
		t.Fatalf("anonymous key %q", key(r))
	}
	r.Header.Set("X-Test-Sub", "alice")
	if key(r) != "sub:https://auth.test|alice" {
		t.Fatalf("subject key %q", key(r))
	}
}

func TestMiddleware(t *testing.T) {
	l := New(1, time.Minute)
	h := Middleware(l, func(r *http.Request) string { return r.RemoteAddr },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("second: %d %v", rec.Code, rec.Header())
	}
}
