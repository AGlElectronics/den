// Package ratelimit limits how often one client may call a route. Den uses
// it on its few unauthenticated routes (pairing redeem, public brand, backup
// downloads). The Limiter interface is the seam for the suite-wide limiter
// from shared/go; Memory is a minimal per-process implementation.
package ratelimit

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter decides whether one more request for key is allowed. When it is
// not, retryAfter says how long the client should wait.
type Limiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// Memory is a fixed-window limiter kept in process memory: at most Limit
// requests per key in each Window. With several replicas each counts on its
// own, so the effective limit is Limit times the number of replicas.
type Memory struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	start time.Time
	count int
}

// NewMemory allows limit requests per key per window.
func NewMemory(limit int, window time.Duration) *Memory {
	return &Memory{limit: limit, window: window, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow implements Limiter.
func (m *Memory) Allow(key string) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if now.Sub(m.swept) > m.window {
		for k, b := range m.buckets {
			if now.Sub(b.start) >= m.window {
				delete(m.buckets, k)
			}
		}
		m.swept = now
	}
	b, ok := m.buckets[key]
	if !ok || now.Sub(b.start) >= m.window {
		m.buckets[key] = &bucket{start: now, count: 1}
		return true, 0
	}
	if b.count >= m.limit {
		return false, b.start.Add(m.window).Sub(now)
	}
	b.count++
	return true, 0
}

// Middleware refuses requests over the limit with 429 and Retry-After.
// keyOf returns the client key (usually the client IP); reject writes the
// error body.
func Middleware(l Limiter, keyOf func(*http.Request) string, reject func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, wait := l.Allow(keyOf(r)); !ok {
				secs := int(wait.Round(time.Second) / time.Second)
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
