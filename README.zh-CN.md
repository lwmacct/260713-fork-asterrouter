<div align="center">

# AsterRouter

**先管好 AI 访问，再持续降低运营成本。**

面向团队、开发者平台和产品接入的私有 AI 网关。

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## 让 AI 接入适配你已经在运营的业务

AsterRouter 位于应用与已获授权的 AI Provider 之间，让研发、运营和安全团队在一个地方管理访问、线路、成本证据与交付运维，而不暴露上游凭据。

| 你关注什么 | AsterRouter 做什么 |
| --- | --- |
| 让产品持续交付 | 为应用提供稳定的 OpenAI 兼容 Models 与 Chat Completions 流式入口。 |
| 让 AI 运行更可靠 | 在授权 Provider 间路由，绕过异常、限流和不可用线路。 |
| 让访问可治理 | 签发和轮换 Key，执行模型策略、限流、配额、预算、告警和审计。 |
| 让业务归属清楚 | 保留接入产品自己的用户、Session、订阅和订单。 |
| 让交付务实可控 | 可以自主部署，也可以在自己的环境中获得私有化交付和托管运维。 |

## 选择运营方式

| 你运营的核心业务 | 选择 | 初始后台 |
| --- | --- | --- |
| 自己或小团队的专注网关 | **Personal** | `/console` |
| 客户余额、套餐、风控与转售运营 | **Relay Operator** | `/operator` 与 `/customer` |
| 员工、部门、SSO 与内部服务治理 | **Enterprise** | `/admin` 与 `/portal` |
| 面向开发者的 API，或向已有 SaaS、OEM、客户端提供 AI 能力 | **AI Platform** | `/platform` |

每个生产实例在安装时先选择一种活动形态。系统管理员可以在设置中切换活动形态；任一时刻只开放当前选择的形态，已有数据会保留。

## 提供 AI 能力，不接管你的产品

当你自己运营 API 关系时，使用 **AsterRouter API Key**。当接入产品已经拥有用户和商业关系时，使用 **AI Platform**。

```text
你的产品保留用户、Session、订阅和订单
                         |
                         v
          AsterRouter 执行 AI 访问与路由控制
                         |
                         v
                 已授权的 AI Provider 接收请求
```

AI Platform 是独立部署角色，不是中转 Customer 的一种用法。它管理自己的 Tenant、非人类调用主体、API Key、HMAC 或 JWT/JWKS 委托接入，并可回传签名、仅含计量字段的用量事件；它不会创建接入产品的终端用户账号、Session、订阅、订单或余额。

## 当前可用

- OpenAI 兼容的 Models 与 Chat Completions，包括流式响应。
- 多 Provider 路由、失败切换、账号容量控制、冷却、熔断与粘性路由。
- API Key 生命周期、模型白名单、限流、Token 配额、预算、用量、成本分摊、告警、Trace、审计和导出。
- 企业身份与访问治理，以及 Personal、Relay Operator、Enterprise、AI Platform 四类后台。
- Platform Tenant 与 Gateway Principal 管理；绑定 Tenant 的直接 Key；HMAC 与 RS256 JWT/JWKS 委托访问；带重试、死信和人工重放的签名 HTTPS 用量回传。
- 私有化部署底座：PostgreSQL、备份恢复、诊断与已验证的 Release 更新。

## 产品将走向哪里

路线图会把同一个 Core 扩展到报价驱动的线路选择、浏览器辅助价格采集、Responses 等公开协议、图片/音频/视频、长任务、产物交付、Redis 协调与 Kubernetes 弹性部署。这些都不是当前产品承诺。

路线图不是 Release 承诺。公开产品边界以本 README 为准；私有化交付规划会随项目交付。

## 快速开始

### 先选择部署角色

首次安装只选择一个业务部署角色。它决定初始后台、业务对象、角色和默认扩展，不是一个只改变首页的显示选项，也不是可以任意叠加的功能开关。应按谁拥有业务关系、商业结算和身份事实选择，不要按是否需要 API Key、流式输出或某个模型来选择。

| 适合选择的部署角色 | 业务与身份事实源 | 初始后台 | 明确不包含 |
| --- | --- | --- | --- |
| **个人**：个人或小团队需要轻量网关 | 自己的 Workspace 与内部协作 | `/console` | 企业组织管理、中转账务和外部产品集成 |
| **中转运营**：已有客户、余额、套餐和风控运营流程 | 自己拥有客户、套餐、余额和风控流程 | `/operator` 与 `/customer` | 企业员工管理和外部产品租户 |
| **企业**：需要治理一个组织内的员工和服务访问 | 组织拥有员工、目录和内部访问事实 | `/admin` 与 `/portal` | 中转转售对象，以及外部终端用户身份和订阅管理 |
| **AI 平台**：运营开发者 API，或为 SaaS/OEM 产品提供 AI 能力 | 平台拥有 Tenant、调用主体和接入边界；接入产品拥有最终用户 | `/platform` | 企业 HR 对象、中转余额/套餐，以及外部终端用户账号和会话 |

AI 平台与中转运营是两个独立角色。两者都可能使用 API 凭据，但中转运营管理客户余额、套餐和风控；AI 平台管理开发者 API Key 或产品委托接入的网关边界，接入产品仍然拥有自己的用户、登录会话、订阅和订单事实。企业管理员工和部门治理，个人只管理自己的 Workspace；它们是不同的业务根对象，不是同一客户模型下的四个页面。

Linux Release 安装必须传入 `--deployment`，也可预设安装器输入 `ASTERROUTER_DEPLOYMENT_ROLE`；Docker 和源码开发通过 `/setup` 显式选择，且不会默认选中任何角色。生产与 Docker 部署会把选择写入 PostgreSQL，并固定为当前实例的业务模型。源码开发未配置 `ASTERROUTER_SERVER_STORAGE_DATABASE_URL` 时使用临时内存存储，进程重启后安装状态会丢失，不能把它当成生产安装。PostgreSQL 部署后续启动时，若配置角色与已持久化角色不一致，服务会拒绝启动。需要第二种业务模型时部署独立实例。

### Linux Release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash -s -- install --deployment enterprise
```

将 `enterprise` 替换为 `personal`、`relay_operator` 或 `platform`。安装器会把 AsterRouter 部署到 `/opt/asterrouter`，并在 `/etc/asterrouter` 下创建服务配置。正式环境启动需要 PostgreSQL、稳定的加密密钥，以及管理员密码或 Token。新的 Linux 安装若未指定角色，会在下载或写配置前被拒绝。

### Docker

```bash
docker compose up --build
```

访问 `http://localhost:8080/setup`，选择一个部署角色并确认其业务边界后完成初始配置。安装完成后，AsterRouter 会清理可能属于其他部署角色的浏览器旧会话，并要求重新登录后再进入所选后台；相同角色的安装请求可以安全重试。系统管理员可以在设置中切换活动形态，旧形态会隐藏但数据会保留。非交互运行时部署设置 `ASTERROUTER_SERVER_BOOTSTRAP_DEPLOYMENT_ROLE=platform`。

## 私有化与托管交付

AsterRouter 支持三种交付方式：

- **自主运维：** 部署在自己的环境中，由团队独立管理。
- **私有化交付：** 部署到客户控制的网络，提供安装、迁移与验收支持。
- **托管运维：** 持续提供升级、备份、健康检查、诊断与运维支持，客户仍然掌握数据和供应商凭据。

每种交付方式都可以从低成本单机部署起步。当前版本只需要 AsterRouter 应用与 PostgreSQL，Redis 和 Kubernetes 不是当前安装前提。V4 路线图保留 4 vCPU / 4 GiB 的轻量负载单机验收目标，再为有明显波峰波谷的多模态工作负载提供按 Role 划分的 Kubernetes 拓扑，同时不把 Provider 并发当成无限容量。

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

需要体验登录页的一键 Demo 时，使用显式演示模式启动：

```bash
bash scripts/dev.sh --demo
```

演示模式会开放内置演示账号并在登录页显示“一键体验 Demo”，只应在本地或隔离的公开演示实例使用。Docker 可通过 `ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true docker compose up --build` 启用；生产实例应保持关闭。

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
