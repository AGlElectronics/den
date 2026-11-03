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

The organization is the top-level tenant. Everything else is scoped to an
organization. For single-tenant deployments there will typically be exactly
one row.

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
| `is_disabled`  | bool        |                                 |
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
own, finer RBAC).

| column           | type        | notes                                      |
| ---------------- | ----------- | ------------------------------------------ |
| `id`             | uuid pk     |                                            |
| `organization_id`| uuid fk     | `organizations(id)` cascade                |
| `user_id`        | uuid fk     | `users(id)` cascade                        |
| `role`           | text        | `owner`, `admin`, `member`                 |
| `created_at`     | timestamptz |                                            |
| `updated_at`     | timestamptz |                                            |

Unique: `(organization_id, user_id)`.

### `role_assignments`

Lets us bind an OIDC group (claim `groups[]`) to an organization role,
without hard-coding anything in the backend. Dev users bypass this and are
provisioned as `admin` for the default org on first login.

| column           | type        | notes                                           |
| ---------------- | ----------- | ----------------------------------------------- |
| `id`             | uuid pk     |                                                 |
| `organization_id`| uuid fk     |                                                 |
| `issuer`         | text        | OIDC issuer to scope the binding                |
| `group`          | text        | OIDC group claim value                          |
| `role`           | text        | `owner`, `admin`, `member`                      |
| `created_at`     | timestamptz |                                                 |
| `updated_at`     | timestamptz |                                                 |

### `modules`

The registry of mated modules. One row per `(org, slug, environment)` so an
org can run both `stash` and `stash-staging`.

| column          | type        | notes                                                               |
| --------------- | ----------- | ------------------------------------------------------------------- |
| `id`            | uuid pk     |                                                                     |
| `organization_id` | uuid fk   |                                                                     |
| `slug`          | text        | `stash`, `events`, `hr`, …                                          |
| `display_name`  | text        | `Stash (Production)`                                                |
| `base_url`      | text        | where the module lives; shown in launcher                           |
| `environment`   | text        | `production`, `staging`, `local` (free-form)                        |
| `enabled`       | bool        | hide/show in launcher without deleting                              |
| `status`        | text        | `unpaired`, `paired`, `degraded`, `disabled` (reported by module)   |
| `manifest`      | jsonb       | module-reported `{version, capabilities, logo_url, …}`              |
| `last_seen_at`  | timestamptz | last heartbeat / branding fetch                                     |
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
redemption time.

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
dev). The `role` names the semantic use (`logo-primary`, `logo-dark`,
`favicon`, `og-image`). Modules **do not** receive bucket credentials;
they fetch via `GET /api/v1/integrations/branding/assets/{id}` with their
pairing credential and Den streams the bytes back.

| column           | type        | notes                                      |
| ---------------- | ----------- | ------------------------------------------ |
| `id`             | uuid pk     |                                            |
| `organization_id`| uuid fk     |                                            |
| `role`           | text        | `logo-primary`, `logo-dark`, `favicon`, …  |
| `storage_bucket` | text        | denormalized so we can migrate buckets     |
| `storage_key`    | text        |                                            |
| `content_type`   | text        |                                            |
| `size_bytes`     | bigint      |                                            |
| `uploaded_by`    | uuid fk     | `users(id)`                                |
| `created_at`     | timestamptz |                                            |

Unique: `(storage_bucket, storage_key)`.

### `audit_events`

Flat log of changes: who did what, to which entity, before/after JSON. Used
for forensics and the admin UI's recent-activity feed.

| column          | type        | notes                              |
| --------------- | ----------- | ---------------------------------- |
| `id`            | uuid pk     |                                    |
| `organization_id`| uuid       | nullable (system-level events)     |
| `actor_user_id` | uuid fk     | nullable (system-triggered events) |
| `action`        | text        | `module.create`, `pairing.redeem`, `branding.update`, … |
| `entity_type`   | text        |                                    |
| `entity_id`     | uuid        |                                    |
| `before_json`   | jsonb       | nullable                           |
| `after_json`    | jsonb       | nullable                           |
| `note`          | text        | optional                           |
| `created_at`    | timestamptz |                                    |
