package http_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestAuditEventsPagingAndFilters(t *testing.T) {
	e := newEnv(t, nil)
	admin := adminTok()
	entity := uuid.New()
	for i := 0; i < 5; i++ {
		if _, err := e.pool.Exec(context.Background(), `
			INSERT INTO audit_events (organization_id, action, entity_type, entity_id, note)
			VALUES ($1, 'test.paging', 'test', $2, $3)`, testOrgID, entity, "event "+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Items []struct {
			Action string `json:"action"`
			Note   string `json:"note"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	base := "/api/v1/audit-events?entity_id=" + entity.String() + "&limit=2"
	var notes []string
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		var p page
		url := base
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		e.expect(e.do(http.MethodGet, url, admin, nil), http.StatusOK).json(t, &p)
		for _, it := range p.Items {
			notes = append(notes, it.Note)
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(notes) != 5 || notes[0] != "event e" || notes[4] != "event a" {
		t.Fatalf("paging returned %v", notes)
	}

	var p page
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?action=test.*&entity_type=test&entity_id="+entity.String(), admin, nil), http.StatusOK).json(t, &p)
	if len(p.Items) != 5 {
		t.Fatalf("prefix filter: %d items", len(p.Items))
	}
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?action=nope&entity_id="+entity.String(), admin, nil), http.StatusOK).json(t, &p)
	if len(p.Items) != 0 {
		t.Fatalf("exact filter: %d items", len(p.Items))
	}

	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?limit=0", admin, nil), http.StatusBadRequest)
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?since=yesterday", admin, nil), http.StatusBadRequest)
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events?cursor=bm9wZQ", admin, nil), http.StatusBadRequest)
	e.expect(e.do(http.MethodGet, "/api/v1/audit-events", memberTok(), nil), http.StatusForbidden)
}
