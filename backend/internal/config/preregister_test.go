package config

import (
	"strings"
	"testing"
)

func TestParsePreregisterModulesEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "[]"} {
		mods, err := ParsePreregisterModules(raw)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", raw, err)
		}
		if len(mods) != 0 {
			t.Fatalf("%q: want no modules, got %v", raw, mods)
		}
	}
}

func TestParsePreregisterModulesValid(t *testing.T) {
	raw := `[
		{"slug":"rally","display_name":" Rally ","base_url":"https://rally.example.org"},
		{"slug":"stash","display_name":"Stash","base_url":"http://stash:8080","environment":"staging"}
	]`
	mods, err := ParsePreregisterModules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []PreregisteredModule{
		{Slug: "rally", DisplayName: "Rally", BaseURL: "https://rally.example.org", Environment: "production"},
		{Slug: "stash", DisplayName: "Stash", BaseURL: "http://stash:8080", Environment: "staging"},
	}
	if len(mods) != len(want) {
		t.Fatalf("want %d modules, got %d", len(want), len(mods))
	}
	for i := range want {
		if mods[i] != want[i] {
			t.Errorf("entry %d: want %+v, got %+v", i, want[i], mods[i])
		}
	}
}

func TestParsePreregisterModulesInvalid(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want string
	}{
		"not json":         {`rally`, "JSON array"},
		"object not array": {`{"slug":"rally"}`, "JSON array"},
		"unknown field":    {`[{"slug":"rally","display_name":"Rally","base_url":"http://r","create_database":true}]`, "unknown field"},
		"trailing data":    {`[] []`, "after the JSON array"},
		"missing slug":     {`[{"display_name":"Rally","base_url":"http://r"}]`, "slug is required"},
		"bad slug":         {`[{"slug":"Rally App","display_name":"Rally","base_url":"http://r"}]`, "slug must be"},
		"missing name":     {`[{"slug":"rally","base_url":"http://r"}]`, "display_name is required"},
		"missing url":      {`[{"slug":"rally","display_name":"Rally"}]`, "base_url is required"},
		"relative url":     {`[{"slug":"rally","display_name":"Rally","base_url":"rally:8080"}]`, "absolute http(s) URL"},
		"ftp url":          {`[{"slug":"rally","display_name":"Rally","base_url":"ftp://rally"}]`, "absolute http(s) URL"},
		"duplicate": {`[{"slug":"rally","display_name":"A","base_url":"http://a"},
			{"slug":"rally","display_name":"B","base_url":"http://b","environment":"production"}]`, "duplicates entry 0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePreregisterModules(tc.raw)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "DEN_PREREGISTER_MODULES") {
				t.Fatalf("error %q should start with DEN_PREREGISTER_MODULES and contain %q", err, tc.want)
			}
		})
	}
}

func TestParsePreregisterModulesSameSlugOtherEnvironment(t *testing.T) {
	raw := `[{"slug":"rally","display_name":"Rally","base_url":"http://a"},
		{"slug":"rally","display_name":"Rally","base_url":"http://b","environment":"staging"}]`
	if _, err := ParsePreregisterModules(raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFailsOnInvalidPreregisterModules(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("S3_ENDPOINT", "http://s3")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("S3_ACCESS_KEY_ID", "k")
	t.Setenv("S3_SECRET_ACCESS_KEY", "s")
	t.Setenv("AUTH_MODE", "dev")

	t.Setenv("DEN_PREREGISTER_MODULES", `[{"slug":"rally"}]`)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DEN_PREREGISTER_MODULES") {
		t.Fatalf("want DEN_PREREGISTER_MODULES error, got %v", err)
	}

	t.Setenv("DEN_PREREGISTER_MODULES", `[{"slug":"rally","display_name":"Rally","base_url":"http://rally:8080"}]`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.PreregisterModules) != 1 || cfg.PreregisterModules[0].Environment != "production" {
		t.Fatalf("unexpected modules: %+v", cfg.PreregisterModules)
	}
}
