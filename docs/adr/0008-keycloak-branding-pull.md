# ADR 0008 — The Keycloak sign-in page pulls the brand from Den

## Status

Accepted — 2026-11-11.

## Context

Den used to push the brand into the Keycloak realm: the login theme name,
the display name and a `conservicesBrand` realm attribute, which the
`conservices` login theme rendered. That needs a service account with the
`realm-management` role `manage-realm`, which can change every setting of
the realm (flows, clients, identity providers). If Den were compromised, so
would be the realm.

In identity mode `existing` (see `docs/proposals/existing-identity-provider.md`
in con-services) the realm belongs to the convention. Den must not change it
at all.

## Decision

- The `conservices` login theme **pulls** the brand: its script fetches
  `GET <Den>/api/v1/public/brand` in the browser and applies the name,
  tagline, colours, logo, favicon, background, layout and caption. Den's
  address comes from the theme property `denPublicUrl`, which reads the
  Keycloak environment variable `DEN_PUBLIC_URL`. The last answer is kept in
  the browser's local storage so the next page load is branded at once.
- `/api/v1/public/*` answers with `Access-Control-Allow-Origin: *` (no
  credentials), so the sign-in page on another origin may read it. It is
  public and rate-limited anyway.
- Den no longer writes the login theme or the brand attribute. The login
  theme is chosen by the realm import (`"loginTheme": "conservices"`).
- The one thing a browser cannot fetch is the **realm display name**, which
  Keycloak puts into its own e-mails and page titles. Den still keeps it in
  line with the organisation name when a Keycloak admin client is configured
  (`KEYCLOAK_ADMIN_URL`, `KEYCLOAK_CLIENT_SECRET`). Keycloak has no role
  smaller than `manage-realm` that allows changing it, so that grant stays
  for this optional sync; `view-realm` is dropped. Without the client the
  sign-in page is branded all the same and e-mails use the display name from
  the realm import.
- With `IDENTITY_STAFF_MODE=existing` the sync is always off and the
  Keycloak card on the Branding page says why.

## Consequences

- A deployment that does not need the organisation name in Keycloak's
  e-mails can delete the `den-keycloak` client and its secret.
- The sign-in page shows Keycloak's neutral look for a moment on the very
  first visit (before the fetch answers); after that the cached brand is
  applied immediately.
- If Den is down, the sign-in page falls back to the cached brand, then to
  the neutral look. Signing in keeps working.
- Den's backend no longer needs to reach Keycloak's admin API unless the
  display-name sync is on.
