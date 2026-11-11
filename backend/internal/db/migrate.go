package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/AGlElectronics/den/backend/migrations"
)

// migrationLockID is the pg_advisory_lock key of Den's migrations ("den-migr").
const migrationLockID int64 = 0x64656e2d6d696772

// MigrateUp applies all pending Goose migrations against the given DSN.
//
// This function is intentionally invoked **only** from `cmd/migrate`. The
// API server (`cmd/server`) must NOT call it: concurrent replicas fighting
// over the Goose version table is a nightmare we never want to debug.
// See docs/adr/0005-no-shared-postgres.md for the broader rationale.
//
// Several replicas each start the migrate initContainer, so the run holds a
// Postgres advisory lock: a second runner waits, then finds nothing to do.
func MigrateUp(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open sql.DB for migrations: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	lock, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect for the migration lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("take the migration lock: %w", err)
	}
	defer func() {
		_, _ = lock.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrateStatus prints pending/applied migrations. Handy for cmd/migrate
// `-status` subcommand later.
func MigrateStatus(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open sql.DB for migrations: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	return goose.StatusContext(ctx, sqlDB, ".")
}
