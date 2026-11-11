package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemoryWindow(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	m := NewMemory(2, time.Minute)
	m.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if ok, _ := m.Allow("a"); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, wait := m.Allow("a")
	if ok || wait != time.Minute {
		t.Fatalf("third request: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := m.Allow("b"); !ok {
		t.Fatal("another key has its own budget")
	}
	now = now.Add(time.Minute)
	if ok, _ := m.Allow("a"); !ok {
		t.Fatal("a new window starts after a minute")
	}
}

func TestMiddleware(t *testing.T) {
	m := NewMemory(1, time.Minute)
	h := Middleware(m, func(r *http.Request) string { return r.RemoteAddr },
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
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second: %d %v", rec.Code, rec.Header())
	}
}
