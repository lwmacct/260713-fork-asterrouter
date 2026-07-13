<div align="center">

# AsterRouter

**Spend less on AI. Keep every request under control.**

One gateway for multiple AI providers, automatic cost optimization, enterprise governance, and managed delivery.

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## One gateway. Better AI economics.

AsterRouter gives your team one controlled AI gateway today and is evolving into a single access layer for text, image, audio, and video AI. Connect the providers you already use, issue secure access to teams and products, and see where every token and dollar goes. The multimodal roadmap extends the same controls to media tasks and artifacts.

The next stage goes beyond cost reporting: pricing plugins keep supplier rates current, and cost-aware routing chooses the lowest-cost route that still meets your policy, health, and capacity requirements.

| | What you get |
| --- | --- |
| Lower cost | Compare eligible provider prices and route each request toward the best available economics. |
| One integration | Keep applications on stable APIs for streaming, media, and long-running jobs while providers and routes change behind the gateway. |
| Reliable traffic | Fail over around unhealthy, rate-limited, or capacity-constrained routes. |
| Enterprise control | Manage team access, model permissions, budgets, alerts, audit trails, and data retention. |
| Flexible delivery | Self-host on your infrastructure or use private deployment and managed operations. |

## How AsterRouter saves money

1. **Connect your providers.** Keep using authorized provider accounts and standard APIs.
2. **Keep prices current.** Price-source plugins sync supplier rates from supported APIs, signed feeds, imports, or the optional browser extension.
3. **Route with constraints.** AsterRouter first protects policy, availability, and capacity, then chooses the lowest expected-cost eligible route.
4. **Prove the result.** Usage and route evidence show the selected price, alternatives, actual cost, and measurable savings.

> Cost-aware quote selection, third-party price plugins, the savings ledger, and the browser extension are roadmap capabilities. The routing, governance, usage, cost allocation, audit, and plugin foundations are available today.

## Built for real organizations

**Engineering teams** get one endpoint and one key workflow across providers.

**Finance and operations** get cost allocation by user, department, group, key, and model, plus budgets and alerts.

**Security teams** get private deployment, encrypted secrets, scoped access, audit evidence, retention controls, and offline-capable operations.

**Platform owners** get route health, fallback, capacity controls, backups, upgrades, diagnostics, and a plugin system for provider-specific extensions.

## Deliver AI to your team or your customers

AsterRouter can power AI access behind internal tools and customer-facing products without forcing every business service to become an AI gateway.

| Scenario | How AsterRouter helps |
| --- | --- |
| Internal AI platform | Give employees, departments, services, and automation scoped AI access with shared governance and cost controls. |
| SaaS and desktop products | Keep your existing customer authentication while AsterRouter validates delegated access and enforces AI policy, quota, routing, and usage. |
| Content and media products | Extend the gateway to image, video, and audio workloads without rebuilding burst queues, provider-capacity scheduling, long-running jobs, artifact delivery, and media billing in every product. |
| Agent and developer platforms | Offer one branded AI endpoint across models and providers without exposing upstream credentials. |
| Partner and OEM delivery | Isolate tenants, apply partner-specific policy, and report usage back to the system that owns the commercial relationship. |

```text
Platform identity, session, plan, or entitlement
                       ↓
Platform-issued credential or signed request context
                       ↓
Internal app or customer client -> AsterRouter Gateway -> AI providers
```

Your platform remains the only source of truth for users, login sessions, tokens, tenants, subscriptions, and entitlements. AsterRouter does not create external users or replace platform authentication. It validates a platform-issued credential or trusted signed context, then owns only AI request enforcement: model policy, quota, routing, cost, usage, risk controls, and audit. Provider credentials never need to ship inside a desktop app, mobile app, or browser client.

The shared key, policy, routing, metering, and isolation foundations are available today. A canonical External Access Context plus standard JWT/JWKS, OIDC introspection, and platform-auth plugin contracts are roadmap capabilities for product and OEM integrations.

## One trusted Core, extended for each scenario

Different customers need different workflows, but they should not create different gateways. AsterRouter keeps security-critical decisions in one Core and adds optional integrations through plugins.

| Trusted Core | Scenario plugins |
| --- | --- |
| Gateway credentials, external access context, and tenant isolation | Platform authentication, JWT/JWKS, introspection, directory, and SSO adapters |
| Model policy, quota, budget, and risk controls | SaaS plans, subscriptions, and entitlement sync |
| Provider routing, fallback, and cost metering | Supplier pricing and browser-assisted collection |
| Fair queues, streaming sessions, media jobs, artifact policy, usage, and billing state | Provider protocols for GPT Image, Gemini, Midjourney-compatible services, Jimeng, and others |
| One Core from a single server to Kubernetes | Replaceable queue and artifact adapters for Redis, S3-compatible storage, Cloudflare R2, Alibaba Cloud OSS, and future brokers |
| Usage, trace, audit, and data governance | Notifications, exports, branding, and customer workflows |

Plugins contribute provider or business facts through controlled interfaces. They cannot bypass Core policy, issue unrestricted tokens, read unrelated secrets, or choose routes outside the trusted scheduler.

## Available today

- OpenAI-compatible model listing and Chat Completions, including streaming.
- Multi-provider routing with priority, weight, capacity, RPM/TPM limits, cooldown, circuit breaking, sticky routing, and fallback.
- Workspace Keys, model allowlists, rate limits, token quotas, budgets, and overage controls.
- Usage analytics, cost allocation, alerts, route traces, policy evidence, audit logs, and exports.
- Enterprise sign-in and access governance with OIDC, Feishu/Lark, DingTalk, departments, groups, and scoped roles.
- Admin Console, employee Portal, plugin center, backup and restore, diagnostics, and verified release updates.
- Personal, relay operator, and enterprise deployment profiles with English and Simplified Chinese interfaces.

The current gateway exposes OpenAI-compatible Models and Chat Completions. Responses, Embeddings, Anthropic Messages, Gemini, realtime sessions, image generation and editing, audio, video, media uploads, asynchronous jobs, and artifact delivery are roadmap capabilities and are not presented as available today.

The multimodal roadmap keeps low-cost deployments practical: PostgreSQL and Redis form the minimum production infrastructure, with no mandatory external message broker. Start with the same Core on one server, then split API, scheduler, modality workers, reconciliation, and artifact delivery into Kubernetes workloads when traffic becomes bursty. Worker pods and optional burst nodes can scale down during quiet periods, while provider capacity, tenant budgets, and cost ceilings remain hard limits. Media can stay local, use S3-compatible storage such as AWS S3, Cloudflare R2, or MinIO, use Alibaba Cloud OSS, or be delivered to customer-owned storage.

## Quick start

### Linux release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash
```

The installer deploys AsterRouter to `/opt/asterrouter` and creates the service configuration under `/etc/asterrouter`. Production startup requires PostgreSQL, a stable encryption key, and an administrator password or token.

### Docker

```bash
docker compose up --build
```

Open `http://localhost:8080/setup` to choose a profile and complete the initial configuration.

## Private deployment and managed delivery

AsterRouter supports three delivery models:

- **Self-managed:** deploy and operate AsterRouter in your own environment.
- **Private delivery:** deploy into a customer-controlled network with installation, migration, and acceptance support.
- **Managed operations:** ongoing upgrades, backups, health checks, diagnostics, and operational support while the customer retains control of data and provider credentials.

Every delivery model can start with a low-cost single-server deployment for predictable traffic, then move the same Core and data model to role-based Kubernetes workloads. Kubernetes scales AsterRouter pods and optional worker nodes around bursts; it never treats upstream provider concurrency as unlimited capacity.

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
