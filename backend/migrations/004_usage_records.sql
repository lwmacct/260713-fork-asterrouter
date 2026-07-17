CREATE TABLE IF NOT EXISTS usage_records (
  id TEXT PRIMARY KEY,
  api_key_id TEXT NOT NULL,
  customer_id TEXT NOT NULL DEFAULT '',
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  upstream_model TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  usage_cost_micros BIGINT,
  usage_cost_currency TEXT NOT NULL DEFAULT 'USD',
  usage_pricing_evaluation_id TEXT NOT NULL DEFAULT '',
  pricing_status TEXT NOT NULL DEFAULT 'unpriced' CHECK (pricing_status IN ('priced', 'free', 'unpriced', 'disputed')),
  created_at TIMESTAMPTZ NOT NULL
);
