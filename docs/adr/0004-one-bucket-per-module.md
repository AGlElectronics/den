# ADR 0004 — One bucket per module, never share credentials

## Status

Accepted — 2026-11-02.

## Context

Stash has a bucket (`stash-data-dev`) for item images, receipts, barcodes.
Den has a bucket (`den-data-dev`) for branding assets. Events and HR will
have their own buckets later.

A cheap-but-wrong design: let Stash read from Den's bucket by handing it
the Den S3 credentials. This "works" but:

- A compromise of any module's secret store leaks object-storage keys that
  can read **and delete** other modules' data.
- Rotating a shared key means coordinating a rolling restart of every
  module.
- Audit trails collapse: object-store access logs cannot tell which
  module read which object.
- IAM policies have to grow to cover every cross-module read pattern,
  which is tedious and error-prone.

## Decision

- Each module owns exactly one bucket and is the only service with
  credentials to it. Buckets are named per-env: `den-data-prod`,
  `den-data-staging`, `den-data-dev`.
- When a mated module needs an asset from Den (e.g. the primary logo), it
  calls `GET /api/v1/integrations/branding/assets/{asset_id}` with its
  **pairing credential** in the Authorization header. Den authenticates
  the request, reads the object from Den's bucket, and streams the bytes
  back.
- The module caches the bytes locally (ETag / Last-Modified) and never
  stores the Den bucket name or credentials.
- The same rule holds in reverse: Den does **not** receive Stash's S3
  credentials and does not read Stash's bucket.

## Consequences

- Branding fetches cost a Den API call plus a bucket read. Cache
  aggressively (per-revision immutable key).
- Replacing an asset is "mint a new row, keep the old one for grace", so
  cached references stay valid for a configurable window.
- The pairing credential is the only thing a module needs to configure to
  access Den's branding. No bucket URL, no AWS key, no region.
