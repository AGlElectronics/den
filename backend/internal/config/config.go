// Package config loads Den's environment-driven configuration and validates
// it. The server and the migrate binaries both call Load() so they fail
// fast with the same error shape.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/AGlElectronics/den/backend/internal/authbase"
	"github.com/AGlElectronics/den/backend/internal/groupmatch"
)

// AuthMode is AUTH_MODE, read and validated by the shared authbase package.
type AuthMode = authbase.AuthMode

const (
	AuthModeOIDC = authbase.AuthOIDC
	AuthModeDev  = authbase.AuthDev
)

// DefaultOIDCAudience is the audience Den's access tokens carry (the
// audience mapper on den-spa); OIDC_AUDIENCE overrides it.
const DefaultOIDCAudience = "den-api"

// Staff identity modes (IDENTITY_STAFF_MODE).
const (
	IdentityOwn      = "own"
	IdentityBrokered = "brokered"
	IdentityExisting = "existing"
)

// Config holds every setting the backend reads.
type Config struct {
	// AppEnv is APP_ENV, normalised (authbase.NormalizeAppEnv): trimmed,
	// lower-cased, production when unset. Only development relaxes rules.
	AppEnv      string
	AppBaseURL  string
	HTTPAddr    string
	DatabaseURL string

	// S3-compatible object storage. In dev this points at the SeaweedFS
	// container shipped in infra/docker-compose.yml. In production, point
	// at a real bucket; see docs/adr/0003-minio-dev-only.md.
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
	S3UsePathStyle    bool

	// AuthMode is AUTH_MODE: oidc (default) or dev, which needs
	// APP_ENV=development.
	AuthMode      AuthMode
	OIDCIssuerURL string
	// OIDCAudience must be in the access token's aud (OIDC_AUDIENCE,
	// default den-api). OIDC_CLIENT_ID is only for the web app's
	// /config.json; the backend does not read it.
	OIDCAudience string
	// GroupMatch is the shared group rule (OIDC_GROUPS_CLAIM,
	// OIDC_GROUP_MATCH, OIDC_GROUP_PREFIX).
	GroupMatch     groupmatch.Config
	DevDefaultUser string
	// AdminGroups are the IdP groups whose members are org admins.
	AdminGroups []string
	// MemberGroups are the IdP groups whose members may use the portal.
	// Empty means every signed-in user is a member.
	MemberGroups []string
	// OpsGroups are the IdP groups whose members may run backups, download
	// them and drop module databases (DEN_OPS_GROUPS).
	OpsGroups []string

	CORSAllowedOrigins []string
	// TrustedProxies lists the proxies whose X-Forwarded-For entries are
	// believed when working out the client IP (TRUSTED_PROXY_CIDRS, parsed
	// by authbase.FromEnv). The zero value trusts no proxy.
	TrustedProxies authbase.TrustedProxies

	// IdentityStaffMode is where staff accounts live (IDENTITY_STAFF_MODE):
	// own (our realm, default), brokered (our realm, linked to the
	// convention's IdP) or existing (the convention's realm, which Den never
	// changes).
	IdentityStaffMode string

	// Keycloak admin access for pushing the realm display name (used in
	// Keycloak's e-mails). Disabled unless KEYCLOAK_ADMIN_URL and
	// KEYCLOAK_CLIENT_SECRET are set, and always in IdentityStaffMode existing.
	KeycloakAdminURL     string
	KeycloakRealm        string
	KeycloakClientID     string
	KeycloakClientSecret string

	// CredentialKey derives module credentials so Den can present them when
	// it calls a module (DEN_CREDENTIAL_KEY, at least 32 bytes). Empty: Den
	// does not call modules (no portal to-dos).
	CredentialKey []byte

	// The internal operator (module databases + backups). Off when empty.
	OperatorURL   string
	OperatorToken string

	// PreregisterModules are registered as unpaired at startup
	// (DEN_PREREGISTER_MODULES). Existing modules are never changed.
	PreregisterModules []PreregisteredModule

	ShutdownTimeout time.Duration
}

// Load reads a .env (if present) and resolves every env var, returning a
// validated Config.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppEnv:             authbase.NormalizeAppEnv(os.Getenv(authbase.EnvAppEnv)),
		AppBaseURL:         getEnv("APP_BASE_URL", "http://localhost:8090"),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8090"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		S3Endpoint:         getEnv("S3_ENDPOINT", ""),
		S3Bucket:           getEnv("S3_BUCKET", ""),
		S3AccessKeyID:      getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey:  getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3Region:           getEnv("S3_REGION", "us-east-1"),
		S3UsePathStyle:     getEnvBool("S3_USE_PATH_STYLE", true),
		OIDCIssuerURL:      getEnv("OIDC_ISSUER_URL", ""),
		OIDCAudience:       getEnv("OIDC_AUDIENCE", DefaultOIDCAudience),
		DevDefaultUser:     getEnv("DEV_DEFAULT_USER", "dev-admin"),
		AdminGroups:        splitCSV(getEnv("DEN_ADMIN_GROUPS", "den-admin")),
		MemberGroups:       splitCSV(getEnv("DEN_MEMBER_GROUPS", "den-member")),
		OpsGroups:          splitCSV(getEnv("DEN_OPS_GROUPS", "den-ops")),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3001")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),

		IdentityStaffMode:    getEnv("IDENTITY_STAFF_MODE", IdentityOwn),
		KeycloakAdminURL:     getEnv("KEYCLOAK_ADMIN_URL", ""),
		KeycloakClientID:     getEnv("KEYCLOAK_CLIENT_ID", "den-keycloak"),
		KeycloakClientSecret: getEnv("KEYCLOAK_CLIENT_SECRET", ""),

		OperatorURL:   strings.TrimRight(getEnv("OPERATOR_URL", ""), "/"),
		OperatorToken: getEnv("OPERATOR_TOKEN", ""),
		CredentialKey: []byte(getEnv("DEN_CREDENTIAL_KEY", "")),
	}
	cfg.KeycloakRealm = getEnv("KEYCLOAK_REALM", realmFromIssuer(cfg.OIDCIssuerURL))

	authCfg, err := authbase.FromEnv(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	cfg.AuthMode = authCfg.AuthMode
	cfg.TrustedProxies = authCfg.TrustedProxies

	cfg.GroupMatch, err = groupmatch.FromEnv(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	prereg, err := ParsePreregisterModules(getEnv("DEN_PREREGISTER_MODULES", ""), cfg.IsDevelopment())
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	cfg.PreregisterModules = prereg

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Auth is the authbase view of AUTH_MODE, APP_ENV and TRUSTED_PROXY_CIDRS.
func (c *Config) Auth() authbase.Config {
	return authbase.Config{AppEnv: c.AppEnv, AuthMode: c.AuthMode, TrustedProxies: c.TrustedProxies}
}

// IsDevelopment reports APP_ENV=development, which allows http module
// addresses.
func (c *Config) IsDevelopment() bool { return c.Auth().Development() }

// DevAuth reports AUTH_MODE=dev together with APP_ENV=development.
func (c *Config) DevAuth() bool { return c.Auth().DevAuth() }

func (c *Config) validate() error {
	var errs []string
	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	// S3 config is required even in dev — the dev S3 server fills the slot. See
	// docs/adr/0003-minio-dev-only.md.
	if c.S3Endpoint == "" {
		errs = append(errs, "S3_ENDPOINT is required")
	}
	if c.S3Bucket == "" {
		errs = append(errs, "S3_BUCKET is required")
	}
	if c.S3AccessKeyID == "" {
		errs = append(errs, "S3_ACCESS_KEY_ID is required")
	}
	if c.S3SecretAccessKey == "" {
		errs = append(errs, "S3_SECRET_ACCESS_KEY is required")
	}
	switch c.IdentityStaffMode {
	case IdentityOwn, IdentityBrokered, IdentityExisting:
	default:
		errs = append(errs, fmt.Sprintf("IDENTITY_STAFF_MODE must be own, brokered or existing, got %q", c.IdentityStaffMode))
	}
	if n := len(c.CredentialKey); n > 0 && n < 32 {
		errs = append(errs, "DEN_CREDENTIAL_KEY must be at least 32 characters (for example: openssl rand -hex 32)")
	}
	if c.OperatorURL != "" && c.OperatorToken == "" {
		errs = append(errs, "OPERATOR_TOKEN is required when OPERATOR_URL is set")
	}
	if err := c.Auth().Validate(); err != nil {
		errs = append(errs, err.Error())
	} else if !c.DevAuth() {
		if c.OIDCIssuerURL == "" {
			errs = append(errs, "OIDC_ISSUER_URL is required when AUTH_MODE=oidc")
		}
		if strings.TrimSpace(c.OIDCAudience) == "" {
			errs = append(errs, "OIDC_AUDIENCE must not be empty when AUTH_MODE=oidc")
		}
	}
	if err := c.GroupMatch.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	// Fail at startup when the SMTP password is configured wrongly (both
	// sources set, or the file cannot be read). The value is not kept.
	if _, err := SMTPPassword(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New("invalid config: " + strings.Join(errs, "; "))
	}
	return nil
}

// MigrateConfig is a reduced subset used by cmd/migrate; it only needs the
// database URL. We expose a dedicated loader so the migrate binary doesn't
// require S3 env vars just to run migrations.
type MigrateConfig struct {
	DatabaseURL string
}

func LoadMigrate() (*MigrateConfig, error) {
	_ = godotenv.Load()
	dsn := getEnv("DATABASE_URL", "")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	return &MigrateConfig{DatabaseURL: dsn}, nil
}

// SMTPPassword is the shared SMTP password paired modules use to log in to
// the mail server. It is not stored in the database.
//
// DEN_SMTP_PASSWORD_FILE, when set, is read on every call so a mounted
// secret can be replaced without a restart. A trailing newline is removed
// (secret files often have one). Otherwise DEN_SMTP_PASSWORD is the
// process environment, which changes only when Den starts again. Empty
// means the mail server needs no login. Setting both is an error.
func SMTPPassword() (string, error) {
	file := strings.TrimSpace(os.Getenv("DEN_SMTP_PASSWORD_FILE"))
	env := os.Getenv("DEN_SMTP_PASSWORD")
	if file != "" && env != "" {
		return "", errors.New("set only one of DEN_SMTP_PASSWORD and DEN_SMTP_PASSWORD_FILE")
	}
	if file == "" {
		return env, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read DEN_SMTP_PASSWORD_FILE: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}

// realmFromIssuer extracts "conservices" from ".../realms/conservices".
func realmFromIssuer(issuer string) string {
	_, realm, ok := strings.Cut(strings.TrimRight(issuer, "/"), "/realms/")
	if !ok {
		return ""
	}
	return realm
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
