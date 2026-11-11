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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/config"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// defaultOrgID is the one organisation a Den serves (seeded by the first
// migration). Every query is still filtered by it, so a row of another
// organisation can never be read or changed by accident.
var defaultOrgID = uuid.MustParse("00000000-0000-0000-0000-00000000d001")

var validVisibility = map[string]bool{"role_holders": true, "everyone": true}

type moduleHandler struct {
	pool *pgxpool.Pool
	ops  *opsClient // nil: Den doesn't manage databases
	// allowHTTP accepts http module addresses (APP_ENV=development).
	allowHTTP bool
}

func newModuleHandler(pool *pgxpool.Pool, ops *opsClient, allowHTTP bool) *moduleHandler {
	return &moduleHandler{pool: pool, ops: ops, allowHTTP: allowHTTP}
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
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organization_id"`
	Slug             string          `json:"slug"`
	DisplayName      string          `json:"display_name"`
	BaseURL          string          `json:"base_url"`
	InternalURL      string          `json:"internal_url"`
	Environment      string          `json:"environment"`
	Enabled          bool            `json:"enabled"`
	Status           string          `json:"status"`
	PortalVisibility string          `json:"portal_visibility"`
	Manifest         json.RawMessage `json:"manifest"`
	LastSeenAt       string          `json:"last_seen_at,omitempty"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	// Database is the module's database on the shared server (by name; it
	// may not exist). DatabaseCreatedByDen is true only when Den created it,
	// and only then can Den delete it.
	Database             string `json:"database"`
	DatabaseCreatedByDen bool   `json:"database_created_by_den"`
}

const moduleColumns = `id, organization_id, slug, display_name, base_url, internal_url, environment,
	enabled, status, portal_visibility, manifest, last_seen_at, created_at, updated_at, database_name`

func scanModule(row pgx.Row) (moduleDTO, error) {
	var (
		dto       moduleDTO
		id, orgID uuid.UUID
		manifest  []byte
		lastSeen  *time.Time
		created   time.Time
		updated   time.Time
		dbName    *string
	)
	if err := row.Scan(&id, &orgID, &dto.Slug, &dto.DisplayName, &dto.BaseURL, &dto.InternalURL,
		&dto.Environment, &dto.Enabled, &dto.Status, &dto.PortalVisibility, &manifest, &lastSeen, &created, &updated, &dbName); err != nil {
		return dto, err
	}
	dto.ID, dto.OrganizationID = id.String(), orgID.String()
	dto.Database = moduleDatabase(dto.Slug, dbName)
	dto.DatabaseCreatedByDen = dbName != nil && *dbName != ""
	if len(manifest) == 0 {
		manifest = []byte("{}")
	}
	dto.Manifest = manifest
	if lastSeen != nil {
		dto.LastSeenAt = lastSeen.Format(time.RFC3339)
	}
	dto.CreatedAt = created.Format(time.RFC3339)
	dto.UpdatedAt = updated.Format(time.RFC3339)
	return dto, nil
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
	rows, err := h.pool.Query(r.Context(), `SELECT `+moduleColumns+` FROM modules WHERE organization_id = $1 ORDER BY display_name`, defaultOrgID)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	defer rows.Close()

	out := make([]moduleDTO, 0)
	for rows.Next() {
		dto, err := scanModule(rows)
		if err != nil {
			httperror.Render(w, r, httperror.Internal(err))
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

type createModuleInput struct {
	Slug             string `json:"slug"`
	DisplayName      string `json:"display_name"`
	BaseURL          string `json:"base_url"`
	InternalURL      string `json:"internal_url"`
	Environment      string `json:"environment"`
	PortalVisibility string `json:"portal_visibility"`
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

// validateAddresses checks the optional module addresses into fields.
func (h *moduleHandler) validateAddresses(fields map[string]string, baseURL, internalURL *string) {
	for key, v := range map[string]*string{"base_url": baseURL, "internal_url": internalURL} {
		if v == nil {
			continue
		}
		*v = strings.TrimRight(strings.TrimSpace(*v), "/")
		if *v == "" {
			continue
		}
		if err := config.ValidateModuleURL(*v, h.allowHTTP); err != nil {
			fields[key] = err.Error()
		}
	}
}

func (h *moduleHandler) create(w http.ResponseWriter, r *http.Request) {
	var in createModuleInput
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	in.Slug = strings.TrimSpace(in.Slug)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Environment = strings.TrimSpace(in.Environment)
	if in.Environment == "" {
		in.Environment = config.DefaultModuleEnvironment
	}
	if in.PortalVisibility == "" {
		in.PortalVisibility = "role_holders"
	}
	fields := map[string]string{}
	if err := config.ValidateModuleSlug(in.Slug); err != nil {
		fields["slug"] = err.Error()
	}
	if in.DisplayName == "" {
		fields["display_name"] = "is required"
	}
	if !validVisibility[in.PortalVisibility] {
		fields["portal_visibility"] = "must be role_holders or everyone"
	}
	h.validateAddresses(fields, &in.BaseURL, &in.InternalURL)
	if len(fields) > 0 {
		httperror.Render(w, r, httperror.Validation("validation failed", fields))
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	if in.CreateDatabase && h.ops == nil {
		httperror.Render(w, r, httperror.Conflict("Den doesn't manage databases in this installation"))
		return
	}

	var dbName *string
	if in.CreateDatabase {
		n := moduleDatabase(in.Slug, nil)
		dbName = &n
	}
	out := map[string]any{}
	// The operator creates the database inside the transaction, so a failed
	// database leaves no module row and a duplicate slug creates no database.
	err := auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(r.Context(), `
			INSERT INTO modules (organization_id, slug, display_name, base_url, internal_url, environment, portal_visibility, database_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`, defaultOrgID, in.Slug, in.DisplayName, in.BaseURL, in.InternalURL, in.Environment, in.PortalVisibility, dbName).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return httperror.Conflict("a module with this slug and environment already exists")
		}
		if err != nil {
			return err
		}
		out["id"] = id.String()
		if err := audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "module.create", EntityType: "module", EntityID: id,
			After: in, Note: auditNote(dbName, "database created: "),
		}); err != nil {
			return err
		}
		if in.CreateDatabase {
			var db createdDatabase
			if err := h.ops.do(r.Context(), http.MethodPost, "/v1/databases", map[string]string{"name": *dbName}, &db); err != nil {
				return err
			}
			out["database"] = db
		}
		return nil
	})
	if err != nil {
		var oe *opsError
		if errors.As(err, &oe) {
			renderOps(w, r, err)
			return
		}
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
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
	dto, err := scanModule(h.pool.QueryRow(r.Context(),
		`SELECT `+moduleColumns+` FROM modules WHERE id = $1 AND organization_id = $2`, id, defaultOrgID))
	if errors.Is(err, pgx.ErrNoRows) {
		httperror.Render(w, r, httperror.NotFound("module"))
		return
	}
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

type updateModuleInput struct {
	DisplayName      *string `json:"display_name,omitempty"`
	BaseURL          *string `json:"base_url,omitempty"`
	InternalURL      *string `json:"internal_url,omitempty"`
	Environment      *string `json:"environment,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
	PortalVisibility *string `json:"portal_visibility,omitempty"`
}

func (h *moduleHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in updateModuleInput
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	fields := map[string]string{}
	if in.DisplayName != nil {
		if *in.DisplayName = strings.TrimSpace(*in.DisplayName); *in.DisplayName == "" {
			fields["display_name"] = "must not be empty"
		}
	}
	if in.Environment != nil {
		if *in.Environment = strings.TrimSpace(*in.Environment); *in.Environment == "" {
			fields["environment"] = "must not be empty"
		}
	}
	if in.PortalVisibility != nil && !validVisibility[*in.PortalVisibility] {
		fields["portal_visibility"] = "must be role_holders or everyone"
	}
	h.validateAddresses(fields, in.BaseURL, in.InternalURL)
	if len(fields) > 0 {
		httperror.Render(w, r, httperror.Validation("validation failed", fields))
		return
	}
	if h.pool == nil {
		httperror.Render(w, r, httperror.Internal(errors.New("database not configured")))
		return
	}
	err = auth.WithTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		before, err := scanModule(tx.QueryRow(r.Context(),
			`SELECT `+moduleColumns+` FROM modules WHERE id = $1 AND organization_id = $2 FOR UPDATE`, id, defaultOrgID))
		if errors.Is(err, pgx.ErrNoRows) {
			return httperror.NotFound("module")
		}
		if err != nil {
			return err
		}
		after, err := scanModule(tx.QueryRow(r.Context(), `
			UPDATE modules SET
			    display_name      = COALESCE($3, display_name),
			    base_url          = COALESCE($4, base_url),
			    internal_url      = COALESCE($5, internal_url),
			    environment       = COALESCE($6, environment),
			    enabled           = COALESCE($7, enabled),
			    portal_visibility = COALESCE($8, portal_visibility),
			    updated_at        = now()
			 WHERE id = $1 AND organization_id = $2
			 RETURNING `+moduleColumns,
			id, defaultOrgID, in.DisplayName, in.BaseURL, in.InternalURL, in.Environment, in.Enabled, in.PortalVisibility))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return httperror.Conflict("a module with this slug and environment already exists")
		}
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Event{
			OrgID: orgRef(), Action: "module.update", EntityType: "module", EntityID: id,
			Before: auditableModule(before), After: auditableModule(after),
		})
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id.String()})
}

// auditableModule keeps the fields an admin can change.
func auditableModule(m moduleDTO) map[string]any {
	return map[string]any{
		"display_name": m.DisplayName, "base_url": m.BaseURL, "internal_url": m.InternalURL,
		"environment": m.Environment, "enabled": m.Enabled, "portal_visibility": m.PortalVisibility,
	}
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
	err = h.pool.QueryRow(r.Context(),
		`SELECT slug, database_name FROM modules WHERE id = $1 AND organization_id = $2`, id, defaultOrgID).Scan(&slug, &recorded)
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
	// unlinks it and keeps the data. Only a database Den created can be
	// dropped, and only by someone with the ops role.
	out := map[string]any{"database_dropped": false}
	note := "data kept"
	if r.URL.Query().Get("database") == "drop" {
		if u, _ := auth.FromContext(r.Context()); !u.HasRoleIn(defaultOrgID, auth.RoleOps) {
			httperror.Render(w, r, httperror.Forbidden("deleting a module's database needs the ops role"))
			return
		}
		if recorded == nil || *recorded == "" {
			httperror.Render(w, r, httperror.Conflict("Den did not create this module's database, so it will not delete it"))
			return
		}
		if h.ops == nil {
			httperror.Render(w, r, httperror.Conflict("Den doesn't manage databases in this installation"))
			return
		}
		if r.URL.Query().Get("confirm") != slug {
			httperror.Render(w, r, httperror.Validation("type the module's slug to confirm deleting its data", nil))
			return
		}
		name := *recorded
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

	// The database may already be gone: finish the removal even if the
	// admin closed the page.
	ctx := context.WithoutCancel(r.Context())
	err = auth.WithTx(ctx, h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM modules WHERE id = $1 AND organization_id = $2`, id, defaultOrgID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Event{
			OrgID: orgRef(), Action: "module.delete", EntityType: "module", EntityID: id,
			Before: map[string]any{"slug": slug}, After: out, Note: note,
		})
	})
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, out)
}
