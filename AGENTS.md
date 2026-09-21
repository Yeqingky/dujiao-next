# Repository Guide

## Scope

Dujiao-Next is a modular Go monolith with two Vue 3 SPAs:

- `cmd/server/`: API, worker, and operator command entrypoint.
- `internal/`: backend modules, bootstrap wiring, infrastructure, and architecture guards.
- `frontend/user/`: customer storefront with classic and vault templates.
- `frontend/admin/`: administrator SPA.
- `internal/web/`: full-stack SPA embedding and runtime mounting.

Follow deeper `AGENTS.md` files when present.

## Architecture

Each backend domain under `internal/modules/<module>/` is a vertical slice. Keep domain and application code independent from Gin, GORM, and concrete infrastructure. Cross-module dependencies must use contracts and bootstrap adapters. Run `go test ./internal/architecture/...` after changing module boundaries, routes, or wiring.

Both frontends must keep user-facing text in i18n resources. Storefront changes must account for both classic views and vault templates. Admin navigation must respect the runtime `web.admin_path` behavior documented in `README.md`.

## Configuration

`config.yml` keeps only what the process needs at startup: `app`, `server`, `log`, `database`, `jwt`, `user_jwt`, `bootstrap`, `redis`, the `queue` connection, `upload`, `cors`, `security`, `reseller`, and `web.admin_path`.

Backend-managed settings live in the database `settings` table and always win over the same-named `config.yml` fallback. Configure them in the admin panel instead of editing the file:

| Setting key | Admin panel |
| --- | --- |
| `smtp_config` | Settings -> SMTP |
| `captcha_config` | Settings -> CAPTCHA |
| `telegram_auth_config` | Settings -> Telegram |
| `google_auth_config` | Settings -> Google |
| `order_config` | Settings -> Basic (order section) |
| `upstream_sync_config` | Settings -> Upstream Sync |

Do not re-add these sections to `config.yml` or `config.yml.example`; a stored row silently shadows the file. `loadRuntimeSettings` in `internal/app/container/services_foundation.go` applies the override before dependent services are built, and a failed read of `google_auth_config` fails closed by disabling Google login.

## CAPTCHA

Supported providers are `none`, `image`, `turnstile`, and `cap`. Display the Cap provider simply as `Cap` in all locales.

- Runtime and persisted settings are defined in `internal/modules/settings/schema/security/captcha.go`.
- Public configuration may expose Cap `endpoint` and `site_key`, but never `secret_key`.
- Cap `endpoint` is the Cap Standalone base URL without the site key path.
- The widget sends `captcha_payload.cap_token`.
- The backend verifies Cap tokens through `/<site-key>/siteverify` and fails closed on configuration, network, HTTP, or response errors.
- Tokens are single-use. Frontends must reset the widget after an attempted protected action.
- CAPTCHA scene switches cover login, registration email codes, password-reset email codes, guest order creation, and gift-card redemption.

## Generic OIDC

The storefront supports one globally configured generic OIDC provider for login and account binding. The implementation uses Discovery, authorization code exchange, `state`, `nonce`, and PKCE by default. `client_secret_basic` and explicit `client_secret_post` modes are available for provider compatibility; `client_secret_post` is not the default. External identities are keyed by a short issuer-derived provider ID plus the OIDC `sub` claim. Email auto-linking requires `email_verified`; unverified claims never attach to an existing local account automatically.

For Microsoft Entra ID, prefer a tenant-specific issuer such as `https://login.microsoftonline.com/<tenant-id>/v2.0` and register the exact `/auth/oidc/callback` storefront redirect URI.

## Development

Run the API and SPAs separately for hot reload:

```bash
go run ./cmd/server
cd frontend/user && pnpm install && pnpm run dev
cd frontend/admin && pnpm install && pnpm run dev
```

Use the root Compose stack with the published GHCR application image:

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f dujiao-next
```

For local source-build verification, use `docker-compose-dev.yml`:

```bash
docker compose -f docker-compose-dev.yml up -d --build
```

The default `docker-compose.yml` uses `ghcr.io/Yeqingky/dujiao-next:${TAG:-latest}`;
keep the source-build configuration in `docker-compose-dev.yml`.

Runtime files are intentionally untracked:

- `.env`
- `config/`
- `data/`
- `config.yml`
- `db/`, `uploads/`, and `logs/`

The development Compose file binds its configuration and runtime data from
`/opt/dujiao-next/config/` and `/opt/dujiao-next/data/`; keep those host paths outside the
repository. Never commit credentials, database files, Redis state, uploads, or production
logs.

## Validation

Run the relevant commands after every change:

```bash
go test ./...
cd frontend/user && pnpm run test && pnpm run build
cd frontend/admin && pnpm run test && pnpm run build
docker compose config -q
```

For release/full-stack behavior, also verify:

```bash
cd frontend/admin && pnpm run build:fullstack
go build -tags release,fullstack ./cmd/server
```

## Version Control and Release

- Keep generated frontend `dist/` directories and embedded `internal/web/dist/` out of Git.
- `README.md` is the default Simplified Chinese documentation; keep the English translation in `README.en.md` in sync.
- Update `README.md`, `config.yml.example`, and this file when commands, configuration, architecture, providers, or runtime behavior change.
- Release builds use the repository `Dockerfile` or `.goreleaser.yaml` and embed both SPAs into one binary.
- The release workflow publishes multi-architecture `linux/amd64` and `linux/arm64` images to `ghcr.io/<owner>/dujiao-next` using the Actions-provided `GITHUB_TOKEN`; do not add Docker Hub credentials.
- The branch image workflow builds every branch push and tags images as `<branch>-<seven-character-commit-sha>` in GHCR.
- Do not commit or push unless explicitly requested.
