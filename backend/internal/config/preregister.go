package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// DefaultModuleEnvironment matches the default of POST /api/v1/modules and
// the modules.environment column.
const DefaultModuleEnvironment = "production"

// PreregisteredModule is one entry of DEN_PREREGISTER_MODULES.
type PreregisteredModule struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	BaseURL     string `json:"base_url"`
	Environment string `json:"environment,omitempty"`
}

var moduleSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateModuleSlug checks a module slug: lowercase letters, digits and
// single hyphens, at most 40 characters.
func ValidateModuleSlug(slug string) error {
	if slug == "" {
		return errors.New("is required")
	}
	if len(slug) > 40 || !moduleSlugPattern.MatchString(slug) {
		return errors.New("must be lowercase letters, digits and single hyphens (at most 40), for example \"rally\"")
	}
	return nil
}

// ValidateModuleURL checks a module address: an absolute https URL without
// user info, query or fragment. allowHTTP also accepts http (APP_ENV=development).
func ValidateModuleURL(raw string, allowHTTP bool) error {
	if raw == "" {
		return errors.New("is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("must be an absolute URL such as https://stash.example.org, got %q", raw)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowHTTP:
	case u.Scheme == "http":
		return fmt.Errorf("must use https (http is only allowed with APP_ENV=development), got %q", raw)
	default:
		return fmt.Errorf("must be an https URL, got %q", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must not contain a user name, query or fragment, got %q", raw)
	}
	return nil
}

// ParsePreregisterModules parses DEN_PREREGISTER_MODULES: a JSON array of
// {"slug","display_name","base_url"[,"environment"]}. Empty means none.
// allowHTTP accepts http base URLs (APP_ENV=development).
func ParsePreregisterModules(raw string, allowHTTP bool) ([]PreregisteredModule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var mods []PreregisteredModule
	if err := dec.Decode(&mods); err != nil {
		return nil, fmt.Errorf("DEN_PREREGISTER_MODULES: must be a JSON array of {\"slug\",\"display_name\",\"base_url\"[,\"environment\"]}: %w", err)
	}
	if dec.More() {
		return nil, errors.New("DEN_PREREGISTER_MODULES: unexpected data after the JSON array")
	}

	var errs []string
	seen := make(map[string]int, len(mods))
	for i := range mods {
		m := &mods[i]
		m.Slug = strings.TrimSpace(m.Slug)
		m.DisplayName = strings.TrimSpace(m.DisplayName)
		m.BaseURL = strings.TrimSpace(m.BaseURL)
		m.Environment = strings.TrimSpace(m.Environment)
		if m.Environment == "" {
			m.Environment = DefaultModuleEnvironment
		}
		where := fmt.Sprintf("entry %d", i)
		if m.Slug != "" {
			where = fmt.Sprintf("entry %d (%q)", i, m.Slug)
		}
		if err := ValidateModuleSlug(m.Slug); err != nil {
			errs = append(errs, where+": slug "+err.Error())
		}
		if m.DisplayName == "" {
			errs = append(errs, where+": display_name is required")
		}
		if err := ValidateModuleURL(m.BaseURL, allowHTTP); err != nil {
			errs = append(errs, where+": base_url "+err.Error())
		}
		key := m.Slug + "\x00" + m.Environment
		if prev, dup := seen[key]; dup && m.Slug != "" {
			errs = append(errs, fmt.Sprintf("%s: duplicates entry %d (same slug and environment)", where, prev))
		} else {
			seen[key] = i
		}
	}
	if len(errs) > 0 {
		return nil, errors.New("DEN_PREREGISTER_MODULES: " + strings.Join(errs, "; "))
	}
	return mods, nil
}
