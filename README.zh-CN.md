<div align="center">

# AsterRouter

**让每一次 AI 调用更省钱，也更可控。**

统一接入多家 AI 供应商，自动优化成本，完成企业治理，并支持私有化与托管交付。

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## 一个网关，管好成本与访问

AsterRouter 当前为团队提供统一、可控的 AI 网关，并将逐步扩展为文本、图片、音频和视频的一体化能力层。接入已有供应商，为团队和产品安全分发访问能力，并清楚看到每一笔 Token 和费用去向；多模态路线图会把相同治理能力延伸到媒体任务与产物。

下一阶段不再停留在成本报表：价格插件持续同步供应商报价，成本感知调度在满足企业策略、健康状态和容量要求的前提下，自动选择成本更低的可用线路。

| | 你能获得什么 |
| --- | --- |
| 降低成本 | 比较合格供应商的有效价格，为每次请求选择更划算的可用线路。 |
| 统一接入 | 应用使用稳定的流式、媒体和长任务 API，供应商与线路变化留在网关内部。 |
| 稳定服务 | 自动绕过异常、限流或容量不足的线路，并执行失败切换。 |
| 企业治理 | 管理团队权限、模型范围、预算、告警、审计和数据保留。 |
| 灵活交付 | 可以自主部署，也可以选择私有化交付和持续托管运维。 |

## AsterRouter 如何省钱

1. **接入现有供应商。** 继续使用经过授权的供应商账号和标准 API。
2. **持续更新价格。** 价格源插件通过供应商 API、签名 Feed、文件导入或可选浏览器扩展同步报价。
3. **带约束地择优。** AsterRouter 先保证策略、可用性与容量，再从合格线路中选择预计成本最低的路径。
4. **证明节省结果。** 用量和路由证据展示选择价格、备选线路、实际成本与可量化节省金额。

> 报价驱动调度、第三方价格插件、节省账本和浏览器扩展属于路线图能力；路由、治理、用量、成本分摊、审计和插件底座已经可用。

## 为真实组织而设计

**研发团队**只需维护一个入口和一套 Key，即可使用多个供应商。

**财务与运营团队**可以按用户、部门、Group、Key 和模型核算成本，并配置预算与告警。

**安全团队**可以获得私有化部署、Secret 加密、权限隔离、审计证据、数据保留和离线运行能力。

**平台负责人**可以管理线路健康、失败切换、容量、备份、升级、诊断，以及承载供应商扩展的插件体系。

## 为内部团队和外部客户提供 AI 能力

AsterRouter 既可以支撑企业内部工具，也可以作为面向客户产品背后的 AI 能力供应层，不需要让每一个业务服务都重复建设 AI 网关。

| 场景 | AsterRouter 提供什么 |
| --- | --- |
| 企业内部 AI 平台 | 为员工、部门、服务和自动化任务提供带权限、额度与成本治理的 AI 访问。 |
| SaaS 与桌面客户端 | 保留平台现有客户鉴权，AsterRouter 验证委托访问并执行 AI 策略、额度、路由和用量。 |
| 内容与媒体产品 | 扩展图片、视频和音频能力，不需要每个产品重复建设突发排队、供应商容量调度、长任务、产物交付和媒体计费。 |
| Agent 与开发者平台 | 用一个自有品牌入口提供多模型能力，不向客户端暴露上游供应商凭据。 |
| 合作伙伴与 OEM | 隔离不同租户，执行合作伙伴策略，并把用量返回给掌握商业关系的业务系统。 |

```text
平台身份、Session、套餐或权益
              ↓
平台签发的凭据或签名请求上下文
              ↓
内部应用或客户客户端 -> AsterRouter Gateway -> AI 供应商
```

产品平台始终是用户、登录 Session、Token、租户、订阅和权益的唯一事实源。AsterRouter 不创建外部用户，也不替代平台鉴权；它只验证平台签发的凭据或可信签名上下文，然后负责 AI 请求执行：模型策略、额度、路由、成本、用量、风控和审计。桌面客户端、移动端和浏览器应用都不需要内置真实 Provider Secret。

通用 Key、策略、路由、计量和隔离底座当前已经具备；统一 External Access Context，以及标准 JWT/JWKS、OIDC Introspection 和平台鉴权插件契约属于产品与 OEM 场景的路线图能力。

## 一个可信 Core，按场景插件化扩展

不同客户需要不同工作流，但不应该因此产生多套网关。AsterRouter 把安全关键决策放在统一 Core 中，把可选业务集成交给插件。

| 可信 Core | 场景插件 |
| --- | --- |
| Gateway Credential、External Access Context 与租户隔离 | 平台鉴权、JWT/JWKS、Introspection、企业目录与 SSO 适配 |
| 模型策略、额度、预算与风控 | SaaS 套餐、订阅和权益同步 |
| Provider 路由、Fallback 与成本计量 | 供应商价格源和浏览器辅助采集 |
| 公平队列、流式会话、媒体任务、产物策略、用量和结算状态 | GPT Image、Gemini、Midjourney 兼容服务、即梦等 Provider 协议适配 |
| 从单机到 Kubernetes 共用一个 Core | Redis、S3 兼容存储、Cloudflare R2、阿里云 OSS 和未来消息队列的可替换基础设施适配 |
| Usage、Trace、Audit 与数据治理 | 通知、导出、品牌和客户工作流 |

插件只能通过受控接口提供供应商或业务事实，不能绕过 Core 策略、签发无限制 Token、读取无关 Secret，或在可信调度器之外决定线路。

## 当前已经提供

- OpenAI 兼容的模型列表和 Chat Completions，包括流式响应。
- 多供应商调度，包括优先级、权重、容量、RPM/TPM、冷却、熔断、Sticky 路由和 Fallback。
- Workspace Key、模型白名单、限流、Token 配额、预算和超额策略。
- 用量分析、成本分摊、告警、路由 Trace、策略证据、审计日志和数据导出。
- OIDC、飞书/Lark、钉钉、部门、Group 和资源范围角色等企业身份与权限治理。
- 管理后台、员工 Portal、插件中心、备份恢复、诊断和可验证更新。
- Personal、Relay Operator 和 Enterprise 三种部署 Profile，以及中英文界面。

当前网关提供 OpenAI 兼容的 Models 和 Chat Completions。Responses、Embeddings、Anthropic Messages、Gemini、实时会话、图片生成与编辑、音频、视频、媒体上传、异步任务和产物交付都属于路线图能力，不能作为当前已交付能力宣传。

多模态路线图会继续控制最低部署成本：生产最低基础设施为 PostgreSQL + Redis，不强制安装额外消息队列。可预测流量先用同一 Core 单机运行，波峰明显后再把 API、调度器、各模态 Worker、对账和产物交付拆成 Kubernetes Workload。波谷时 Worker Pod 和可选突发节点可以缩容，但供应商容量、租户预算和全局成本上限始终是硬约束。媒体可以保存在本地、AWS S3、Cloudflare R2、MinIO 等 S3 兼容存储、阿里云 OSS，或可靠交付到客户自己的存储。

## 快速开始

### Linux Release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash
```

安装器会把 AsterRouter 部署到 `/opt/asterrouter`，并在 `/etc/asterrouter` 下创建服务配置。正式环境启动需要 PostgreSQL、稳定的加密密钥，以及管理员密码或 Token。

### Docker

```bash
docker compose up --build
```

访问 `http://localhost:8080/setup`，选择使用模式并完成初始配置。

## 私有化与托管交付

AsterRouter 支持三种交付方式：

- **自主运维：** 部署在自己的环境中，由团队独立管理。
- **私有化交付：** 部署到客户控制的网络，提供安装、迁移与验收支持。
- **托管运维：** 持续提供升级、备份、健康检查、诊断与运维支持，客户仍然掌握数据和供应商凭据。

每种交付方式都可以从低成本单机部署起步，在流量出现明显波峰后把同一 Core 和数据模型迁移为按 Role 运行的 Kubernetes Workload。Kubernetes 负责伸缩 AsterRouter Pod 和可选 Worker 节点，但不会把第三方供应商并发误当成无限容量。

官方在线服务不可用时，Core 仍可继续运行。官方 Feed 同步路径不会上传 Prompt、Response、Provider Secret、Workspace Key、详细网关用量或浏览器采集的供应商会话。

## 项目入口

- [版本发布](https://github.com/astercloud/asterrouter/releases)
- [构建状态](https://github.com/astercloud/asterrouter/actions)
- [部署环境变量模板](./deploy/asterrouter.env.example)
- [English README](./README.md)

<details>
<summary><strong>本地开发</strong></summary>

安装前端依赖并同时启动前后端：

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

前端运行在 `http://localhost:5173`，并把 API 请求代理到 `http://localhost:8080`。

运行后端测试：

```bash
cd backend
go test ./...
```

构建前端：

```bash
cd frontend
npm run build
```

环境变量见[部署模板](./deploy/asterrouter.env.example)。

</details>

## 开源许可

AsterRouter 使用 [Apache License 2.0](./LICENSE)。
