package http_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
	denhttp "github.com/AGlElectronics/den/backend/internal/http"
)

// Integration: runs twice against a real database and checks that existing
// rows are left alone. Skips unless DEN_TEST_DATABASE_URL is set.
// testPool applies migrations, so this passes on an empty database even
// when no other test has run.
func TestPreregisterModulesPostgres(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	slug := "pre-" + uuid.NewString()[:12]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM modules WHERE organization_id = $1 AND slug = $2`, testOrgID, slug)
	})
	mods := []config.PreregisteredModule{{Slug: slug, DisplayName: "Pre", BaseURL: "http://pre:8080", Environment: "dev"}}

	res, err := denhttp.PreregisterModules(ctx, pool, mods, zerolog.Nop())
	if err != nil || res.Created != 1 {
		t.Fatalf("first run: res=%+v err=%v", res, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE modules SET display_name = 'Renamed', status = 'paired' WHERE organization_id = $1 AND slug = $2`, testOrgID, slug); err != nil {
		t.Fatalf("update: %v", err)
	}

	res, err = denhttp.PreregisterModules(ctx, pool, mods, zerolog.Nop())
	if err != nil || res.Created != 0 || res.Existing != 1 {
		t.Fatalf("second run: res=%+v err=%v", res, err)
	}
	var name, status string
	var enabled bool
	var dbName *string
	var audits int
	if err := pool.QueryRow(ctx, `
		SELECT m.display_name, m.status, m.enabled, m.database_name,
		       (SELECT count(*) FROM audit_events a WHERE a.entity_id = m.id AND a.actor_user_id IS NULL)
		  FROM modules m WHERE m.organization_id = $1 AND m.slug = $2`, testOrgID, slug).
		Scan(&name, &status, &enabled, &dbName, &audits); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "Renamed" || status != "paired" || !enabled || dbName != nil || audits != 1 {
		t.Fatalf("existing row changed or wrong audit: name=%s status=%s enabled=%v db=%v audits=%d", name, status, enabled, dbName, audits)
	}
}
