# Changelog

All notable changes to Den are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

Released versions are tagged `MAJOR.MINOR.PATCH`; the images
`ghcr.io/aglelectronics/den-backend`, `den-frontend` and `den-operator` are
published as `MAJOR.MINOR.PATCH-BUILD`, where `BUILD` counts rebuilds of the
same version. Rebuilds get no entry here.

### Added

- `DEN_PREREGISTER_MODULES` lists modules (JSON) that Den registers as
  unpaired at startup, so an admin only has to click Pair. Existing modules
  are never changed.
- `TRUSTED_PROXY_CIDRS` (optional): comma-separated CIDRs of the proxies in
  front of the backend whose `X-Forwarded-For` entries are believed. The
  default, `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,::1/128,fc00::/7`,
  covers the Gateway, nginx and Caddy; change it only for a proxy outside
  the private ranges, or set `none` when no proxy is in front. See
  [Client IP addresses](https://aglelectronics.github.io/con-services/operations/production/#client-ip-addresses).
- CI on every pull request (tests, lint, Trivy scans) and signed images with an SBOM published to GHCR on version tags (linux/amd64; linux/arm64 optional).
- Den's backend and operator can serve HTTPS: set `TLS_CERT_FILE` and
  `TLS_KEY_FILE` to a certificate and key. Renewed certificates are picked
  up within a minute, without a restart. Without these settings they serve
  plain HTTP as before. On Kubernetes, the manifests turn this on with
  certificates from the internal certificate authority; see
  [Internal TLS](https://aglelectronics.github.io/con-services/operations/internal-tls/)
  and [ADR 0006](docs/adr/0006-internal-tls.md).
- New operator setting `MODULE_DB_PARAMS`: the connection settings written
  into the database URLs Den shows for new modules, for example
  `sslmode=verify-full&sslrootcert=/etc/conservices-internal-ca/ca.crt`.
  By default the operator copies them from `PG_ADMIN_URL`.
- Kubernetes deployment: Den runs in the namespace `conservices-den`, with
  migrations in an initContainer (`/den-migrate` from the `den-backend`
  image), `/livez` and `/readyz` probes, and a NetworkPolicy that lets only
  Den's backend reach the operator. See the
  [documentation](https://aglelectronics.github.io/con-services/getting-started/den-quickstart/).

- Portal home for members and a separate admin console for admins and
  owners, with convention branding: colours, fonts, wording, logo, favicon.
- Pairing: one-time pairing URLs that give a module a credential to read
  Den's brand and settings.
- Organisation settings, a JSON document paired modules read and show as
  "Managed in Den", and a public brand bundle for sign-in screens.
- The public brand can be read from other origins.
- Den pushes its branding into the Keycloak realm (login theme, display name
  and the `conservicesBrand` attribute), with status and "Sync now" on the
  Branding page.
- Sign-in background picture, with a panel or centred layout and an optional
  caption.
- Den operator: creates and drops module databases, with a backup before
  every drop, and makes a daily encrypted backup of every database, the
  logins and the module buckets. Backups page with status, schedule,
  retention, "Back up now" and downloads.
- User preferences (light, dark or system theme), shared with paired
  modules.
- Organisation setting `wiki_public_links`.
- The web app reads its installation settings (sign-in mode, issuer, client
  id, scopes, API address) from `/config.json` when it starts, so
  the published `den-frontend` image works for every installation. nginx serves the
  file from `/etc/conservices/config.json` with `Cache-Control: no-store`,
  so a change shows on the next page load. Without a mounted file the web app
  shows "Not configured yet"; a broken file gives a clear error page. See
  [Web app settings](https://aglelectronics.github.io/con-services/modules/den/configuration/#web-app-settings-configjson).
- `infra/docker-compose.yml` mounts `infra/frontend-config/compose.json`
  (dev sign-in) as the web app's `/config.json`. Set `FRONTEND_CONFIG` in
  `.env` to use another file, for example one with real sign-in.

### Changed

- The development S3 server in `infra/docker-compose.yml` is now SeaweedFS 4.52 (`chrislusf/seaweedfs:4.52`, service `s3`, container `den-s3`) instead of MinIO, which no longer publishes community images. It still listens on `localhost:9010` with the same dev key and creates the `den-data-dev` bucket at start; there is no init job and no web console (port 9011 is gone). The backend waits until `s3` is healthy and defaults `S3_ENDPOINT` to `http://s3:9000`. To clean up, run `docker compose -f infra/docker-compose.yml up -d --remove-orphans` and remove the old MinIO volume with `docker volume rm den_minio-data` (copy data you need first). Production is unchanged: use a managed S3 service. Presigned links are rewritten to `localhost` only when the storage host is exactly `s3` or `minio` and `S3_PUBLIC_BASE_URL` is unset (before: any host containing `minio`).
- Database URLs Den shows for new modules take their connection settings
  from the operator's `PG_ADMIN_URL` (or `MODULE_DB_PARAMS`) instead of
  always ending in `sslmode=disable`.
- **BREAKING:** The web app image (`den-frontend`) now runs nginx as an unprivileged user (UID 101) and listens on port **8080** inside the container instead of 80. If you run it with Docker Compose or behind your own proxy, change the target from `den-frontend:80` to `den-frontend:8080`; published host ports stay the same. The Helm chart and
  the Kubernetes manifests already use port 8080 and need no change.
- The backend is built with Go 1.27.3 (was Go 1.25). Building from source
  needs Go 1.27.3 or newer; the `go` command downloads it automatically.
- **BREAKING:** `infra/Dockerfile.frontend` no longer takes `VITE_AUTH_MODE`, `VITE_OIDC_AUTHORITY`, `VITE_OIDC_CLIENT_ID`, `VITE_OIDC_SCOPE` and `VITE_API_BASE_URL`
  build arguments. If you built your own frontend image to get your sign-in
  address in, use the published image instead and put the same values in its
  `/config.json` (Helm: `frontend.config`, filled in from
  `config.OIDC_ISSUER_URL` and `config.OIDC_CLIENT_ID` when left out). The
  `VITE_*` variables now only configure the dev server (`npm run dev`).

### Security

- The backend no longer believes client-supplied `X-Forwarded-For`,
  `X-Real-IP` or `True-Client-IP` headers when working out the client
  address: `X-Forwarded-For` is only read when the connection comes from a
  trusted proxy (`TRUSTED_PROXY_CIDRS`), and the other two are ignored.
- On Kubernetes, every connection Den makes or accepts inside the cluster
  is encrypted and the other side's certificate is checked: PostgreSQL,
  MinIO, Keycloak, the operator, and modules calling Den.
- All images are rebuilt with Go 1.27.3 and updated Go libraries
  (`go-chi/chi/v5` 5.5.0, `go-jose/v4` 4.1.9, `klauspost/compress` 1.20.3, `golang.org/x/crypto` 0.58.0, `golang.org/x/net` 0.62.0 and `golang.org/x/text` 0.43.0), fixing known vulnerabilities in
  the Go standard library and in these libraries. The CI image scan (Trivy,
  HIGH and CRITICAL issues that have a fix) passes for every image.
- The web app image is built on `nginxinc/nginx-unprivileged:1.30.6-alpine`
  with current Alpine packages (fixes for musl, zlib, curl, expat and
  brotli) and no longer runs as root. CI now rejects a web app image that
  runs as root.
- The `den-operator` image is now `alpine:3.24.4` with only the PostgreSQL
  16 client tools (`pg_dump`, `pg_dumpall`, `psql`, `pg_restore`) and
  `age`, instead of the full `postgres:16-alpine` server image. This
  removes the bundled `gosu` binary and its 26 HIGH and CRITICAL findings.
  It still runs as `postgres` (UID 70) and still has a shell (a documented
  exception); nothing changes in the Kubernetes manifests, the Helm chart
  or Compose.
- A production build never falls back to dev sign-in: a `/config.json`
  without `authMode` means OIDC.

### Fixed

- The admin role follows Keycloak groups on every request.
- The sign-in session stays in sync across browser tabs, and an expired
  session sends you back to sign-in.
