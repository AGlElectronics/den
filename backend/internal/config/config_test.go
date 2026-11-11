package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"DATABASE_URL": "postgres://x", "S3_ENDPOINT": "http://s3", "S3_BUCKET": "b",
		"S3_ACCESS_KEY_ID": "k", "S3_SECRET_ACCESS_KEY": "s", "AUTH_MODE": "dev", "APP_ENV": "development",
		"DEN_PREREGISTER_MODULES": "", "DEN_CREDENTIAL_KEY": "", "IDENTITY_STAFF_MODE": "", "OPERATOR_URL": "",
		"OPERATOR_TOKEN": "", "DEN_OPS_GROUPS": "", "OIDC_ISSUER_URL": "", "OIDC_CLIENT_ID": "", "KEYCLOAK_REALM": "",
		"OIDC_AUDIENCE": "", "OIDC_GROUPS_CLAIM": "", "OIDC_GROUP_MATCH": "", "OIDC_GROUP_PREFIX": "",
		"BACKUP_SAME_STORAGE_AS_DATA": "", "DEN_SMTP_PASSWORD": "", "DEN_SMTP_PASSWORD_FILE": "",
	} {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.OpsGroups, ",") != "den-ops" || strings.Join(cfg.AdminGroups, ",") != "den-admin" ||
		cfg.IdentityStaffMode != IdentityOwn || len(cfg.CredentialKey) != 0 || !cfg.IsDevelopment() {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"short credential key": {map[string]string{"DEN_CREDENTIAL_KEY": "short"}, "DEN_CREDENTIAL_KEY must be at least 32"},
		"bad identity mode":    {map[string]string{"IDENTITY_STAFF_MODE": "shared"}, "IDENTITY_STAFF_MODE must be own, brokered or existing"},
		"operator no token":    {map[string]string{"OPERATOR_URL": "http://op:8080"}, "OPERATOR_TOKEN is required"},
		"oidc no issuer":       {map[string]string{"AUTH_MODE": "oidc", "OIDC_CLIENT_ID": "den-spa"}, "OIDC_ISSUER_URL is required"},
		"oidc by default":      {map[string]string{"AUTH_MODE": ""}, "OIDC_ISSUER_URL is required"},
		"unknown auth mode":    {map[string]string{"AUTH_MODE": "magic"}, "AUTH_MODE must be"},
		"dev outside dev":      {map[string]string{"APP_ENV": "production"}, "AUTH_MODE=dev lets anyone sign in as anyone"},
		"dev without app env":  {map[string]string{"APP_ENV": ""}, "AUTH_MODE=dev lets anyone sign in as anyone"},
		"bad group match":      {map[string]string{"OIDC_GROUP_MATCH": "prefix"}, "OIDC_GROUP_MATCH must be"},
		"missing database":     {map[string]string{"DATABASE_URL": ""}, "DATABASE_URL is required"},
		"bad proxy cidr":       {map[string]string{"TRUSTED_PROXY_CIDRS": "nope"}, "TRUSTED_PROXY_CIDRS"},
		"smtp password both":   {map[string]string{"DEN_SMTP_PASSWORD": "s3cret", "DEN_SMTP_PASSWORD_FILE": "missing-file"}, "set only one of DEN_SMTP_PASSWORD and DEN_SMTP_PASSWORD_FILE"},
		"smtp password file":   {map[string]string{"DEN_SMTP_PASSWORD_FILE": "missing-smtp-password-file"}, "read DEN_SMTP_PASSWORD_FILE"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			baseEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoadAcceptsValidValues(t *testing.T) {
	baseEnv(t)
	t.Setenv("DEN_CREDENTIAL_KEY", strings.Repeat("k", 32))
	t.Setenv("IDENTITY_STAFF_MODE", IdentityExisting)
	t.Setenv("DEN_OPS_GROUPS", "ops-a, ops-b")
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/conservices/")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IdentityStaffMode != IdentityExisting || strings.Join(cfg.OpsGroups, "|") != "ops-a|ops-b" || cfg.KeycloakRealm != "conservices" {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestLoadOIDCDefaults(t *testing.T) {
	baseEnv(t)
	t.Setenv("AUTH_MODE", "")
	t.Setenv("APP_ENV", " Production ")
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/conservices")
	t.Setenv("OIDC_GROUP_MATCH", "LAST")
	t.Setenv("OIDC_GROUP_PREFIX", "/con2026")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMode != AuthModeOIDC || cfg.AppEnv != "production" || cfg.IsDevelopment() || cfg.DevAuth() {
		t.Fatalf("auth: mode %q env %q", cfg.AuthMode, cfg.AppEnv)
	}
	if cfg.OIDCAudience != "den-api" {
		t.Fatalf("audience %q", cfg.OIDCAudience)
	}
	if cfg.GroupMatch.ClaimName() != "groups" || !cfg.GroupMatch.Match([]string{"/con2026/teams/den-admin"}, "den-admin") {
		t.Fatalf("group rule %+v", cfg.GroupMatch)
	}
}

func TestAppEnvDefaultsToProduction(t *testing.T) {
	baseEnv(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/conservices")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "production" || cfg.IsDevelopment() {
		t.Fatalf("APP_ENV %q", cfg.AppEnv)
	}
}

func TestSMTPPassword(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		t.Setenv("DEN_SMTP_PASSWORD", "s3cret")
		t.Setenv("DEN_SMTP_PASSWORD_FILE", "")
		got, err := SMTPPassword()
		if err != nil || got != "s3cret" {
			t.Fatalf("got %q %v", got, err)
		}
	})
	t.Run("unset", func(t *testing.T) {
		t.Setenv("DEN_SMTP_PASSWORD", "")
		t.Setenv("DEN_SMTP_PASSWORD_FILE", "")
		got, err := SMTPPassword()
		if err != nil || got != "" {
			t.Fatalf("got %q %v", got, err)
		}
	})
	t.Run("file", func(t *testing.T) {
		t.Setenv("DEN_SMTP_PASSWORD", "")
		path := filepath.Join(t.TempDir(), "smtp")
		if err := os.WriteFile(path, []byte("file-secret\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DEN_SMTP_PASSWORD_FILE", path)
		got, err := SMTPPassword()
		if err != nil || got != "file-secret" {
			t.Fatalf("got %q %v", got, err)
		}
		// A replacement is visible on the next read.
		if err := os.WriteFile(path, []byte("rotated"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err = SMTPPassword()
		if err != nil || got != "rotated" {
			t.Fatalf("after replace: %q %v", got, err)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		t.Setenv("DEN_SMTP_PASSWORD", "")
		path := filepath.Join(t.TempDir(), "smtp")
		if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DEN_SMTP_PASSWORD_FILE", path)
		got, err := SMTPPassword()
		if err != nil || got != "" {
			t.Fatalf("got %q %v", got, err)
		}
	})
	t.Run("both", func(t *testing.T) {
		t.Setenv("DEN_SMTP_PASSWORD", "s3cret")
		t.Setenv("DEN_SMTP_PASSWORD_FILE", filepath.Join(t.TempDir(), "smtp"))
		got, err := SMTPPassword()
		if err == nil || got != "" || !strings.Contains(err.Error(), "set only one of DEN_SMTP_PASSWORD and DEN_SMTP_PASSWORD_FILE") || strings.Contains(err.Error(), "s3cret") {
			t.Fatalf("got %q %v", got, err)
		}
	})
}
