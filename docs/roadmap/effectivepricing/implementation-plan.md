# 实施计划

> 本计划将 [第三方 API 有效价格与缓存感知路由](./README.md) 落到 AsterRouter 现有 Gateway、Control Plane、PostgreSQL、Redis、Trace 和 Vue 管理界面。

> 交付状态：Phase 0-6 核心闭环已交付。亲和绑定支持 PostgreSQL 共享存储和可选 Redis 原子首写协调；连续窗口自动提升/回滚支持多实例去重、事务证据和默认关闭的 Kill Switch。`sub2api-compatible` 账单源已支持配置持久化、余额/聚合快照、手动与定时同步、多实例租约和历史证据；账单源健康已联动路由硬阻断与有效价格经济切换门禁。PostgreSQL 16 专用环境验收，以及具备逐请求明细/价格 Feed 的其他 Adapter 仍待完成。

## 0. MVP 交付矩阵

| 能力 | 状态 | 当前边界 |
| --- | --- | --- |
| Cache Usage、TTFT、第三方请求 ID | 已交付 | OpenAI-compatible、Anthropic、Gemini JSON/SSE；Gemini 原生网关入口仍不在本专题范围 |
| 采购报价、账单行、有效倍率 | 部分交付 | 支持管理 API/人工导入；`sub2api-compatible` 已持久化余额/额度和聚合成本快照，但该上游契约没有逐请求账单行或价格 Feed |
| 受控探针与号池疑似碎片化 | 已交付 | 合成长前缀、预算、冷却、并发、负对照、连续失败门禁 |
| 客户/会话两级亲和 | 已交付 | HMAC 键、有界重排、PostgreSQL 共享绑定、Redis 原子首写和已验证第三方亲和信号注入已落地；百万绑定长期压测待补 |
| 缓存质量决胜 | 已交付 | 10% 命中改善且净缓存节省真实提高，或 10% 亲和改善；最多 2% 成本回退，并执行错误率和 P95 相对质量门禁 |
| Canary、激活与回滚 | 已交付 | 人工批准 Canary；连续窗口自动激活/回滚默认关闭，可独立启用并审计 |
| 账单健康与切换门禁 | 已交付 | active 来源的明确无效 Key、认证拒绝、Key 配额耗尽和有限订阅额度硬阻断；同步失败/证据过期只冻结经济性提升；监控查询异常对推理 fail-open |
| 管理端与原型 | 已交付 | 五个 Tab、策略、预算确认、账单源检测/配置/同步/历史证据、桌面/移动布局 |

## 1. 实施约束

- 不新增旁路 Router，继续使用现有 Candidate Planner、Provider Account Scheduler 和 Gateway Pipeline。
- 不改变 `model_pricings` 的下游计费含义。
- 不假设第三方内部源头或账号池可见。
- 不默认重写客户 Prompt；第一阶段只透传已验证字段并提供缓存友好度诊断。
- 新调度能力从 `observe_only` 开始，完成账单验证后才允许自动切换。
- Direct 请求只做短时有界等待；不会为了缓存或低价隐式变成持久任务。

## 2. 当前代码映射

| 能力 | 当前基础 | 需要补齐 |
| --- | --- | --- |
| Sticky Key | 已读取 `X-AsterRouter-Sticky-Key`、OpenAI `user`，使用 HMAC 保存来源隔离后的绑定键，并按已验证能力注入第三方 Header、Body 字段或 `prompt_cache_key` | 扩展更多第三方映射 Fixture，禁止未经验证的字段自动注入 |
| Sticky 绑定 | 已实现客户级供应商亲和和会话级采购账号亲和，Memory/PostgreSQL Repository 均可保存，Redis Lua 首写协调可选启用，仍受容量、熔断与 Fallback 约束 | 增加百万绑定长期压测与容量告警 |
| 候选执行 | 已接入有效价格决策、Canary/Active 重排、连续窗口自动提升和回滚 | 增加具体供应商账单同步后的端到端长周期灰度 |
| Usage | 已增加缓存读写、未缓存输入、TTFT、第三方请求 ID、采购成本与可信度 | 增加更多第三方别名 Fixture |
| JSON/SSE 解析 | OpenAI-compatible、Anthropic 与 Gemini 缓存 Usage 已归一化且不改变透传 | 增加更多第三方别名 Fixture |
| 成本 | 已新增采购报价、第三方账单行、对账和真实有效倍率；`sub2api-compatible` 已支持持久化账单源、余额/聚合快照和定时同步 | 增加有逐请求明细、价格 Feed 或增量游标的具体 Adapter |
| 缓存质量 | 已提供生产/探针分离指标、号池亲和等级、预算探针、窗口趋势和切换门禁 | 大规模长期基准待补 |

## 3. Phase 0：契约和术语收敛

交付：

1. 定义 Canonical Cache Usage、采购成本可信度、能力状态和切换原因码。
2. 明确 `ProviderConnection -> ProviderAccount -> 第三方内部号池` 边界。
3. 在上位 [成本感知调度设计](../../savemoney/cost-aware-routing.md) 中只保留通用 Price Observation；本专题负责第三方 API 场景细节。
4. 为 Provider Adapter 增加只读能力描述：

```go
type CacheCapability struct {
    AffinityTransport string
    CacheControlMode  string
    UsageSchema       string
    SupportsProbe     bool
}
```

验收：相同术语在 Core、插件、API 和界面中含义一致；没有把宣传倍率命名为 `actual_cost`。

## 4. Phase 1：Usage 与流式可观测性（部分完成）

### 4.1 数据迁移

为 Usage Ledger 增加 nullable 缓存字段、TTFT、第三方请求 ID、采购成本与可信度。为历史记录设置 `usage_normalization_status=unknown`，不得回填伪造的 0。

### 4.2 协议归一化

在 Provider Adapter 或独立 Usage Normalizer 实现：

- OpenAI Chat/Responses JSON。
- OpenAI Chat/Responses SSE 终态事件。
- Anthropic Messages JSON/SSE。
- Gemini generateContent/streamGenerateContent。
- OpenAI-compatible 第三方的兼容别名，但必须保留来源 Schema。

解析器输出 `field present` 和 `value`，不使用 `int` 零值判断字段是否存在。

当前状态：OpenAI-compatible、Anthropic 与 Gemini JSON/SSE 已交付；Gemini 已支持原生 `generateContent` 受控探针和管理端协议选择，但原生 Gemini 网关转发入口仍不在本专题范围。

### 4.3 SSE 旁路累积

SSE 转发过程中只复制必要终态 Usage 字段到有界状态机：

- 不缓存完整响应。
- 不改变事件顺序、分块和客户端可见内容。
- 客户断开后仍按已观察到的上游 Usage 写入最终记录。
- 上游没有终态 Usage 时保存 `partial/unknown`。

### 4.4 TTFT

记录上游请求开始到首个语义 Token/事件的时间。心跳、注释和纯元数据事件不算 TTFT。

验收：JSON 与 SSE 的同一 Usage Fixture 归一化结果一致；字段缺失和 0 有单独测试。

## 5. Phase 2：采购报价、账单与真实倍率（MVP 已完成）

### 5.1 价格观测

实现 `provider_price_observations` 和不可变 `provider_price_snapshots`：

- 允许 API、签名 Feed、人工导入和受控浏览器来源。
- 记录缓存读、5 分钟写、1 小时写、普通输入、输出和请求费。
- 保存币种、汇率、税费、充值折扣、适用套餐和有效期；充值实付系数进入估算成本和缓存净节省计算。
- API 要求八个价格分项显式出现，明确免费提交 `0`，不得把遗漏字段静默解释为免费。
- 冲突或过期价格不能发布为自动调度快照。

### 5.2 第三方账单 Adapter

当前只读检测端口按供应商实现：

```go
type ProviderBillingReader interface {
    ID() string
    Inspect(ctx context.Context, target ProviderBillingReadTarget) (ProviderBillingSourceInspection, error)
}
```

默认注册表先实现 `sub2api_compatible`：使用采购账号原有 API Key 读取 `/v1/usage`，自动识别钱包余额、API Key 配额或订阅周期额度，并读取今日/累计与默认近 30 天模型维度的 Token、缓存 Token、模型原价成本和第三方实扣聚合。结构匹配只标记 `schema_match`，不确认供应商内部实现。

该契约没有逐请求账单行、价格 Feed 或增量游标，因此：

- `usage_cost_lines=false`、`incremental_sync=false`、`price_feed=false`。
- 聚合总额只做校验证据，不写入 `provider_billing_lines`。
- Key 配额和订阅额度不伪装成钱包余额。
- 没有账单 API 的供应商继续人工导入；只有余额变化时保持 `unallocated`。

持久化阶段已新增：

1. `provider_billing_sources`：采购账号、Adapter、启用状态、同步间隔、游标、下次运行时间、连续失败和最后错误码。
2. `provider_balance_snapshots`：不可变金额、`wallet_balance/api_key_quota_remaining/subscription_period_remaining` 语义、币种、观测时间和证据哈希。
3. `provider_usage_aggregate_snapshots`：不可变统计范围、模型维度、请求数、输入/输出/缓存 Token、模型原价成本、第三方实扣聚合、币种、观测时间和证据哈希。
4. `provider_billing_sync_runs`：每次检测/同步的开始结束时间、能力快照、发现/导入/跳过数量和失败原因。

以上四张表已由 `062_provider_billing_sources.sql` 和 runtime schema 同步创建。配置更新使用 `version` CAS；自动同步默认关闭，最短间隔 60 秒。调度器通过 PostgreSQL `FOR UPDATE SKIP LOCKED` 或 Memory 临界区认领，到期租约会将旧 run 标记为 `lease_expired`，旧 worker 的完成写入会被 fencing token 和过期时间共同拒绝。成功完成在同一事务中提交 run、source、余额和聚合快照；失败只保存稳定错误码、能力状态和审计，不保存第三方原始错误正文，也不会产生任何快照。

管理 API 已提供配置列表/保存、手动同步和证据查询；后台 Worker 挂载现有 `monitorCtx`，默认每分钟扫描有限批次，单一账单源失败不会阻塞其他来源。`sub2api-compatible` 没有增量游标，因此当前 `cursor` 保持为空，但调度和表结构不会把聚合结果伪装成增量账单行。

### 5.3 对账

匹配顺序：

1. 第三方请求 ID。
2. 供应商账单行 ID/请求时间/模型/Token 的唯一组合。
3. 时间窗口聚合，只产生 `derived/unallocated`。

对账不得修改原始 Usage 或账单行，只写关联和状态。

### 5.4 有效价格汇总

生成 1 小时、24 小时、7 天滚动汇总，页面同时展示：

- 标称倍率。
- 账单倍率。
- 工作负载有效倍率。
- 标准化有效倍率。
- 样本量、覆盖率和可信度。

验收：低标称倍率但缓存差的 Fixture 能显示为实际更贵；只有总额的账单不会生成虚假分项单价。

### 5.5 账单健康与路由联动

账单源是采购账号的外部证据，不是上游源头的内部状态。系统只在 `active` 来源上让证据改变路由：

- `observe_only` 和 `disabled` 只展示健康状态，不过滤流量，也不参与自动经济切换。
- `provider_billing_key_invalid`、`provider_billing_auth_rejected`、`provider_billing_key_quota_exhausted` 和 `provider_billing_subscription_exhausted` 是硬阻断原因；该账号从模型候选中移除，全部候选被移除时返回 `route_unavailable`。
- `provider_billing_sync_unhealthy`、`provider_billing_evidence_stale` 和 `provider_billing_evidence_missing` 不阻断正常推理，只将 `economic_switch_eligible` 置为 false，禁止新 Canary/Active 提升，并阻止既有经济决策把候选重排到首位。
- 钱包余额为零不直接硬阻断，因为可能是后付费；只有 API Key 配额为零，或非 unlimited 的订阅周期额度不大于零，才按额度耗尽处理。
- 最新余额按每个 `source_id` 的 `observed_at DESC, id DESC` 选择；证据新鲜度为 `max(2 * sync_interval, 6h)`。监控读取失败不影响正常推理，经济切换管理操作则 fail-closed。

派生状态通过 `ProviderBillingRoutingHealth` 暴露：`status`、`hard_blocked`、`economic_switch_eligible`、`reason_codes`、证据观测时间和新鲜度阈值。该状态不持久化，不新增迁移；来源、同步运行和余额快照仍是唯一事实源。

## 6. Phase 3：缓存能力、号池识别与探针（MVP 已完成）

### 6.1 能力注册

Provider Adapter 声明第三方支持的字段和映射，例如：

- 透传 `prompt_cache_key`。
- 透传或翻译经过验证的 `session_id`/自定义 Header。
- 透传 `cache_control`。
- 从响应映射 `cached_tokens`、`cache_write_tokens` 或 Anthropic 缓存字段。

默认策略是 `passthrough_if_present`。协议间自动翻译必须由具体 Adapter 显式启用并有契约测试。

### 6.2 探针执行器

实现 PostgreSQL 任务状态 + 现有 Worker/Job 基础上的低频探针，不新增消息队列：

- 每账号/模型/协议串行。
- 每日 Token 和金额预算。
- `warm -> reuse -> negative_control` 三阶段。
- 指数冷却和手动暂停。
- 探针流量使用独立 API Key/Principal 和 Trace 标签，不混入客户成本 KPI。

### 6.3 号池碎片化判断

仅在以下条件同时成立时标记 `pool_fragmentation_suspected`：

1. 前缀达到最低 Token，字段被接受。
2. 同一会话、同一第三方采购账号、同一模型、TTL 内重复。
3. 多轮重复仍冷写或 Miss。
4. 不同前缀负对照行为符合预期。
5. 已排除价格/模型变更、TTL 超时、并发首写未完成和 AsterRouter 主动 Fallback。

不能通过第三方请求 ID、延迟或响应内容猜测具体内部账号。

验收：探针预算、冷却、并发和审计都有失败测试；单次 Miss 不会把供应商降为 `fragmented`。

## 7. Phase 4：客户与会话亲和（Redis 原子协调闭环已完成）

### 7.1 复用现有 Sticky 契约

继续使用 `X-AsterRouter-Sticky-Key`，并记录来源枚举。客户端 SDK/文档建议每个多轮会话使用稳定、无 PII 的随机 ID。

### 7.2 两级绑定

Redis-compatible 实际 Key：

```text
asterrouter:{<namespace>:routing_affinity}:affinity_<hmac_scope>
```

Redis Hash 保存不可逆 owner hash 和 Provider/Route/Account、绑定版本、创建时间、最后复用时间及到期时间；Key 只包含 HMAC scope，不包含客户、会话或 Prompt 原文。Repository 抽象继续提供 Memory 和 PostgreSQL 实现，作为审计副本和 Redis 故障时的降级路径。

当前状态：HMAC 绑定键、两级亲和重排、PostgreSQL 持久化和 Redis Lua 原子首写已交付。生产请求仅在能力为 `accepted/observed/billed_verified` 且存在 Sticky Key 时，向第三方注入由客户/会话/模型/协议/供应商账号共同派生的 HMAC 亲和值；Header、Body 和 `prompt_cache_key` 映射均禁止覆盖认证及协议保留字段，且不会把整个租户压到单一缓存 Key。能力或 Redis 查询异常时 fail-open 继续转发，并保留 Repository 降级路径。

运行时通过以下配置独立启用，不要求 AI Job 队列同时使用 Redis：

```text
ASTER_ROUTING_AFFINITY_DRIVER=redis
ASTER_REDIS_URL=redis://<host>:<port>/<db>
ASTER_REDIS_NAMESPACE=asterrouter
```

### 7.3 路由顺序

```text
Canonical Auth / Request
  -> 治理、模型、健康、余额和容量硬过滤
  -> Canary/Active 有效价格决策重排
  -> 当前会话采购账号亲和是否仍在合格候选
  -> 当前客户供应商亲和是否仍在合格候选
  -> Capacity Permit
  -> 请求成功后刷新绑定
```

没有会话键时：

- 供应商层仍使用客户亲和。
- 不建立采购账号级绑定，避免把客户的全部并发请求长期锁到一个我方账号。
- 不读取或保存完整 Prompt 来制造隐式长期会话。

### 7.4 亲和中断

统一原因：

```text
account_disabled, balance_blocked, health_failed, circuit_open
rpm_exhausted, tpm_exhausted, concurrency_exhausted
model_incompatible, policy_changed, price_degraded
cache_degraded, binding_expired, emergency_failover
```

每次中断进入 Trace。经济切换只对尚无有效绑定的客户 cohort 重新分桶；已有客户/会话绑定继续复用，紧急故障可以立即打破绑定。

验收：PostgreSQL 共享存储下多实例最终收敛到同一供应商/账号；Redis 下 64 路并发首次请求只有一个 owner，同 owner 刷新 TTL、不同 owner 不能覆盖；10,000 个账号绑定、抽样读取和清理已在真实 Redis 完成。熔断或容量不足时仍可安全 Fallback，租户之间绑定不泄漏。

## 8. Phase 5：切换决策与自动调度（已完成）

### 8.1 决策模式

| 模式 | 行为 |
| --- | --- |
| `observe_only` | 计算候选和潜在差异，不改路由 |
| `recommend` | 生成管理员可审批建议 |
| `canary` | 用客户级 HMAC cohort key，把配置比例的未绑定客户发往候选 |
| `balanced` | 在质量门禁后综合有效成本和缓存经济性 |
| `cost_first` | 在硬门禁内优先有效采购成本 |
| `fixed_route` | 保持固定供应商，只在硬故障时切换 |

### 8.2 排序输入

决策创建时要求管理员分别确认公共 `gateway_model` 和证据 `upstream_model`。当前/候选账号列表先按 `upstream_model + protocol` 过滤并去重，服务端再按 `provider_account_id + upstream_model + protocol` 精确校验。决策列表从 `candidate_snapshot_id` 派生上游模型；热路径只使用 `gateway_model + protocol` 命中决策，不能将两者互换。

硬过滤后按以下顺序：

1. 强制路由与企业策略。
2. 当前健康会话/客户亲和。
3. 可信的标准化有效成本。
4. 缓存经济收益和号池亲和一致性。
5. 错误率、延迟、容量余量和价格新鲜度。
6. 同等级候选使用现有权重分散。

当没有候选在有效成本上达到显著改善阈值时，优先选择缓存账单已验证、号池亲和更稳定的供应商，而不是回到宣传倍率排序。

MVP 默认策略为：`min_cache_hit_rate_improvement=0.10`、`min_affinity_improvement=0.10`、`max_cache_tiebreak_cost_regression=0.02`、`max_error_rate_regression=0.005`、`max_p95_latency_regression=0.20`。缓存决胜要求命中率改善达标且候选净缓存节省率高于当前线路；高命中但无折价、缓存分项不完整或 Token 不守恒时不得以缓存胜出。缓存决胜不能绕过错误率、账单一致性、指标覆盖率、样本量和成本可信度门禁；除 `cost_first` 外，P95 缺失或相对当前线路越界同样保持当前线路。

### 8.3 滞回与回滚

- 提升需要连续多窗口满足样本、成本和质量门槛。
- 经济降级也需要连续窗口，防止短时波动来回切换。
- 硬故障立即 Fallback，不等待统计窗口。
- Canary 只接收按客户级 HMAC 稳定分桶且当前无有效绑定的流量；旧绑定按 TTL 自然到期。
- Canary 的有效成本、错误率或延迟越界时自动回到上一快照。

验收：相同决策快照可重放出相同候选顺序；自动切换有审计、回滚和 Kill Switch。

当前状态：推荐、Canary、激活、候选重排、人工回滚、连续窗口监控和自动动作已交付。策略持久化 `evaluation_interval_minutes`、`promotion_window_count`、`degradation_window_count` 与 `automatic_actions_enabled`；自动动作默认关闭。每个窗口保存成本、缓存、亲和、错误率、P95、覆盖率和账单一致率证据；`decision_id + window_end` 唯一约束和事务 CAS 防止多实例重复累计或覆盖人工动作。缺失样本和证据归类为 `inconclusive`，不会自动回滚。

## 9. Phase 6：管理界面（MVP 已完成）

实现以[管理端原型](./ui-prototype.md)为交互基线，并保持与现有管理端导航、表格、抽屉、权限和审计模式一致。

### 9.1 有效价格页

在同一表格内比较：

- 供应商、采购账号、模型、协议。
- 标称倍率、账单倍率、工作负载有效倍率、标准化有效倍率。
- 缓存读写价格、观测有效价格、真实总成本。
- `cost_available` 明确区分零成本与缺少证据。
- 数据可信度、样本量、覆盖率和更新时间。

默认按标准化有效成本排序；标称倍率只作为参考列，不使用醒目的“最便宜”标签。

### 9.2 缓存质量页

- 缓存能力状态、号池亲和等级。
- 生产与探针两套命中率。
- Token 命中率、写读比、净节省、账单一致率。
- 字段缺失、疑似剥离、疑似号池碎片化和最近探针失败。
- 提供亲和传输、亲和字段、缓存控制模式和 Usage Schema 配置入口；号池等级与生产指标保持系统只读。

### 9.3 切换建议页

- 保持/推荐/Canary/提升/降级状态。
- 当前与候选的有效成本和质量差。
- 当前与候选的错误率和成功请求 P95 延迟。
- 阈值、样本和原因码。
- 审批、启动 Canary、回滚和暂停自动调度。

### 9.4 第三方账单源页

- 先对采购账号执行只读 Adapter 检测，再保存 `observe_only/active/disabled` 配置。
- 自动同步开关默认关闭，间隔限制为 60-86400 秒；保存使用版本 CAS，防止覆盖其他管理员或 Worker 的更新。
- 支持立即同步，并展示 `running/succeeded/failed/lease_expired` 运行历史、稳定错误码和触发来源。
- 分开展示钱包余额、API Key 配额、订阅周期额度，以及模型/周期聚合历史；币种不依赖余额字段存在。
- 聚合金额保持证据语义，不生成 `provider_billing_lines`，不参与请求级精确对账。

## 10. 测试策略

### 后端单元测试

- OpenAI、Anthropic、Gemini JSON/SSE Usage Fixture。
- 缓存字段缺失、零、字符串错误、溢出和部分字段。
- 价格、倍率、盈亏平衡和币种舍入。
- 对账精确、派生、未分配和冲突。
- 探针状态机、预算、冷却和负对照。
- 亲和绑定、过期、HMAC 隔离、容量等待和故障转移。
- 切换样本门槛、滞回、Canary 和回滚。
- 账单源 CAS、并发认领、租约过期恢复、旧 worker fencing、失败无快照和错误正文脱敏。
- 账单健康原因码、observe-only 不过滤、active 硬阻断、额度语义、过期证据冻结经济切换，以及最新余额 Memory/PostgreSQL 契约。

### PostgreSQL 与迁移测试

- 新旧 Usage 兼容读取。
- Nullable 缓存字段不被默认 0 污染。
- 不可变价格快照和账单幂等导入。
- Rollup 可从原始事实重算。
- `062` 重复执行、状态/区间/唯一约束、runtime schema parity，以及账单源重启持久化。
- Memory nearest-rank P95 与 PostgreSQL `PERCENTILE_DISC(0.95)` 对成功请求采用一致口径，并排除错误请求。

### Gateway 契约测试

- 不修改客户请求中未知字段。
- 只有 Adapter 声明后才添加/翻译亲和字段。
- SSE 事件字节、顺序和 Flush 行为不回归。
- 上游提交点之后不会因经济策略重复调用。
- Trace 包含选择价格、缓存状态和亲和中断原因。

### UI 测试

- 标称倍率与真实有效倍率并列且口径清晰。
- 缺少账单时不显示伪精确单价。
- 小样本、过期和冲突有明显状态。
- 账单源配置、自动同步、手动同步、失败错误码、余额和聚合历史可见。
- 账单源路由健康、硬阻断、自动经济切换资格和原因码可见；移动视图不发生横向溢出。
- 桌面与移动视口无表格/数字溢出。

### 远程与性能测试

- PostgreSQL 生产迁移和回滚演练。
- Redis 多实例亲和一致性、故障降级和 10k 账号绑定已验证；百万绑定规模下的内存、Key 过期和热路径延迟仍待长期压测。
- Rollup 与账单同步不阻塞 Gateway 主链路。

## 11. 灰度顺序

1. 只上线 Usage 和缓存字段采集，不改变路由。
2. 上线采购报价和账单对账，只展示真实倍率。
3. 对少量测试账号启用低预算探针。
4. 配置 `ASTER_ROUTING_AFFINITY_DRIVER=redis`，灰度观察客户/会话亲和与 Repository 降级指标。
5. 启用 `observe_only`，对比当前路由与建议路由至少 7 天。
6. 管理员批准后，对未绑定客户 cohort 做 1% -> 5% -> 20% Canary。
7. 达到成本、质量和账单一致性门槛后才允许自动提升。

任一阶段都提供独立 Kill Switch：缓存字段透传、探针、有效成本排序、客户亲和和自动切换可以分别关闭。

## 12. 完成标准

- 能回答每家第三方在指定模型上的标称倍率和真实有效倍率。
- 能区分缓存字段缺失、明确为 0、缓存写、缓存读和账单折价。
- 能判断缓存问题主要来自客户工作负载、AsterRouter 路由打散，还是第三方疑似号池碎片化。
- 同一客户尽量保持同一供应商，同一会话尽量保持同一采购账号。
- 健康、容量和策略变化时可以有证据地打破亲和并 Fallback。
- 没有显著更便宜供应商时，能优先选择缓存经济性和号池亲和更好的供应商。
- 自动切换不使用宣传倍率，且所有选择可回放、可灰度、可回滚。
