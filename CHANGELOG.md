# Changelog

All notable changes to Den are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

First release, `1.0.0`. Released versions are tagged `vMAJOR.MINOR.PATCH`;
the images `ghcr.io/aglelectronics/den-backend`, `den-frontend` and
`den-operator` are published as `MAJOR.MINOR.PATCH-BUILD`, where `BUILD`
counts rebuilds of the same version. Rebuilds get no entry here. Den's
MAJOR is the MAJOR every module is built for
([Versions and upgrades](https://aglelectronics.github.io/con-services/operations/versioning/)).

### Added

- Portal home for members and a separate admin console for admins and
  owners, with convention branding: colours, fonts, wording, logo, favicon.
- Pairing: one-time pairing URLs that give a module a credential to read
  Den's brand and settings.
- `DEN_PREREGISTER_MODULES` lists modules (JSON) that Den registers as
  unpaired at startup, so an admin only has to click Pair. Existing modules
  are never changed.
- Organisation settings, a JSON document paired modules read and show as
  "Managed in Den", including `wiki_public_links`, and a public brand
  bundle for sign-in screens that can be read from other origins.
- The Keycloak sign-in page follows Den's brand: the `conservices` login
  theme fetches it from Den's public brand endpoint in the browser (set
  `DEN_PUBLIC_URL` on Keycloak), so Den needs no write access to the realm
  for it. Optionally Den keeps the realm's display name (used in Keycloak's
  e-mails) equal to the organisation name, shown with "Sync name now" on
  the Branding page; see
  [ADR 0008](docs/adr/0008-keycloak-branding-pull.md).
- `IDENTITY_STAFF_MODE` (`own`, `brokered` or `existing`, default `own`):
  with `existing` Den never writes to the realm and the Branding page says
  why the name sync is off.
- `ops` role for backups and database deletion (`DEN_OPS_GROUPS`, default
  `den-ops`), published in the role catalog next to `admin` and `member`.
  Running, scheduling and downloading backups and deleting a module's
  database need it; admins without it see the backup status only. See
  [ADR 0007](docs/adr/0007-roles-from-idp-groups.md).
- Modules → **Credentials**: list a module's pairing credentials and revoke
  one or all of them (`GET/DELETE /api/v1/modules/{id}/credentials`,
  `DELETE /api/v1/modules/{id}/credentials/{credentialID}`). A module
  without an active credential becomes unpaired; pairing it again is always
  a manual admin action.
- Modules → **Edit**: name, address, server address (`internal_url`, where
  Den calls the module), environment, on/off, and who sees the module on the
  portal (`portal_visibility`: people with a role in it, or every member).
- **Audit log** page and `GET /api/v1/audit-events` (filters, paging). Den
  records module, pairing, credential, branding, organisation and module
  settings changes, Keycloak syncs, backups, schedule changes and backup
  downloads, each with the person who did it.
- **Module settings** page (`GET/PUT /api/v1/module-settings`): retention
  periods (marked pending legal review), SMTP (the password is write-only),
  Finance seller details, Stash Lost & Found retention, the Rally
  submission-site host and identity links (account console, sign-in name,
  identity provider admin console). Paired modules receive them in the
  integration bundle (`module_settings`) or from
  `GET /api/v1/integrations/module-settings`, each module only the parts it
  uses. The admin menu links to the identity provider console from here.
- Portal to-dos come from Den's backend (`GET /api/v1/portal/todos`): Den
  asks each module the person may use for `GET /api/portal/todos` with the
  module's pairing credential and the person's issuer and subject
  (`X-Den-User-Issuer`, `X-Den-User-Subject`). Needs `DEN_CREDENTIAL_KEY`;
  modules paired before the key was set must be paired again.
- `den-server healthcheck` and `den-operator healthcheck` probe the running
  process's `/livez`, for Docker Compose healthchecks.
- `DEN_PREREGISTER_MODULES` entries accept `internal_url`.
- The backend can run as several replicas: `/den-migrate` holds a Postgres
  advisory lock, so migrate initContainers starting together wait for each
  other, and backup download links live in the database.
- Helm chart `deploy/charts/den` (still `1.0.0`, unreleased): `backend.replicas`
  may be raised (rolling updates instead of `Recreate`), new settings
  `DEN_OPS_GROUPS` and `IDENTITY_STAFF_MODE` in `config`,
  `DEN_CREDENTIAL_KEY` as an optional key of `existingSecret`, and
  `networkPolicy.allowToModules` (default on) lets den-backend reach module
  backends on 8080 for role catalogs and to-dos.
- Sign-in background picture, with a panel or centred layout and an optional
  caption.
- User preferences (light, dark or system theme), shared with paired
  modules.
- Den operator: creates and drops module databases, with a backup before
  every drop, and makes a daily encrypted backup of every database, the
  logins and the module buckets. Backups page with status, schedule,
  retention, "Back up now" and downloads.
- `MODULE_DB_PARAMS` (operator, optional): the connection settings written
  into the database URLs Den shows for new modules, for example
  `sslmode=verify-full&sslrootcert=/etc/conservices-internal-ca/ca.crt`.
  By default the operator copies them from `PG_ADMIN_URL`.
- Den's backend and operator can serve HTTPS: set `TLS_CERT_FILE` and
  `TLS_KEY_FILE` to a certificate and key. Renewed certificates are picked
  up within a minute, without a restart. Without these settings they serve
  plain HTTP. On Kubernetes, the manifests and the Helm chart turn this on
  with certificates from the internal certificate authority; see
  [Internal TLS](https://aglelectronics.github.io/con-services/operations/internal-tls/)
  and [ADR 0006](docs/adr/0006-internal-tls.md).
- `TRUSTED_PROXY_CIDRS` (optional): comma-separated CIDRs of the proxies in
  front of the backend whose `X-Forwarded-For` entries are believed. The
  default, `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,::1/128,fc00::/7`,
  covers the Gateway, nginx and Caddy; change it only for a proxy outside
  the private ranges, or set `none` when no proxy is in front. See
  [Client IP addresses](https://aglelectronics.github.io/con-services/operations/production/#client-ip-addresses).
- The web app reads its installation settings (sign-in mode, issuer, client
  id, scopes, API address) from `/config.json` when it starts, so the
  published `den-frontend` image works for every installation. nginx serves
  the file from `/etc/conservices/config.json` with `Cache-Control: no-store`,
  so a change shows on the next page load. Without a mounted file the web app
  shows "Not configured yet"; a broken file gives a clear error page. See
  [Web app settings](https://aglelectronics.github.io/con-services/modules/den/configuration/#web-app-settings-configjson).
- Kubernetes: Den runs in the namespace `conservices-den`, with migrations
  in an initContainer (`/den-migrate` from the `den-backend` image),
  `/livez` and `/readyz` probes, and a NetworkPolicy that lets only Den's
  backend reach the operator. Helm chart `deploy/charts/den` `1.0.0` in
  `con-services`, released with the tag `den/v1.0.0`. See the
  [documentation](https://aglelectronics.github.io/con-services/getting-started/den-quickstart/).
- Development: `infra/docker-compose.yml` with SeaweedFS as the S3 server
  (`localhost:9010`, bucket `den-data-dev`), and
  `infra/frontend-config/compose.json` (dev sign-in) mounted as the web
  app's `/config.json`; set `FRONTEND_CONFIG` in `.env` to use another
  file. Building from source needs Go 1.27.3 or newer.
- CI on every pull request (tests, lint, Trivy scans) and signed images with
  an SBOM published to GHCR on version tags (linux/amd64; linux/arm64
  optional).

### Security

- A pairing URL gives exactly one credential, also when it is redeemed
  twice at the same moment. `module_id` and `module_slug` sent by the module
  must match the module the URL was made for. Pairing again revokes every
  older credential of the module at once. Runbook for a leaked pairing URL
  or credential:
  [Disaster recovery](https://aglelectronics.github.io/con-services/operations/disaster-recovery/).
- Rate limits per client IP: 10 pairing redeems, 120 public brand requests
  and 10 backup downloads a minute (`429` with `Retry-After`).
- Branding uploads accept only the roles `logo`, `favicon` and
  `login-background` and only PNG, JPEG, WebP, ICO and SVG files, recognised
  from their content. Assets are served with `X-Content-Type-Options:
  nosniff`; SVG is served as a download with a sandbox policy.
- Module addresses must be `https` (`http` only with
  `APP_ENV=development`), and slugs are checked, in the API and in
  `DEN_PREREGISTER_MODULES`.
- Den deletes a module's database only when Den created it.
- Members see only the modules they may use and no organisation-wide
  counts. Paired modules can read or change preferences only of their own
  users.
- Disabled accounts (`users.is_disabled`) are refused. The seeded dev owner
  account is removed by a migration, and the unused `role_assignments`
  table is dropped.
- One-time backup download links are stored in the database and work once,
  on any replica.
- The browser no longer sends the person's token to modules for to-dos;
  Den calls the modules itself.
- The backend reads the client address from `X-Forwarded-For` only when
  the connection comes from a trusted proxy (`TRUSTED_PROXY_CIDRS`), and
  ignores `X-Real-IP` and `True-Client-IP`, so rate limits cannot be
  bypassed with a fake header.
- On Kubernetes, every connection Den makes or accepts inside the cluster
  is encrypted and the other side's certificate is checked: PostgreSQL,
  S3, Keycloak, the operator, and modules calling Den.
- No image runs as root. The web app runs on
  `nginxinc/nginx-unprivileged:1.30.6-alpine` as UID 101 and listens on
  port 8080. The `den-operator` image is `alpine:3.24.4` with only the
  PostgreSQL 16 client tools and `age`, running as `postgres` (UID 70).
  All images are built with Go 1.27.3 and pass the CI image scan (Trivy,
  HIGH and CRITICAL issues that have a fix).
- A production build never falls back to dev sign-in: a `/config.json`
  without `authMode` means OIDC.
- The admin role follows Keycloak groups on every request; the sign-in
  session stays in sync across browser tabs, and an expired session sends
  you back to sign-in.
