# ADR 0005 — No shared PostgreSQL between modules

## Status

Accepted — 2026-11-02.

## Context

The ecosystem (Stash, Events, HR, Den, …) could in principle share a single
PostgreSQL cluster or even a single database with per-module schemas.
Several teams have tried this and been bitten by:

- Cross-module joins that silently create coupling. Six months in, Events
  cannot be deployed without Stash because an inner join has grown across
  schemas.
- Lock contention from one module (long analytical queries) stalls every
  other module.
- Backup / restore granularity collapses — restoring Events to a
  point-in-time means restoring Stash with it.
- Migrations have to be globally serialized across teams.

## Decision

- Each module runs against its own PostgreSQL database. Preferably its own
  cluster.
- Cross-module data references use UUID keys and go through the owning
  module's API, not through SQL.
- Den exposes `/api/v1/integrations/branding/*` for the branding data a
  mated module needs; mated modules never read Den's `branding_*` tables
  directly.
- No read replicas are shared across module boundaries either.

## Consequences

- Reporting that spans modules has to be done at the API or ETL layer
  (export to a warehouse), not with SQL joins across schemas.
- `cmd/migrate` migrates **only** Den's database. Stash's migrations are
  Stash's problem.
- The ecosystem can scale and fail independently per module, which is
  the whole point.
