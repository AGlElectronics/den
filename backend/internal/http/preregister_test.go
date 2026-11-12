package http

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		"INSERT INTO modules (organization_id, slug, display_name, base_url, internal_url, environment, enabled, status)",
		"VALUES ($1, $2, $3, $4, $5, $6, TRUE, 'unpaired')",
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
	if first[0] != defaultOrgID || first[1] != "rally" || first[2] != "Rally" || first[3] != "http://rally:8080" || first[4] != "" || first[5] != "production" {
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
