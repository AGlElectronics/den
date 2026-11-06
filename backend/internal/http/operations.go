package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// opsClient talks to the internal operator (module databases + backups).
type opsClient struct {
	base  string
	token string
	hc    *http.Client
}

func newOpsClient(base, token string) *opsClient {
	if base == "" {
		return nil
	}
	return &opsClient{base: base, token: token, hc: &http.Client{Timeout: 30 * time.Minute}}
}

// opsError is an error answer from the operator; its message is meant for
// admins.
type opsError struct {
	Status  int
	Message string
}

func (e *opsError) Error() string { return e.Message }

func (c *opsClient) do(ctx context.Context, method, p string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+p, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return &opsError{Status: http.StatusBadGateway, Message: "Can't reach the operator: " + err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return &opsError{Status: res.StatusCode, Message: e.Error}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c *opsClient) open(ctx context.Context, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/backups/download?key="+url.QueryEscape(key), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, &opsError{Status: http.StatusBadGateway, Message: "Can't reach the operator: " + err.Error()}
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, &opsError{Status: res.StatusCode, Message: "backup not available"}
	}
	return res, nil
}

func renderOps(w http.ResponseWriter, r *http.Request, err error) {
	if e, ok := err.(*opsError); ok {
		switch e.Status {
		case http.StatusBadRequest:
			httperror.Render(w, r, httperror.BadRequest(e.Message))
		case http.StatusNotFound:
			httperror.Render(w, r, httperror.NotFound("backup"))
		case http.StatusConflict, http.StatusForbidden:
			httperror.Render(w, r, httperror.Conflict(e.Message))
		default:
			httperror.Render(w, r, httperror.Unavailable(e.Message))
		}
		return
	}
	httperror.Render(w, r, httperror.Internal(err))
}

// operationsHandler is the Den admin "Backups" page API.
type operationsHandler struct {
	ops *opsClient

	mu    sync.Mutex
	links map[string]downloadLink
}

type downloadLink struct {
	key     string
	expires time.Time
}

func (h *operationsHandler) routes(r chi.Router) {
	r.Get("/status", h.status)
	r.Put("/settings", h.settings)
	r.Get("/backups", h.list)
	r.Post("/backups", h.backupNow)
	r.Post("/backups/download-link", h.downloadLink)
}

func (h *operationsHandler) status(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	var st map[string]any
	if err := h.ops.do(r.Context(), http.MethodGet, "/v1/status", nil, &st); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "reachable": false, "error": err.Error()})
		return
	}
	st["configured"], st["reachable"] = true, true
	writeJSON(w, http.StatusOK, st)
}

func (h *operationsHandler) settings(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	var in map[string]int
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	var out map[string]int
	if err := h.ops.do(r.Context(), http.MethodPut, "/v1/settings", in, &out); err != nil {
		renderOps(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *operationsHandler) list(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	var out []map[string]any
	if err := h.ops.do(r.Context(), http.MethodGet, "/v1/backups", nil, &out); err != nil {
		renderOps(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *operationsHandler) backupNow(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	if err := h.ops.do(r.Context(), http.MethodPost, "/v1/backups", nil, nil); err != nil {
		renderOps(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// downloadLink mints a one-time URL valid for five minutes so the browser
// can stream a (possibly large) backup without holding it in memory.
func (h *operationsHandler) downloadLink(w http.ResponseWriter, r *http.Request) {
	if h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("the operator is not set up"))
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	token := hex.EncodeToString(b)
	h.mu.Lock()
	if h.links == nil {
		h.links = map[string]downloadLink{}
	}
	for t, l := range h.links {
		if time.Now().After(l.expires) {
			delete(h.links, t)
		}
	}
	h.links[token] = downloadLink{key: in.Key, expires: time.Now().Add(5 * time.Minute)}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"url": "/api/v1/backup-downloads/" + token})
}

// download is public: the one-time token is the credential.
func (h *operationsHandler) download(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	h.mu.Lock()
	link, ok := h.links[token]
	delete(h.links, token)
	h.mu.Unlock()
	if !ok || time.Now().After(link.expires) || h.ops == nil {
		httperror.Render(w, r, httperror.NotFound("download link"))
		return
	}
	res, err := h.ops.open(r.Context(), link.key)
	if err != nil {
		renderOps(w, r, err)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", res.Header.Get("Content-Length"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(link.key)))
	_, _ = io.Copy(w, res.Body)
}
