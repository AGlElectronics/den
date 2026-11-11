package http_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sync"
	"testing"

	"github.com/google/uuid"

	denhttp "github.com/AGlElectronics/den/backend/internal/http"
)

type memStore struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *memStore) PutObject(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objs == nil {
		m.objs = map[string][]byte{}
	}
	m.objs[key] = b
	return nil
}

func (m *memStore) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStore) Bucket() string { return "test-bucket" }

func withStore(s *memStore) func(*denhttp.Deps) { return func(d *denhttp.Deps) { d.Files = s } }

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func (e *testEnv) upload(token, role, filename, contentType string, data []byte) resp {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("role", role)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/brand/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return resp{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

func (e *testEnv) cleanupAssets() {
	e.t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM branding_assets WHERE storage_bucket = 'test-bucket'`)
	})
}

func TestBrandingAssetWhitelist(t *testing.T) {
	store := &memStore{}
	e := newEnv(t, nil, withStore(store))
	e.cleanupAssets()
	admin := adminTok()

	e.expect(e.upload(admin, "banner", "a.png", "image/png", pngBytes), http.StatusUnprocessableEntity)
	e.expect(e.upload(admin, "logo", "a.png", "image/png", []byte("<html><script>alert(1)</script></html>")), http.StatusUnprocessableEntity)
	e.expect(e.upload(admin, "logo", "a.html", "text/html", []byte("<html><body>hi</body></html>")), http.StatusUnprocessableEntity)
	e.expect(e.upload(memberTok(), "logo", "a.png", "image/png", pngBytes), http.StatusForbidden)

	// The client says text/plain, the bytes are a PNG: Den stores image/png.
	var created struct {
		ID string `json:"id"`
	}
	e.expect(e.upload(admin, "logo", "logo.bin", "text/plain", pngBytes), http.StatusCreated).json(t, &created)
	r := e.expect(e.do(http.MethodGet, "/api/v1/public/brand/assets/logo", "", nil), http.StatusOK)
	if r.Hdr.Get("Content-Type") != "image/png" || r.Hdr.Get("X-Content-Type-Options") != "nosniff" || r.Hdr.Get("Content-Disposition") != "" {
		t.Fatalf("png headers: %v", r.Hdr)
	}
	if !bytes.Equal(r.Body, pngBytes) {
		t.Fatal("served bytes differ from the upload")
	}
	if e.auditCount("branding.asset.create", uuid.MustParse(created.ID)) != 1 {
		t.Fatal("missing branding.asset.create audit event")
	}

	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	e.expect(e.upload(admin, "favicon", "icon.svg", "image/svg+xml", svg), http.StatusCreated)
	r = e.expect(e.do(http.MethodGet, "/api/v1/public/brand/assets/favicon", "", nil), http.StatusOK)
	if r.Hdr.Get("Content-Type") != "image/svg+xml" || r.Hdr.Get("Content-Disposition") != "attachment" ||
		r.Hdr.Get("X-Content-Type-Options") != "nosniff" || r.Hdr.Get("Content-Security-Policy") == "" {
		t.Fatalf("svg headers: %v", r.Hdr)
	}

	e.expect(e.do(http.MethodGet, "/api/v1/public/brand/assets/banner", "", nil), http.StatusNotFound)

	e.expect(e.do(http.MethodDelete, "/api/v1/brand/assets/"+created.ID, admin, nil), http.StatusNoContent)
	if e.auditCount("branding.asset.delete", uuid.MustParse(created.ID)) != 1 {
		t.Fatal("missing branding.asset.delete audit event")
	}
	e.expect(e.do(http.MethodDelete, "/api/v1/brand/assets/"+created.ID, admin, nil), http.StatusNotFound)
}

func TestBrandingProfileAudit(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	var res struct {
		Revision int `json:"revision"`
	}
	e.expect(e.do(http.MethodPut, "/api/v1/brand/profile", admin, map[string]any{
		"theme": map[string]any{"primary": "#123456"}, "copy": map[string]any{"tagline": "Hi"},
	}), http.StatusCreated).json(t, &res)
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(),
		`SELECT id FROM branding_profiles WHERE organization_id = $1 AND revision = $2`, testOrgID, res.Revision).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if e.auditCount("branding.profile.update", id) != 1 {
		t.Fatal("missing branding.profile.update audit event")
	}
	e.expect(e.do(http.MethodPut, "/api/v1/brand/profile", memberTok(), map[string]any{}), http.StatusForbidden)
}
