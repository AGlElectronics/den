package operator

import "testing"

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
