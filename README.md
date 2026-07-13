# AsterRouter

AsterRouter is an enterprise AI API Gateway and governance control plane. It provides a stable, private, and auditable access layer for authorized, documented AI APIs.

The current product direction is standard-API-first: OpenAI-compatible, Anthropic Messages, and Gemini APIs are the supported integration contracts. Upstream providers or third-party gateways remain the source of upstream accounts, quotas, and provider-specific operations; AsterRouter does not implement account scraping, browser sessions, reverse-engineered APIs, token refresh automation, or private management APIs.

See [the global product positioning](docs/product-positioning.md) and [the V3 roadmap](docs/roadmap/v3/README.md) for the current scope. The V2 documents are retained as historical design records.

## Product Build

The current product build provides:

- Single-origin routes for `/setup`, `/admin`, `/portal`, `/api/v1/*`, and `/v1/*`.
- Basic settings APIs using a key/value settings model.
- Provider Connection, Workspace Key, policy, usage, and Audit Log control-plane APIs.
- Admin settings UI for site, profile, OIDC, data governance, service-center mode, and system update operations.
- Admin Console pages for overview, providers, workspace keys, policies, plugin center, audit logs, and system settings.
- Built-in plugin registry with free core, profile bundle, and paid add-on entitlement gates.
- Employee Portal workspace summary backed by the same control-plane data.
- Gateway API key authentication for `/v1/models` and `/v1/chat/completions`.
- OpenAI-compatible gateway authentication, model policy validation, provider forwarding, usage recording, and audit logging.
- OpenAI-compatible provider forwarding when a matching Provider Connection has an encrypted secret configured.
- Setup wizard for choosing `personal`, `relay_operator`, or `enterprise`.
- English-first i18n with Simplified Chinese as the second locale.

## Development

Install frontend dependencies once:

```bash
cd frontend
npm install
```

Run backend and frontend together for local UI development:

```bash
bash scripts/dev.sh
```

The development frontend listens on `http://localhost:5173` and proxies `/api/*` and `/v1/*` to the backend on `http://localhost:8080`. If either development port is occupied, the script shows the listener, sends `SIGTERM`, and waits up to five seconds for the port to be released.

To keep occupied processes running and fail instead:

```bash
./scripts/dev.sh --no-kill-occupied
```

For IDE tasks or persistent shell configuration, set `ASTER_DEV_KILL_OCCUPIED=0`. Automatic cleanup never escalates to `SIGKILL`.

Backend:

```bash
cd backend
go test ./...
go run ./cmd/asterrouter
```

Frontend:

```bash
cd frontend
npm install
npm run build
npm run dev
```

Single-origin preview:

```bash
cd frontend
npm run build
cd ../backend
go run ./cmd/asterrouter
```

Then open `http://localhost:8080/setup`, `http://localhost:8080/admin/settings`, or `http://localhost:8080/portal`.

Docker single-service deployment:

```bash
docker compose up --build
```

The container builds the frontend and serves it from the Go backend, so one route origin is enough for private deployments.

Environment:

```bash
export DATABASE_URL="postgres://asterrouter:asterrouter@localhost:5432/asterrouter?sslmode=disable"
export ASTER_ADMIN_TOKEN="change-me"
export ASTER_ADMIN_USERNAME="admin"
export ASTER_ADMIN_PASSWORD="change-me"
export ASTER_PROFILES="enterprise"
export ASTER_DEFAULT_PROFILE="enterprise"
export ASTER_SECRET_KEY="replace-with-a-stable-random-secret"
export ASTER_BUILD_TYPE="source"
export ASTER_UPDATE_MANIFEST_URL=""
export ASTER_CATALOG_MODE="disabled"
export ASTER_CATALOG_BOOTSTRAP_URL=""
export ASTER_OFFICIAL_SERVICES_URL=""
export ASTER_PLUGIN_HOST_URL=""
export ASTER_REDEEM_URL=""
export ASTER_ALLOW_RESTART="false"
export ASTER_BACKUP_DIR="data/backups"
export ASTER_DIAGNOSTIC_DIR="data/diagnostics"
export ASTER_MAX_ARCHIVE_BYTES="2147483648"
```

If `DATABASE_URL` is not set, the backend uses an in-memory settings repository for local development preview. PostgreSQL remains the intended persistent store.
Use a stable `ASTER_SECRET_KEY` before adding Provider secrets; changing it prevents existing encrypted Provider secrets from being decrypted.
The local login page uses `ASTER_ADMIN_USERNAME` and `ASTER_ADMIN_PASSWORD`. If `ASTER_ADMIN_PASSWORD` is empty, it falls back to `ASTER_ADMIN_TOKEN`; if both are empty, the local development default is `admin/admin`.

## Linux Release Deployment

AsterRouter ships Linux-only GitHub Release assets for `amd64` and `arm64`.

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash
```

The installer deploys to `/opt/asterrouter`, installs the `asterrouter` command wrapper, and creates `/etc/asterrouter/asterrouter.env` when missing. Production release builds refuse to start until `DATABASE_URL`, a stable `ASTER_SECRET_KEY`, and an admin password or token are configured.

Common operations:

```bash
asterrouter status
asterrouter logs -n 200
asterrouter upgrade
asterrouter upgrade -v v0.1.0
asterrouter rollback v0.1.0
```

System update:

- `ASTER_BUILD_TYPE=source` disables in-place binary replacement and reports manual update guidance.
- `ASTER_BUILD_TYPE=release` enables one-click update when `ASTER_UPDATE_MANIFEST_URL` points to a trusted JSON manifest with a matching `os`/`arch` asset and `sha256`.
- `ASTER_REDEEM_URL` configures the official one-time redemption endpoint; redemption codes are not stored locally.
- `ASTER_CATALOG_MODE` selects `online`, `private_mirror`, `offline`, or `disabled` official service behavior.
- `ASTER_OFFICIAL_SERVICES_URL` optionally overrides the canonical `/official/v1/services` base URL used by encrypted Feed synchronization. When empty, the client derives it from the signed catalog bootstrap or catalog URL.
- `ASTER_PLUGIN_HOST_URL` overrides the loopback Host API passed to Sidecar plugins. The default is derived from `ASTER_ADDR` and is never intended for browser or external integration use.
- Online Feed synchronization sends only License and instance binding metadata, the instance X25519 public key, Core version, and a request ID. It never uploads prompts, responses, Provider secrets, Workspace Keys, or Gateway usage details.
- `ASTER_ALLOW_RESTART=true` allows the Admin Console restart action to terminate the process so an external supervisor can restart it.
- `ASTER_BACKUP_DIR` controls PostgreSQL and local plugin asset backup archives; the default is `data/backups`.
- `ASTER_DIAGNOSTIC_DIR` controls redacted diagnostic bundles; the default is `data/diagnostics`.
- `ASTER_MAX_ARCHIVE_BYTES` limits backup and diagnostic archive size. PostgreSQL deployments require `pg_dump` and `pg_restore` on the service host.
