CREATE TABLE IF NOT EXISTS gateway_traces (
  id TEXT PRIMARY KEY,
  api_key_id TEXT NOT NULL,
  api_fingerprint TEXT NOT NULL,
  model TEXT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT false,
  message_count INTEGER NOT NULL DEFAULT 0,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  gateway_model_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  route_group TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  route_source TEXT NOT NULL DEFAULT '',
  route_reason TEXT NOT NULL DEFAULT '',
  policy_id TEXT NOT NULL DEFAULT '',
  policy_name TEXT NOT NULL DEFAULT '',
  policy_source TEXT NOT NULL DEFAULT '',
  policy_version INTEGER NOT NULL DEFAULT 0,
  policy_snapshot TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  http_status INTEGER NOT NULL DEFAULT 0,
  error_type TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  request_summary TEXT NOT NULL DEFAULT '',
  response_summary TEXT NOT NULL DEFAULT '',
  route_attempts TEXT NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS gateway_traces_created_idx
  ON gateway_traces(created_at DESC);

CREATE INDEX IF NOT EXISTS gateway_traces_route_idx
  ON gateway_traces(provider_id, provider_account_id, created_at DESC);

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_name TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_source TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS policy_snapshot TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS gateway_traces_policy_idx
  ON gateway_traces(policy_id, created_at DESC);
