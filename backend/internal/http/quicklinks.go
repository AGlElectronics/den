package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// Personal Quick links on the portal home. Shown above the paired-module
// pills. URLs must point at Den or an enabled module base URL.
//
//	GET/PUT /api/v1/me/quick-links

const (
	maxQuickLinks      = 20
	maxQuickLinkLabel  = 80
	maxQuickLinkURLLen = 2000
)

type quickLink struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type quickLinksDTO struct {
	Links     []quickLink `json:"links"`
	UpdatedAt *string     `json:"updated_at"`
}

type quickLinksHandler struct {
	pool       *pgxpool.Pool
	appBaseURL string
	allowHTTP  bool
}

func (h *quickLinksHandler) getMine(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("not signed in"))
		return
	}
	out, err := h.load(r.Context(), u.Issuer, u.Subject)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *quickLinksHandler) putMine(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("not signed in"))
		return
	}
	var in struct {
		Links []quickLink `json:"links"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	allowed, err := h.allowedBases(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	cleaned, err := normalizeQuickLinks(in.Links, allowed, h.allowHTTP)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	out, err := h.save(r.Context(), u.Issuer, u.Subject, cleaned)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *quickLinksHandler) load(ctx context.Context, issuer, subject string) (*quickLinksDTO, error) {
	var (
		raw     []byte
		updated time.Time
	)
	err := h.pool.QueryRow(ctx,
		`SELECT links, updated_at FROM user_quick_links WHERE issuer = $1 AND subject = $2`,
		issuer, subject).Scan(&raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return &quickLinksDTO{Links: []quickLink{}}, nil
	}
	if err != nil {
		return nil, httperror.Internal(err)
	}
	links := []quickLink{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &links); err != nil {
			return nil, httperror.Internal(err)
		}
	}
	if links == nil {
		links = []quickLink{}
	}
	ts := updated.Format(time.RFC3339)
	return &quickLinksDTO{Links: links, UpdatedAt: &ts}, nil
}

func (h *quickLinksHandler) save(ctx context.Context, issuer, subject string, links []quickLink) (*quickLinksDTO, error) {
	raw, err := json.Marshal(links)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO user_quick_links (issuer, subject, links, updated_at)
		VALUES ($1, $2, $3::jsonb, now())
		ON CONFLICT (issuer, subject) DO UPDATE SET links = EXCLUDED.links, updated_at = now()
	`, issuer, subject, raw); err != nil {
		return nil, httperror.Internal(err)
	}
	return h.load(ctx, issuer, subject)
}

func (h *quickLinksHandler) allowedBases(ctx context.Context) ([]string, error) {
	bases := []string{}
	if b := normalizeBase(h.appBaseURL); b != "" {
		bases = append(bases, b)
	}
	rows, err := h.pool.Query(ctx, `
		SELECT base_url FROM modules
		 WHERE organization_id = $1 AND enabled = true AND coalesce(base_url, '') <> ''`,
		defaultOrgID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, httperror.Internal(err)
		}
		if b := normalizeBase(raw); b != "" {
			bases = append(bases, b)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, httperror.Internal(err)
	}
	return bases, nil
}

func normalizeQuickLinks(in []quickLink, allowed []string, allowHTTP bool) ([]quickLink, error) {
	if in == nil {
		in = []quickLink{}
	}
	if len(in) > maxQuickLinks {
		return nil, httperror.Validation("validation failed", map[string]string{
			"links": "at most 20 quick links",
		})
	}
	out := make([]quickLink, 0, len(in))
	seen := map[string]bool{}
	for _, link := range in {
		label := strings.TrimSpace(link.Label)
		rawURL := strings.TrimSpace(link.URL)
		id := strings.TrimSpace(link.ID)
		field := func(msg string) error {
			return httperror.Validation("validation failed", map[string]string{"links": msg})
		}
		if label == "" {
			return nil, field("label is required")
		}
		if len(label) > maxQuickLinkLabel {
			return nil, field("label is too long")
		}
		if rawURL == "" {
			return nil, field("url is required")
		}
		if len(rawURL) > maxQuickLinkURLLen {
			return nil, field("url is too long")
		}
		u, err := url.Parse(rawURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, field("url must be an absolute http(s) URL")
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "https" && (!allowHTTP || scheme != "http") {
			return nil, field("url must use https")
		}
		if u.User != nil || u.Fragment != "" {
			return nil, field("url must not include credentials or a fragment")
		}
		if !urlAllowed(rawURL, allowed) {
			return nil, field("url must point at Den or a paired Con Services module")
		}
		if id == "" {
			id = uuid.NewString()
		} else if _, err := uuid.Parse(id); err != nil {
			return nil, field("id must be a UUID")
		}
		if seen[id] {
			return nil, field("duplicate id")
		}
		seen[id] = true
		out = append(out, quickLink{ID: id, Label: label, URL: rawURL})
	}
	return out, nil
}

func normalizeBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.Path
}

func urlAllowed(raw string, allowed []string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	u.RawQuery = ""
	u.Fragment = ""
	cand := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/")
	for _, base := range allowed {
		if cand == base || strings.HasPrefix(cand, base+"/") {
			return true
		}
	}
	return false
}
