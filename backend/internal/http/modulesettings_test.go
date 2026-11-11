package http_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestModuleSettings(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	t.Cleanup(func() { _, _ = e.pool.Exec(context.Background(), `DELETE FROM module_settings`) })

	var bad struct {
		Error struct {
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, map[string]any{
		"retention": map[string]any{"inactive_users_days": 0},
		"smtp":      map[string]any{"host": "not a host", "port": 70000, "from": "nobody"},
		"finance":   map[string]any{"vat_number": "123"},
		"stash":     map[string]any{"lost_and_found": map[string]any{"mode": "forever"}},
		"rally":     map[string]any{"submission_site_host": "https://submit.example.org"},
		"identity":  map[string]any{"account_console_url": "ftp://x"},
	}), http.StatusUnprocessableEntity).json(t, &bad)
	for _, k := range []string{"retention.inactive_users_days", "smtp.host", "smtp.port", "smtp.from", "finance.vat_number",
		"stash.lost_and_found.mode", "rally.submission_site_host", "identity.account_console_url"} {
		if bad.Error.Details[k] == "" {
			t.Errorf("missing error for %s: %v", k, bad.Error.Details)
		}
	}
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, map[string]any{"unknown": 1}), http.StatusBadRequest)

	doc := map[string]any{
		"retention": map[string]any{"offboarded_volunteers_days": 365, "backup_note": "Backups are kept for 6 months."},
		"smtp":      map[string]any{"host": "SMTP.example.org", "port": 587, "from": "Example Con <noreply@example.org>", "username": "noreply", "password": "s3cret"},
		"finance":   map[string]any{"seller_name": "Example Con e.V.", "seller_address": "Main St 1, Utrecht", "vat_number": "nl 1234.567.89 B01"},
		"stash":     map[string]any{"lost_and_found": map[string]any{"mode": "hours_after_closing", "hours": 48, "days": 9}},
		"rally":     map[string]any{"submission_site_host": "submit.example.org"},
		"identity": map[string]any{"account_console_url": "https://auth.example.org/realms/conservices/account", "sso_name": "Example SSO",
			"idp_admin_console_url": "https://auth.example.org/admin/master/console/#/conservices"},
	}
	var saved struct {
		Data struct {
			Retention map[string]any `json:"retention"`
			SMTP      map[string]any `json:"smtp"`
			Finance   map[string]any `json:"finance"`
			Stash     struct {
				LostAndFound map[string]any `json:"lost_and_found"`
			} `json:"stash"`
		} `json:"data"`
	}
	r := e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK)
	r.json(t, &saved)
	if strings.Contains(string(r.Body), "s3cret") {
		t.Fatal("the admin API must never return the SMTP password")
	}
	if saved.Data.SMTP["password_set"] != true || saved.Data.SMTP["host"] != "smtp.example.org" ||
		saved.Data.Retention["pending_legal_review"] != true || saved.Data.Finance["vat_number"] != "NL123456789B01" ||
		saved.Data.Stash.LostAndFound["days"] != nil {
		t.Fatalf("saved: %+v", saved.Data)
	}

	// Saving again without a password keeps it.
	delete(doc["smtp"].(map[string]any), "password")
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK).json(t, &saved)
	if saved.Data.SMTP["password_set"] != true {
		t.Fatal("password must be kept when not sent")
	}

	var page struct {
		Items []struct {
			Note string `json:"note"`
		} `json:"items"`
	}
	raw := e.expect(e.do(http.MethodGet, "/api/v1/audit-events?action=module_settings.update&since="+time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), admin, nil), http.StatusOK)
	raw.json(t, &page)
	if len(page.Items) < 2 || strings.Contains(string(raw.Body), "s3cret") || page.Items[len(page.Items)-1].Note != "SMTP password set" {
		t.Fatalf("audit: %s", raw.Body)
	}

	// Each module gets the shared sections and only its own section.
	stashID, _ := e.newModule("https://stash.example.test")
	if _, err := e.pool.Exec(context.Background(), `UPDATE modules SET slug = 'stash' WHERE id = $1`, stashID); err != nil {
		t.Fatal(err)
	}
	stash := e.pair(stashID, nil)
	var bundle struct {
		ModuleSettings map[string]any `json:"module_settings"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/bundle", stash, nil), http.StatusOK).json(t, &bundle)
	ms := bundle.ModuleSettings
	if ms["stash"] == nil || ms["finance"] != nil || ms["rally"] != nil || ms["retention"] == nil || ms["identity"] == nil {
		t.Fatalf("stash bundle sections: %v", ms)
	}
	if ms["smtp"].(map[string]any)["password"] != "s3cret" {
		t.Fatal("paired modules receive the SMTP password")
	}
	other, _ := e.newModule("https://other.example.test")
	cred := e.pair(other, nil)
	var direct map[string]any
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/module-settings", cred, nil), http.StatusOK).json(t, &direct)
	if direct["stash"] != nil || direct["finance"] != nil {
		t.Fatalf("a module without its own section: %v", direct)
	}

	// Identity links on the portal: the IdP admin console only for admins.
	var d struct {
		IdentityLinks map[string]string `json:"identity_links"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/dashboard", memberTok(), nil), http.StatusOK).json(t, &d)
	if d.IdentityLinks["account_console_url"] == "" || d.IdentityLinks["idp_admin_console_url"] != "" {
		t.Fatalf("member identity links: %v", d.IdentityLinks)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/dashboard", admin, nil), http.StatusOK).json(t, &d)
	if d.IdentityLinks["idp_admin_console_url"] == "" {
		t.Fatalf("admin identity links: %v", d.IdentityLinks)
	}

	// Removing the SMTP section clears the password.
	delete(doc, "smtp")
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK)
	var secrets string
	_ = e.pool.QueryRow(context.Background(), `SELECT secrets::text FROM module_settings`).Scan(&secrets)
	if strings.Contains(secrets, "s3cret") {
		t.Fatal("password must be cleared with the SMTP section")
	}
	e.expect(e.do(http.MethodGet, "/api/v1/module-settings", memberTok(), nil), http.StatusForbidden)
}
