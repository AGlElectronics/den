package http_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AGlElectronics/den/backend/migrations"
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
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, map[string]any{"data": map[string]any{
		"retention": map[string]any{"inactive_users_days": -1},
		"smtp":      map[string]any{"host": "not a host", "port": 70000, "from": "nobody", "security": "ssl"},
		"finance":   map[string]any{"seller": map[string]any{"vat_id": "123", "country": "Austria", "email": "x", "website": "ftp://x"}},
		"stash":     map[string]any{"lost_found": map[string]any{"mode": "forever"}},
		"wiki":      map[string]any{"revision_retention_days": -5},
		"tasks":     map[string]any{"reminders": map[string]any{"due_soon_days": 31}},
		"rally":     map[string]any{"submission_site_host": "https://submit.example.org"},
		"identity":  map[string]any{"account_console_url": "ftp://x"},
	}}), http.StatusUnprocessableEntity).json(t, &bad)
	for _, k := range []string{"retention.inactive_users_days", "smtp.host", "smtp.port", "smtp.from", "smtp.security",
		"finance.seller.vat_id", "finance.seller.country", "finance.seller.email", "finance.seller.website",
		"stash.lost_found.mode", "wiki.revision_retention_days", "tasks.reminders.due_soon_days",
		"rally.submission_site_host", "identity.account_console_url"} {
		if bad.Error.Details[k] == "" {
			t.Errorf("missing error for %s: %v", k, bad.Error.Details)
		}
	}
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, map[string]any{"data": map[string]any{"unknown": 1}}), http.StatusBadRequest)
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, map[string]any{"retention": map[string]any{}}), http.StatusBadRequest)

	data := map[string]any{
		"retention": map[string]any{"offboarded_volunteers_days": 365, "module_data_days": 0},
		"smtp": map[string]any{"host": "SMTP.example.org", "from": "noreply@example.org", "from_name": "Example Con",
			"username": "noreply", "password": "s3cret"},
		"finance": map[string]any{"seller": map[string]any{"legal_name": "Example Con e.V.", "address": "Main St 1\n1010 Vienna",
			"country": "at", "vat_id": "atu 1234 5678", "email": "finance@example.org", "website": "https://example.org"}},
		"stash":    map[string]any{"lost_found": map[string]any{"mode": "deleted_after_closing", "days": 9}},
		"wiki":     map[string]any{"revision_retention_days": 365},
		"tasks":    map[string]any{"retention": map[string]any{"trash_days": 30}, "reminders": map[string]any{"due_soon_days": 1}},
		"rally":    map[string]any{"submission_site_host": "submit.example.org"},
		"identity": map[string]any{"account_console_url": "https://auth.example.org/realms/conservices/account", "sso_name": "Example SSO", "idp_admin_console_url": "https://auth.example.org/admin/master/console/#/conservices"},
	}
	doc := map[string]any{"data": data}
	var saved struct {
		Data struct {
			Retention map[string]any `json:"retention"`
			SMTP      map[string]any `json:"smtp"`
			Finance   struct {
				Seller map[string]any `json:"seller"`
			} `json:"finance"`
			Stash struct {
				LostFound map[string]any `json:"lost_found"`
			} `json:"stash"`
		} `json:"data"`
		UpdatedAt string `json:"updated_at"`
	}
	r := e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK)
	r.json(t, &saved)
	if strings.Contains(string(r.Body), "s3cret") {
		t.Fatal("the admin API must never return the SMTP password")
	}
	if saved.Data.SMTP["password_set"] != true || saved.Data.SMTP["host"] != "smtp.example.org" ||
		saved.Data.SMTP["port"] != float64(587) || saved.Data.SMTP["security"] != "starttls" ||
		saved.Data.Finance.Seller["vat_id"] != "ATU12345678" || saved.Data.Finance.Seller["country"] != "AT" ||
		saved.Data.Retention["module_data_days"] != float64(0) ||
		saved.Data.Stash.LostFound["days"] != nil || saved.Data.Stash.LostFound["hours"] != float64(48) || saved.UpdatedAt == "" {
		t.Fatalf("saved: %s", r.Body)
	}

	// Saving again without a password keeps it; a GET answer can be sent back.
	delete(data["smtp"].(map[string]any), "password")
	doc["updated_at"] = saved.UpdatedAt
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK).json(t, &saved)
	if saved.Data.SMTP["password_set"] != true {
		t.Fatal("password must be kept when not sent")
	}
	delete(doc, "updated_at")

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

	// Each module gets retention, smtp and only its own section, under data.
	stashID, _ := e.newModule("https://stash.example.test")
	if _, err := e.pool.Exec(context.Background(), `UPDATE modules SET slug = 'stash' WHERE id = $1`, stashID); err != nil {
		t.Fatal(err)
	}
	stash := e.pair(stashID, nil)
	var bundle struct {
		ModuleSettings struct {
			Data      map[string]any `json:"data"`
			UpdatedAt string         `json:"updated_at"`
		} `json:"module_settings"`
	}
	b := e.expect(e.do(http.MethodGet, "/api/v1/integrations/bundle", stash, nil), http.StatusOK)
	b.json(t, &bundle)
	ms := bundle.ModuleSettings.Data
	if ms["stash"] == nil || ms["finance"] != nil || ms["rally"] != nil || ms["wiki"] != nil || ms["identity"] != nil ||
		ms["retention"] == nil || bundle.ModuleSettings.UpdatedAt == "" {
		t.Fatalf("stash bundle sections: %s", b.Body)
	}
	if strings.Contains(string(b.Body), "s3cret") || ms["smtp"].(map[string]any)["password_set"] != true {
		t.Fatalf("the bundle carries password_set, never the password: %s", b.Body)
	}
	var secrets map[string]map[string]string
	sr := e.expect(e.do(http.MethodGet, "/api/v1/integrations/module-settings/secrets", stash, nil), http.StatusOK)
	sr.json(t, &secrets)
	if secrets["smtp"]["password"] != "s3cret" || sr.Hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("secrets: %s", sr.Body)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/module-settings/secrets", "", nil), http.StatusUnauthorized)

	finID, _ := e.newModule("https://finance.example.test")
	if _, err := e.pool.Exec(context.Background(), `UPDATE modules SET slug = 'finance' WHERE id = $1`, finID); err != nil {
		t.Fatal(err)
	}
	fin := e.pair(finID, nil)
	var direct struct {
		Data struct {
			Finance struct {
				Seller map[string]string `json:"seller"`
			} `json:"finance"`
			Stash any `json:"stash"`
		} `json:"data"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/integrations/module-settings", fin, nil), http.StatusOK).json(t, &direct)
	if direct.Data.Finance.Seller["legal_name"] != "Example Con e.V." || direct.Data.Finance.Seller["vat_id"] != "ATU12345678" || direct.Data.Stash != nil {
		t.Fatalf("finance module settings: %+v", direct.Data)
	}

	// Admins see what a module receives.
	var view struct {
		Data map[string]any `json:"data"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/modules/"+finID.String()+"/settings", admin, nil), http.StatusOK).json(t, &view)
	if view.Data["finance"] == nil || view.Data["stash"] != nil {
		t.Fatalf("module view: %v", view.Data)
	}
	e.expect(e.do(http.MethodGet, "/api/v1/modules/"+finID.String()+"/settings", memberTok(), nil), http.StatusForbidden)

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
	delete(data, "smtp")
	e.expect(e.do(http.MethodPut, "/api/v1/module-settings", admin, doc), http.StatusOK)
	var stored string
	_ = e.pool.QueryRow(context.Background(), `SELECT secrets::text FROM module_settings`).Scan(&stored)
	if strings.Contains(stored, "s3cret") {
		t.Fatal("password must be cleared with the SMTP section")
	}
	e.expect(e.do(http.MethodGet, "/api/v1/module-settings", memberTok(), nil), http.StatusForbidden)
}

// migrationUp returns the "-- +goose Up" part of a migration file.
func migrationUp(t *testing.T, name string) string {
	t.Helper()
	raw, err := migrations.FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	return up
}

func TestModuleSettingsMigrationKeepsOldValues(t *testing.T) {
	e := newEnv(t, nil)
	t.Cleanup(func() { _, _ = e.pool.Exec(context.Background(), `DELETE FROM module_settings`) })
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO module_settings (organization_id, data) VALUES ($1, $2)
		ON CONFLICT (organization_id) DO UPDATE SET data = EXCLUDED.data`, testOrgID,
		`{"finance":{"seller_name":"Old e.V.","vat_number":"ATU1"},"stash":{"lost_and_found":{"mode":"days_after_con","days":10}},"retention":{"pending_legal_review":true,"inactive_users_days":5}}`); err != nil {
		t.Fatal(err)
	}
	sql := migrationUp(t, "20261111000006_module_settings_contract.sql")
	if _, err := e.pool.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := e.pool.QueryRow(ctx, `SELECT data::text FROM module_settings WHERE organization_id = $1`, testOrgID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"seller": {"vat_id": "ATU1", "legal_name": "Old e.V."}`, `"lost_found": {"days": 10, "mode": "kept_after_con"}`, `"retention": {"inactive_users_days": 5}`} {
		if !strings.Contains(got, want) {
			t.Errorf("migrated data %s lacks %s", got, want)
		}
	}
}
