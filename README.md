# Den

Den is the **optional central portal** for an organization running the
Con Services module ecosystem (Stash, Events, HR, …). It is the source of truth
for:

- **Org identity & branding** (logo, colors, typography, legal name, website)
- **Module registry / launcher** (which modules the org has, where they live,
  are they healthy, enabled, who can launch them)
- **Pairing** of mated modules via one-time pairing URL + token, which the
  module later redeems for its long-lived credential against Den's
  integration API

A module (Stash, Events, HR) can run **without** Den — in that case the
module owns its own copy of branding and user bootstrap. When paired to Den,
the module stops storing branding itself and reads it from Den's integration
API instead.

> This is an MVP scaffold. It is **not** production. All of the human UI,
> pairing flow, and branding API handlers are wired end-to-end with real
> routes and real types, but some of them currently return dev/fake data
> while the full persistence path is being filled in. See
> [docs/domain-model.md](docs/domain-model.md) and [docs/adr/](docs/adr).

## Stack

- **Backend**: Go 1.25, [chi](https://github.com/go-chi/chi), PostgreSQL via
  [pgx](https://github.com/jackc/pgx) and [sqlc](https://sqlc.dev),
  [Goose](https://github.com/pressly/goose) migrations run from a dedicated
  `cmd/migrate` binary, [zerolog](https://github.com/rs/zerolog), MinIO/S3
  for branding assets (MinIO is **DEV ONLY** — see warning below).
- **Frontend**: React 19, TypeScript, [Rsbuild](https://rsbuild.dev)
  (**not** Vite — project policy), [TanStack Router + Query](https://tanstack.com),
  React Hook Form + Zod, Tailwind, Lucide.
- **Infra**: PostgreSQL 16, MinIO (dev), Docker Compose.

## Ports (chosen to not collide with Stash's defaults)

| Service   | Host port       | Container port   | Notes                             |
| --------- | --------------- | ---------------- | --------------------------------- |
| backend   | `8090`          | `8090`           | HTTP API, `/api/v1/...`           |
| frontend  | `3001`          | `80` (nginx)     | SPA + `/api` reverse proxy        |
| postgres  | `5434`          | `5432`           | database `den`, user/pw `den/den` |
| minio     | `9010` / `9011` | `9000` / `9001`  | API / console; **DEV ONLY**       |

## Quick start (Docker Desktop)

```sh
cp .env.example .env
docker compose -f infra/docker-compose.yml up --build
```

This brings up, in order:

1. `postgres` (healthchecked)
2. `minio` (started)
3. `migrate` — **one-shot** job: runs Goose migrations against Postgres and
   exits with code 0 on success. Nothing else starts until this completes.
4. `backend` — only boots after `migrate` has succeeded. The API never
   runs migrations itself; replicas scaling in production will **not** fight
   over the migration lock.
5. `frontend` — nginx serving the SPA bundle and reverse-proxying `/api/` to
   the backend.

Visit <http://localhost:3001>. In dev the human UI uses `AUTH_MODE=dev` and
sends `X-Dev-User: dev-admin`.

## Why MinIO is dev-only

MinIO in `infra/docker-compose.yml` is a convenience for Docker Desktop.
Den's own branding bucket (`den-data-dev`) is created on first boot by the
backend if it does not exist. **Do not** run MinIO in production:

- Credentials in `docker-compose.yml` are static and world-readable.
- There is no replication, no lifecycle policy, no server-side encryption
  configured.
- Backups are the volume mount — one `docker volume rm` away from gone.

In production, point `S3_ENDPOINT`, `S3_BUCKET`, and the `S3_*` credentials
at a real S3-compatible service (AWS S3, Cloudflare R2, Backblaze B2, GCS
via its S3 API, …) with IAM/role-scoped access. See [docs/adr/0003-minio-dev-only.md](docs/adr/0003-minio-dev-only.md).

## One bucket per module

Den owns a single bucket (`den-data-dev` in dev). It is the only service
with credentials for that bucket. Mated modules **never** receive bucket
credentials — when Stash or Events needs a branding asset, it calls Den's
integration API with its pairing credential and Den streams the bytes from
its own bucket.

Each mated module gets its own bucket owned exclusively by that module. This
is the same rule as Stash's `stash-data-dev` bucket: there is no shared
object storage, no shared Postgres, no shared service account.
See [docs/adr/0004-one-bucket-per-module.md](docs/adr/0004-one-bucket-per-module.md).

## Human UI auth

Dev mode (`AUTH_MODE=dev`) accepts `X-Dev-User: <name>` and provisions the
user on the fly. OIDC mode is **stubbed** — `/internal/auth` has a hook for
an OIDC verifier that is wired exactly like Stash's, but Den does not have
to be the IdP (and should not be). The MVP assumes "IdP first": the org
already runs Keycloak / Entra / Okta, and Den is just one of several
relying parties. See [docs/adr/0001-idp-first-ecosystem.md](docs/adr/0001-idp-first-ecosystem.md).

## Pairing flow (one-time URL + token)

1. Operator opens Den → **Modules** → **New pairing** for a module (`Stash`
   production, `Events` staging, …). Den mints a `pairing_token` row and
   displays a one-shot **pairing URL** of the form:
   `https://den.example.org/api/v1/integrations/pairing/redeem?token=<opaque>`.
2. Operator pastes that URL into the mated module's admin UI.
3. The module POSTs to that URL (server-to-server). Den validates the token,
   marks it consumed, mints a long-lived `module_credential`, and returns:
   - `credential` (Bearer token the module stores securely)
   - `den_base_url`, `module_id`, `organization_id`
4. From that point the module uses `Authorization: Bearer <credential>` to
   call `GET /api/v1/integrations/branding/profile` and
   `GET /api/v1/integrations/branding/assets/{asset_id}`.

See [docs/adr/0002-pairing-url-and-token.md](docs/adr/0002-pairing-url-and-token.md).

## Operator: module databases and backups

`cmd/operator` builds `den-operator`, an internal service and the only
component with the Postgres admin login. Den's backend calls it
(`OPERATOR_URL`, `OPERATOR_TOKEN`) to create and drop module databases and
to run the nightly encrypted backup of the whole suite. Admins use it through
Den admin → Modules and Backups. How it works, what is covered, the
encryption key and the restore procedure:
[docs/operations.md](../docs/operations.md).

## Repository layout

```text
den/
  README.md, .gitignore, .dockerignore, .env.example
  docs/
    domain-model.md
    adr/
      0001-idp-first-ecosystem.md
      0002-pairing-url-and-token.md
      0003-minio-dev-only.md
      0004-one-bucket-per-module.md
      0005-no-shared-postgres.md
  backend/
    cmd/server/      # API server; never runs migrations
    cmd/migrate/     # one-shot migration binary (used by Compose + K8s Job)
    cmd/operator/    # internal service: module databases + encrypted backups
    internal/
      app/           # server bootstrap (config, db, files, http)
      auth/          # dev header + stub OIDC middleware, user context
      audit/         # audit_events writer
      config/        # env loading + validation
      db/            # pgx pool + WithTx + Goose runner
      domain/        # enums, permission names, module slugs
      files/         # S3/MinIO client; creates bucket if missing
      httperror/     # typed service errors + JSON renderer
      http/          # chi router + handlers (health, me, modules, pairing, branding, integrations)
      branding/      # branding profile + asset services (DB + S3)
      modules/       # module registry service
      organizations/ # org service
      pairing/       # pairing token + module credential service
    migrations/      # Goose SQL (embedded for cmd/migrate)
    queries/         # sqlc source
    sqlc.yaml
    go.mod
  frontend/
    rsbuild.config.ts, package.json, tsconfig.json, tailwind.config.ts, postcss.config.js
    public/
    src/
      api/           # typed fetch wrappers
      auth/          # dev X-Dev-User shim
      components/    # Layout + UI kit
      hooks/         # TanStack Query hooks (useMe, …)
      lib/
      pages/         # Dashboard, Modules, Branding, Settings, Login (dev)
      main.tsx, router.tsx, styles.css
  infra/
    docker-compose.yml
    Dockerfile.backend
    Dockerfile.frontend
    nginx.conf
    nginx.k8s.conf
  deploy/
    kubernetes/base/   # minimal placeholders: Deployment, Service, migrate Job
```
