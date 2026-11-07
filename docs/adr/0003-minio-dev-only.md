# ADR 0003 — MinIO is dev-only

## Status

Accepted — 2026-11-02.

> **Update 2026-11-07:** MinIO stopped publishing community images, so the development and local-trial S3 server is now SeaweedFS (`chrislusf/seaweedfs:4.52`, service `s3`). The decision is unchanged: the bundled S3 server is for development and local trials only, and production uses a managed S3 service.

## Context

The MVP needs an S3-compatible object store for branding assets. Developers
run on Docker Desktop with no cloud credentials, so we ship a MinIO
container in `infra/docker-compose.yml`.

Shipping MinIO in production is tempting but wrong for our scale and
threat model.

## Decision

- MinIO runs **only** in `infra/docker-compose.yml` under the stated
  warning in `README.md` and `.env.example`.
- The compose MinIO uses static credentials (`den` / `denden123`) that are
  checked into the repo. These credentials are safe only because the
  compose stack binds to `localhost`.
- In production, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, and
  `S3_SECRET_ACCESS_KEY` must point at a managed, backed-up,
  server-side-encrypted bucket (AWS S3, Cloudflare R2, Backblaze B2,
  GCS via S3 API, …).
- The backend never assumes it is talking to MinIO. The MinIO SDK speaks
  S3; `files.Client` uses the generic SDK configured by env.

## Consequences

- `infra/Dockerfile.frontend` and `infra/Dockerfile.backend` do not pull
  in any MinIO server-side bits.
- `deploy/kubernetes/base/` deliberately contains **no** MinIO manifests.
  If an operator wants an in-cluster MinIO as a stopgap while a real
  bucket is being provisioned, that is a separate opt-in file (not part
  of `base/`).
- Code comments at `backend/internal/files/client.go` must call out that
  the client is pointed at MinIO only in dev.
