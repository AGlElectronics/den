package http_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AGlElectronics/den/backend/internal/config"
)

func countAudit(e *testEnv, action string, since time.Time) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = $1 AND created_at >= $2`, action, since).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestOperationsAuditAndDownloadLinks(t *testing.T) {
	op := &fakeOperator{}
	cfgFn := withOperator(t, op)
	e := newEnv(t, cfgFn)
	// A second router on the same database stands for a second Den replica.
	other := newEnv(t, func(c *config.Config) { c.OperatorURL, c.OperatorToken = e.cfg.OperatorURL, e.cfg.OperatorToken })
	ops := opsTok()
	start := time.Now().Add(-time.Second)

	e.expect(e.do(http.MethodPost, "/api/v1/operations/backups", ops, nil), http.StatusAccepted)
	e.expect(e.do(http.MethodPut, "/api/v1/operations/settings", ops, map[string]int{"hour": 4, "keep_daily": 7, "keep_weekly": 4, "keep_monthly": 6}), http.StatusOK)
	if countAudit(e, "operations.backup.start", start) != 1 || countAudit(e, "operations.schedule.update", start) != 1 {
		t.Fatal("backup start and schedule change must be audited")
	}

	e.expect(e.do(http.MethodPost, "/api/v1/operations/backups/download-link", ops, map[string]string{"key": ""}), http.StatusUnprocessableEntity)
	var link struct {
		URL string `json:"url"`
	}
	key := "backups/20261111T030000Z-scheduled.tar.age"
	e.expect(e.do(http.MethodPost, "/api/v1/operations/backups/download-link", ops, map[string]string{"key": key}), http.StatusOK).json(t, &link)
	if !strings.HasPrefix(link.URL, "/api/v1/backup-downloads/") {
		t.Fatalf("url %q", link.URL)
	}
	r := other.expect(other.do(http.MethodGet, link.URL, "", nil), http.StatusOK)
	if string(r.Body) != "age" || !strings.Contains(r.Hdr.Get("Content-Disposition"), "20261111T030000Z-scheduled.tar.age") {
		t.Fatalf("download: %q %v", r.Body, r.Hdr)
	}
	e.expect(e.do(http.MethodGet, link.URL, "", nil), http.StatusNotFound)
	if countAudit(e, "operations.download_link.create", start) != 1 || countAudit(e, "operations.backup.download", start) != 1 {
		t.Fatal("link creation and download must be audited")
	}

	e.expect(e.do(http.MethodPost, "/api/v1/operations/backups/download-link", ops, map[string]string{"key": key}), http.StatusOK).json(t, &link)
	if _, err := e.pool.Exec(context.Background(), `UPDATE backup_download_links SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do(http.MethodGet, link.URL, "", nil), http.StatusNotFound)
	e.expect(e.do(http.MethodGet, "/api/v1/backup-downloads/not-a-token", "", nil), http.StatusNotFound)
}

func TestOrgSettingsAuditAndValidation(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	start := time.Now().Add(-time.Second)
	e.expect(e.do(http.MethodPut, "/api/v1/org-settings", admin, map[string]any{"org_email_domain": "not a domain"}), http.StatusUnprocessableEntity)
	e.expect(e.do(http.MethodPut, "/api/v1/org-settings", admin, map[string]any{"organization_name": "Before Con"}), http.StatusOK)
	var s struct {
		Data map[string]any `json:"data"`
	}
	e.expect(e.do(http.MethodPut, "/api/v1/org-settings", admin, map[string]any{"organization_name": " Example Con ", "org_email_domain": "@Example.ORG"}), http.StatusOK).json(t, &s)
	if s.Data["organization_name"] != "Example Con" || s.Data["org_email_domain"] != "example.org" {
		t.Fatalf("normalised settings: %v", s.Data)
	}
	var page struct {
		Items []struct {
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		} `json:"items"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?action=org_settings.update&limit=1&since="+start.UTC().Format(time.RFC3339), admin, nil), http.StatusOK).json(t, &page)
	if len(page.Items) != 1 || page.Items[0].Before["organization_name"] != "Before Con" || page.Items[0].After["organization_name"] != "Example Con" {
		t.Fatalf("org settings audit: %+v", page.Items)
	}
	e.expect(e.do(http.MethodPut, "/api/v1/org-settings", memberTok(), map[string]any{}), http.StatusForbidden)

	id, _ := e.newModule("https://settings.example.test")
	cred := e.pair(id, nil)
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/settings", cred, nil), http.StatusOK).json(t, &s)
	if s.Data["organization_name"] != "Example Con" {
		t.Fatalf("integration settings: %v", s.Data)
	}
}
