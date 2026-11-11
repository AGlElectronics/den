package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// GET /api/v1/audit-events: the audit log, newest first, for admins.
//
// Filters (all optional): action (exact, or a prefix ending in "*", e.g.
// "module.*"), entity_type, entity_id, actor_user_id, since and until
// (RFC 3339). Paging: limit (1–200, default 50) and cursor (next_cursor of
// the previous page).

type auditHandler struct {
	pool *pgxpool.Pool
}

type auditActorDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
}

type auditEventDTO struct {
	ID         string          `json:"id"`
	CreatedAt  string          `json:"created_at"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Actor      *auditActorDTO  `json:"actor"`
	Note       string          `json:"note"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
}

type auditPage struct {
	Items      []auditEventDTO `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func encodeAuditCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "," + id.String()))
}

func decodeAuditCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	ts, idStr, ok := strings.Cut(string(raw), ",")
	if !ok {
		return time.Time{}, uuid.Nil, errors.New("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(idStr)
	return t, id, err
}

func (h *auditHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			httperror.Render(w, r, httperror.BadRequest("limit must be between 1 and 200"))
			return
		}
		limit = n
	}

	where := []string{"(a.organization_id = $1 OR a.organization_id IS NULL)"}
	args := []any{defaultOrgID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if v := q.Get("action"); v != "" {
		if prefix, ok := strings.CutSuffix(v, "*"); ok {
			add("starts_with(a.action, ?)", prefix)
		} else {
			add("a.action = ?", v)
		}
	}
	if v := q.Get("entity_type"); v != "" {
		add("a.entity_type = ?", v)
	}
	for _, f := range []struct{ param, col string }{{"entity_id", "a.entity_id"}, {"actor_user_id", "a.actor_user_id"}} {
		if v := q.Get(f.param); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				httperror.Render(w, r, httperror.BadRequest("invalid "+f.param))
				return
			}
			add(f.col+" = ?", id)
		}
	}
	for _, f := range []struct{ param, cond string }{{"since", "a.created_at >= ?"}, {"until", "a.created_at < ?"}} {
		if v := q.Get(f.param); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httperror.Render(w, r, httperror.BadRequest(f.param+" must be an RFC 3339 time, e.g. 2026-11-11T00:00:00Z"))
				return
			}
			add(f.cond, t)
		}
	}
	if v := q.Get("cursor"); v != "" {
		t, id, err := decodeAuditCursor(v)
		if err != nil {
			httperror.Render(w, r, httperror.BadRequest("invalid cursor"))
			return
		}
		args = append(args, t, id)
		where = append(where, "(a.created_at, a.id) < ($"+strconv.Itoa(len(args)-1)+", $"+strconv.Itoa(len(args))+")")
	}
	args = append(args, limit+1)

	rows, err := h.pool.Query(r.Context(), `
		SELECT a.id, a.created_at, a.action, a.entity_type, a.entity_id, a.note,
		       a.before_json, a.after_json, u.id, u.display_name, u.email
		  FROM audit_events a
		  LEFT JOIN users u ON u.id = a.actor_user_id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY a.created_at DESC, a.id DESC
		 LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	defer rows.Close()

	page := auditPage{Items: make([]auditEventDTO, 0, limit)}
	var lastAt time.Time
	var lastID uuid.UUID
	for rows.Next() {
		var (
			e             auditEventDTO
			id, entityID  uuid.UUID
			created       time.Time
			before, after []byte
			actorID       *uuid.UUID
			actorName     *string
			actorEmail    *string
		)
		if err := rows.Scan(&id, &created, &e.Action, &e.EntityType, &entityID, &e.Note,
			&before, &after, &actorID, &actorName, &actorEmail); err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeAuditCursor(lastAt, lastID)
			break
		}
		e.ID, e.EntityID = id.String(), entityID.String()
		e.CreatedAt = created.UTC().Format(time.RFC3339)
		e.Before, e.After = before, after
		if actorID != nil {
			a := &auditActorDTO{ID: actorID.String()}
			if actorName != nil {
				a.DisplayName = *actorName
			}
			if actorEmail != nil {
				a.Email = *actorEmail
			}
			e.Actor = a
		}
		page.Items = append(page.Items, e)
		lastAt, lastID = created, id
	}
	if err := rows.Err(); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, page)
}
