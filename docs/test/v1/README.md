# AsterRouter 全项目测试计划 v1

> 状态：Phase 0-4 实施与候选环境证据完成；等待将修正后的 Nightly 失败传播门禁推送并固化为 `main` required checks
>
> 制定日期：2026-07-13
>
> 适用范围：AsterRouter v0.3.x 及后续 V3 增量
>
> 事实来源：当前代码、CI、`docs/product-positioning.md`、`docs/roadmap/v3/README.md`

实施进度、最近验证证据与发布阻断项见 [`IMPLEMENTATION_STATUS.md`](./IMPLEMENTATION_STATUS.md)。
随机失败的判定、隔离、retry 和解除规则见 [`FLAKY_TEST_POLICY.md`](./FLAKY_TEST_POLICY.md)。

## 1. 结论与执行原则

本计划采用风险优先的分层测试，不以单一覆盖率数字代替业务验收。测试顺序固定为：变更点回归测试、包级测试、后端与前端全量测试、PostgreSQL 集成、关键 E2E、发布环境与非功能测试。

最高优先级是以下不可逆或高影响边界：

1. 身份认证、会话撤销、TOTP、外部身份绑定。
2. RBAC、部门/用户/Surface 隔离和资源不存在语义。
3. Workspace Key、模型策略、配额、预算、限流与风险阻断。
4. 网关路由、流式响应、失败切换、用量/成本/Trace/审计证据。
5. Provider Secret、插件签名、License、Sidecar 权限和外部服务信任边界。
6. PostgreSQL 事务、Schema 演进、导出、保留期清理、备份恢复与升级回滚。
7. Operator/Customer 额度、账单、通知的原子性、幂等性和租户隔离。

前端不能再以“类型检查通过、构建成功”替代功能测试。v1 的主要建设目标是补齐前端单元/组件测试、真实 PostgreSQL 集成测试、关键浏览器 E2E 和发布包验收。

## 2. 当前基线

基线检查于 2026-07-13 在本地工作区执行。

| 项目 | 当前状态 | 基线结果 |
| --- | --- | --- |
| 后端 | Go 1.26.1，使用 cfgm 统一文件、环境变量与 CLI 配置 | `go test ./...` 通过 |
| 后端覆盖面 | auth/config/controlplane/operator/plugins/server/settings/system | server 83、controlplane 68、plugins 35 个测试函数，分布不均 |
| PostgreSQL | 仅 1 个测试由 `ASTER_TEST_DATABASE_URL` 控制 | 默认本地/CI 会跳过该测试 |
| 前端 | Vue 3 + TypeScript + Vite | 0 个单元、组件或 E2E 测试文件 |
| 前端静态门禁 | `typecheck`、`build` | 均通过 |
| 产品边界检查 | `check:enterprise-surface` 已存在 | 本地通过，但未进入 `ci.yml` |
| CI | 后端 `go test ./...`；前端 typecheck/build | 缺少 race、覆盖率、PostgreSQL、前端测试、E2E、发布烟测 |
| 安全 | CodeQL 覆盖 Go 与 JS/TS | 缺少依赖漏洞和容器镜像门禁 |
| 发布 | Linux amd64/arm64 产物、checksum、manifest | 构建产物未在独立运行环境执行验收 |

需要在 Phase 0 先处理的结构性风险：

- Go module、CI 和 Docker 构建统一以 Go 1.26 系列为基线；发布验收必须继续验证实际工具链版本。
- PostgreSQL Schema 由各 Repository 内联 SQL 在运行时创建，同时保留 `backend/migrations/*.sql`；两套事实源存在漂移风险。
- `backend/migrations` 的编号从 018 跳到 020，历史 `019` 已删除。必须明确迁移文件是部署事实源、审计快照还是历史参考，并为相应规则加自动校验。
- 当前大多数后端测试使用内存 Repository，不能证明 SQL 约束、事务原子性、重启持久化和升级兼容性。
- 当前产品代码只注册 OpenAI-compatible `/v1/models` 和 `/v1/chat/completions`。Anthropic Messages、Gemini 等路线图协议仅在实现后进入强制契约矩阵，测试不得把路线图当成已交付事实。

## 3. 质量目标与门禁

### 3.1 缺陷门禁

| 级别 | 定义 | 合并/发布规则 |
| --- | --- | --- |
| P0 | 数据丢失、越权、Secret 泄漏、认证绕过、错误计费、不可恢复升级 | 0 个，立即阻断 |
| P1 | 核心流程不可用、跨租户可见、网关错误路由、备份不可恢复 | 合并与发布均为 0 个 |
| P2 | 有规避方式的功能错误、明显 UI/兼容性问题、非关键审计缺失 | 必须有 owner、修复版本和回归用例 |
| P3 | 低影响体验问题 | 可进入 backlog，不得掩盖更高等级问题 |

### 3.2 覆盖要求

- 每个 Bug 修复必须新增一个能复现原问题的回归测试。
- auth、RBAC、gateway、billing、migration、backup/restore、plugin trust 的行为变更必须同时覆盖成功和拒绝/失败路径。
- 新增或修改的关键业务分支必须有可观察行为断言；仅断言“不报错”不算覆盖。
- Phase 0 先采集 Go 覆盖率，不立即用未知基线设置全局硬阈值。之后采用 ratchet：变更包覆盖率不得下降，核心包逐步达到并保持 75% 以上。
- 前端测试基础建立后，新增/修改的 API client、router guard、状态逻辑和关键表单目标行覆盖率不低于 80%；视觉和多步骤流程由 E2E 行为断言补足。
- 覆盖率是缺口提示，不替代关键旅程、隔离矩阵和失败注入。

### 3.3 稳定性要求

- PR 必跑测试连续 3 次无随机失败后才能设为 required check。
- 随机失败不能通过无条件 retry 隐藏；隔离根因后才可对明确的网络/进程边界使用有限重试。
- 测试不得依赖真实公网 Provider、官方服务、真实邮件、真实对象存储或生产身份源。
- 时间、随机 ID、端口、数据库和文件目录必须可隔离；并行执行不得共享可变业务数据。

## 4. 测试分层与工具

| 层级 | 目标 | 工具/方式 | 主要门禁 |
| --- | --- | --- | --- |
| L0 静态检查 | 格式、类型、构建、产品边界、基础安全 | `gofmt`、`go vet`、vue-tsc、Vite、现有文案检查、CodeQL | PR |
| L1 单元测试 | 纯验证、策略、转换、加解密、调度算法 | Go `testing`、Vitest | PR |
| L2 组件/API 集成 | Service + Repository 接口、HTTP、鉴权、mock upstream | `httptest`、Vue Test Utils | PR |
| L3 PostgreSQL 集成 | SQL、约束、事务、Schema、重启持久化 | PostgreSQL 16 service + 独立测试库 | PR/夜间 |
| L4 契约测试 | OpenAI-compatible 请求/响应、流式、错误分类 | fake upstream + golden schema/assertions | PR |
| L5 浏览器 E2E | 多角色、多 Surface、真实前后端工作流 | Playwright，Chromium 为 PR 门禁 | PR/夜间 |
| L6 发布环境 | 单源静态资源、Docker、Linux 包、安装升级回滚 | Docker、Ubuntu amd64/arm64 runner/QEMU | 发布 |
| L7 非功能 | race、性能、稳定性、安全、恢复演练 | `go test -race`、基准/压测、依赖与镜像扫描 | 夜间/发布 |

前端测试依赖保持最小集合：

- Vitest：单元测试运行器。
- Vue Test Utils：Vue 组件挂载与交互。
- `happy-dom` 或 `jsdom`：选择一个 DOM 环境，不并存。
- Playwright：浏览器 E2E；PR 只跑 Chromium，Firefox/WebKit 进入夜间矩阵。

不引入第二套 E2E 框架，不为测试单独复制业务客户端。

## 5. 环境与测试数据

### 5.1 环境矩阵

| 环境 | 存储 | 用途 | 外部依赖 |
| --- | --- | --- | --- |
| Fast | Memory | Go 单元、Service、HTTP 快速回归 | 全部 fake |
| DB | PostgreSQL 16 | Repository、Schema、事务、导出、重启 | fake upstream/identity/official services |
| Full stack | PostgreSQL 16 + Vite/单源构建 | 浏览器关键旅程 | 全部 fake |
| Container | `docker compose`/生产镜像 | 非 root、网络、健康、单源资源、信号 | 本地 fake services |
| Release | Ubuntu Linux amd64/arm64 | 包内容、checksum、安装、升级、回滚 | 隔离测试机 |
| Remote | 授权的 staging | 部署差异、代理/TLS、真实拓扑 | 仅批准的 sandbox 服务 |

### 5.2 固定测试角色

每次全栈测试至少创建以下独立身份：

- `global_admin`：全局管理权限。
- `department_admin_a`：仅 Department A。
- `developer_a1`、`developer_a2`：同部门不同 owner。
- `developer_b1`：Department B。
- `operator_user`：relay operator Surface。
- `customer_user_a`、`customer_user_b`：相互隔离的客户身份。
- `disabled_user`：验证登录与旧会话失效。
- `auditor`：只读审计权限。

资源 ID、邮箱、Key 名称和 request ID 均带唯一 `test_run_id`。测试结束只清理该 run 创建的数据。

### 5.3 Fake 服务

建立一个可编程的 fake upstream，支持：

- `/v1/models` 与 `/v1/chat/completions`；
- 普通 JSON 和 SSE streaming；
- 200、400、401、429、500、超时、断流、畸形 JSON；
- 可记录请求头、模型映射、body、取消信号和调用顺序；
- 可配置首节点失败、次节点成功以验证 failover；
- 不记录真实 prompt/secret，测试数据使用明确的 synthetic marker。

OIDC/Feishu/DingTalk/GitHub/Google、SMTP、S3、官方 catalog/license/feed 和 Sidecar Host 使用同样的本地 fake 原则。

## 6. 全项目覆盖矩阵

| 域 | 优先级 | 必测内容 | 主要层级 |
| --- | --- | --- | --- |
| 启动与配置 | P0 | source/release 配置校验、profile/surface、缺失 Secret/DB fail-closed、健康/就绪、优雅退出 | L1/L2/L6 |
| 本地认证 | P0 | 登录、注册、邮箱验证、找回密码、限流、协议勾选、登出、密码变更后旧 token 失效 | L1/L2/L5 |
| 企业身份 | P0 | OIDC PKCE/state/nonce/JWKS、Feishu/Lark 区域、DingTalk、GitHub/Google verified email、绑定/解绑冲突 | L1/L2/L5 |
| TOTP 与会话 | P0 | enrollment、confirm、recovery code 一次性、disable、session version 持久化撤销 | L1/L2/L3/L5 |
| RBAC/隔离 | P0 | global/resource/department scope，用户/Key/usage/trace/alert/export 所有权，不可见统一按不存在 | L1/L2/L3/L5 |
| Provider 与路由 | P0 | Secret 加密/掩码、健康检查、模型映射、优先级、权重、sticky、RPM/TPM、并发、circuit/cooldown | L1/L2/L4 |
| Gateway 契约 | P0 | Key 鉴权、allowlist、body 限制、普通/streaming、取消、上游错误透传/归类、failover | L2/L4/L7 |
| 策略与额度 | P0 | QPS、token quota、budget、overage、policy version/snapshot、拒绝请求 evidence、告警去重升级 | L1/L2/L3 |
| 用量与成本 | P0 | token/cost 计算、模型定价、用户/部门/Group/Key 聚合、分页外汇总、幂等写入 | L1/L2/L3 |
| Operator | P0 | customer/group/plan/key/balance/risk 生命周期、额度流水、自动阻断与人工解除、跨客户隔离 | L1/L2/L3/L5 |
| Customer | P0 | wallet、账单分页/导出、兑换原子性、无支付 Provider 不改余额、通知偏好/已读/广播隔离 | L1/L2/L3/L5 |
| 审计/Trace/告警 | P0 | 关键写操作证据、脱敏、筛选/汇总/导出、状态生命周期、拒绝请求记录 | L1/L2/L3/L5 |
| 数据保留/导出 | P0 | owner 限制、异步状态、下载、过期、分类清理、活动告警保留、清理自身审计 | L2/L3/L5 |
| 插件与供应链 | P0 | catalog/license/feed 签名、篡改/回滚/revoke、package checksum、安装回滚、token scope、Sidecar 权限 | L1/L2/L3/L6 |
| 备份/恢复/升级 | P0 | 手动/定时备份、S3、保留策略、诊断脱敏、空库恢复、升级失败回滚、版本 manifest | L2/L3/L6 |
| 前端公共能力 | P1 | API 401 跳转、router guard、profile surface、i18n、theme、错误/loading/empty state、响应式与 a11y | L1/L2/L5 |
| Admin/Console UI | P1 | CRUD、确认对话框、Secret 一次显示、筛选/分页/导出、模拟器、系统设置 | L2/L5 |
| Portal UI | P0 | 仅本人 Key/usage/trace、创建/轮换/禁用权限、集成文档不泄漏 Provider 内部信息 | L2/L5 |
| Operator/Customer UI | P0 | Surface 门禁、额度与账单操作、通知、风险处理、跨客户不可见 | L2/L5 |
| 发布与兼容 | P0 | Go/Node 版本一致性、Linux amd64/arm64、容器非 root、SPA deep link、checksum/install/rollback | L6 |

## 7. 关键端到端旅程

以下旅程是 v1 的最小发布 E2E 集合。每个旅程都要断言 UI、API、数据库结果以及应有的 audit/usage/trace/alert 证据。

### J01 首次部署到首个网关请求

1. 空库启动并完成 setup/profile。
2. 管理员登录，创建 Provider、Gateway Model、Route、Policy 和 Workspace Key。
3. 通过 fake upstream 发起普通和流式 chat completion。
4. 验证模型映射、Key 最后使用时间、用量、成本、Trace、路由 attempts 和审计。

### J02 会话即时撤销

分别执行登出、改密、密码重置、禁用用户、角色变化和 TOTP 变化，验证旧 Bearer Token 对所有受保护 Surface 立即失效，重启后仍失效。

### J03 部门与 Owner 隔离

用 Department A 管理员和三个 developer 交叉访问用户、Key、usage、trace、alert、cost、同步/异步 export。验证显式查询条件与授权范围取交集，跨域资源返回不存在语义。

### J04 路由、流式与失败切换

配置两个 provider account，注入 timeout/429/500/断流/circuit/cooldown/concurrency 场景。验证候选顺序、容量释放、是否允许 failover、最终错误、usage 与 trace 一致。

### J05 配额、预算与告警

把 token quota 和 budget 推进至 80% 与 100%，验证预警去重、严重度升级、`overage_action`、拒绝请求 evidence 和下一周期边界。

### J06 Operator/Customer 账务与通知

执行额度分配/收回/成本修正、兑换码并发兑换、无支付 Provider 的充值单、账单导出、用量通知、月账单和广播。验证原子性、幂等和跨客户隔离。

### J07 插件信任链

覆盖 signed catalog -> package download -> checksum -> install -> enable -> scoped token -> Sidecar feed；逐点注入签名篡改、版本回滚、revocation 和 repository write failure。

### J08 数据生命周期与灾难恢复

生成 usage/trace/audit/alert/export/plugin 数据，执行 retention、备份、进程重启、空库恢复和版本升级。按关键表计数与抽样记录验证恢复，确认 Secret 仍可解密且过期数据规则正确。

### J09 多 Surface 浏览器验收

在 admin、console、operator、portal、customer、account 间验证允许访问、导航和拒绝跳转；覆盖中英文、明暗主题、桌面与移动视口、键盘操作和浏览器刷新持久化。

## 8. 分阶段实施计划

工作量是工程人日估算，不含业务缺陷修复时间。

### Phase 0：建立可重复基线（2-3 人日）

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| TST-001 | 统一本地与 CI 的 Go/Node 版本，明确 Docker toolchain | 版本矩阵和通过的容器构建 |
| TST-002 | 增加统一测试入口，保留窄测与全量命令 | 可组合的 backend/frontend/ci 命令 |
| TST-003 | 采集 Go coverage，接入 `go vet`、`gofmt` 检查和 Enterprise surface check | CI artifact 与 required checks |
| TST-004 | PostgreSQL 16 CI service 和每次 run 独立数据库 | `ASTER_TEST_DATABASE_URL` 在 CI 生效 |
| TST-005 | 定义 runtime SQL 与 migration 文件的唯一事实源及漂移校验 | Schema 规则和自动测试 |

退出条件：现有基线全绿；PostgreSQL 测试不再默认跳过；CI 能报告覆盖率和明确的 skip。

### Phase 1：后端与数据库补强（4-6 人日）

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| TST-101 | 建立共享 fake upstream/identity/official service fixture | 可编程 HTTP fixture |
| TST-102 | 对所有 PostgreSQL Repository 增加契约测试 | memory/Postgres 共用行为矩阵 |
| TST-103 | 新库、历史库升级、重复初始化、失败回滚和 Schema parity | migration integration suite |
| TST-104 | 补齐 auth/RBAC/gateway/billing/plugin/system 的 P0 负路径 | P0 回归集 |
| TST-105 | 对 scheduler、rate/concurrency、streaming、store 运行 race | 夜间 race job |

退出条件：覆盖矩阵所有 P0 后端域至少有一个成功与一个失败用例；DB 与 memory 的关键契约一致；race 无已知数据竞争。

### Phase 2：前端测试基础（4-5 人日）

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| TST-201 | 引入 Vitest + Vue Test Utils + 单一 DOM 环境 | `test:unit`、coverage 配置 |
| TST-202 | 覆盖 API client、401、router/profile guard、i18n/theme | 公共层单元测试 |
| TST-203 | 覆盖 P0 表单和状态组件 | loading/empty/error/success/disabled 测试 |
| TST-204 | 引入 Playwright，建立登录态、角色、数据库 seed fixture | `test:e2e` 和 trace/screenshot artifact |
| TST-205 | 建立 Chromium 桌面/移动 smoke | PR E2E smoke |

退出条件：前端新增逻辑有自动回归；关键路由和 401 不依赖手工验证；E2E 可在空环境一条命令启动并清理。

### Phase 3：关键旅程与发布验收（5-7 人日）

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| TST-301 | 自动化 J01-J09 | 关键旅程 suite |
| TST-302 | 单源生产构建和 SPA deep-link smoke | production smoke suite |
| TST-303 | Docker 非 root、健康、信号、配置 fail-closed | container acceptance suite |
| TST-304 | Linux amd64/arm64 包、checksum、version、install/upgrade/rollback | release acceptance job |
| TST-305 | 备份到空库恢复并抽样校验关键数据 | recovery rehearsal |

退出条件：J01-J09 全绿；release artifact 在独立环境运行成功；备份恢复有机器可读证据。

### Phase 4：非功能与长期门禁（3-5 人日）

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| TST-401 | 建立固定硬件/runner 的 gateway baseline 和回归阈值 | latency/throughput/resource 报告 |
| TST-402 | 30 分钟普通+streaming soak，观察连接、goroutine、内存 | soak 报告与泄漏门禁 |
| TST-403 | 依赖漏洞、Secret、容器镜像扫描 | security required checks |
| TST-404 | 全浏览器、全 locale/theme、可访问性与响应式夜间矩阵 | nightly UI report |
| TST-405 | 随机失败看板和 quarantine 时限 | flaky test policy |

退出条件：性能以已确认 baseline 做相对回归门禁；无未归属 flaky；安全高危为 0。

## 9. CI 分层

| 触发 | 必跑内容 | 目标时长 | 失败策略 |
| --- | --- | --- | --- |
| 每个 PR | L0、Go 全量、前端 unit、PostgreSQL P0、契约、Chromium smoke | 10-15 分钟 | required，禁止合并 |
| main push | PR 全集 + production single-origin + container smoke | 20 分钟内 | 阻断发布候选 |
| nightly | race、全部 PostgreSQL、全 E2E 浏览器、soak short、漏洞扫描 | 60 分钟内 | 建 issue，P0/P1 阻断发布 |
| tag/release | clean DB、upgrade fixture、J01/J08、Linux artifacts、install/rollback、checksum | 按发布窗口 | 任一失败禁止发布 |

CI artifact 至少保留：JUnit、coverage、Playwright trace、失败截图、容器日志、fake upstream 请求摘要、Schema diff、发布包清单和恢复校验结果。任何 artifact 都必须脱敏。

## 10. 当前命令与目标命令

当前已存在并已验证：

```bash
cd backend && go test ./...
cd frontend && npm run typecheck
cd frontend && npm run build
cd frontend && npm run check:enterprise-surface
```

当前可按风险执行：

```bash
cd backend && go test -race ./...
cd backend && go test -coverprofile=coverage.out ./...
cd backend && ASTER_TEST_DATABASE_URL='<isolated-url>' go test ./... -count=1
```

Phase 0-2 已提供以下命令：

```bash
npm run test:unit
npm run test:unit:coverage
npm run test:e2e
npm run test:e2e:smoke
```

Phase 3-4 新增以下入口：

```bash
# 空 runtime 的首装 profile 浏览器旅程（source runtime 或候选 binary）
bash scripts/test-setup-browser-journey.sh

# 候选 Linux archive 的首装和关键浏览器旅程（Linux amd64 + isolated PostgreSQL）
bash scripts/test-release-browser-journeys.sh <version>

# J04 失败注入矩阵与机器可读 evidence
node scripts/gateway-failure-matrix.mjs

# benchmark 文本、相对阈值报告和 flaky trend JSON
bash scripts/benchmark.sh
node scripts/flaky-trend.mjs --input <junit-or-go-json-dir> --output <report.json>
```

## 11. 测试执行与证据规范

每次执行记录以下信息：

- commit SHA、工作区差异、测试计划版本；
- OS/arch、Go/Node/PostgreSQL/浏览器版本；
- profile、storage mode、feature/config class；
- 执行命令、开始/结束时间、通过/失败/跳过数量；
- test run ID、失败 request ID、相关日志和 artifact；
- 未覆盖风险、skip 原因、owner 和到期时间。

失败报告必须包含最短复现步骤、期望/实际结果、影响 Surface/角色、是否可稳定复现和第一个错误证据。不得只附整段日志。

## 12. 发布退出条件

满足以下全部条件才可把候选版本标记为可发布：

1. 所有 required checks 通过，P0/P1 缺陷为 0。
2. J01-J09 在候选产物而不是开发服务器上通过。
3. PostgreSQL clean install、支持版本升级、重复初始化和恢复演练通过。
4. auth/RBAC/owner/customer 隔离矩阵无缺口。
5. 普通与 streaming gateway 契约、配额、失败切换、usage/trace/audit 一致。
6. Linux amd64/arm64 产物 checksum、`--version`、安装、升级、回滚通过。
7. CodeQL、依赖和镜像扫描无未接受的高危问题。
8. 所有 skip 和 flaky 均有 owner，不包含 P0 发布路径。
9. 测试证据已归档且不含 Secret、Token、真实 prompt 或个人数据。

## 13. 项目测试 skills

本计划配套以下项目级 skills：

| Codex 来源 | AsterRouter skill | 适配内容 |
| --- | --- | --- |
| `code-review-testing` | `$code-review-testing` | 从 Rust agent 测试规则改为 Go/Vue 风险分层与回归编写 |
| `remote-tests` | `$remote-tests` | 从 remote executor/Wine 改为 PostgreSQL、容器、Linux 发布和授权 staging |
| `test-tui` | `$test-ui` | 从 Codex TUI 交互改为 AsterRouter 多 Surface 浏览器测试 |

skills 负责单次任务的执行纪律；本文件负责全项目优先级、矩阵和发布门禁。发生冲突时，以更严格且更接近当前代码事实的规则为准。
