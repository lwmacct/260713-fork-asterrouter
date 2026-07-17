CREATE TABLE IF NOT EXISTS ai_operations (
  id TEXT PRIMARY KEY,
  profile_scope TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL,
  credential_id TEXT NOT NULL,
  credential_source TEXT NOT NULL,
  integration_id TEXT NOT NULL DEFAULT '',
  principal_type TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  external_subject_reference TEXT NOT NULL DEFAULT '',
  client_request_id TEXT NOT NULL DEFAULT '',
  request_fingerprint TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  protocol TEXT NOT NULL,
  operation TEXT NOT NULL,
  modality TEXT NOT NULL,
  lane TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_operations_idempotency_idx
  ON ai_operations(profile_scope, tenant_id, credential_id, idempotency_key)
  WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS ai_operations_tenant_created_idx
  ON ai_operations(profile_scope, tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_attempts (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_number INTEGER NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  UNIQUE(operation_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS ai_attempts_operation_created_idx
  ON ai_attempts(operation_id, created_at);

CREATE TABLE IF NOT EXISTS billing_ledger_entries (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL DEFAULT '',
  usage_version INTEGER NOT NULL,
  usage_record_id TEXT NOT NULL,
  request_fingerprint TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost', 'customer_charge')),
  amount_micros BIGINT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
  pricing_evaluation_id TEXT NOT NULL REFERENCES pricing_evaluations(id) ON DELETE RESTRICT,
  pricing_rule_version_id TEXT NOT NULL REFERENCES pricing_rule_versions(id) ON DELETE RESTRICT,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(operation_id, attempt_id, usage_version, purpose)
);

CREATE INDEX IF NOT EXISTS billing_ledger_operation_created_idx
  ON billing_ledger_entries(operation_id, created_at);

CREATE TABLE IF NOT EXISTS transactional_outbox (
  id TEXT PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  event_version INTEGER NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  available_at TIMESTAMPTZ NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(aggregate_type, aggregate_id, event_type, event_version)
);

CREATE INDEX IF NOT EXISTS transactional_outbox_due_idx
  ON transactional_outbox(status, available_at, created_at);

ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN IF NOT EXISTS request_fingerprint TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS usage_records_ledger_identity_idx
  ON usage_records(operation_id, attempt_id, usage_version)
  WHERE operation_id <> '';

ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_traces ADD COLUMN IF NOT EXISTS request_fingerprint TEXT NOT NULL DEFAULT '';
