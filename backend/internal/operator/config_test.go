package operator

import (
	"strings"
	"testing"

	"filippo.io/age"
)

func TestSameStorageAsData(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, flag, files, backup string
		want                      bool
	}{
		{"off by default", "", "https://s3.data.example.org", "https://s3.backup.example.net", false},
		{"flag set", "true", "https://s3.data.example.org", "https://s3.backup.example.net", true},
		{"same endpoint", "false", "https://S3.example.org/", "https://s3.example.org", true},
		{"no live storage", "", "", "https://s3.example.org", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPERATOR_TOKEN", "t")
			t.Setenv("PG_ADMIN_URL", "postgres://postgres:pw@db:5432/postgres")
			t.Setenv("BACKUP_ENABLED", "true")
			t.Setenv("BACKUP_S3_ENDPOINT", tc.backup)
			t.Setenv("BACKUP_S3_BUCKET", "backups")
			t.Setenv("BACKUP_S3_ACCESS_KEY_ID", "k")
			t.Setenv("BACKUP_S3_SECRET_ACCESS_KEY", "s")
			t.Setenv("BACKUP_AGE_RECIPIENTS", id.Recipient().String())
			t.Setenv("BACKUP_SAME_STORAGE_AS_DATA", tc.flag)
			t.Setenv("FILES_S3_ENDPOINT", tc.files)
			t.Setenv("FILES_S3_BUCKETS", "")
			c, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if c.SameStorageAsData != tc.want {
				t.Fatalf("SameStorageAsData = %v, want %v", c.SameStorageAsData, tc.want)
			}
		})
	}
}

func TestRestoreGuidePointsAtTheRunbook(t *testing.T) {
	if !strings.Contains(RestoreGuide, "/operations/databases-and-backups/#restore") || strings.Contains(RestoreGuide, "docs/operations.md") {
		t.Fatalf("restore hint %q", RestoreGuide)
	}
}

func TestModuleDBParams(t *testing.T) {
	cases := []struct {
		name, adminURL, params, want string
	}{
		{"copied from the admin URL", "postgres://postgres:pw@db:5432/postgres?sslmode=verify-full&sslrootcert=/ca/ca.crt", "", "sslmode=verify-full&sslrootcert=/ca/ca.crt"},
		{"plain when the admin URL has none", "postgres://postgres:pw@db:5432/postgres", "", "sslmode=disable"},
		{"explicit setting wins", "postgres://postgres:pw@db:5432/postgres?sslmode=disable", "sslmode=require", "sslmode=require"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPERATOR_TOKEN", "t")
			t.Setenv("BACKUP_ENABLED", "false")
			t.Setenv("PG_ADMIN_URL", tc.adminURL)
			t.Setenv("MODULE_DB_PARAMS", tc.params)
			c, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if c.ModuleDBParams != tc.want {
				t.Fatalf("got %q, want %q", c.ModuleDBParams, tc.want)
			}
		})
	}
}
