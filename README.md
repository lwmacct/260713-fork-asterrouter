<div align="center">

# AsterRouter

**Control AI access today. Build lower-cost operations over time.**

A private AI gateway for teams, developer platforms, and connected products.

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## AI access that fits the business you already run

AsterRouter sits between your applications and the AI providers you are authorized to use. It gives engineering, operations, and security teams one place to control access, routes, spend evidence, and delivery operations without exposing upstream credentials.

| What matters | What AsterRouter does |
| --- | --- |
| Keep product work moving | Give applications one stable OpenAI-compatible endpoint for models and Chat Completions streaming. |
| Keep AI operations reliable | Route across authorized providers and avoid unhealthy, limited, or unavailable paths. |
| Keep access governed | Issue and rotate keys, apply model policy, rate limits, quotas, budgets, alerts, and audit trails. |
| Keep ownership clear | Preserve the connected product's users, sessions, subscriptions, and orders. |
| Keep delivery practical | Start self-managed, or use private delivery and managed operations in your own environment. |

## Choose the operating model

| You operate | Choose | First console |
| --- | --- | --- |
| A focused gateway for yourself or a small team | **Personal** | `/console` |
| Customer balances, plans, risk, and resale operations | **Relay Operator** | `/operator` and `/customer` |
| Employee, department, SSO, and internal-service governance | **Enterprise** | `/admin` and `/portal` |
| A developer API or AI capabilities inside an existing SaaS, OEM, or client product | **AI Platform** | `/platform` |

Each production instance starts with one active role during installation. A system administrator can switch the active role later from Settings; only the selected role is exposed at a time, while existing data is retained.

## Offer AI without taking over your product

Use **AsterRouter API Keys** when you operate the API relationship yourself. Use **AI Platform** when another product already owns its users and commercial relationship.

```text
Your product keeps users, sessions, subscriptions, and orders
                         |
                         v
          AsterRouter applies AI access and routing controls
                         |
                         v
            Authorized AI providers receive the request
```

AI Platform is a separate deployment role, not a relay-customer feature. It has its own tenants, non-human callers, API Keys, delegated HMAC or JWT/JWKS access, and signed metering-only usage delivery. It never creates the connected product's end-user accounts, sessions, subscriptions, orders, or balances.

## What is available now

- OpenAI-compatible Models and Chat Completions, including streaming.
- Multi-provider routing, fallback, account capacity controls, cooldowns, circuit breaking, and sticky routing.
- API Key lifecycle, model allowlists, rate limits, token quotas, budgets, usage, cost allocation, alerts, traces, audit, and export.
- Enterprise identity and access governance, plus Personal, Relay Operator, Enterprise, and AI Platform consoles.
- Platform Tenant and Gateway Principal management; direct tenant-bound keys; HMAC and RS256 JWT/JWKS delegated access; signed HTTPS usage delivery with retry, dead letter, and requeue.
- Private deployment foundations: PostgreSQL, backup and restore, diagnostics, and verified release updates.

## Where the product is going

The roadmap extends the same Core to price-aware provider selection, browser-assisted price collection, Responses and other public protocols, image/audio/video, long-running jobs, artifact delivery, Redis-backed coordination, and Kubernetes scaling. These are not current product claims.

The roadmap is intentionally not a release promise. The public product boundary is kept in this README; private deployment planning is delivered with the engagement.

## Quick start

### Choose a deployment role

Choose one business deployment role during installation. This controls the initial console, business objects, roles, and default extensions; it is not a cosmetic home-page choice or a set of composable feature flags. Choose by the owner of the business relationship, commercial settlement, and identity facts, not by whether an integration needs API Keys, streaming, or a particular model.

| Choose this deployment role when | Business and identity source of truth | Initial console | It deliberately does not include |
| --- | --- | --- | --- |
| **Personal**: one person or a small team needs a focused gateway | Your own Workspace and collaboration | `/console` | Enterprise organization management, relay billing, and external-product integration |
| **Relay Operator**: you operate an existing customer, balance, plan, and risk workflow | You own the customer, plan, balance, and risk workflow | `/operator` and `/customer` | Enterprise employee management and external-product tenancy |
| **Enterprise**: you govern employee and service access inside one organization | Your organization owns its employee, directory, and access facts | `/admin` and `/portal` | Relay resale objects and external end-user identity or subscription management |
| **AI Platform**: you operate developer APIs or add AI to SaaS/OEM products | You own the gateway tenant, caller, and integration boundary; the connected product owns end users | `/platform` | Enterprise HR objects, relay balances/plans, and external end-user accounts or sessions |

AI Platform is separate from relay operations. Both can issue or accept API credentials, but a relay operator owns customer balances, plans, and risk workflows. An AI platform owns the gateway boundary for developer API Keys or delegated product access; the connected product remains the source of truth for its users, sessions, subscriptions, and orders. Enterprise owns employee and department governance; Personal owns only its workspace. These are different business roots, not four pages of one customer model.

Linux Release installation requires `--deployment` (or installer input `ASTERROUTER_DEPLOYMENT_ROLE`); Docker and source development use `/setup`, which requires an explicit choice and does not preselect a role. Production and Docker deployments persist the selection in PostgreSQL and fix it for that instance. Source development without `ASTERROUTER_SERVER_STORAGE_DATABASE_URL` uses temporary in-memory storage, so its setup is intentionally lost when the process restarts and must not be treated as a production installation. At later PostgreSQL-backed starts, a configured role must match the persisted role or startup fails. Run another instance when a second model is needed.

### Linux release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash -s -- install --deployment enterprise
```

Replace `enterprise` with `personal`, `relay_operator`, or `platform` as appropriate. The installer deploys AsterRouter to `/opt/asterrouter` and creates the service configuration under `/etc/asterrouter`. Production startup requires PostgreSQL, a stable encryption key, and an administrator password or token. New Linux installations without a role are rejected before download or configuration changes.

### Docker

```bash
docker compose up --build
```

Open `http://localhost:8080/setup` to choose one deployment role and review its business boundary before completing setup. After installation, AsterRouter clears any browser session left by another deployment role and asks you to sign in again before opening the selected console. Retrying the same installation choice is safe. A system administrator can switch the active role later from Settings; the previous role is hidden but its data is retained. For non-interactive runtime deployment, set `ASTERROUTER_SERVER_BOOTSTRAP_DEPLOYMENT_ROLE=platform`.

## Private deployment and managed delivery

AsterRouter supports three delivery models:

- **Self-managed:** deploy and operate AsterRouter in your own environment.
- **Private delivery:** deploy into a customer-controlled network with installation, migration, and acceptance support.
- **Managed operations:** ongoing upgrades, backups, health checks, diagnostics, and operational support while the customer retains control of data and provider credentials.

Every delivery model can start with a low-cost single-server deployment for predictable traffic. The current release runs with the AsterRouter application and PostgreSQL; Redis and Kubernetes are not current installation requirements. The V4 roadmap keeps a 4 vCPU / 4 GiB single-node acceptance target for modest workloads, then adds a role-based Kubernetes topology for bursty multimodal workloads without treating upstream provider concurrency as unlimited capacity.

The Core remains usable when official online services are unavailable. Prompts, responses, Provider Secrets, Workspace Keys, detailed gateway usage, and browser-captured supplier sessions are not uploaded by the official Feed synchronization path.

## Project links

- [Releases](https://github.com/astercloud/asterrouter/releases)
- [Build status](https://github.com/astercloud/asterrouter/actions)
- [Deployment environment template](./deploy/asterrouter.env.example)
- [简体中文说明](./README.zh-CN.md)

<details>
<summary><strong>Local development</strong></summary>

Install frontend dependencies and start both services:

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

The frontend runs at `http://localhost:5173` and proxies API traffic to the backend at `http://localhost:8080`.

To expose the one-click demo entry on the sign-in page, start the isolated demo mode explicitly:

```bash
bash scripts/dev.sh --demo
```

Demo mode enables the built-in demo account and must only be used locally or on an isolated public demo instance. Docker users can enable it with `ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true docker compose up --build`; production instances should keep it disabled.

Run backend tests:

```bash
cd backend
go test ./...
```

Build the frontend:

```bash
cd frontend
npm run build
```

See the [deployment environment template](./deploy/asterrouter.env.example).

</details>

## License

AsterRouter is licensed under the [Apache License 2.0](./LICENSE).
