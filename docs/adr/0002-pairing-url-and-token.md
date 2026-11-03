# ADR 0002 — Pairing URL + token

## Status

Accepted — 2026-11-02.

## Context

When an operator wants to connect Stash (or any other mated module) to a
Den instance, we need:

- Mutual verification of the Den URL the module is being pointed at.
- A way to prove to Den that this specific module instance is the one the
  operator intended, without the operator typing API keys into two UIs.
- Something short-lived so a leaked URL does not become a long-lived
  credential.
- Something auditable — the operator who initiated the pairing should
  appear in Den's audit log.

## Decision

- Operator opens Den's **Modules → New pairing** flow. Den creates a
  `pairing_tokens` row with `expires_at = now() + 15m` and `consumed_at
  = null`. Den stores only the **SHA-256 hash** of the raw token.
- Den displays exactly one copy of the full pairing URL:
  `https://den.example.org/api/v1/integrations/pairing/redeem?token=<opaque>`
  (shown once, with a copy button and a warning that the URL is secret).
- The operator pastes that URL into the mated module's "Pair with Den"
  screen. The module **POSTs** to the URL from its own backend (not from
  the browser) with a JSON body `{module_id, module_version,
  module_slug}`. The module may discover `module_id` from `/api/v1/me`
  on Den, or it can be provided by the pairing UI — up to the module.
- Den validates: token hash exists, not expired, not consumed, `module_id`
  (if provided) matches the row's `module_id`. On success, Den:
  1. Marks the token consumed.
  2. Mints a `module_credential` (opaque random string, 32 bytes b64url).
  3. Stores only the SHA-256 hash of the credential.
  4. Writes an `audit_events` row (`pairing.redeem`).
  5. Returns `{credential, module_id, organization_id, den_base_url}`.
- The module stores the credential in its own secret store and uses it as
  `Authorization: Bearer <credential>` for every subsequent call to
  `/api/v1/integrations/branding/...`.
- Credentials can be revoked by setting `revoked_at`. Revoked credentials
  are refused at the middleware.

## Consequences

- The pairing URL is as sensitive as the credential it eventually mints —
  the UI must warn the operator and never log it.
- We never give any module AWS/S3 credentials. The module reads branding
  bytes via Den's integration API, which authenticates with the module
  credential; see [0004-one-bucket-per-module.md](0004-one-bucket-per-module.md).
- Rotation = mint a new credential and revoke the old one. No UI pain
  because the module's own admin UI handles pairing again.
