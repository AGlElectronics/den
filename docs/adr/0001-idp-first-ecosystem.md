# ADR 0001 — IdP-first ecosystem

## Status

Accepted — 2026-11-02.

## Context

Den sits in the middle of a module ecosystem (Stash, Events, HR, …) that
every end-user also needs to log into. The naive design is to make Den the
identity provider: Den owns password hashes, forgot-password flows, MFA,
session cookies that other modules trust via some custom SSO.

That is the wrong shape for us:

- The organization almost certainly already runs a real IdP (Keycloak,
  Entra, Okta, Google Workspace, …) because their employees use dozens of
  other apps.
- Password reset, MFA, audit, session management, enterprise SSO
  attestation, SCIM provisioning — these are large, security-critical
  features. Building them in Den means re-implementing all of them.
- If Den is the IdP, Den is a single point of failure for *every* module
  the organization runs. A Den outage locks everyone out of Stash too.
- An org that has not yet set up Den must still be able to use Stash. If
  Stash depends on Den's identity, that is impossible.

## Decision

- Den is **not** the IdP. There is no password column anywhere.
- Each user has a `users` row and one or more `external_identities` rows
  that record `(issuer, subject)` from an external OIDC provider. Dev mode
  uses the synthetic issuer `dev:` so the shape is identical.
- `external_identities` is unique on `(issuer, subject)`, **not** on
  `subject` alone. Two IdPs can legitimately mint the same subject value.
- Each mated module (Stash, Events) also authenticates its human users
  against the same external IdP directly. The module does not call Den to
  verify tokens.
- Server-to-server, modules talk to Den over the **module credential**
  issued at pairing time. See [0002-pairing-url-and-token.md](0002-pairing-url-and-token.md).

## Consequences

- Den has an OIDC verifier (stubbed in the MVP; see
  `backend/internal/auth/oidc.go`) but no user-managed password UI.
- The organization can swap IdPs by changing `OIDC_ISSUER_URL` and seeding
  a new `role_assignments` binding — users will be re-matched on next
  login via `(new_issuer, same_group_name) → same role`.
- A Stash deployment can run with zero Den dependency. Branding, in that
  case, lives in Stash's own small `branding.json` instead of being
  fetched from Den.
