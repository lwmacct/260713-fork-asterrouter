# AsterRouter 产品定位

## 产品定义

AsterRouter 是面向企业团队、AI API 平台和客户产品的 AI Access Gateway，提供统一接入、成本治理与私有化/托管交付。

它位于内部应用或外部客户与已获授权的 AI Provider 之间，负责下游凭据、标准协议、模型策略、额度、供应商账号调度、用量、成本和审计。业务系统不需要重复承担 Provider 代理、流式转发、失败切换和用量解析。

```text
内部应用 / 外部客户
  |- AsterRouter API Key
  `- 产品平台委托凭据
              |
              v
       AsterRouter Gateway
              |
              v
  已授权的标准 AI Provider
```

## 核心价值

- **统一供给：** 用一个稳定入口向员工、服务、开发者和产品客户提供多供应商 AI 能力。
- **更省钱：** 在策略、健康和容量约束内比较合格线路，逐步实现价格插件、成本感知调度和可验证节省。
- **两种对外接入：** 可以直接签发 AsterRouter Gateway API Key，也可以保留 SaaS/OEM 的用户、Session 和权益体系，只委托 AI 访问上下文。
- **企业治理：** 按 Tenant、用户、部门、Group、Key Principal、模型和外部 Subject 管理权限、预算、用量与审计。
- **灵活交付：** 支持自主部署、私有化交付和托管运维；当前以低成本单机运行，Kubernetes 弹性部署属于后续路线图。
- **统一 Core：** 协议、Credential Source 和 Provider 可以扩展，但 Policy、候选规划、账号调度、Usage、Billing 和 Audit 只有一套事实源。

## 商业场景

| 场景 | 接入方式 | AsterRouter 责任 |
| --- | --- | --- |
| 企业内部 AI 平台 | 企业身份 + Personal/Workspace/Service Key | 模型权限、部门预算、路由、用量和审计 |
| AI API / 中转 / 开发者平台 | AsterRouter Customer/Service API Key | Key 生命周期、额度、账号调度、计费归属和上游隐藏 |
| SaaS / 桌面 / 移动产品 | 平台委托凭据或签名上下文 | 不接管平台用户体系，只执行 AI Policy、路由、成本和 Usage |
| 内容与媒体产品 | 两种外部 Credential 均可 | 同步/流式调用、异步 Job、供应商容量、Artifact 和媒体计费 |
| Partner / OEM | 独立 Tenant + API Key 或委托 Integration | Policy Ceiling、租户隔离、可靠签名用量回传和品牌交付 |
| 私有化与托管运维 | 同一 Core 的单机或 Kubernetes 部署 | 安装、迁移、备份、升级、诊断、弹性与成本运营 |

外部 API Key 只代表 Gateway Principal，不自动创建登录账号、Session 或 Portal 用户。平台委托模式中，平台仍是用户、登录、订阅、订单和支付的事实源。两种方式鉴权后都进入同一 Canonical Gateway Pipeline。

## 安装与产品形态

首次安装不是启用一个“大而全”的管理后台，而是在四个互相独立的部署角色中选择一个。四者共享 Provider、路由、策略、Credential 校验、计量和审计 Core；但不共享业务对象、后台导航、默认角色或数据查询范围。`profile` 仅是内部兼容标识，不表示可叠加的功能包。

| 部署角色（内部 ID） | 选择条件 | 管理边界 |
| --- | --- | --- |
| `personal` | 个人或小团队管理 Workspace、Key、Provider 和 Route | 不安装企业组织、中转客户账务或外部产品接入领域 |
| `relay_operator` | 业务核心是 Customer、Balance、Plan 和 Risk | 不将其 Customer/Plan/Balance 当作通用平台 Tenant、订阅或订单事实源 |
| `enterprise` | 业务核心是员工、部门、企业 SSO、内部服务与治理 | 不管理中转转售，也不保存外部产品最终用户的身份和订阅 |
| `platform` | 对外提供开发者 API，或为 SaaS/OEM/Partner 提供 AI 网关能力 | 管理 Gateway Principal 与接入 Integration；外部产品继续拥有用户、Session、订阅和订单 |

`platform` 是第四个独立部署角色，不是 Enterprise 的功能页，也不是 Relay Operator 的另一种 Customer。虽然 Platform 和 Relay 都可使用 API Key，选择的依据是业务所有权：需要余额、套餐、分配和风控运营时选择 Relay；需要把 AI 能力嵌入已有产品、同时不接管该产品用户体系时选择 Platform。Platform 的用量回传只投递可幂等、脱敏的计量事实；产品订单、订阅、余额与用户档案始终留在所属业务系统。

首次运行只启用一个部署角色。交互安装会显示初始后台以及该角色包含和排除的业务范围，且不预选任何角色；无人值守运行时使用 `ASTERROUTER_SERVER_BOOTSTRAP_DEPLOYMENT_ROLE`。该选择在新生产实例中不可切换，避免 Provider、Credential、Usage 和 Audit 跨业务模型混用；需要另一种形态时部署独立实例。完整规则见 [V4 部署角色与安装分流](./roadmap/v4/profile-bundles-and-installation.md)。

## 当前正式接入范围

当前版本采用标准协议优先策略：

| 协议 | 状态 | 说明 |
| --- | --- | --- |
| OpenAI Compatible Models / Chat Completions | 正式支持 | 当前已交付，包含 Chat Completions 流式响应 |
| Responses / Embeddings / Anthropic Messages / Gemini | 路线图 | 通过统一 Protocol Edge 与 Canonical Request 接入 |
| 图片 / 音频 / 视频 / Realtime / 异步 Job | 路线图 | 使用 Direct/Durable 双通道、Provider Capacity 与 Artifact 生命周期 |
| 其他公开协议 | 单独评审 | 只有在协议稳定、文档公开且具备长期维护条件时纳入 |

第三方网关或企业自建兼容服务可以作为上游，但必须通过上述标准 API 契约接入。AsterRouter 不依赖其管理 API、内部数据库或私有字段。

## 信任边界

AsterRouter 不实现以下能力：

- 把浏览器 Session、Cookie 或价格采集会话转换为生产 Provider Credential。
- 非公开 API、逆向协议、规避限制或持续对抗上游变更的适配。
- 未经授权的上游账号采集、凭据共享、余额抓取或自动续期。
- 上游 Provider 的私有管理 API、渠道内部状态和账号风控系统。
- 把支付、订单、退款和税务扩展进 Gateway Core；这类商业事实属于外部业务系统或独立场景模块。
- 让插件直接签发 Gateway Key、选择真实 Provider Account、修改 Usage/Billing 或绕过 Policy。

价格浏览器扩展仅用于管理员授权的价格采集，并通过独立设备凭据、短任务和最小化回传隔离；它不进入 Gateway 上游鉴权链路。

## 企业治理范围

当前已经提供：

- 飞书（中国大陆）/ Lark（国际版）、钉钉企业登录与自动入职，OIDC 作为通用企业 SSO 兼容入口。
- GitHub、Google 作为已验证邮箱的辅助登录源；本地安全登录保留为企业救援入口。
- 企业账户支持多身份记录、安全解绑和持久化会话撤销；身份变更、改密与人员禁用必须即时收回旧令牌权限。
- Relay Operator 管理 Customer、Plan、余额分配、兑换、账单视图和风险运营；真实支付渠道、订单、退款、税务与订阅合同仍由外部业务系统或独立插件负责，不能进入 Gateway Core。Enterprise 不复用这些中转商业对象。
- 用户、部门、Group 和资源级 RBAC。
- Workspace Key 生命周期、模型白名单、限流、预算和过期策略。
- User/Customer/Service Key 基础类型、Bearer 鉴权、Hash/Fingerprint、禁用、立即轮换和用量归属。
- 按用户、部门、Key 和模型统计用量与成本。
- 审计日志、数据保留、脱敏、导出和诊断。
- 单实例私有化部署、备份恢复和可验证升级。

员工 Portal 只展示本人可见的 Key、模型、额度和用量，不暴露上游账号、渠道或运营信息。

后续路线图会在同一 Core 上增加 API Key Principal 的细粒度 Scope 与分布式并发、OIDC Introspection/撤销事件与其他平台鉴权适配、Responses/Anthropic/Gemini、多模态 Job/Artifact、价格插件、成本感知调度、PostgreSQL + Redis 多模态基础设施，以及可选 Kubernetes 弹性部署。HMAC 与 RS256 JWT/JWKS 平台委托鉴权已经交付；未交付能力不能作为当前产品承诺。

## 产品原则

1. **标准 API 优先**：只承诺公开、文档化、可测试的协议。
2. **一条网关流水线**：Credential Source、客户端协议和 Provider 不得复制 Policy、Scheduler、Usage 或 Billing Core。
3. **稳定性优先**：不为短期兼容引入需要持续逆向或攻防的实现。
4. **治理优先**：Credential、策略、预算、容量、用量和审计属于 AsterRouter 的核心职责。
5. **事实边界清晰**：Provider Secret 与账号归 Core 受控管理；平台用户、订单和支付仍由所属业务系统管理。
6. **私有化可用**：官方服务不可用时，核心网关、已批准价格快照和治理能力仍可独立运行。
