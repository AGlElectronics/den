// Package audit writes rows into audit_events, usually inside the same
// transaction as the state change they describe.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AGlElectronics/den/backend/internal/auth"
)

// Executor is anything pgx-pool-like that can Exec. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so callers inside a transaction can hand us their
// transaction and get an atomic insert alongside their state change.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Record persists an audit_events row. `before` and `after` may be any
// JSON-marshallable value (or nil); they are stored as JSONB. orgID is the
// organization scope of the event; pass nil for system-wide events.
func Record(
	ctx context.Context,
	exec Executor,
	orgID *uuid.UUID,
	action string,
	entityType string,
	entityID uuid.UUID,
	before, after any,
	note string,
) error {
	beforeJSON, err := marshalForJSONB(before)
	if err != nil {
		return fmt.Errorf("marshal audit before: %w", err)
	}
	afterJSON, err := marshalForJSONB(after)
	if err != nil {
		return fmt.Errorf("marshal audit after: %w", err)
	}

	_, err = exec.Exec(ctx, `
		INSERT INTO audit_events (
			organization_id, actor_user_id, action,
			entity_type, entity_id, before_json, after_json, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		orgID,
		auth.ActorID(ctx),
		action,
		entityType,
		entityID,
		beforeJSON,
		afterJSON,
		note,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func marshalForJSONB(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
