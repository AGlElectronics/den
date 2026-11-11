package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultOrgID matches the seed row in migrations/20260101000001_init.sql.
// Den serves exactly one organisation; roles in it come from IdP groups
// (docs/adr/0007-roles-from-idp-groups.md).
var defaultOrgID = uuid.MustParse("00000000-0000-0000-0000-00000000d001")

// ErrUserDisabled is returned for an account with users.is_disabled set.
var ErrUserDisabled = errors.New("account disabled")

// Resolver loads users (or provisions them if missing) from the users /
// external_identities tables.
type Resolver struct {
	pool   *pgxpool.Pool
	groups GroupConfig
}

// GroupConfig names the IdP groups behind Den's roles. Members of Admin are
// org admins, members of Member are org members (everyone signed in when
// Member is empty), members of Ops also get RoleOps.
type GroupConfig struct {
	Admin  []string
	Member []string
	Ops    []string
}

// NewResolver builds a resolver for the given group configuration.
func NewResolver(pool *pgxpool.Pool, groups GroupConfig) *Resolver {
	return &Resolver{pool: pool, groups: groups}
}

// LoadOrProvisionDevUser is called from dev-mode middleware. The name is
// treated as the OIDC `subject` under the synthetic `dev:` issuer so dev
// and OIDC paths share the same external_identities shape. Dev users are
// admins with RoleOps: dev mode is a single-person trial.
func (r *Resolver) LoadOrProvisionDevUser(ctx context.Context, name string) (*User, error) {
	if r == nil || r.pool == nil {
		// Should not happen in production. Returning a zero user keeps
		// local smoke tests of handlers that are DB-less from blowing up.
		return &User{
			ID:           uuid.New(),
			DisplayName:  name,
			Issuer:       "dev:",
			Subject:      name,
			RequestRoles: []string{RoleOps},
		}, nil
	}

	const q = `
		SELECT u.id, u.email, u.display_name, u.is_disabled
		  FROM users u
		  JOIN external_identities ei ON ei.user_id = u.id
		 WHERE ei.issuer = 'dev:' AND ei.subject = $1
	`
	var (
		userID      uuid.UUID
		email       *string
		displayName string
		disabled    bool
	)
	err := r.pool.QueryRow(ctx, q, name).Scan(&userID, &email, &displayName, &disabled)
	switch {
	case err == nil:
		if disabled {
			return nil, ErrUserDisabled
		}
	case errors.Is(err, pgx.ErrNoRows):
		id, emailOut, displayOut, perr := r.provisionDevUser(ctx, name)
		if perr != nil {
			return nil, fmt.Errorf("provision dev user: %w", perr)
		}
		userID = id
		email = emailOut
		displayName = displayOut
	default:
		return nil, fmt.Errorf("lookup dev user: %w", err)
	}

	memberships, err := r.loadMemberships(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load memberships: %w", err)
	}

	// Best-effort last_login_at touch; ignore the error.
	_, _ = r.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)

	emailStr := ""
	if email != nil {
		emailStr = *email
	}
	return &User{
		ID:           userID,
		DisplayName:  displayName,
		Email:        emailStr,
		Issuer:       "dev:",
		Subject:      name,
		Memberships:  memberships,
		RequestRoles: []string{RoleOps},
	}, nil
}

func (r *Resolver) provisionDevUser(ctx context.Context, name string) (uuid.UUID, *string, string, error) {
	var (
		userID      uuid.UUID
		email       = fmt.Sprintf("%s@den.local", name)
		displayName = name
	)
	err := WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name)
			VALUES ($1, $2)
			RETURNING id
		`, email, displayName).Scan(&userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO external_identities (user_id, issuer, subject, email)
			VALUES ($1, 'dev:', $2, $3)
		`, userID, name, email); err != nil {
			return err
		}
		// Dev users are admins of the default org. Not production policy —
		// just a sane default so the UI isn't a wasteland on first boot.
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, 'admin')
			ON CONFLICT (organization_id, user_id) DO NOTHING
		`, defaultOrgID, userID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, nil, "", err
	}
	return userID, &email, displayName, nil
}

// LoadOrProvisionOIDCUser upserts the local user from verified token claims
// and syncs their org role from the IdP on every request: members of an
// admin group are admins, members of a member group are members, and anyone
// else has no role (no portal access). Removing someone from a group demotes
// them; an "owner" row is never changed. The groups are stored so Den can
// tell which modules a person may use when a module asks about them.
func (r *Resolver) LoadOrProvisionOIDCUser(ctx context.Context, claims *OIDCClaims) (*User, error) {
	if claims == nil || claims.Subject == "" || claims.Issuer == "" {
		return nil, errors.New("oidc claims missing issuer/subject")
	}
	if r == nil || r.pool == nil {
		return nil, errors.New("resolver has no database")
	}

	display := displayNameFromClaims(claims)
	var emailPtr *string
	if e := strings.TrimSpace(claims.Email); e != "" {
		emailPtr = &e
	}
	groups := claims.Groups
	if groups == nil {
		groups = []string{}
	}

	userID, err := r.upsertOIDCUser(ctx, claims.Issuer, claims.Subject, display, emailPtr, groups)
	if err != nil {
		return nil, err
	}

	role := ""
	switch {
	case InAnyGroup(groups, r.groups.Admin):
		role = "admin"
	case len(r.groups.Member) == 0 || InAnyGroup(groups, r.groups.Member):
		role = "member"
	}
	if role == "" {
		if _, err := r.pool.Exec(ctx, `
			DELETE FROM organization_memberships
			 WHERE organization_id = $1 AND user_id = $2 AND role <> 'owner'
		`, defaultOrgID, userID); err != nil {
			return nil, fmt.Errorf("drop org membership: %w", err)
		}
	} else if _, err := r.pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, user_id) DO UPDATE
		  SET role = EXCLUDED.role
		  WHERE organization_memberships.role <> 'owner'
	`, defaultOrgID, userID, role); err != nil {
		return nil, fmt.Errorf("ensure org membership: %w", err)
	}

	memberships, err := r.loadMemberships(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load memberships: %w", err)
	}

	var requestRoles []string
	if InAnyGroup(groups, r.groups.Ops) {
		requestRoles = append(requestRoles, RoleOps)
	}

	email := ""
	if emailPtr != nil {
		email = *emailPtr
	}
	return &User{
		ID:           userID,
		DisplayName:  display,
		Email:        email,
		Issuer:       claims.Issuer,
		Subject:      claims.Subject,
		Memberships:  memberships,
		RequestRoles: requestRoles,
		Groups:       groups,
	}, nil
}

func (r *Resolver) upsertOIDCUser(
	ctx context.Context,
	issuer, subject, display string,
	email *string,
	groups []string,
) (uuid.UUID, error) {
	var (
		userID   uuid.UUID
		disabled bool
	)
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.is_disabled
		  FROM external_identities ei
		  JOIN users u ON u.id = ei.user_id
		 WHERE ei.issuer = $1 AND ei.subject = $2`,
		issuer, subject,
	).Scan(&userID, &disabled)
	switch {
	case err == nil:
		if disabled {
			return uuid.Nil, ErrUserDisabled
		}
		if _, err := r.pool.Exec(ctx, `
			UPDATE users
			   SET display_name  = $2,
			       email         = COALESCE($3, email),
			       groups        = $4,
			       last_login_at = now(),
			       updated_at    = now()
			 WHERE id = $1
		`, userID, display, email, groups); err != nil {
			return uuid.Nil, fmt.Errorf("update oidc user: %w", err)
		}
		return userID, nil
	case errors.Is(err, pgx.ErrNoRows):
		id, err := r.provisionOIDCUser(ctx, issuer, subject, display, email, groups)
		if err != nil {
			return uuid.Nil, fmt.Errorf("provision oidc user: %w", err)
		}
		return id, nil
	default:
		return uuid.Nil, fmt.Errorf("lookup oidc user: %w", err)
	}
}

func (r *Resolver) provisionOIDCUser(
	ctx context.Context,
	issuer, subject, display string,
	email *string,
	groups []string,
) (uuid.UUID, error) {
	var userID uuid.UUID
	err := WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name, groups, last_login_at)
			VALUES ($1, $2, $3, now())
			RETURNING id
		`, email, display, groups).Scan(&userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO external_identities (user_id, issuer, subject, email)
			VALUES ($1, $2, $3, $4)
		`, userID, issuer, subject, email); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// InAnyGroup reports whether any token group matches one of want.
// Comparison is case-insensitive and ignores a leading "/" (Keycloak's
// full-path form).
func InAnyGroup(groups, want []string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "/")) }
	set := map[string]struct{}{}
	for _, w := range want {
		set[norm(w)] = struct{}{}
	}
	for _, g := range groups {
		if _, ok := set[norm(g)]; ok {
			return true
		}
	}
	return false
}

func (r *Resolver) loadMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT organization_id, role
		  FROM organization_memberships
		 WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Membership, 0)
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrganizationID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// WithTx is re-exported here so the resolver doesn't have to import the db
// package (which would cause a cycle in some test setups).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
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
