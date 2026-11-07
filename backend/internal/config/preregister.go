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

// ParsePreregisterModules parses DEN_PREREGISTER_MODULES: a JSON array of
// {"slug","display_name","base_url"[,"environment"]}. Empty means none.
func ParsePreregisterModules(raw string) ([]PreregisteredModule, error) {
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
		switch {
		case m.Slug == "":
			errs = append(errs, where+": slug is required")
		case !moduleSlugPattern.MatchString(m.Slug):
			errs = append(errs, where+": slug must be lowercase letters, digits and single hyphens, for example \"rally\"")
		}
		if m.DisplayName == "" {
			errs = append(errs, where+": display_name is required")
		}
		if err := validateModuleBaseURL(m.BaseURL); err != nil {
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

func validateModuleBaseURL(raw string) error {
	if raw == "" {
		return errors.New("is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	return nil
}
