# Den

Den is the portal and admin console of [Con Services](https://github.com/AGlElectronics/con-services).
People land on Den's portal after signing in; administrators use it to
connect modules (HR, Stash, Rally, …), set the organisation's branding and
settings, and manage module databases and backups.

**Documentation: <https://aglelectronics.github.io/con-services/modules/den/>**

- [How the platform starts (Den first)](https://aglelectronics.github.io/con-services/getting-started/how-the-platform-starts/)
- [Run Den on Kubernetes](https://aglelectronics.github.io/con-services/getting-started/den-quickstart/)
- [Admin guide](https://aglelectronics.github.io/con-services/modules/den/admin-guide/)
- [Configuration](https://aglelectronics.github.io/con-services/modules/den/configuration/)
- [API reference](https://aglelectronics.github.io/con-services/modules/den/api/)
- [Databases, backups and restore](https://aglelectronics.github.io/con-services/operations/databases-and-backups/)

Changes are listed in [CHANGELOG.md](CHANGELOG.md).

## Running Den

Den runs on Kubernetes from the published images
`ghcr.io/aglelectronics/den-backend`, `den-frontend` and `den-operator`
(pinned `MAJOR.MINOR.PATCH-BUILD` tags), using the manifests in
[con-services](https://github.com/AGlElectronics/con-services/tree/main/deploy/kubernetes).
Follow the [tutorial](https://aglelectronics.github.io/con-services/getting-started/den-quickstart/).

To work on the code, clone con-services, clone this repository into its
`den/` folder and use the
[Docker Compose development stack](https://aglelectronics.github.io/con-services/contributing/local-development/).

## Repository layout

```text
backend/
  cmd/server/     API server (never runs migrations)
  cmd/migrate/    one-shot migration job
  cmd/operator/   internal service: module databases and encrypted backups
  internal/       handlers, auth, config, storage, Keycloak sync, operator
  migrations/     Goose SQL migrations, embedded in the binaries
frontend/         React web app (Rsbuild, TanStack Router and Query, Tailwind)
infra/            Dockerfiles, nginx configurations, a standalone compose file
docs/             design decisions (adr/) and the domain model
```

Design decisions are recorded in [docs/adr/](docs/adr), and the data model
in [docs/domain-model.md](docs/domain-model.md).
