# ADR 0006 — TLS for every connection inside the cluster

## Status

Accepted — 2026-11-07.

## Context

On Kubernetes, the ingress controller ended HTTPS and everything behind it
was plain text: web app to backend, every backend to PostgreSQL
(`sslmode=disable`) and MinIO (`http://`), modules to Den during pairing,
Den to Keycloak and the operator, HR to Learn, Wiki and Tasks. Those
connections carry sign-in tokens, pairing tokens, module credentials,
service tokens, database passwords and all the data.

Namespaces and NetworkPolicies decide *who may connect*; they do not encrypt
anything and do not prove which pod answers. Cilium's WireGuard encryption
covers traffic between nodes, but not traffic between pods on the same node,
not a database outside the cluster, and it is a cluster setting an operator
can forget or switch off.

The options considered:

1. **Rely on the CNI** (Cilium WireGuard or IPsec). No application change,
   but no server identity and no protection when it is off.
2. **A service mesh** (Istio, Linkerd) with automatic mTLS. Strong, but a
   large extra system to run and learn for a small team, and it does not
   cover the managed database.
3. **cert-manager with a private CA, trust-manager for distribution, TLS in
   the applications.** Small, well-known add-ons; every client checks the
   server's name; the same `verify-full` setting works for in-cluster and
   managed databases.

## Decision

Option 3, for every connection in phase 1, including the internal endpoints
of Learn, Wiki and Tasks:

- `deploy/kubernetes/base/pki` creates a self-signed root (ten years, in the
  `cert-manager` namespace) and the `ClusterIssuer`
  `conservices-internal-ca`. Every server gets a `Certificate` for its
  Service names (90 days, renewed 30 days before expiry).
- trust-manager copies the CA's public certificate into the ConfigMap
  `conservices-internal-ca` in every namespace labelled
  `app.kubernetes.io/part-of: conservices`.
- Go servers serve HTTPS when `TLS_CERT_FILE` and `TLS_KEY_FILE` are set and
  re-read the files every minute (`internal/tlscert`). Without them they
  serve plain HTTP, so Docker Compose development is unchanged. Go clients
  trust the CA through `SSL_CERT_DIR`; no client code changed.
- PostgreSQL accepts only `hostssl` connections; every URL uses
  `sslmode=verify-full`. The operator hands out module URLs with
  `MODULE_DB_PARAMS`.
- nginx, MinIO and Keycloak serve TLS with their certificate and reload it
  after renewal. ingress-nginx checks the web apps' certificates.
- Cilium WireGuard stays recommended alongside; the two layers complement
  each other.

## Consequences

- cert-manager and trust-manager are required before applying the platform
  or a module chart. This is a breaking change for existing installations.
- Keycloak moved from port 8080 to 8443 in the cluster; NetworkPolicies and
  `KEYCLOAK_ADMIN_URL` follow.
- Database URLs with `sslmode=disable` stop working against the in-cluster
  PostgreSQL.
- Go reads the trusted CAs once at start, so rotating the root CA needs a
  two-root overlap and pod restarts (documented in Internal TLS).
- Other ingress controllers than ingress-nginx need their own backend-TLS
  settings (Traefik: `ServersTransport`).
- The Docker production guide is not covered yet.

Details and operations: [Internal TLS](../../../docs/operations/internal-tls.md).
