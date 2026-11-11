// Package auth attaches the authenticated principal to each request. The
// human UI uses either dev-header or OIDC auth (see middleware.go). The
// integration API uses a module credential (ModuleBearerMiddleware).
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// RoleOps may run backups, download them, change the backup schedule and
// drop module databases. It comes from IdP groups on every request and is
// never stored.
const RoleOps = "ops"

// Membership records that a user has a role within an organization.
type Membership struct {
	OrganizationID uuid.UUID
	Role           string
}

// User is the authenticated human principal for a request.
type User struct {
	ID          uuid.UUID
	DisplayName string
	Email       string

	// Issuer and Subject uniquely identify the external identity that was
	// presented. Dev auth uses issuer="dev:" and subject=<dev user name>.
	Issuer  string
	Subject string

	Memberships []Membership
	// RequestRoles are granted from IdP groups for this request only (for
	// example RoleOps); Den serves a single organisation, so they apply to it.
	RequestRoles []string
	// Groups are the IdP groups from the token (empty in dev mode).
	Groups []string
}

// HasRoleIn reports whether the user holds any of the given roles in the
// supplied organization.
func (u *User) HasRoleIn(orgID uuid.UUID, roles ...string) bool {
	if u == nil {
		return false
	}
	for _, r := range roles {
		for _, have := range u.RequestRoles {
			if have == r {
				return true
			}
		}
	}
	for _, m := range u.Memberships {
		if m.OrganizationID != orgID {
			continue
		}
		for _, r := range roles {
			if m.Role == r {
				return true
			}
		}
	}
	return false
}

// Roles lists every role the user holds: membership roles, then request roles.
func (u *User) Roles() []string {
	out := make([]string, 0, len(u.Memberships)+len(u.RequestRoles))
	for _, m := range u.Memberships {
		out = append(out, m.Role)
	}
	return append(out, u.RequestRoles...)
}

type ctxKey int

const (
	userKey   ctxKey = iota
	moduleKey        // ModulePrincipal for integration API requests
)

func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func FromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userKey).(*User)
	if !ok || u == nil {
		return nil, false
	}
	return u, true
}

func MustFromContext(ctx context.Context) (*User, error) {
	u, ok := FromContext(ctx)
	if !ok {
		return nil, errors.New("no authenticated user on context")
	}
	return u, nil
}

// ActorID returns a *uuid.UUID for the user, or nil when no user is present.
// Audit writers use this to populate audit_events.actor_user_id.
func ActorID(ctx context.Context) *uuid.UUID {
	u, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	id := u.ID
	return &id
}

// --- Module (integration API) principal --------------------------------------

// ModulePrincipal is attached by the bearer middleware when a mated module
// calls /api/v1/integrations/*. OrganizationID is the org that owns the
// module, so handlers can scope queries without an extra DB lookup.
type ModulePrincipal struct {
	ModuleID       uuid.UUID
	OrganizationID uuid.UUID
	Slug           string
	CredentialID   uuid.UUID
}

func WithModule(ctx context.Context, m *ModulePrincipal) context.Context {
	return context.WithValue(ctx, moduleKey, m)
}

func ModuleFromContext(ctx context.Context) (*ModulePrincipal, bool) {
	m, ok := ctx.Value(moduleKey).(*ModulePrincipal)
	if !ok || m == nil {
		return nil, false
	}
	return m, true
}
