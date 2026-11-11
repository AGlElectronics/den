# ADR 0007 — Den roles come from IdP groups

## Status

Accepted — 2026-11-11. Replaces the `role_assignments` idea in ADR 0001.

## Context

Den needs three kinds of people: members (use the portal), convention
admins (modules, pairing, branding, settings, audit log) and operations
staff (backups, downloads, deleting module databases). The first schema had
a `role_assignments` table to bind IdP groups to roles from inside Den, but
nothing ever read it: the backend has always mapped groups from environment
settings. A seeded `owner` user from the first migration was never demoted
by that mapping, so it would have stayed an owner in every installation.

Every other Con Services module already takes its roles from the `groups`
claim of the sign-in token, and HR assigns people to those groups. A second
place to bind groups to roles (a table edited in Den) would compete with HR.

## Decision

- Den roles come only from IdP groups named in the environment:
  `DEN_ADMIN_GROUPS` (default `den-admin`) → `admin`, `DEN_MEMBER_GROUPS`
  (default `den-member`; empty = everyone signed in) → `member`,
  `DEN_OPS_GROUPS` (default `den-ops`) → `ops`.
- `admin` and `member` are written to `organization_memberships` on every
  request, so removing someone from a group demotes them at once. `ops` is
  granted per request and never stored. A person can be `admin` and `ops`.
- `ops` is separate from `admin`: admins without `ops` see the backup status
  but cannot run, schedule or download backups, or delete a module's
  database.
- The role catalog (`GET /api/role-catalog`) publishes all three roles with
  their groups so HR can assign them.
- `role_assignments` is dropped and the seeded dev owner is deleted (new
  migrations; the first migration stays as it was). Nothing assigns `owner`;
  an existing `owner` row is left alone by the sync.
- `users.is_disabled` refuses every request with 403.
- Dev mode (`AUTH_MODE=dev`) is a single-person trial: every dev user is
  `admin` and `ops`.

## Consequences

- Changing who is an admin or may run backups is done in the IdP (or in HR),
  never in Den.
- Swapping IdPs means creating the same group names there.
- The group names are compared case-insensitively and without a leading `/`.
  The suite-wide rule (and a configurable groups claim) will come from the
  shared `groupmatch` package; Den's `auth.InAnyGroup` is the seam.
- Den stores each person's groups from their last sign-in (`users.groups`)
  to decide which modules they may use.
