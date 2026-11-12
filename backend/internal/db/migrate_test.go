package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Two migrate runners at once (two pods starting) on a fresh database: both
// succeed and the schema is applied once. Needs a superuser connection in
// DEN_TEST_PG_ADMIN_URL to create the scratch database.
func TestMigrateUpConcurrently(t *testing.T) {
	admin := os.Getenv("DEN_TEST_PG_ADMIN_URL")
	if admin == "" {
		t.Skip("DEN_TEST_PG_ADMIN_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	// Close after the drop cleanup. A defer would run first and the drop
	// would then execute on a closed connection, leaving the scratch database.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	name := fmt.Sprintf("den_migrate_test_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop scratch database %s: %v", name, err)
		}
	})

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	dsn := u.String()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = MigrateUp(ctx, dsn)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("runner %d: %v", i, err)
		}
	}
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var applied, orgs int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id > 0`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs); err != nil {
		t.Fatal(err)
	}
	if orgs != 1 {
		t.Fatalf("the seed ran %d times", orgs)
	}
	entries, _ := os.ReadDir("../../migrations")
	sqlFiles := 0
	for _, e := range entries {
		if len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".sql" {
			sqlFiles++
		}
	}
	if applied != sqlFiles {
		t.Fatalf("applied %d migrations, want %d", applied, sqlFiles)
	}
}
