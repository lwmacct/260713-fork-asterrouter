# AsterRouter 测试计划 v1 实施状态

> 最近核验：2026-07-16 CST
>
> 基准提交：`0c2b61d70f9eb05aec9638e402ba624bbdc3629a`（`main`）
>
> 结论：Phase 0-4 的实现和历史候选发布验收已完成，性能基线已确认。`main` 的配置重构已通过 CI、Security Scan 和 Build Artifacts；`v0.15.0` 候选仍必须在本轮提交上重新取得同等证据。

配置与启动重构已在 `0c2b61d` 通过 PostgreSQL、浏览器、容器、安全、候选运行时以及 Linux 安装/升级/回滚门禁。当前发布分支新增空集合 API 契约修复和前端响应归一化，必须重新运行全量测试并由 GitHub Actions 验证。

## 1. 当前自动化与证据

| 能力 | 自动化入口 | 已核验证据 |
| --- | --- | --- |
| 后端全量 | `cd backend && go test -count=1 ./...` | `0c2b61d` 的 PostgreSQL 16 CI 通过；发布分支待复验 |
| 后端 race | `cd backend && go test -race -count=1 -timeout=15m ./...` | Go 1.26.1 本地基线通过；发布分支待复验 |
| 后端覆盖率 | `bash scripts/test.sh backend-coverage` | 项目基线使用 Go 1.26.1；关键包继续执行 75% 渐进目标 |
| 前端单元/组件 | `cd frontend && npm run test:unit` | `0c2b61d` 的 34 文件、102 用例通过；发布分支待复验 |
| 开发浏览器 smoke | `cd frontend && npm run test:e2e:smoke` | `0c2b61d` 的 demo 多 Surface Chromium smoke 通过 |
| 首装与候选旅程 | `bash scripts/test-setup-browser-journey.sh`、`bash scripts/test-release-browser-journeys.sh <version>` | `0c2b61d` 的空库首装、四个单 Profile 候选 runtime 和浏览器旅程通过 |
| 非 demo 候选路径模拟 | 每个部署角色使用独立 runtime 和数据库 | `enterprise`、`relay_operator`、`personal`、`platform` 候选路径通过 |
| J04 失败矩阵 | `node scripts/gateway-failure-matrix.mjs` | 6 个场景通过：401/429/5xx、timeout、SSE 中断、并发、circuit、cooldown |
| J07 信任链 | `go test -json -count=1 ./internal/plugins -run '^(TestPluginTrustChainCatalogToSidecarFeed|...)$'` | catalog 到真实 sidecar helper、篡改、revocation、checksum、回滚、token scope 通过 |
| 30 分钟 soak | `ASTER_GATEWAY_SOAK=1 ASTER_GATEWAY_SOAK_DURATION=30m go test ...` | 三次历史 Nightly 均通过；新发布提交需再次运行 |
| 发布验收 | build/release workflows | `0c2b61d` 的 amd64/arm64、QEMU、checksum、候选 runtime 和安装/升级/回滚通过 |
| 安全 | CodeQL、govulncheck、npm audit、gitleaks、Trivy | `0c2b61d` 全部通过，无未接受高危结果 |

本地环境版本仅用于快速反馈。项目 CI 的 Go 1.26、Node 24 与 Ubuntu Linux 结果才是版本和发布门禁的事实来源。

## 2. Phase 状态

| Phase | 状态 | 完成证据 | 后续治理 |
| --- | --- | --- | --- |
| Phase 0 | 实现与环境证据完成 | 统一入口、版本事实、format/vet/coverage、PostgreSQL、schema parity | 将 checks 设为 required |
| Phase 1 | 完成 | fake upstream、Repository 契约、migration、P0 负路径、race | 覆盖率按变更包 ratchet |
| Phase 2 | 完成 | Vitest、Vue Test Utils、API/router/store/component、三视口 Chromium、独立 setup | 新增/修改关键前端逻辑执行 80% 目标 |
| Phase 3 | 完成 | J01-J09、单源构建、container/release、候选 archive、恢复与升级证据 | 无环境证据缺口 |
| Phase 4 | 完成，门禁修正待推送 | 三次 race/30 分钟 soak/全浏览器/benchmark/flaky trend；安全矩阵；confirmed baseline | 推送 `pipefail` 防回归检查并在新 SHA 复验 |

## 3. J01-J09 覆盖状态

| Journey | 自动化 | 候选环境证据 |
| --- | --- | --- |
| J01 首装到首个网关请求 | setup、Provider/Account/Model/Route/Key、JSON/SSE、usage/trace/audit | Linux candidate + PostgreSQL 16 通过 |
| J02 会话即时撤销 | logout、角色变化、禁用、改密/TOTP/session version | candidate + PostgreSQL 通过 |
| J03 部门/Owner 隔离 | 用户、Key、usage、trace、alert、cost、同步/异步 export | PostgreSQL 全栈通过 |
| J04 路由/流式/失败切换 | 浏览器 fallback + 六场景机器可读矩阵 | Linux CI 通过 |
| J05 配额/预算/告警 | 80%/100%、去重、升级、429、usage/trace/audit | PostgreSQL 全栈通过 |
| J06 Operator/Customer | allocation/reclaim/correction、账单、通知、充值、export、并发兑换 | PostgreSQL 并发与候选旅程通过 |
| J07 插件信任链 | signed catalog -> package -> install -> enable -> scoped token -> sidecar | CI JSON evidence 通过 |
| J08 生命周期/恢复/升级 | retention、backup/empty-db restore、关键数据抽样、v0.3.0 upgrade | PostgreSQL 16 recovery/upgrade 通过 |
| J09 多 Surface | 六个 Surface、locale/theme/reload/keyboard/a11y/响应式 | Chromium/Firefox/WebKit Nightly 通过 |

## 4. GitHub Actions 证据

- `7eb4c5e`：CI [29311002515](https://github.com/astercloud/asterrouter/actions/runs/29311002515)、Build Artifacts [29311002517](https://github.com/astercloud/asterrouter/actions/runs/29311002517)、Security Scan [29311002477](https://github.com/astercloud/asterrouter/actions/runs/29311002477) 均成功。
- `v0.6.0`：GitHub Release [29311004993](https://github.com/astercloud/asterrouter/actions/runs/29311004993) 成功，发布资产已生成。
- 三次 Nightly：[29313835627](https://github.com/astercloud/asterrouter/actions/runs/29313835627)、[29313835542](https://github.com/astercloud/asterrouter/actions/runs/29313835542)、[29313835587](https://github.com/astercloud/asterrouter/actions/runs/29313835587) 均成功。
- flaky trend 最终汇总覆盖 386 个测试、790 次通过、0 次失败、0 suspected flaky、0 repeated failure；当前 quarantine 为空。
- `performance-baseline.ubuntu-24.04.json` 使用上述三次运行的 per-run median 再取中位数，状态为 `confirmed`；后续门禁为 latency 1.2x、bytes 1.1x、allocations 1.0x。

## 5. 当前未闭环项

1. Nightly soak 已使用显式 `pipefail`，并由前端 `check-workflow-pipefail.mjs` 门禁防止失败传播回归；仍需在新发布 SHA 上运行一次 Nightly。
2. `v0.15.0` 候选必须重新通过 CI、Build Artifacts、Security Scan 和 GitHub Release，历史成功记录不能替代本轮证据。
3. `main` 当前没有 branch protection 或 ruleset。需将 `backend`、`frontend`、`e2e`、`recovery`、`container-smoke`、`build`、`secrets`、`codeql`、`dependencies`、`container` 配置为 required checks。
4. 覆盖率的 75%/80% 是渐进 ratchet 目标，当前总量尚未达到；不得把现有总覆盖率描述为达标。它不替代已完成的 P0 旅程、隔离、恢复、发布和安全证据。

当前 quarantine：无。当前本地 Docker：不可用。当前本地 PostgreSQL CLI 可用，但无本地 PostgreSQL 服务；环境相关事实以 GitHub Actions 产物为准。
