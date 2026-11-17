package http_test

import (
	"net/http"
	"testing"
)

func TestMyQuickLinks(t *testing.T) {
	e := newEnv(t, nil)
	tok := memberTok()

	var out struct {
		Links []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			URL   string `json:"url"`
		} `json:"links"`
		UpdatedAt *string `json:"updated_at"`
	}
	e.expect(e.do(http.MethodGet, "/api/v1/me/quick-links", tok, nil), http.StatusOK).json(t, &out)
	if len(out.Links) != 0 || out.UpdatedAt != nil {
		t.Fatalf("default: %+v", out)
	}

	e.expect(e.do(http.MethodPut, "/api/v1/me/quick-links", tok, map[string]any{
		"links": []map[string]string{{
			"label": "External",
			"url":   "https://example.com/nope",
		}},
	}), http.StatusUnprocessableEntity)

	for name, bad := range map[string]string{
		"look-alike host": "http://localhost:8090.evil.example/x",
		"credentials":     "http://user:pw@localhost:8090/x",
		"fragment":        "http://localhost:8090/x#frag",
		"script scheme":   "javascript:alert(1)",
		"relative":        "/admin",
	} {
		e.expect(e.do(http.MethodPut, "/api/v1/me/quick-links", tok, map[string]any{
			"links": []map[string]string{{"label": name, "url": bad}},
		}), http.StatusUnprocessableEntity)
	}
	e.expect(e.do(http.MethodPut, "/api/v1/me/quick-links", tok, map[string]any{
		"links": []map[string]string{{"label": "  ", "url": "http://localhost:8090/admin"}},
	}), http.StatusUnprocessableEntity)

	e.expect(e.do(http.MethodPut, "/api/v1/me/quick-links", tok, map[string]any{
		"links": []map[string]string{{
			"label": "HR onboarding",
			"url":   "http://localhost:8090/admin",
		}},
	}), http.StatusOK).json(t, &out)
	if len(out.Links) != 1 || out.Links[0].Label != "HR onboarding" || out.UpdatedAt == nil || out.Links[0].ID == "" {
		t.Fatalf("saved: %+v", out)
	}

	e.expect(e.do(http.MethodGet, "/api/v1/me/quick-links", tok, nil), http.StatusOK).json(t, &out)
	if len(out.Links) != 1 {
		t.Fatalf("reload: %+v", out)
	}

	e.expect(e.do(http.MethodPut, "/api/v1/me/quick-links", tok, map[string]any{
		"links": []any{},
	}), http.StatusOK).json(t, &out)
	if len(out.Links) != 0 {
		t.Fatalf("cleared: %+v", out)
	}

	e.expect(e.do(http.MethodGet, "/api/v1/me/quick-links", "", nil), http.StatusUnauthorized)
}
