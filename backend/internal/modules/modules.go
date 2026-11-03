// Package modules is the thin service in front of the `modules` table. It
// is used by the human admin UI (register/list/edit modules) and read by
// the pairing flow when a token is minted.
package modules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/den/backend/internal/audit"
	"github.com/AGlElectronics/den/backend/internal/domain"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Module is the API-facing representation. `Manifest` is kept as a raw
// JSON message so we don't have to shoehorn a module's self-reported shape
// into a Go struct.
type Module struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Slug           string          `json:"slug"`
	DisplayName    string          `json:"display_name"`
	BaseURL        string          `json:"base_url"`
	Environment    string          `json:"environment"`
	Enabled        bool            `json:"enabled"`
	Status         string          `json:"status"`
	Manifest       json.RawMessage `json:"manifest"`
	LastSeenAt     *time.Time      `json:"last_seen_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateInput struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Slug           string    `json:"slug"`
	DisplayName    string    `json:"display_name"`
	BaseURL        string    `json:"base_url"`
	Environment    string    `json:"environment"`
}

func (in CreateInput) validate() error {
	in.Slug = strings.TrimSpace(in.Slug)
	if in.OrganizationID == uuid.Nil {
		return httperror.Validation("organization_id is required", nil)
	}
	if in.Slug == "" {
		return httperror.Validation("slug is required", nil)
	}
	if in.DisplayName == "" {
		return httperror.Validation("display_name is required", nil)
	}
	return nil
}

func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]Module, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, organization_id, slug, display_name, base_url, environment,
		       enabled, status, manifest, last_seen_at, created_at, updated_at
		  FROM modules
		 WHERE organization_id = $1
		 ORDER BY display_name
	`, orgID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()

	out := make([]Module, 0)
	for rows.Next() {
		m, err := scanModule(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Module, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, slug, display_name, base_url, environment,
		       enabled, status, manifest, last_seen_at, created_at, updated_at
		  FROM modules
		 WHERE id = $1
	`, id)
	m, err := scanModule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Module{}, httperror.NotFound("module")
		}
		return Module{}, httperror.Internal(err)
	}
	return m, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Module, error) {
	if err := in.validate(); err != nil {
		return Module{}, err
	}
	env := strings.TrimSpace(in.Environment)
	if env == "" {
		env = "production"
	}
	var out Module
	err := withTx(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO modules (organization_id, slug, display_name, base_url, environment)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, organization_id, slug, display_name, base_url, environment,
			          enabled, status, manifest, last_seen_at, created_at, updated_at
		`, in.OrganizationID, in.Slug, in.DisplayName, in.BaseURL, env)
		m, err := scanModule(row)
		if err != nil {
			return httperror.Internal(err)
		}
		out = m
		return audit.Record(ctx, tx, &in.OrganizationID,
			domain.ActionModuleCreate, "module", m.ID,
			nil, m, "")
	})
	return out, err
}

type UpdateInput struct {
	DisplayName string `json:"display_name"`
	BaseURL     string `json:"base_url"`
	Environment string `json:"environment"`
	Enabled     bool   `json:"enabled"`
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (Module, error) {
	var out Module
	err := withTx(ctx, s.pool, func(tx pgx.Tx) error {
		before, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE modules
			   SET display_name = $2,
			       base_url     = $3,
			       environment  = $4,
			       enabled      = $5,
			       updated_at   = now()
			 WHERE id = $1
			RETURNING id, organization_id, slug, display_name, base_url, environment,
			          enabled, status, manifest, last_seen_at, created_at, updated_at
		`, id, in.DisplayName, in.BaseURL, in.Environment, in.Enabled)
		m, err := scanModule(row)
		if err != nil {
			return httperror.Internal(err)
		}
		out = m
		return audit.Record(ctx, tx, &m.OrganizationID,
			domain.ActionModuleUpdate, "module", m.ID,
			before, m, "")
	})
	return out, err
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return withTx(ctx, s.pool, func(tx pgx.Tx) error {
		before, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM modules WHERE id = $1`, id); err != nil {
			return httperror.Internal(err)
		}
		return audit.Record(ctx, tx, &before.OrganizationID,
			domain.ActionModuleDelete, "module", id,
			before, nil, "")
	})
}

// --- helpers -----------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanModule(r rowScanner) (Module, error) {
	var m Module
	var manifest []byte
	if err := r.Scan(
		&m.ID, &m.OrganizationID, &m.Slug, &m.DisplayName, &m.BaseURL, &m.Environment,
		&m.Enabled, &m.Status, &manifest, &m.LastSeenAt, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return Module{}, err
	}
	if len(manifest) == 0 {
		manifest = []byte("{}")
	}
	m.Manifest = manifest
	return m, nil
}

func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
