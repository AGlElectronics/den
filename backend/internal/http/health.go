package http

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type healthHandler struct {
	pool      *pgxpool.Pool
	startedAt time.Time
	version   string
}

func newHealthHandler(pool *pgxpool.Pool, version string) *healthHandler {
	return &healthHandler{pool: pool, startedAt: time.Now(), version: version}
}

// livez is a liveness probe: it only reports whether the process is up.
// Kubernetes should restart the pod when this fails. It intentionally does
// NOT touch the database — a flaky database should not cause pod restarts.
func (h *healthHandler) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"version":    h.version,
		"uptime_sec": int64(time.Since(h.startedAt).Seconds()),
	})
}

// readyz is a readiness probe: the service reports it can serve traffic.
// Here we require Postgres to answer a ping. Returns 503 on failure so
// load balancers stop sending traffic to this replica.
func (h *healthHandler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "ok"
	status := http.StatusOK
	if h.pool != nil {
		if err := h.pool.Ping(ctx); err != nil {
			dbStatus = "down"
			status = http.StatusServiceUnavailable
		}
	} else {
		dbStatus = "not_configured"
		status = http.StatusServiceUnavailable
	}

	overall := "ok"
	if dbStatus != "ok" {
		overall = "degraded"
	}
	writeJSON(w, status, map[string]any{
		"status":   overall,
		"database": dbStatus,
		"version":  h.version,
	})
}

// health is the human-friendly /api/health response (kept for parity with
// Stash; the SPA may use it instead of /readyz when embedded).
func (h *healthHandler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "ok"
	if h.pool != nil {
		if err := h.pool.Ping(ctx); err != nil {
			dbStatus = "down"
		}
	} else {
		dbStatus = "not_configured"
	}

	overall := "ok"
	status := http.StatusOK
	if dbStatus != "ok" {
		overall = "degraded"
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, map[string]any{
		"status":     overall,
		"database":   dbStatus,
		"version":    h.version,
		"uptime_sec": int64(time.Since(h.startedAt).Seconds()),
	})
}
