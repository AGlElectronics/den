package http

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
)

type fakeRow struct {
	id  uuid.UUID
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*uuid.UUID) = r.id
	return nil
}

type fakeCall struct {
	sql  string
	args []any
}

// fakeDB treats slugs in existing as already registered.
type fakeDB struct {
	existing map[string]bool
	failOn   string
	queries  []fakeCall
	execs    []fakeCall
}

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queries = append(f.queries, fakeCall{sql, args})
	slug := args[1].(string)
	switch {
	case slug == f.failOn:
		return fakeRow{err: errors.New("boom")}
	case f.existing[slug]:
		return fakeRow{err: pgx.ErrNoRows}
	}
	return fakeRow{id: uuid.New()}
}

func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, fakeCall{sql, args})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestPreregisterInsertStatement(t *testing.T) {
	sql := strings.Join(strings.Fields(preregisterInsertSQL), " ")
	for _, want := range []string{
		"INSERT INTO modules (organization_id, slug, display_name, base_url, environment, enabled, status)",
		"VALUES ($1, $2, $3, $4, $5, TRUE, 'unpaired')",
		"ON CONFLICT (organization_id, slug, environment) DO NOTHING",
		"RETURNING id",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("insert statement missing %q:\n%s", want, sql)
		}
	}
	for _, banned := range []string{"DO UPDATE", "database_name", "DELETE"} {
		if strings.Contains(sql, banned) {
			t.Errorf("insert statement must not contain %q", banned)
		}
	}
	audit := strings.Join(strings.Fields(preregisterAuditSQL), " ")
	if !strings.Contains(audit, "VALUES ($1, NULL, 'module.create', 'module', $2, $3)") {
		t.Errorf("audit statement should record a system actor (NULL user):\n%s", audit)
	}
}

func TestPreregisterModulesInsertsOnlyNew(t *testing.T) {
	db := &fakeDB{existing: map[string]bool{"stash": true}}
	mods := []config.PreregisteredModule{
		{Slug: "rally", DisplayName: "Rally", BaseURL: "http://rally:8080", Environment: "production"},
		{Slug: "stash", DisplayName: "Stash", BaseURL: "http://stash:8080", Environment: "production"},
		{Slug: "hr", DisplayName: "HR", BaseURL: "http://hr:8080", Environment: "staging"},
	}
	res, err := PreregisterModules(context.Background(), db, mods, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Created != 2 || res.Existing != 1 {
		t.Fatalf("want 2 created / 1 existing, got %+v", res)
	}
	if len(db.queries) != 3 {
		t.Fatalf("want 3 inserts, got %d", len(db.queries))
	}
	first := db.queries[0].args
	if first[0] != defaultOrgID || first[1] != "rally" || first[2] != "Rally" || first[3] != "http://rally:8080" || first[4] != "production" {
		t.Errorf("unexpected insert args: %v", first)
	}
	if len(db.execs) != 2 {
		t.Fatalf("want audit events only for the 2 inserted modules, got %d", len(db.execs))
	}
	for _, e := range db.execs {
		if e.args[0] != defaultOrgID || e.args[2] != preregisterAuditNote {
			t.Errorf("unexpected audit args: %v", e.args)
		}
	}
}

func TestPreregisterModulesReturnsDBError(t *testing.T) {
	db := &fakeDB{failOn: "rally"}
	mods := []config.PreregisteredModule{{Slug: "rally", DisplayName: "Rally", BaseURL: "http://r", Environment: "production"}}
	if _, err := PreregisterModules(context.Background(), db, mods, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "rally") {
		t.Fatalf("want error naming the module, got %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatal("no audit event expected after a failed insert")
	}
}

// Integration: runs twice against a real database and checks that existing
// rows are left alone. Skips unless DEN_TEST_DATABASE_URL is set.
func TestPreregisterModulesPostgres(t *testing.T) {
	dsn := os.Getenv("DEN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DEN_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	slug := "pre-" + uuid.NewString()[:12]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM modules WHERE organization_id = $1 AND slug = $2`, defaultOrgID, slug)
	})
	mods := []config.PreregisteredModule{{Slug: slug, DisplayName: "Pre", BaseURL: "http://pre:8080", Environment: "dev"}}

	res, err := PreregisterModules(ctx, pool, mods, zerolog.Nop())
	if err != nil || res.Created != 1 {
		t.Fatalf("first run: res=%+v err=%v", res, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE modules SET display_name = 'Renamed', status = 'paired' WHERE organization_id = $1 AND slug = $2`, defaultOrgID, slug); err != nil {
		t.Fatalf("update: %v", err)
	}

	res, err = PreregisterModules(ctx, pool, mods, zerolog.Nop())
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
		  FROM modules m WHERE m.organization_id = $1 AND m.slug = $2`, defaultOrgID, slug).
		Scan(&name, &status, &enabled, &dbName, &audits); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "Renamed" || status != "paired" || !enabled || dbName != nil || audits != 1 {
		t.Fatalf("existing row changed or wrong audit: name=%s status=%s enabled=%v db=%v audits=%d", name, status, enabled, dbName, audits)
	}
}
