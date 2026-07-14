ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS ttft_ms BIGINT;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS total_input_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS uncached_input_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_write_5m_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_write_1h_tokens INTEGER;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS cache_fields_present BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_normalization_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_micros BIGINT;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_currency TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_source TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_cost_confidence TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS procurement_price_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS provider_billing_line_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS usage_records_effective_pricing_idx
  ON usage_records(provider_account_id, upstream_model, protocol, created_at DESC)
  WHERE provider_account_id <> '';

CREATE INDEX IF NOT EXISTS usage_records_upstream_request_idx
  ON usage_records(provider_account_id, upstream_request_id)
  WHERE upstream_request_id <> '';

CREATE TABLE IF NOT EXISTS procurement_prices (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  uncached_input_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_read_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_5m_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_1h_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  output_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  request_micros BIGINT NOT NULL DEFAULT 0,
  reference_input_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  reference_output_micros_per_1m_tokens BIGINT NOT NULL DEFAULT 0,
  quoted_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  recharge_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
  source_kind TEXT NOT NULL DEFAULT 'manual',
  source_reference TEXT NOT NULL DEFAULT '',
  evidence_hash TEXT NOT NULL DEFAULT '',
  confidence TEXT NOT NULL DEFAULT 'estimated',
  status TEXT NOT NULL DEFAULT 'active',
  effective_from TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS procurement_prices_lookup_idx
  ON procurement_prices(provider_account_id, upstream_model, protocol, status, effective_from DESC);

CREATE TABLE IF NOT EXISTS provider_billing_lines (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  external_line_id TEXT NOT NULL DEFAULT '',
  external_request_id TEXT NOT NULL DEFAULT '',
  usage_record_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  currency TEXT NOT NULL DEFAULT 'USD',
  amount_micros BIGINT NOT NULL DEFAULT 0,
  input_cost_micros BIGINT,
  output_cost_micros BIGINT,
  cache_read_cost_micros BIGINT,
  cache_write_cost_micros BIGINT,
  source_kind TEXT NOT NULL DEFAULT 'manual',
  confidence TEXT NOT NULL DEFAULT 'unknown',
  reconciliation_status TEXT NOT NULL DEFAULT 'pending',
  raw_payload_hash TEXT NOT NULL DEFAULT '',
  usage_started_at TIMESTAMPTZ,
  usage_ended_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_billing_lines_external_unique_idx
  ON provider_billing_lines(provider_account_id, external_line_id)
  WHERE external_line_id <> '';

CREATE INDEX IF NOT EXISTS provider_billing_lines_request_idx
  ON provider_billing_lines(provider_account_id, external_request_id)
  WHERE external_request_id <> '';

CREATE TABLE IF NOT EXISTS provider_cache_capabilities (
  id TEXT PRIMARY KEY,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  support_status TEXT NOT NULL DEFAULT 'unknown',
  pool_affinity_grade TEXT NOT NULL DEFAULT 'unknown',
  affinity_transport TEXT NOT NULL DEFAULT 'none',
  affinity_field TEXT NOT NULL DEFAULT '',
  cache_control_mode TEXT NOT NULL DEFAULT 'passthrough_if_present',
  usage_schema TEXT NOT NULL DEFAULT 'auto',
  metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  eligible_request_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_write_read_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
  affinity_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  production_sample_count BIGINT NOT NULL DEFAULT 0,
  probe_sample_count BIGINT NOT NULL DEFAULT 0,
  degraded_reason TEXT NOT NULL DEFAULT '',
  last_observed_at TIMESTAMPTZ,
  last_verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(provider_account_id, upstream_model, protocol)
);

CREATE TABLE IF NOT EXISTS provider_cache_probe_runs (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  probe_series_id TEXT NOT NULL,
  session_hash TEXT NOT NULL,
  prefix_fingerprint TEXT NOT NULL,
  prefix_tokens BIGINT NOT NULL DEFAULT 0,
  warm_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  warm_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  warm_ttft_ms BIGINT NOT NULL DEFAULT 0,
  warm_upstream_request_id TEXT NOT NULL DEFAULT '',
  reuse_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  reuse_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  reuse_ttft_ms BIGINT NOT NULL DEFAULT 0,
  reuse_upstream_request_id TEXT NOT NULL DEFAULT '',
  control_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  control_cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  control_ttft_ms BIGINT NOT NULL DEFAULT 0,
  control_upstream_request_id TEXT NOT NULL DEFAULT '',
  cache_fields_present BOOLEAN NOT NULL DEFAULT FALSE,
  estimated_cost_micros BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  failure_reason TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS warm_upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS reuse_upstream_request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_cache_probe_runs ADD COLUMN IF NOT EXISTS control_upstream_request_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS provider_cache_probe_runs_account_started_idx
  ON provider_cache_probe_runs(provider_account_id, upstream_model, protocol, started_at DESC);

CREATE TABLE IF NOT EXISTS effective_pricing_policies (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL DEFAULT 'observe_only',
  window_hours INTEGER NOT NULL DEFAULT 24,
  min_sample_count BIGINT NOT NULL DEFAULT 200,
  min_metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0.8,
  min_billing_consistency DOUBLE PRECISION NOT NULL DEFAULT 0.95,
  min_cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.08,
  min_cache_hit_rate_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10,
  min_affinity_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10,
  max_cache_tiebreak_cost_regression DOUBLE PRECISION NOT NULL DEFAULT 0.02,
  max_error_rate_regression DOUBLE PRECISION NOT NULL DEFAULT 0.005,
  max_p95_latency_regression DOUBLE PRECISION NOT NULL DEFAULT 0.2,
  canary_percent INTEGER NOT NULL DEFAULT 5,
  supplier_affinity_ttl_seconds INTEGER NOT NULL DEFAULT 86400,
  account_affinity_ttl_seconds INTEGER NOT NULL DEFAULT 1800,
  probe_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  probe_daily_token_budget BIGINT NOT NULL DEFAULT 100000,
  probe_daily_cost_budget_micros BIGINT NOT NULL DEFAULT 10000000,
  probe_cooldown_seconds INTEGER NOT NULL DEFAULT 1800,
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS min_cache_hit_rate_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS min_affinity_improvement DOUBLE PRECISION NOT NULL DEFAULT 0.10;
ALTER TABLE effective_pricing_policies ADD COLUMN IF NOT EXISTS max_cache_tiebreak_cost_regression DOUBLE PRECISION NOT NULL DEFAULT 0.02;

CREATE TABLE IF NOT EXISTS effective_price_snapshots (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_connections(id) ON DELETE RESTRICT,
  provider_account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE RESTRICT,
  upstream_model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  currency TEXT NOT NULL,
  effective_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  effective_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  quoted_multiplier DOUBLE PRECISION NOT NULL DEFAULT 0,
  cache_token_hit_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  metrics_coverage DOUBLE PRECISION NOT NULL DEFAULT 0,
  billing_consistency_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  request_count BIGINT NOT NULL DEFAULT 0,
  cost_confidence TEXT NOT NULL DEFAULT 'unknown',
  price_id TEXT NOT NULL DEFAULT '',
  window_start TIMESTAMPTZ NOT NULL,
  window_end TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS effective_price_snapshots_lookup_idx
  ON effective_price_snapshots(provider_account_id, upstream_model, protocol, created_at DESC);

CREATE TABLE IF NOT EXISTS effective_pricing_decisions (
  id TEXT PRIMARY KEY,
  model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  current_provider_account_id TEXT NOT NULL DEFAULT '',
  candidate_provider_account_id TEXT NOT NULL DEFAULT '',
  current_snapshot_id TEXT NOT NULL DEFAULT '',
  candidate_snapshot_id TEXT NOT NULL DEFAULT '',
  current_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  candidate_cost_micros_per_1m BIGINT NOT NULL DEFAULT 0,
  cost_improvement DOUBLE PRECISION NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'hold',
  reason_codes TEXT NOT NULL DEFAULT '[]',
  canary_percent INTEGER NOT NULL DEFAULT 0,
  sample_count BIGINT NOT NULL DEFAULT 0,
  confidence TEXT NOT NULL DEFAULT 'unknown',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS effective_pricing_decisions_model_idx
  ON effective_pricing_decisions(model, protocol, updated_at DESC);

CREATE TABLE IF NOT EXISTS routing_affinity_bindings (
  scope_key TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  protocol TEXT NOT NULL,
  policy_version INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  last_reused_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS routing_affinity_bindings_expiry_idx
  ON routing_affinity_bindings(expires_at);
