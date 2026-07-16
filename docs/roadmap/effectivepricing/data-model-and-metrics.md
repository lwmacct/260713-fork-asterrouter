# 数据模型、真实价格与缓存质量指标

> 本文定义 [第三方 API 有效价格与缓存感知路由](./README.md) 的事实模型和统一计算口径。

## 1. 设计原则

1. **原始事实不可覆盖。** 第三方原始 Usage、账单行、报价快照和规范化结果分别保存。
2. **缺失不等于零。** 缓存字段没有返回时保存 `NULL` 和 `present=false`；明确返回 0 才是零。
3. **报价不等于实扣。** 宣传倍率、采购目录价和账单成本必须分层。
4. **请求成本不等于输入单价。** 只有总扣费时不能反推精确的输入、输出和缓存单价。
5. **工作负载与供应商能力分开。** 生产命中率低可能是 Prompt 不可复用，不必然代表供应商差。
6. **调度只消费发布快照。** 原始观测和未验证账单不直接进入热路径。

## 2. 三层价格事实

| 层次 | 事实 | 用途 |
| --- | --- | --- |
| 采购报价 | 第三方目录价、套餐组、充值折扣、宣传倍率 | 预估和异常对比 |
| 采购实扣 | Usage Cost Line、账单行、余额变化、充值现金成本 | 财务事实和自动调度 |
| 下游计费 | AsterRouter `model_pricings` 和客户计费策略 | 下游预算/销售，不参与采购价反推 |

采购成本可信度：

| 级别 | 定义 | 能否自动切换 |
| --- | --- | --- |
| `exact` | 账单行与请求 ID 精确匹配，包含分项成本 | 可以 |
| `derived` | 总扣费可匹配请求，并能用同版本可信费率卡分摊 | 达到样本门槛后可以 |
| `estimated` | 仅根据费率卡和本地 Usage 估算 | 只能建议/灰度 |
| `unallocated` | 只有窗口余额变化或总扣费，不能分到请求/模型 | 不可以 |
| `unknown` | 没有足够证据 | 不可以 |

## 3. Canonical Cache Usage

Provider Adapter 把不同协议归一化为互斥维度：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `total_input_tokens` | `BIGINT` | 本请求完整输入 Token |
| `uncached_input_tokens` | `BIGINT NULL` | 未读缓存、未写缓存的输入 Token |
| `cache_read_tokens` | `BIGINT NULL` | 从缓存读取的 Token |
| `cache_write_5m_tokens` | `BIGINT NULL` | 写入短 TTL 缓存的 Token |
| `cache_write_1h_tokens` | `BIGINT NULL` | 写入长 TTL 缓存的 Token |
| `output_tokens` | `BIGINT` | 输出 Token |
| `cache_fields_present` | `BOOLEAN` | 第三方是否返回缓存字段 |
| `normalization_status` | enum | `exact/derived/partial/unknown` |
| `raw_usage` | `JSONB` | 脱敏后的原始 Usage 证据 |

协议差异由 Adapter 处理：

- OpenAI 的总输入字段包含缓存部分，`cached_tokens`/`cache_write_tokens` 是细分字段。
- Anthropic 的 `input_tokens`、`cache_creation_input_tokens`、`cache_read_input_tokens` 是不同部分，总输入需要相加。
- Gemini 隐式缓存通过缓存 Token Usage 字段报告，具体字段随 API 表面不同，由 Adapter 映射。

任何归一化都必须满足：

```text
total_input_tokens
  = uncached_input_tokens
  + cache_read_tokens
  + cache_write_5m_tokens
  + cache_write_1h_tokens
```

如果第三方语义不足以完成互斥拆分，保留原始总量并把状态设为 `partial`，不能为了满足等式强行填 0。

## 4. 建议数据结构

第一阶段只增加完成闭环所需的表和字段，避免复制参考项目的用户、开 Key 和本地分组业务。

### 4.1 Usage 扩展

在现有 Usage Ledger 增加：

```text
ttft_ms BIGINT NULL
total_input_tokens BIGINT NULL
uncached_input_tokens BIGINT NULL
cache_read_tokens BIGINT NULL
cache_write_5m_tokens BIGINT NULL
cache_write_1h_tokens BIGINT NULL
cache_fields_present BOOLEAN NOT NULL DEFAULT false
usage_normalization_status TEXT NOT NULL DEFAULT 'unknown'
upstream_request_id TEXT NOT NULL DEFAULT ''
procurement_cost_micros BIGINT NULL
procurement_cost_currency TEXT NOT NULL DEFAULT ''
procurement_cost_source TEXT NOT NULL DEFAULT 'unknown'
procurement_cost_confidence TEXT NOT NULL DEFAULT 'unknown'
price_snapshot_id TEXT NOT NULL DEFAULT ''
billing_line_id TEXT NOT NULL DEFAULT ''
```

保留当前 `cost_cents` 作为兼容字段；在完成迁移前明确命名为下游估算成本，不改变其既有语义。

### 4.2 `provider_price_observations`

保存插件、API 或人工导入的原始采购报价：

```text
id, provider_id, provider_account_id, protocol, upstream_model
currency, uncached_input_price, output_price
cache_read_price, cache_write_5m_price, cache_write_1h_price
request_price, quoted_multiplier, recharge_multiplier
source_kind, source_reference, evidence_hash
observed_at, effective_from, expires_at, confidence, raw_payload
```

### 4.3 `provider_price_snapshots`

Core 校验后发布的不可变快照。增加统一币种、汇率版本、税费和归一化结果。Gateway 只引用 Snapshot ID，不直接读取 Observation。

### 4.4 `provider_billing_lines`

保存第三方账单或 Usage Cost Line：

```text
id, provider_id, provider_account_id
external_request_id, external_line_id, upstream_model
started_at, ended_at, currency, amount_micros
input_cost_micros, output_cost_micros
cache_read_cost_micros, cache_write_cost_micros
source_kind, confidence, raw_payload_hash
reconciliation_status, usage_id
```

余额快照只能产生 `unallocated` 成本，除非有稳定请求关联或账单明细支持分摊。

### 4.5 `provider_cache_capabilities`

每个账号、模型和协议的能力状态：

```text
provider_account_id, upstream_model, protocol
support_status, pool_affinity_grade
affinity_transport, cache_control_mode, usage_mapping_version
claimed_at, first_observed_at, billed_verified_at
last_success_at, last_failure_at, degraded_reason
production_sample_count, probe_sample_count
updated_at
```

`affinity_transport` 由 Adapter 声明，例如 `none/header/body/custom`，并保存字段映射版本。未经验证时只透传原字段，不自动把一个协议的缓存语义翻译成另一个协议。

### 4.6 `provider_cache_probe_runs`

```text
id, provider_account_id, upstream_model, protocol
probe_series_id, phase, session_hash
prefix_fingerprint, prefix_tokens, suffix_variant
cache_fields_present, cache_read_tokens, cache_write_tokens
ttft_ms, upstream_request_id
quoted_cost_micros, billed_cost_micros, billing_confidence
status, failure_reason, started_at, finished_at
```

`phase` 使用 `warm/reuse/negative_control`，便于验证“重复前缀命中、突变前缀不命中”。

### 4.7 `provider_effective_cost_rollups`

按 `provider_account + upstream_model + protocol + workload_class + window` 生成不可变或可重算汇总：

```text
request_count, eligible_request_count
cache_metrics_covered_requests
total_input_tokens, cache_read_tokens, cache_write_tokens
actual_procurement_cost_micros
uncached_equivalent_cost_micros
quoted_multiplier, billed_multiplier
workload_effective_multiplier, standardized_effective_multiplier
cache_support_grade, workload_cache_effectiveness
affinity_consistency_rate, billing_consistency_rate
cost_confidence, window_start, window_end, computed_at
```

## 5. 缓存指标

### 5.1 覆盖率

```text
cache_metrics_coverage
  = cache_fields_present_request_count / successful_request_count
```

覆盖率低时，命中率只能代表可见子集，不能参与自动切换。

### 5.2 合格请求命中率

```text
eligible_request_hit_rate
  = eligible_requests_with_cache_read / eligible_requests_with_cache_metrics
```

`eligible` 由模型最低 Token、已知缓存模式、稳定前缀和 TTL 判断；无法判断时标记 `unknown`，不能混入分母。

### 5.3 Token 命中率

```text
cache_token_hit_rate
  = sum(cache_read_tokens) / sum(total_input_tokens)
```

同时展示 P50/P95 请求级 Token 命中率，避免少量超长 Prompt 掩盖大量 Miss。

### 5.4 写读比

```text
cache_write_read_ratio
  = sum(cache_write_tokens) / max(sum(cache_read_tokens), 1)
```

长期写读比高，说明重复写入多、复用少，常见原因包括会话被打散、TTL 不合适或前缀不稳定。

### 5.5 亲和一致率

```text
affinity_consistency_rate
  = successful_reuse_probe_series / completed_reuse_probe_series
```

生产流量另算：

```text
supplier_affinity_reuse_rate
  = requests_reusing_bound_supplier / requests_with_existing_supplier_binding
```

必须按 `affinity_break_reason` 排除主动故障转移、限流和策略变更，避免把合理切换误判为号池碎片化。

### 5.6 缓存经济收益

```text
uncached_equivalent_input_cost
  = total_input_tokens * uncached_input_price

net_cache_savings
  = uncached_equivalent_input_cost
  - actual_input_cost

cache_savings_rate
  = net_cache_savings / uncached_equivalent_input_cost
```

缓存写可能比普通输入更贵，因此单次请求的 `net_cache_savings` 可以为负数，不能只累计正数。

TTFT 改善仅作为辅助指标：

```text
ttft_improvement_rate
  = (uncached_probe_ttft - cached_probe_ttft) / uncached_probe_ttft
```

延迟变化受队列、网络和模型负载影响，不构成缓存命中的独立证据。

## 6. 真正价格和倍率换算

### 6.1 请求实扣成本

```text
actual_procurement_cost
  = billing_line.amount
```

没有精确账单行时：

```text
estimated_procurement_cost
  = recharge_multiplier * (
      uncached_input_tokens * snapshot.uncached_input_price
      + cache_write_5m_tokens * snapshot.cache_write_5m_price
      + cache_write_1h_tokens * snapshot.cache_write_1h_price
      + cache_read_tokens * snapshot.cache_read_price
      + output_tokens * snapshot.output_price
      + request_fee
    )
```

API 创建或更新采购价时，上述八个价格分项必须显式出现；明确免费应提交 `0`，字段缺失会被拒绝。`recharge_multiplier` 未提交或为兼容旧数据的零值时按 `1` 处理，新写入值必须大于零。

### 6.2 观测有效输入单价

只有账单存在输入/输出分项或可可信分摊时计算：

```text
observed_effective_input_price_per_1m
  = sum(actual_input_cost) / sum(total_input_tokens) * 1,000,000
```

若账单只有请求总额，改为展示：

```text
observed_effective_request_cost
  = sum(actual_procurement_cost) / request_count
```

### 6.3 账单倍率

用于检查第三方宣传倍率是否真实：

```text
billed_multiplier
  = actual_procurement_cost_without_cache_benefit
  / official_uncached_equivalent_cost
```

`actual_procurement_cost_without_cache_benefit` 只有在账单分项足够时才可推导；否则不展示。

### 6.4 工作负载有效倍率

用于回答“这个供应商在我们的真实流量下到底多少钱”：

```text
workload_effective_multiplier
  = actual_procurement_cost
  / official_uncached_equivalent_cost
```

该值包含第三方倍率、充值折扣、缓存读写、输出、请求费和实际工作负载分布。

当前报告 API 同时返回 `cost_available`、采购价八个分项、`recharge_multiplier`、`uncached_cost_micros_per_1m`、`cache_savings_micros_per_1m`、`cache_savings_rate` 和 `cache_economics_available`。`cost_available` 用于区分已验证零成本和缺少成本证据。只有窗口内存在缓存观测、缓存分项 Token 与总输入守恒且采购缓存单价存在时，才把净节省标记为可用；正常错误率不会单独抹掉指标，无法解释的 Token 缺口仍会拒绝计算，缓存写入溢价可使净节省为负数。

### 6.5 标准化有效倍率

直接比较生产流量会被客户结构影响。对供应商切换还要使用同一标准工作负载：

```text
standardized_effective_cost(provider)
  = standard_uncached_tokens * provider_uncached_price
  + standard_cache_write_tokens * provider_cache_write_price
  + standard_cache_read_tokens * provider_cache_read_price
  + standard_output_tokens * provider_output_price
```

标准工作负载来自同一租户/模型最近窗口，或受控探针固定分布。所有候选必须使用同一个快照版本。

### 6.6 盈亏平衡命中率

在简化为普通输入、缓存写和缓存读三种价格时：

```text
effective_input_price
  = miss_rate * uncached_price
  + write_rate * cache_write_price
  + hit_rate * cache_read_price
```

页面可求出候选供应商相对当前线路的盈亏平衡 `hit_rate`，但必须展示假设的写入率、TTL 和价格版本，不能把模拟值当成实扣事实。

## 7. 双维度评分

### 7.1 `cache_support_grade`

判断第三方是否真实透传并兑现缓存：

- 协议字段接受率。
- Usage 覆盖率。
- 受控探针成功率。
- 账单一致率。
- 号池亲和一致率。

### 7.2 `workload_cache_effectiveness`

判断我们的请求是否适合缓存：

- 合格请求占比。
- 稳定前缀复用率。
- Token 命中率。
- 写读比。
- 净缓存节省。

供应商评分不能因某个客户的唯一 Prompt 很多而下降；工作负载问题应反馈给客户或 Prompt 设计，而不是触发供应商切换。

## 8. 切换决策快照

每次评估保存：

```text
decision_id, policy_id, current_provider_account_id
candidate_provider_account_id, gateway_model, upstream_model, protocol, workload_class
current_cost_snapshot_id, candidate_cost_snapshot_id
current_effective_cost, candidate_effective_cost
cache_support_grade, affinity_consistency_rate
error_rate_delta, latency_delta, cost_delta
sample_count, confidence, decision
reason_codes, observed_at, valid_until
```

`gateway_model` 是客户请求的公共路由键；`upstream_model` 是采购证据维度。评估必须用 `provider_account_id + upstream_model + protocol` 精确定位当前与候选报告行，运行时则用 `gateway_model + protocol` 匹配客户请求。现有决策表不重复持久化 `upstream_model`，API 从不可变的候选价格快照派生该字段；这样避免双写漂移，也不需要新增数据库列。

`decision` 使用：

- `hold`：保持当前供应商。
- `recommend`：有优势但需人工确认。
- `canary`：只让配置比例的未绑定客户 cohort 进入候选，已有客户/会话绑定不主动打断。
- `promote`：成为尚无有效绑定客户的默认供应商，现有绑定按 TTL 或硬故障迁移。
- `degrade`：降低权重，不中断现有健康会话。
- `emergency_failover`：健康或容量硬故障立即切换。

原因码至少包括：

```text
effective_cost_better
cache_economics_better
pool_affinity_better
billing_verified
insufficient_samples
price_stale
cache_metrics_missing
cache_economics_evidence_missing
billing_mismatch
pool_fragmentation_suspected
error_rate_regression_exceeded
p95_latency_regression_exceeded
p95_latency_evidence_missing
cache_quality_tiebreaker
affinity_preserved
affinity_broken_for_health
```

## 9. 数据保留与隐私

- 会话、客户和前缀只保存 HMAC；不同租户使用隔离命名空间。
- 不保存探针以外的 Prompt 内容，不用客户 Prompt 做缓存探针。
- 原始账单 Payload 加密或只保存内容哈希和必要字段。
- 请求级原始证据按企业策略短期保留；日/周汇总长期保留。
- 删除 API Key 或客户时清理可关联亲和绑定，聚合财务事实按审计策略匿名保留。
- 插件只能写自己负责供应商的 Observation，不能读取客户 Prompt、Gateway Key 原文或其他供应商账单。
