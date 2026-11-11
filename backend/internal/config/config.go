// Package config loads Den's environment-driven configuration and validates
// it. The server and the migrate binaries both call Load() so they fail
// fast with the same error shape.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/AGlElectronics/den/backend/internal/clientip"
)

type AuthMode string

const (
	AuthModeOIDC AuthMode = "oidc"
	AuthModeDev  AuthMode = "dev"
)

// Config holds every setting the backend reads.
type Config struct {
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

	AuthMode       AuthMode
	OIDCIssuerURL  string
	OIDCClientID   string
	OIDCAudience   string
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
	// TrustedProxyCIDRs lists the proxies whose X-Forwarded-For entries are
	// believed when working out the client IP (TRUSTED_PROXY_CIDRS).
	TrustedProxyCIDRs []netip.Prefix

	// Keycloak admin access for pushing login branding into the realm.
	// Disabled unless KEYCLOAK_ADMIN_URL and KEYCLOAK_CLIENT_SECRET are set.
	KeycloakAdminURL     string
	KeycloakRealm        string
	KeycloakClientID     string
	KeycloakClientSecret string

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
		AppEnv:             getEnv("APP_ENV", "development"),
		AppBaseURL:         getEnv("APP_BASE_URL", "http://localhost:8090"),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8090"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		S3Endpoint:         getEnv("S3_ENDPOINT", ""),
		S3Bucket:           getEnv("S3_BUCKET", ""),
		S3AccessKeyID:      getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey:  getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3Region:           getEnv("S3_REGION", "us-east-1"),
		S3UsePathStyle:     getEnvBool("S3_USE_PATH_STYLE", true),
		AuthMode:           AuthMode(getEnv("AUTH_MODE", "dev")),
		OIDCIssuerURL:      getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:       getEnv("OIDC_CLIENT_ID", ""),
		OIDCAudience:       getEnv("OIDC_AUDIENCE", ""),
		DevDefaultUser:     getEnv("DEV_DEFAULT_USER", "dev-admin"),
		AdminGroups:        splitCSV(getEnv("DEN_ADMIN_GROUPS", "den-admin")),
		MemberGroups:       splitCSV(getEnv("DEN_MEMBER_GROUPS", "den-member")),
		OpsGroups:          splitCSV(getEnv("DEN_OPS_GROUPS", "den-ops")),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3001")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),

		KeycloakAdminURL:     getEnv("KEYCLOAK_ADMIN_URL", ""),
		KeycloakClientID:     getEnv("KEYCLOAK_CLIENT_ID", "den-keycloak"),
		KeycloakClientSecret: getEnv("KEYCLOAK_CLIENT_SECRET", ""),

		OperatorURL:   strings.TrimRight(getEnv("OPERATOR_URL", ""), "/"),
		OperatorToken: getEnv("OPERATOR_TOKEN", ""),
	}
	cfg.KeycloakRealm = getEnv("KEYCLOAK_REALM", realmFromIssuer(cfg.OIDCIssuerURL))

	trusted, err := clientip.ParseCIDRs(getEnv("TRUSTED_PROXY_CIDRS", clientip.DefaultTrustedProxies))
	if err != nil {
		return nil, fmt.Errorf("invalid config: TRUSTED_PROXY_CIDRS: %w", err)
	}
	cfg.TrustedProxyCIDRs = trusted

	prereg, err := ParsePreregisterModules(getEnv("DEN_PREREGISTER_MODULES", ""))
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	cfg.PreregisterModules = prereg

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

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
	if c.OperatorURL != "" && c.OperatorToken == "" {
		errs = append(errs, "OPERATOR_TOKEN is required when OPERATOR_URL is set")
	}
	switch c.AuthMode {
	case AuthModeOIDC:
		if c.OIDCIssuerURL == "" {
			errs = append(errs, "OIDC_ISSUER_URL is required when AUTH_MODE=oidc")
		}
		if c.OIDCClientID == "" {
			errs = append(errs, "OIDC_CLIENT_ID is required when AUTH_MODE=oidc")
		}
	case AuthModeDev:
		// no extra required fields
	default:
		errs = append(errs, fmt.Sprintf("AUTH_MODE must be 'oidc' or 'dev', got %q", c.AuthMode))
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
