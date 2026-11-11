# Den — Domain Model

Den is deliberately small. It only owns the data no other module can
reasonably own:

- **Organization identity**: who are we as a brand.
- **Who belongs to the organization**: users and the IdP identities they
  present (OIDC `iss` + `sub`), plus organization-level memberships.
- **Which modules the organization runs**: a registry of mated modules
  (Stash, Events, HR, …), their base URLs, and whether they are currently
  enabled / healthy.
- **How a mated module authenticates back to Den**: pairing tokens that are
  redeemed once for a long-lived module credential.
- **Branding payload the modules read**: logo variants, colors, typography,
  theme name, revision number.
- **Audit trail** of the above changes.

Den does **not** own inventory items, event programs, HR records, or
anything that belongs inside a mated module. Those stay in that module's
own database.

## Entities

### `organizations`

The organization is the top-level tenant. **A Den serves exactly one
organisation**: the row seeded by the first migration
(`00000000-0000-0000-0000-00000000d001`). Every table is still scoped by
`organization_id` and every query filters on it, so a row that belongs to
another organisation can never be read or changed by accident, but there is
no way to create or switch organisations. A second convention runs its own
Den.

| column         | type        | notes                                        |
| -------------- | ----------- | -------------------------------------------- |
| `id`           | uuid pk     | `gen_random_uuid()`                          |
| `slug`         | text unique | url-safe short name, e.g. `conservices`      |
| `display_name` | text        | human-friendly, e.g. `Con Services`          |
| `legal_name`   | text        | legal entity name, optional                  |
| `website_url`  | text        | optional                                     |
| `created_at`   | timestamptz |                                              |
| `updated_at`   | timestamptz |                                              |

### `users`

A user is a human (or service account) that can sign in to Den's UI. We do
**not** mix in external identities inline — those get their own table so a
user can own multiple logins (OIDC, SAML, …).

| column         | type        | notes                           |
| -------------- | ----------- | ------------------------------- |
| `id`           | uuid pk     |                                 |
| `email`        | text        | may be null for service users   |
| `display_name` | text        |                                 |
| `is_disabled`  | bool        | `true` refuses every request (403) |
| `groups`       | text[]      | IdP groups from the last sign-in; decides which modules the person may use |
| `last_login_at`| timestamptz |                                 |
| `created_at`   | timestamptz |                                 |
| `updated_at`   | timestamptz |                                 |

### `external_identities`

One row per IdP identity. `(issuer, subject)` is unique — never `subject`
alone, because the same `sub` from two different issuers must not collide.
Dev auth uses the synthetic issuer `dev:` and the dev user name as subject.

| column        | type        | notes                                   |
| ------------- | ----------- | --------------------------------------- |
| `id`          | uuid pk     |                                         |
| `user_id`     | uuid fk     | `users(id)` cascade                     |
| `issuer`      | text        | OIDC `iss` or `dev:` for dev auth       |
| `subject`     | text        | OIDC `sub` or dev user name             |
| `email`       | text        | snapshot at last login                  |
| `created_at`  | timestamptz |                                         |
| `updated_at`  | timestamptz |                                         |

Unique constraint: `UNIQUE (issuer, subject)`.

### `organization_memberships`

Attaches a user to an organization. The role string is granted at the
organization level (not at the module level — mated modules have their
own, finer RBAC). The row is rewritten from the person's IdP groups on every
request (`DEN_ADMIN_GROUPS` → `admin`, `DEN_MEMBER_GROUPS` → `member`); see
[ADR 0007](adr/0007-roles-from-idp-groups.md). Nothing assigns `owner` any
more; an `owner` row is never changed by the sync. The `ops` role
(`DEN_OPS_GROUPS`) is granted per request and never stored. Dev users are
provisioned as `admin` (with `ops`) on first request.

| column           | type        | notes                                      |
| ---------------- | ----------- | ------------------------------------------ |
| `id`             | uuid pk     |                                            |
| `organization_id`| uuid fk     | `organizations(id)` cascade                |
| `user_id`        | uuid fk     | `users(id)` cascade                        |
| `role`           | text        | `owner`, `admin`, `member`                 |
| `created_at`     | timestamptz |                                            |
| `updated_at`     | timestamptz |                                            |

Unique: `(organization_id, user_id)`.

### `modules`

The registry of mated modules. One row per `(org, slug, environment)` so an
org can run both `stash` and `stash-staging`.

| column          | type        | notes                                                               |
| --------------- | ----------- | ------------------------------------------------------------------- |
| `id`            | uuid pk     |                                                                     |
| `organization_id` | uuid fk   |                                                                     |
| `slug`          | text        | `stash`, `events`, `hr`, …                                          |
| `display_name`  | text        | `Stash (Production)`                                                |
| `base_url`      | text        | where the module lives; shown in launcher. https (http only with `APP_ENV=development`) |
| `internal_url`  | text        | where Den calls the module (role catalog, to-dos); empty = `base_url` |
| `environment`   | text        | `production`, `staging`, `local` (free-form)                        |
| `enabled`       | bool        | hide/show in launcher without deleting                              |
| `status`        | text        | `unpaired`, `paired`, `degraded`, `disabled`; `unpaired` again when every credential is revoked |
| `portal_visibility` | text    | `role_holders` (people with a group from the module's role catalog) or `everyone` |
| `manifest`      | jsonb       | module-reported `{version, capabilities, logo_url, …}`              |
| `last_seen_at`  | timestamptz | last heartbeat / branding fetch                                     |
| `database_name` | text        | set **only** when Den created the module's database; only then can Den drop it |
| `created_at`    | timestamptz |                                                                     |
| `updated_at`    | timestamptz |                                                                     |

Unique: `(organization_id, slug, environment)`.

### `pairing_tokens`

Short-lived, single-use tokens issued when an operator starts a pairing
flow. The displayed pairing URL carries the raw token; only the SHA-256
hash is stored. Expired or consumed tokens are kept for audit.

| column         | type        | notes                                           |
| -------------- | ----------- | ----------------------------------------------- |
| `id`           | uuid pk     |                                                 |
| `module_id`    | uuid fk     | which module the token pairs                    |
| `token_hash`   | text        | hex sha-256 of the raw token (never stored raw) |
| `expires_at`   | timestamptz | typically now()+15m                             |
| `consumed_at`  | timestamptz | null until redeemed                             |
| `created_by`   | uuid fk     | `users(id)`                                     |
| `created_at`   | timestamptz |                                                 |

Unique: `(token_hash)`.

### `module_credentials`

The long-lived credential issued after a successful pairing. Stored as a
hashed value only; the plain credential is handed to the module once at
redemption time. With `DEN_CREDENTIAL_KEY` set the credential is
`HMAC-SHA256(key, id)`, so Den can recompute it to call the module (portal
to-dos) while the table still holds only the hash. Pairing again revokes
every older credential of the module in the same transaction; admins can
revoke credentials by hand.

| column            | type        | notes                                           |
| ----------------- | ----------- | ----------------------------------------------- |
| `id`              | uuid pk     |                                                 |
| `module_id`       | uuid fk     |                                                 |
| `credential_hash` | text        | hex sha-256 of the raw credential               |
| `label`           | text        | optional operator label                         |
| `last_used_at`    | timestamptz |                                                 |
| `revoked_at`      | timestamptz | null until revoked                              |
| `created_at`      | timestamptz |                                                 |

Unique: `(credential_hash)`.

### `branding_profiles`

Theme fields the modules read. Every save bumps `revision` and keeps the
previous row for simple history. A `GET branding/profile` always returns
the latest non-null row for the org.

| column         | type        | notes                                                    |
| -------------- | ----------- | -------------------------------------------------------- |
| `id`           | uuid pk     |                                                          |
| `organization_id` | uuid fk  |                                                          |
| `revision`     | int         | monotonically increasing per org                         |
| `theme`        | jsonb       | `{primary, primaryHover, accent, background, text, …}`   |
| `typography`   | jsonb       | `{headingFont, bodyFont}`                                |
| `copy`         | jsonb       | `{tagline, legalFooter}`                                 |
| `created_by`   | uuid fk     | `users(id)`                                              |
| `created_at`   | timestamptz |                                                          |

Unique: `(organization_id, revision)`.

### `branding_assets`

Logo and image assets. Bytes live in Den's own bucket (`den-data-dev` in
dev). The `role` names the semantic use: `logo`, `favicon` or
`login-background` (nothing else is accepted). `content_type` is detected
from the file's bytes: PNG, JPEG, WebP, ICO or SVG. Modules **do not** receive bucket credentials;
they fetch via `GET /api/v1/integrations/branding/assets/{id}` with their
pairing credential and Den streams the bytes back.

| column           | type        | notes                                      |
| ---------------- | ----------- | ------------------------------------------ |
| `id`             | uuid pk     |                                            |
| `organization_id`| uuid fk     |                                            |
| `role`           | text        | `logo`, `favicon`, `login-background`      |
| `storage_bucket` | text        | denormalized so we can migrate buckets     |
| `storage_key`    | text        |                                            |
| `content_type`   | text        |                                            |
| `size_bytes`     | bigint      |                                            |
| `uploaded_by`    | uuid fk     | `users(id)`                                |
| `created_at`     | timestamptz |                                            |

Unique: `(storage_bucket, storage_key)`.

### `audit_events`

Flat log of changes: who did what, to which entity, before/after JSON.
Written by `backend/internal/audit` in the same transaction as the change
(operator actions, which change no Den row, right after the operator
answered). Admins read it with `GET /api/v1/audit-events` and on the
**Audit log** page. Secrets (credentials, tokens, the SMTP password) are
never written into it.

| column          | type        | notes                              |
| --------------- | ----------- | ---------------------------------- |
| `id`            | uuid pk     |                                    |
| `organization_id`| uuid       | nullable (system-level events)     |
| `actor_user_id` | uuid fk     | nullable (system-triggered events); for a redeem, the admin who minted the token |
| `action`        | text        | see the list below                 |
| `entity_type`   | text        | `module`, `branding_profile`, `branding_asset`, `organization`, `backup` |
| `entity_id`     | uuid        | the organisation id for `organization` and `backup` events |
| `before_json`   | jsonb       | nullable                           |
| `after_json`    | jsonb       | nullable                           |
| `note`          | text        | optional                           |
| `created_at`    | timestamptz |                                    |

Actions: `module.create`, `module.update`, `module.delete`,
`pairing.token.create`, `pairing.token.redeem`, `module.credential.revoke`,
`module.credentials.revoke_all`, `branding.profile.update`,
`branding.asset.create`, `branding.asset.delete`, `org_settings.update`,
`module_settings.update`, `keycloak.sync`, `operations.backup.start`,
`operations.schedule.update`, `operations.download_link.create`,
`operations.backup.download`.

### `organization_settings`

One JSON document per organisation (the **Organisation** page): known keys
are validated, unknown keys are kept and passed to paired modules.

| column            | type        | notes                                  |
| ----------------- | ----------- | -------------------------------------- |
| `organization_id` | uuid pk fk  |                                        |
| `data`            | jsonb       | e.g. `{"organization_name": "…", "org_email_domain": "example.org"}` |
| `updated_at`      | timestamptz |                                        |
| `updated_by`      | text        | user id of the last editor             |

### `module_settings`

The **Module settings** page: what Den hands to paired modules in the
integration bundle (retention, SMTP, Finance seller details, Stash Lost &
Found, Rally submission site, identity links). Each module receives the
shared sections and only its own.

| column            | type        | notes                                  |
| ----------------- | ----------- | -------------------------------------- |
| `organization_id` | uuid pk fk  |                                        |
| `data`            | jsonb       | the sections, without secrets          |
| `secrets`         | jsonb       | `{"smtp_password": "…"}`; never returned by the admin API |
| `updated_at`      | timestamptz |                                        |
| `updated_by`      | uuid fk     | `users(id)`, set null on delete        |

### `user_preferences`

Per-person UI preferences shared by every app, keyed by the IdP identity
(not `users.id`) so a person who only ever opened a module still has one.
Paired modules may only read and write them for their own users (people who
signed in to Den and may use the module).

| column       | type        | notes                                  |
| ------------ | ----------- | -------------------------------------- |
| `issuer`     | text pk     |                                        |
| `subject`    | text pk     |                                        |
| `theme`      | text        | `system`, `light` or `dark`            |
| `updated_at` | timestamptz |                                        |

### `backup_download_links`

One-time links for downloading a backup (Backups → Download). In the
database rather than in memory so any Den replica can serve them.

| column       | type        | notes                                  |
| ------------ | ----------- | -------------------------------------- |
| `token_hash` | text pk     | SHA-256 of the token in the link       |
| `backup_key` | text        | object key in the backup bucket        |
| `expires_at` | timestamptz | five minutes after creation            |
| `used_at`    | timestamptz | set on the one download                |
| `created_by` | uuid fk     | the ops user who asked for the link    |
| `created_at` | timestamptz | rows are cleaned up a day after expiry |
