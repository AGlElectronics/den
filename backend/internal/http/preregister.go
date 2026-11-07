package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/den/backend/internal/config"
)

// preregisterInsertSQL never touches an existing row: a conflict returns no
// row, which is how inserted and already-present modules are told apart.
const preregisterInsertSQL = `
		INSERT INTO modules (organization_id, slug, display_name, base_url, environment, enabled, status)
		VALUES ($1, $2, $3, $4, $5, TRUE, 'unpaired')
		ON CONFLICT (organization_id, slug, environment) DO NOTHING
		RETURNING id
	`

// The actor is the system, so actor_user_id stays NULL.
const preregisterAuditSQL = `
		INSERT INTO audit_events (organization_id, actor_user_id, action, entity_type, entity_id, note)
		VALUES ($1, NULL, 'module.create', 'module', $2, $3)
	`

const preregisterAuditNote = "pre-registered at startup from DEN_PREREGISTER_MODULES"

// PreregisterDB is satisfied by *pgxpool.Pool and pgx.Tx.
type PreregisterDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PreregisterResult counts what PreregisterModules did.
type PreregisterResult struct {
	Created  int
	Existing int
}

// PreregisterModules inserts each configured module into the default
// organisation as unpaired, skipping any that already exist.
func PreregisterModules(ctx context.Context, db PreregisterDB, mods []config.PreregisteredModule, logger zerolog.Logger) (PreregisterResult, error) {
	var res PreregisterResult
	for _, m := range mods {
		var id uuid.UUID
		err := db.QueryRow(ctx, preregisterInsertSQL,
			defaultOrgID, m.Slug, m.DisplayName, m.BaseURL, m.Environment).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			res.Existing++
			continue
		}
		if err != nil {
			return res, fmt.Errorf("pre-register module %q (%s): %w", m.Slug, m.Environment, err)
		}
		res.Created++
		logger.Info().
			Str("module_id", id.String()).
			Str("slug", m.Slug).
			Str("environment", m.Environment).
			Str("base_url", m.BaseURL).
			Msg("pre-registered module (unpaired)")
		if _, err := db.Exec(ctx, preregisterAuditSQL, defaultOrgID, id, preregisterAuditNote); err != nil {
			logger.Warn().Err(err).Str("slug", m.Slug).Msg("pre-register audit event failed")
		}
	}
	return res, nil
}
