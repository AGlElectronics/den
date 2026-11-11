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
  and `module_slug` (if provided) match the token's module (409 otherwise;
  the token stays unused). A body that is not valid JSON, or has unknown
  fields, is refused with 400. On success, Den, in **one transaction** that
  locks the token row:
  1. Marks the token consumed (`... WHERE consumed_at IS NULL`, and checks
     that exactly one row changed), so concurrent redeems of one URL yield
     exactly one credential.
  2. Revokes every older credential of the module.
  3. Mints a `module_credential` (32 bytes b64url; with `DEN_CREDENTIAL_KEY`
     derived as HMAC-SHA256 of its id, so Den can present it to the module).
  4. Stores only the SHA-256 hash of the credential.
  5. Writes an `audit_events` row (`pairing.token.redeem`) with the admin who
     minted the token as the actor.
  6. Returns `{credential, credential_id, module_id, organization_id, den_base_url}`.
- The module stores the credential in its own secret store and uses it as
  `Authorization: Bearer <credential>` for every subsequent call to
  `/api/v1/integrations/branding/...`.
- Credentials can be revoked by setting `revoked_at`, by an admin in
  Modules → Credentials (`DELETE /api/v1/modules/{id}/credentials[/{credentialID}]`).
  Revoked credentials are refused at the middleware (401). A module left
  without an active credential is `unpaired`.
- Pairing is always a manual admin action. Den never re-pairs a module by
  itself, and a module that gets 401 must tell its admin to pair again.

## Consequences

- The pairing URL is as sensitive as the credential it eventually mints —
  the UI must warn the operator and never log it.
- We never give any module AWS/S3 credentials. The module reads branding
  bytes via Den's integration API, which authenticates with the module
  credential; see [0004-one-bucket-per-module.md](0004-one-bucket-per-module.md).
- Rotation = pair again: the new credential replaces every older one in
  the same transaction. No UI pain because the module's own admin UI
  handles pairing again.
- Token minting and redeem are audited; the redeem endpoint is
  rate-limited per client IP (10 a minute).
