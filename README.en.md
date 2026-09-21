# Dujiao-Next

Dujiao-Next is a digital goods e-commerce platform. This repository contains the complete
application: the Go backend, the customer storefront, and the admin panel.

[中文版](README.md)

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26 · Gin · GORM · SQLite / PostgreSQL |
| Auth | JWT (separate admin / user realms) · Casbin RBAC · TOTP 2FA |
| Async | asynq on Redis (optional — the server runs without it) |
| Config | Viper (`config.yml`) |
| Frontend | Vue 3 · Vite · TypeScript · Tailwind CSS v4 · pnpm 10 |
| Admin UI | shadcn-vue / reka-ui |

## Repository Layout

```
.
├── cmd/server/               # entry point; also hosts the `admin` operator subcommands
├── internal/
│   ├── app/                  # composition root
│   │   ├── container/        # dependency-injection container
│   │   ├── httpserver/       # Gin router, route groups, middleware
│   │   └── jobs/             # asynq worker service and consumers
│   ├── bootstrap/            # per-module wiring (adapters.go + wiring.go)
│   ├── modules/              # 35 business modules — one vertical slice per domain
│   ├── workflows/            # use cases that span several modules
│   ├── platform/             # framework-facing infrastructure
│   │   ├── database/gormdb/  # connection, auto-migration
│   │   └── http/             # response envelope, Gin helpers
│   ├── shared/               # dependency-free primitives (money, jsonmap, serial …)
│   ├── authz/                # Casbin RBAC: policy model, built-in role seeds
│   ├── web/                  # SPA embedding and mounting (build-tag gated)
│   ├── architecture/         # architecture guard tests — no production code
│   ├── cache/ config/ constants/ crypto/ i18n/ logger/ queue/ version/
│   └── admincmd/ htmltext/ persistence/ telegramidentity/ testkit/ upstream/
├── frontend/
│   ├── admin/                # admin panel SPA        (dev :5174)
│   └── user/                 # customer storefront SPA (dev :5173)
├── config.yml.example
├── Dockerfile                # single full-stack image
└── .goreleaser.yaml
```

Runtime directories created on first start: `db/` (SQLite), `uploads/`, `logs/`.

## Architecture

A modular monolith. Each domain under `internal/modules/<name>/` is a vertical slice with its
own layers:

| Layer | Holds | May import |
| --- | --- | --- |
| `domain/` | entities, value objects, business invariants | nothing from the other layers |
| `application/` | use cases, port interfaces | `domain`, `contract` |
| `infrastructure/` | GORM stores, gateways, queue adapters | `domain`, `application` ports |
| `transport/` | HTTP handlers, presenters | `application` contracts |
| `contract/` | port interfaces the application layer depends on, and the module's public surface for other modules | — |

**These rules are enforced by tests, not convention.** `internal/architecture/` parses every
import in the tree and fails the build on violations. The main ones:

- `domain` must not reach into `application`, `infrastructure`, or `transport`
- `application` must not import Gin or asynq — no transport libraries in use cases
- only a module's `infrastructure/gormstore` adapter may import GORM
- `transport` depends on application contracts, never on concrete stores
- `internal/shared` stays free of modules, GORM, Gin, and asynq
- `internal/platform` must not depend on business modules

Run them with the rest of the suite: `go test ./internal/architecture/...`

Modules never import each other's internals — they talk through `contract/`, and the wiring
lives in `internal/bootstrap/<module>/`.

### RBAC

Every `/api/v1/admin/...` route passes through Casbin. The permission catalog is generated
from the live route table, but the **built-in roles are hand-maintained** in
`internal/authz/bootstrap.go`. Adding an admin route without adding it to a role seed leaves
that route reachable only by the super admin. `internal/app/httpserver/rbac_coverage_test.go`
checks that every registered route is covered.

## Build Tags

| Tag | Effect |
| --- | --- |
| *(none)* | API only. No SPAs mounted — the default for local development. |
| `fullstack` | Embeds `internal/web/dist/{admin,user}` into the binary via `go:embed`. |
| `release` | Production behavior for outbound URL building. |

`go:embed all:dist/admin all:dist/user` requires **both** directories to exist, so a
`fullstack` build fails outright if the frontends were not built first. A plain `go build`
does not compile `embed_fullstack.go` — after touching `internal/web/`, verify with
`go build -tags release,fullstack ./cmd/server`.

## Run Modes

```bash
./dujiao-next                 # all    — HTTP server + background worker (default)
./dujiao-next -mode api       # HTTP server only
./dujiao-next -mode worker    # background worker only
```

Operator subcommands ship in the same binary, so a container needs no extra tooling:

```bash
./dujiao-next admin list-admins
./dujiao-next admin reset-password
./dujiao-next admin reset-2fa
```

## Frontend Notes

Two independent SPAs, both built with Vite and embedded at release time.

**Mount points.** The storefront is served at `/`; the admin panel at `web.admin_path`
(default `/admin`). `/api`, `/uploads`, and `/health` are reserved prefixes — an unmatched
path under them returns 404 instead of falling through to the SPA shell. Adding a new
top-level backend prefix means updating `reservedPaths` in `internal/web/handler.go`.

**The admin base path is resolved at runtime, not at build time.** Since `web.admin_path` is
configurable, `pnpm run build:fullstack` only injects a `<base href="__DJ_ADMIN_BASE__/">`
placeholder, which the server rewrites on startup. Consequences for admin code:

- native `<a href>` and `window.location` navigation must go through `adminUrl()` in
  `src/utils/adminBase.ts`
- `<router-link :to>` and `router.push()` must **not** — vue-router already carries the base,
  and prefixing again yields `/admin/admin/...`

**Storefront templates.** The customer frontend ships more than one look, selected by the
`storefront_template` site setting (`classic`, `vault`). Template pages live in
`src/templates/<name>/` and fall back to `src/views/` when a page has no template-specific
version; see `src/templates/registry.ts`. Append `?template=vault` to preview one locally.

**i18n.** Both frontends and all API responses are localized — Simplified Chinese, Traditional
Chinese, and English. Do not hard-code user-facing strings on either side.

## Quick Start (Deploy)

### Official one-click installer (Ubuntu / Debian)

On a fresh Ubuntu 22.04+ or Debian 12+ server, download and run the official
interactive installer:

```bash
curl -fsSL https://raw.githubusercontent.com/dujiao-next/dujiao-next/main/scripts/dujiao-next-manager.sh \
  -o /tmp/dujiao-next-manager.sh
sudo bash /tmp/dujiao-next-manager.sh install
```

The installer deploys the release binary with systemd, an isolated local Redis,
Nginx, SQLite, and a Let's Encrypt certificate. After installation, reopen the
management menu with:

```bash
sudo dujiao-next-manager
```

Common automation-friendly commands are also available:

```bash
sudo dujiao-next-manager status
sudo dujiao-next-manager logs app
sudo dujiao-next-manager restart
sudo dujiao-next-manager configure-domain
sudo dujiao-next-manager configure-admin-path
sudo dujiao-next-manager renew-cert
sudo dujiao-next-manager admin-reset-password
sudo dujiao-next-manager admin-reset-2fa
sudo dujiao-next-manager uninstall
```

The first release supports a single non-wildcard domain on Ubuntu/Debian only.
It does not adopt an existing manual installation. If SMTP is skipped, configure
it in the admin panel before enabling email-verification registration. Application
data lives in `/opt/dujiao-next`; installer state is stored in
`/etc/dujiao-next/install-state.json`. TLS failures leave only the ACME challenge
endpoint enabled, and `install` can be rerun after DNS or firewall repair. Safe
uninstall creates and verifies a `0600` recovery archive under
`/var/backups/dujiao-next` before deleting managed data.

### Manual binary installation

Download the latest `dujiao-next_*.tar.gz` from [Releases](https://github.com/dujiao-next/dujiao-next/releases):

```bash
tar -xzf dujiao-next_*.tar.gz
cp config.yml.example config.yml
# edit config.yml: set jwt.secret, user_jwt.secret, and web.admin_path
# config.yml only keeps settings required at startup; SMTP email, CAPTCHA, Telegram/Google login, order settings, and the upstream sync interval are configured in the admin panel
./dujiao-next
```

Full instructions: https://dujiao-next.com/deploy/

Or with Docker:

```bash
docker run -d -p 8080:8080 -v $PWD/config.yml:/app/config.yml:ro ghcr.io/Yeqingky/dujiao-next:latest
```

Release images are published to GitHub Container Registry as multi-architecture
`linux/amd64` and `linux/arm64` images. Available tags include the release tag,
semantic version, and `latest`.

### Branch Docker images

Every branch push also builds and publishes a multi-architecture image tagged with the branch
name and the seven-character commit hash. For example:

```text
ghcr.io/Yeqingky/dujiao-next:dev-cap-captcha-13876af
```

Branch slashes are replaced with hyphens in image tags.

### Docker Compose

The root `docker-compose.yml` pulls the application image from GHCR and starts PostgreSQL,
Redis, the API, and both embedded SPAs:

```bash
cp .env.example .env
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f dujiao-next
```

Set `TAG` in `.env` to select another published GHCR tag; it defaults to `latest`.

For local source builds from the current checkout's `Dockerfile`, use
`docker-compose-dev.yml`:

```bash
docker compose -f docker-compose-dev.yml up -d --build
```

The application listens on `127.0.0.1:${APP_PORT}`. Runtime credentials are read from `.env`.
The development Compose file reads its application configuration from
`/opt/dujiao-next/config/config.yml` and stores persistent files under
`/opt/dujiao-next/data/`. These paths contain secrets or runtime data and are intentionally
excluded from Git and the Docker build context. Stop the stack with `docker compose down`;
bind-mounted data remains under `/opt/dujiao-next/data/`.

## Quick Start (Develop)

Run the backend and the two frontends separately for hot reload:

```bash
go mod tidy && go run ./cmd/server   # :8080 — API only, no SPAs mounted

cd frontend/user  && pnpm install && pnpm run dev   # :5173
cd frontend/admin && pnpm install && pnpm run dev   # :5174
```

Both dev servers proxy `/api`, `/uploads`, `/sitemap.xml`, and `/robots.txt` to
`localhost:8080`. In production everything is same-origin, so these proxies are a
development-only concern.

## Generic OIDC Login

The project supports one globally configured OIDC provider for user login and account binding. The admin settings support these modes:

- `PKCE + none`: public client with PKCE, the default;
- `PKCE + client_secret_basic`: recommended for server-side web applications;
- client secret basic with PKCE disabled: compatibility mode for legacy providers;
- `client_secret_post`: only for special compatibility cases and not recommended by default.

Configure an Issuer URL, Client ID, redirect URI, and a scope set containing `openid`. The server uses OIDC Discovery to resolve authorization, token, and JWKS endpoints, and validates `state`, `nonce`, issuer, audience, signature, and expiration. For Microsoft Entra ID, prefer a tenant-specific issuer such as:

```text
https://login.microsoftonline.com/<tenant-id>/v2.0
```

Register this exact redirect URI with the provider:

```text
https://your-store-domain/auth/oidc/callback
```

External identities are keyed by `iss + sub`. Unverified email claims are never used to automatically link an existing local account.

## Optional Cap CAPTCHA

Cap is deployed as a separate service. Follow the [Cap Standalone guide](https://trycap.dev/guide/standalone/)
to run the Cap container with Valkey/Redis, expose it through an HTTPS hostname, and create a
site key. Configure `CORS_ORIGIN` on Cap to allow the storefront and admin origins.

In Dujiao-Next, open **Settings -> CAPTCHA**, select `Cap`, then configure:

- **Server endpoint**: the public Cap base URL, for example `https://cap.example.com`;
- **Site Key**: the public key created in the Cap dashboard;
- **Site Secret Key**: the site key secret, not Cap's dashboard `ADMIN_KEY`.

The browser uses `https://cap.example.com/<site-key>/`; Dujiao-Next verifies the returned token
server-side through Cap's `/siteverify` endpoint. The secret is never included in public config.
These values live only in the database-backed settings; `config.yml` no longer exposes a
`captcha.cap` section, so do not duplicate them in two places.

> Use `pnpm` via corepack. `pnpm --dir X` does not read the `packageManager` field of the
> target directory and will pick the wrong version — `cd` into the package first.

## Building the Full-Stack Binary

```bash
goreleaser build --snapshot --single-target --clean
```

This builds both frontends, embeds them, and compiles with `-tags fullstack` — the same path
CI uses for releases. The manual equivalent:

```bash
(cd frontend/admin && pnpm run build:fullstack)   # injects the <base> placeholder
(cd frontend/user  && pnpm run build)
rm -rf internal/web/dist && mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
go build -tags release,fullstack -o dujiao-next ./cmd/server
```

Note that admin uses `build:fullstack`, not `build`. Plain `build` produces a bundle pinned to
`/`, which silently breaks a custom `web.admin_path`.

## Testing

```bash
go test ./...                              # full suite
go test ./internal/architecture/...        # dependency and layering guards
go test ./internal/modules/order/...       # one module

cd frontend/user  && pnpm run build        # includes vue-tsc type checking
cd frontend/admin && pnpm run build
```

Health check endpoint: `GET /health`

## Notes on Data Access

SQLite runs with `MaxOpenConns=1`. A store opens a transaction through
`WithinTransaction(func(tx contract.Transaction) error)`, and every query inside the closure
must go through that `tx` handle or a store bound to it via `WithTx(tx)`. Reaching for the
global DB handle instead asks for a second connection that will never be granted, deadlocking
the process — including indirectly, by calling a service that queries on its own. Read any
settings you need *before* opening the transaction, and keep outbound HTTP calls (payment
gateways and the like) outside it.

## Online Documentation

- https://dujiao-next.com
