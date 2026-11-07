package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// defaultOrgID is the seeded org slug 'default'. For the MVP the modules
// handlers assume everything belongs to it. Multi-tenant scoping lands
// alongside OIDC provisioning.
var defaultOrgID = uuid.MustParse("00000000-0000-0000-0000-00000000d001")

type moduleHandler struct {
	pool *pgxpool.Pool
	ops  *opsClient // nil: Den doesn't manage databases
}

func newModuleHandler(pool *pgxpool.Pool, ops *opsClient) *moduleHandler {
	return &moduleHandler{pool: pool, ops: ops}
}

// moduleDatabase is the database a module uses on the shared server: the
// one recorded when Den created it, else one named after the slug.
func moduleDatabase(slug string, recorded *string) string {
	if recorded != nil && *recorded != "" {
		return *recorded
	}
	return strings.ReplaceAll(slug, "-", "_")
}

type moduleDTO struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	Slug           string          `json:"slug"`
	DisplayName    string          `json:"display_name"`
	BaseURL        string          `json:"base_url"`
	Environment    string          `json:"environment"`
	Enabled        bool            `json:"enabled"`
	Status         string          `json:"status"`
	Manifest       json.RawMessage `json:"manifest"`
	LastSeenAt     string          `json:"last_seen_at,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	Database       string          `json:"database"`
}

func (h *moduleHandler) routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Get("/{id}", h.get)
	r.Patch("/{id}", h.update)
	r.Delete("/{id}", h.delete)
}

func (h *moduleHandler) list(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		writeJSON(w, http.StatusOK, []moduleDTO{})
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, organization_id, slug, display_name, base_url, environment,
		       enabled, status, manifest, last_seen_at, created_at, updated_at, database_name
		  FROM modules
		 WHERE organization_id = $1
		 ORDER BY display_name
	`, defaultOrgID)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	defer rows.Close()

	out := make([]moduleDTO, 0)
	for rows.Next() {
		var (
			dto       moduleDTO
			id, orgID uuid.UUID
			manifest  []byte
			lastSeen  *time.Time
			created   time.Time
			updated   time.Time
			dbName    *string
		)
		if err := rows.Scan(&id, &orgID, &dto.Slug, &dto.DisplayName, &dto.BaseURL,
			&dto.Environment, &dto.Enabled, &dto.Status, &manifest, &lastSeen, &created, &updated, &dbName); err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		dto.Database = moduleDatabase(dto.Slug, dbName)
		dto.ID = id.String()
		dto.OrganizationID = orgID.String()
		if len(manifest) == 0 {
			manifest = []byte("{}")
		}
		dto.Manifest = manifest
		if lastSeen != nil {
			dto.LastSeenAt = lastSeen.Format(time.RFC3339)
		}
		dto.CreatedAt = created.Format(time.RFC3339)
		dto.UpdatedAt = updated.Format(time.RFC3339)
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

type createModuleInput struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	BaseURL     string `json:"base_url"`
	Environment string `json:"environment"`
	// CreateDatabase asks the operator for a database and login; the
	// password is returned once.
	CreateDatabase bool `json:"create_database"`
}

type createdDatabase struct {
	Name        string `json:"name"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DatabaseURL string `json:"database_url"`
}

func (h *moduleHandler) create(w http.ResponseWriter, r *http.Request) {
	var in createModuleInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	in.Slug = strings.TrimSpace(in.Slug)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.Slug == "" || in.DisplayName == "" {
		httperror.Render(w, r, httperror.Validation("slug and display_name are required", nil))
		return
	}
	if in.Environment == "" {
		in.Environment = "production"
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	if in.CreateDatabase && h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("Den doesn't manage databases in this installation"))
		return
	}

	var id uuid.UUID
	var dbName *string
	if in.CreateDatabase {
		n := moduleDatabase(in.Slug, nil)
		dbName = &n
	}
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO modules (organization_id, slug, display_name, base_url, environment, database_name)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, defaultOrgID, in.Slug, in.DisplayName, in.BaseURL, in.Environment, dbName).Scan(&id)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	out := map[string]any{"id": id.String()}
	if in.CreateDatabase {
		var db createdDatabase
		if err := h.ops.do(r.Context(), http.MethodPost, "/v1/databases", map[string]string{"name": *dbName}, &db); err != nil {
			_, _ = h.pool.Exec(r.Context(), `DELETE FROM modules WHERE id = $1`, id)
			renderOps(w, r, err)
			return
		}
		out["database"] = db
	}
	writeJSON(w, http.StatusCreated, out)

	// best-effort audit; swallow error so a broken audit path can't fail the create
	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO audit_events (organization_id, actor_user_id, action, entity_type, entity_id, note)
		VALUES ($1, $2, 'module.create', 'module', $3, $4)
	`, defaultOrgID, auth.ActorID(r.Context()), id, auditNote(dbName, "database created: "))
}

func auditNote(db *string, prefix string) string {
	if db == nil {
		return ""
	}
	return prefix + *db
}

func (h *moduleHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}

	var (
		dto      moduleDTO
		modID    uuid.UUID
		orgID    uuid.UUID
		manifest []byte
		lastSeen *time.Time
		created  time.Time
		updated  time.Time
		dbName   *string
	)
	err = h.pool.QueryRow(r.Context(), `
		SELECT id, organization_id, slug, display_name, base_url, environment,
		       enabled, status, manifest, last_seen_at, created_at, updated_at, database_name
		  FROM modules
		 WHERE id = $1
	`, id).Scan(&modID, &orgID, &dto.Slug, &dto.DisplayName, &dto.BaseURL,
		&dto.Environment, &dto.Enabled, &dto.Status, &manifest, &lastSeen, &created, &updated, &dbName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httperror.Render(w, r, httperror.NotFound("module"))
			return
		}
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	dto.ID = modID.String()
	dto.OrganizationID = orgID.String()
	dto.Database = moduleDatabase(dto.Slug, dbName)
	if len(manifest) == 0 {
		manifest = []byte("{}")
	}
	dto.Manifest = manifest
	if lastSeen != nil {
		dto.LastSeenAt = lastSeen.Format(time.RFC3339)
	}
	dto.CreatedAt = created.Format(time.RFC3339)
	dto.UpdatedAt = updated.Format(time.RFC3339)
	writeJSON(w, http.StatusOK, dto)
}

type updateModuleInput struct {
	DisplayName *string `json:"display_name,omitempty"`
	BaseURL     *string `json:"base_url,omitempty"`
	Environment *string `json:"environment,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func (h *moduleHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in updateModuleInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE modules SET
		    display_name = COALESCE($2, display_name),
		    base_url     = COALESCE($3, base_url),
		    environment  = COALESCE($4, environment),
		    enabled      = COALESCE($5, enabled),
		    updated_at   = now()
		 WHERE id = $1
	`, id, in.DisplayName, in.BaseURL, in.Environment, in.Enabled)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	if tag.RowsAffected() == 0 {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id.String()})
}

func (h *moduleHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	var slug string
	var recorded *string
	err = h.pool.QueryRow(r.Context(), `SELECT slug, database_name FROM modules WHERE id = $1`, id).Scan(&slug, &recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	// ?database=drop&confirm=<slug> deletes the module's data (the operator
	// backs it up first and disconnects every session); anything else only
	// unlinks it and keeps the data.
	out := map[string]any{"database_dropped": false}
	note := "data kept"
	if r.URL.Query().Get("database") == "drop" {
		if h.ops == nil {
			httperror.Render(w, r, httperror.Conflict("Den doesn't manage databases in this installation"))
			return
		}
		if r.URL.Query().Get("confirm") != slug {
			httperror.Render(w, r, httperror.Validation("type the module's slug to confirm deleting its data", nil))
			return
		}
		name := moduleDatabase(slug, recorded)
		var res struct {
			BackupKey string `json:"backup_key"`
		}
		if err := h.ops.do(context.WithoutCancel(r.Context()), http.MethodDelete, "/v1/databases/"+url.PathEscape(name), nil, &res); err != nil {
			renderOps(w, r, err)
			return
		}
		out["database_dropped"], out["database"], out["backup_key"] = true, name, res.BackupKey
		note = "database dropped: " + name
		if res.BackupKey != "" {
			note += ", backup " + res.BackupKey
		}
	}

	if _, err := h.pool.Exec(r.Context(), `DELETE FROM modules WHERE id = $1`, id); err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO audit_events (organization_id, actor_user_id, action, entity_type, entity_id, note)
		VALUES ($1, $2, 'module.delete', 'module', $3, $4)
	`, defaultOrgID, auth.ActorID(r.Context()), id, note)
	writeJSON(w, http.StatusOK, out)
}
