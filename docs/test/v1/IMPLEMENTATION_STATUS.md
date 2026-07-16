# AsterRouter 测试计划 v1 实施状态

> 最近核验：2026-07-16 CST
>
> 基准提交：`aee45c9dec4f5a50d1bf515fd834fef6e4d07f4f`（dirty worktree）
>
> 结论：v1 的本地实现、测试入口和 CI/release/nightly 门禁已落地。当前不能宣称候选版本可发布，直到本轮变更在 GitHub Actions 的 PostgreSQL 16、Linux、Docker、arm64 和 nightly 环境产生真实成功证据。

配置与启动重构本地复核已通过：Go 1.26.1 全量与 race、34 个前端测试文件的 102 个用例、单源生产 smoke、installer 安装/升级/回滚，以及 Go 1.26 容器的 PostgreSQL ready、非 root、fail-closed 和 SIGTERM 验收。Linux candidate archive 和 GitHub Actions 证据仍由发布门禁负责。

## 1. 当前自动化入口

| 能力 | 自动化入口 | 当前本地证据 |
| --- | --- | --- |
| 后端全量 | `cd backend && go test -count=1 ./...` | 通过；本机无 PostgreSQL 时环境依赖测试按显式条件跳过 |
| 后端 race | `cd backend && go test -race -count=1 -timeout=15m ./...` | Go 1.26.1 本地运行通过；长期 nightly 仍需 Ubuntu 证据 |
| 后端覆盖率 | `bash scripts/test.sh backend-coverage` | 项目基线已升级到 Go 1.26.1；coverage profile 已生成，关键包继续执行 75% 渐进目标 |
| 前端单元/组件 | `cd frontend && npm run test:unit` | 34 文件、102 用例通过；覆盖率持续以渐进 ratchet 管理 |
| 开发浏览器 smoke | `cd frontend && npm run test:e2e:smoke` | demo 多 Surface Chromium smoke 通过；关键 API 旅程仅在桌面运行，其余视口按设计 skip |
| 首装浏览器旅程 | `bash scripts/test-setup-browser-journey.sh` | 空 runtime、单源构建、`platform` 首装持久化；桌面 1 通过、其余视口按设计 skip |
| 非 demo 候选路径模拟 | 每个 `enterprise`、`relay_operator`、`personal`、`platform` 启动独立 runtime/数据库；`configure-e2e-profiles.mjs` 验证其单 Profile 状态 | 本机已验证 `enterprise` J01-J05 与 `platform` Surface；候选 archive 的 Linux/PostgreSQL 实跑由 CI 负责 |
| J04 失败矩阵 | `node scripts/gateway-failure-matrix.mjs` | 6 个场景通过：401/429/5xx、timeout、SSE 中断、并发、circuit、cooldown |
| J07 信任链 | `go test -json -count=1 ./internal/plugins -run '^(TestPluginTrustChainCatalogToSidecarFeed|...)$'` | catalog 到真实 sidecar helper、篡改、revocation、checksum、回滚、token scope 通过 |
| 短 soak 回归 | `ASTER_GATEWAY_SOAK=1 ASTER_GATEWAY_SOAK_DURATION=20s ...` | 1,615 普通/流式请求通过；goroutine 增量 3；30 分钟 nightly 仍待执行 |
| 发布包静态验收 | `ASTER_DIST_DIR=<empty-dir> bash scripts/build-release.sh 0.5.0 && ...test-release-artifacts.sh 0.5.0` | amd64/arm64 archive、checksum、manifest 与二进制元数据通过；本机不能执行 Linux binary |

本地环境版本仅用于快速反馈。项目 CI 的 Go 1.26、Node 24 与 Ubuntu Linux 结果才是版本和发布门禁的事实来源。

## 2. Phase 状态

| Phase | 状态 | 已完成 | 待真实环境验收 |
| --- | --- | --- | --- |
| Phase 0 | 实现完成 | 统一入口、Go/Node/Docker 版本事实、format/vet/coverage、PostgreSQL CI、schema parity | 将 checks 设为仓库 required checks 需管理员权限；PostgreSQL 16 CI 需在本轮 commit 运行 |
| Phase 1 | 实现完成 | fake OpenAI、Repository 持久化/事务/幂等、migration、P0 负路径、race | fake identity/SMTP/S3 仍按各领域 fixture 实现；本轮 PostgreSQL CI 需通过 |
| Phase 2 | 实现完成 | Vitest、Vue Test Utils、API/router/store/component、Chromium desktop/compact/mobile、独立空库 setup | P0 表单与 branch coverage 仍需逐步提高 |
| Phase 3 | 实现完成，待发布环境证据 | J01-J09 自动化、单源构建、container/release acceptance、候选 archive browser journey、恢复与升级 jobs | Linux amd64/arm64、Docker、PostgreSQL recovery、candidate archive browser journey 的本轮 Actions artifact |
| Phase 4 | 实现完成，基线采集中 | race、30 分钟 soak、CodeQL/govulncheck/npm audit/gitleaks/Trivy、axe、全浏览器 nightly、性能 JSON、flaky trend artifact | 3 次固定 Ubuntu 24.04 benchmark 后确认基线；nightly 30 分钟 soak、Firefox/WebKit 与 trend 首次真实报告 |

## 3. J01-J09 覆盖状态

| Journey | 当前自动化 | 本地验证 | 剩余发布证据 |
| --- | --- | --- | --- |
| J01 首装到首个网关请求 | 独立空 runtime setup；Playwright Provider/Account/Model/Route/Key；JSON/SSE；usage/trace/audit | 首装通过；`enterprise` 单 Profile source runtime 关键旅程通过 | Linux candidate archive + PostgreSQL 16 |
| J02 会话即时撤销 | 浏览器 logout、角色变更、禁用；HTTP 改密/TOTP/session version | smoke 通过 | candidate archive、PostgreSQL restart 行为 |
| J03 部门/Owner 隔离 | 浏览器部门用户、Key、usage、trace、alert、cost、export；Service/HTTP | 桌面旅程通过 | PostgreSQL 全栈 CI |
| J04 路由/流式/失败切换 | 浏览器 primary 500 fallback；机器可读 6 场景 Go 矩阵 | browser 与 matrix 通过 | Linux CI artifact |
| J05 配额/预算/告警 | 浏览器 80%/100%、去重、升级、429、usage/trace/audit | 桌面旅程通过 | PostgreSQL full-stack CI |
| J06 Operator/Customer | 浏览器 allocation/reclaim/correction、usage、账单、通知、无支付 recharge、export；PostgreSQL 并发兑换 | 桌面旅程和 memory 契约通过 | PostgreSQL 并发 CI |
| J07 插件信任链 | signed catalog -> package -> install -> enable -> scoped token -> real sidecar helper/feed；负路径矩阵 | 窄测通过 | CI JSON artifact |
| J08 生命周期/恢复/升级 | retention、PostgreSQL backup 到空库恢复、plugin/export 抽样、v0.3.0 schema candidate upgrade | 本机无 PostgreSQL；测试为显式 skip | PostgreSQL 16 recovery/upgrade artifact |
| J09 多 Surface | admin/console/operator/portal/customer/account、locale/theme、reload、keyboard、a11y、三视口 | demo 多 Surface smoke 与 `platform` 单 Profile Surface 旅程通过 | Firefox/WebKit nightly artifact |

## 4. 新增证据与约束

- `scripts/test-release-browser-journeys.sh` 先以 archive 内 Linux binary 完成一次空数据库 `platform` 首装，再为 `enterprise`、`relay_operator`、`personal`、`platform` 分别启动候选 runtime 与独立 PostgreSQL 数据库；这避免将业务 Profile 混入同一实例。首装和各 profile 的 Chromium artifact 分别保存。
- `scripts/test-setup-browser-journey.sh` 支持 source runtime 或 `ASTER_SETUP_JOURNEY_BINARY`；release job 使用后者，避免只验证开发服务器。
- `scripts/benchmark-report.mjs` 将五次 Go benchmark 转为 JSON。`docs/test/v1/performance-baseline.ubuntu-24.04.json` 处于 `bootstrap_required`：三次成功 nightly 后须在评审中写入中位数并标记 `confirmed`，随后 20% latency、10% allocation 与 0 allocation 增长成为门禁。
- `scripts/flaky-trend.mjs` 聚合 Go JSON、JUnit 和最近 nightly health artifact。它区分跨运行 observation 与实际 retry；发现同一测试既失败又通过时会写 artifact 并失败，P0 不允许 quarantine。
- CI 的 JSON evidence pipeline 使用 `pipefail`，测试失败不能被 `tee` 掩盖。
- 普通 `scripts/test.sh all` 包含浏览器 smoke；`@setup` 只通过独立空 runtime 执行，避免与 demo 数据共享状态。

## 5. 未完成的发布阻断项

以下不是待实现代码，而是必须取得的外部、不可由当前 macOS 工作站代替的证据：

1. 本轮工作树提交后，GitHub Actions `CI`、`Build Artifacts`、`Security Scan` 全绿；历史 `f8378c5c` 成功记录不能证明当前变更。
2. PostgreSQL 16：全后端、J06 并发兑换、J08 backup/empty-db restore、v0.3.0 upgrade fixture 和 PostgreSQL full-stack browser run。
3. Ubuntu Linux：container non-root/health/SIGTERM、amd64 candidate runtime、arm64 QEMU、install/upgrade/rollback、archive browser journey。
4. Nightly：30 分钟 normal/streaming soak、Firefox/WebKit、race、性能 bootstrap artifact、flaky trend artifact。
5. 仓库管理员将所需 CI/security checks 配置为 required checks；本计划无权替仓库修改分支保护。
6. 覆盖率目标仍是渐进门槛：核心 Go 包和前端关键分支尚未全部达到文档中的 75%/80% 目标，不能用现有总覆盖率声称达标。

当前 quarantine：无。当前本地 Docker：不可用。当前本地 PostgreSQL：不可用。
