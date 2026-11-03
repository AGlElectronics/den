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

	// S3-compatible object storage. In dev this points at the MinIO
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

	CORSAllowedOrigins []string

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
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3001")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

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
	// S3 config is required even in dev — MinIO fills the slot. See
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
