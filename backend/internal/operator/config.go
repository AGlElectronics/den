// Package operator is Den's internal operations service. It is the only
// component holding the Postgres admin login: it creates and drops module
// databases on Den's behalf and takes encrypted backups of the whole suite
// (every database on the shared server plus module file storage) to
// S3-compatible object storage. Only Den calls it, with a shared token, over
// the internal network. Docs:
// https://aglelectronics.github.io/con-services/operations/databases-and-backups/
package operator

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
)

// S3Target is one S3-compatible endpoint.
type S3Target struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	Region       string
	UsePathStyle bool
}

type Config struct {
	Addr  string
	Token string

	// PGAdminURL is a superuser (or equivalent) connection to the shared
	// server's maintenance database.
	PGAdminURL string
	// ModuleDBHost is the host:port modules use to reach the server; it is
	// put into the connection URL handed out for new databases.
	ModuleDBHost string
	// ModuleDBParams is the query string of the connection URL handed out
	// for new databases (TLS settings such as sslmode and sslrootcert).
	ModuleDBParams string
	// ManagedDatabases lets Den create and drop module databases. Off for
	// deployments where every service has its own database server.
	ManagedDatabases bool
	// Protected databases are never dropped (Den's own, Keycloak's, …).
	Protected map[string]bool

	BackupsEnabled bool
	Backup         S3Target
	BackupBucket   string
	Recipients     []age.Recipient
	RecipientText  []string
	Location       *time.Location
	// SameStorageAsData says the backup bucket is on the same S3 service as
	// the live data (BACKUP_SAME_STORAGE_AS_DATA). Den then warns: losing
	// that service loses the data and its backups together.
	SameStorageAsData bool

	// Files is the live module file storage; FileBuckets are copied into
	// every backup.
	Files       S3Target
	FileBuckets []string
}

func LoadConfig() (*Config, error) {
	c := &Config{
		Addr:             env("OPERATOR_ADDR", ":8080"),
		Token:            env("OPERATOR_TOKEN", ""),
		PGAdminURL:       env("PG_ADMIN_URL", ""),
		ModuleDBHost:     env("MODULE_DB_HOST", ""),
		ModuleDBParams:   env("MODULE_DB_PARAMS", ""),
		ManagedDatabases: envBool("MANAGED_DATABASES", true),
		Protected:        map[string]bool{"postgres": true, "template0": true, "template1": true},
		BackupsEnabled:   envBool("BACKUP_ENABLED", true),
		Backup: S3Target{
			Endpoint:     env("BACKUP_S3_ENDPOINT", ""),
			AccessKey:    env("BACKUP_S3_ACCESS_KEY_ID", ""),
			SecretKey:    env("BACKUP_S3_SECRET_ACCESS_KEY", ""),
			Region:       env("BACKUP_S3_REGION", "us-east-1"),
			UsePathStyle: envBool("BACKUP_S3_USE_PATH_STYLE", true),
		},
		BackupBucket:      env("BACKUP_S3_BUCKET", ""),
		SameStorageAsData: envBool("BACKUP_SAME_STORAGE_AS_DATA", false),
		Files: S3Target{
			Endpoint:     env("FILES_S3_ENDPOINT", ""),
			AccessKey:    env("FILES_S3_ACCESS_KEY_ID", ""),
			SecretKey:    env("FILES_S3_SECRET_ACCESS_KEY", ""),
			Region:       env("FILES_S3_REGION", "us-east-1"),
			UsePathStyle: envBool("FILES_S3_USE_PATH_STYLE", true),
		},
		FileBuckets: splitCSV(env("FILES_S3_BUCKETS", "")),
	}
	for _, d := range splitCSV(env("PROTECTED_DATABASES", "keycloak,den")) {
		c.Protected[d] = true
	}

	var errs []string
	if c.Token == "" {
		errs = append(errs, "OPERATOR_TOKEN is required")
	}
	if c.PGAdminURL == "" {
		errs = append(errs, "PG_ADMIN_URL is required")
	} else if u, err := url.Parse(c.PGAdminURL); err != nil {
		errs = append(errs, "PG_ADMIN_URL is not a URL")
	} else {
		if c.ModuleDBHost == "" {
			c.ModuleDBHost = u.Host
		}
		if c.ModuleDBParams == "" {
			c.ModuleDBParams = u.RawQuery
		}
	}
	if c.ModuleDBParams == "" {
		c.ModuleDBParams = "sslmode=disable"
	}
	if _, err := url.ParseQuery(c.ModuleDBParams); err != nil {
		errs = append(errs, fmt.Sprintf("MODULE_DB_PARAMS: %v", err))
	}

	loc, err := time.LoadLocation(env("BACKUP_TIMEZONE", "UTC"))
	if err != nil {
		errs = append(errs, fmt.Sprintf("BACKUP_TIMEZONE: %v", err))
	}
	c.Location = loc

	if c.BackupsEnabled {
		if c.Backup.Endpoint == "" || c.BackupBucket == "" || c.Backup.AccessKey == "" || c.Backup.SecretKey == "" {
			errs = append(errs, "BACKUP_S3_ENDPOINT, BACKUP_S3_BUCKET, BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY are required when BACKUP_ENABLED=true")
		}
		c.RecipientText = splitCSV(env("BACKUP_AGE_RECIPIENTS", ""))
		if len(c.RecipientText) == 0 {
			errs = append(errs, "BACKUP_AGE_RECIPIENTS (age public keys) is required when BACKUP_ENABLED=true")
		}
		for _, r := range c.RecipientText {
			rec, err := age.ParseX25519Recipient(r)
			if err != nil {
				errs = append(errs, fmt.Sprintf("BACKUP_AGE_RECIPIENTS: %v", err))
				continue
			}
			c.Recipients = append(c.Recipients, rec)
		}
		if len(c.FileBuckets) > 0 && c.Files.Endpoint == "" {
			errs = append(errs, "FILES_S3_ENDPOINT is required when FILES_S3_BUCKETS is set")
		}
		if c.Files.Endpoint != "" && sameEndpoint(c.Files.Endpoint, c.Backup.Endpoint) {
			c.SameStorageAsData = true
		}
	}
	if len(errs) > 0 {
		return nil, errors.New("invalid operator config: " + strings.Join(errs, "; "))
	}
	return c, nil
}

// sameEndpoint compares two S3 endpoints by host and port, ignoring the
// scheme, case and a trailing slash.
func sameEndpoint(a, b string) bool {
	host := func(s string) string {
		s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			return u.Host
		}
		return s
	}
	return a != "" && b != "" && host(a) == host(b)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
