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

// Event is one audit_events row.
type Event struct {
	// OrgID is the organisation scope; nil for system-wide events.
	OrgID *uuid.UUID
	// Actor is the user who caused the event. Nil means the signed-in user
	// on ctx, or the system when there is none.
	Actor      *uuid.UUID
	Action     string
	EntityType string
	EntityID   uuid.UUID
	// Before and After may be any JSON-marshallable value (or nil).
	Before, After any
	Note          string
}

// Write persists e.
func Write(ctx context.Context, exec Executor, e Event) error {
	beforeJSON, err := marshalForJSONB(e.Before)
	if err != nil {
		return fmt.Errorf("marshal audit before: %w", err)
	}
	afterJSON, err := marshalForJSONB(e.After)
	if err != nil {
		return fmt.Errorf("marshal audit after: %w", err)
	}
	actor := e.Actor
	if actor == nil {
		actor = auth.ActorID(ctx)
	}
	_, err = exec.Exec(ctx, `
		INSERT INTO audit_events (
			organization_id, actor_user_id, action,
			entity_type, entity_id, before_json, after_json, note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, e.OrgID, actor, e.Action, e.EntityType, e.EntityID, beforeJSON, afterJSON, e.Note)
	if err != nil {
		return fmt.Errorf("insert audit event %s: %w", e.Action, err)
	}
	return nil
}

// Record persists an audit_events row with the signed-in user on ctx as the
// actor. orgID is the organization scope of the event; pass nil for
// system-wide events.
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
	return Write(ctx, exec, Event{
		OrgID: orgID, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, Note: note,
	})
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
