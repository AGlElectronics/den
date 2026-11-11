package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

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
