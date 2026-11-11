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

## Amendment — 2026-11-04: one server, Den-managed databases

Small deployments run every module database on **one PostgreSQL server**.
The decision above still applies: one database and one login per module,
`CONNECT` revoked from everyone else, no SQL across modules. What is shared is
the server (CPU, memory, disk), not data. A module that outgrows it moves to
its own server.

Den's operator (`cmd/operator`) is the only component with the server's admin
login. It creates and drops module databases on Den's request and backs up
the whole server. `MANAGED_DATABASES=false` turns database management off for
deployments where each module has its own server. Details, backups and
restore: [Databases, backups and restore](https://aglelectronics.github.io/con-services/operations/databases-and-backups/).
