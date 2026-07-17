CREATE TABLE IF NOT EXISTS governance_policies (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  scope_type TEXT NOT NULL DEFAULT 'global',
  scope_id TEXT NOT NULL DEFAULT '',
  model_allowlist TEXT NOT NULL DEFAULT '[]',
  model_denylist TEXT NOT NULL DEFAULT '[]',
  qps_limit INTEGER NOT NULL DEFAULT 0,
  monthly_token_limit INTEGER NOT NULL DEFAULT 0,
  monthly_budget_micros BIGINT NOT NULL DEFAULT 0,
  overage_action TEXT NOT NULL DEFAULT 'block',
  prompt_logging_mode TEXT NOT NULL DEFAULT 'metadata_only',
  retention_days INTEGER NOT NULL DEFAULT 30,
  tool_call_allowed BOOLEAN NOT NULL DEFAULT true,
  image_input_allowed BOOLEAN NOT NULL DEFAULT true,
  web_access_allowed BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS governance_policies_scope_idx
  ON governance_policies(scope_type, scope_id);

CREATE INDEX IF NOT EXISTS governance_policies_status_idx
  ON governance_policies(status);
