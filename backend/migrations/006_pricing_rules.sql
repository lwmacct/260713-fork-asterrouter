CREATE TABLE IF NOT EXISTS pricing_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost', 'customer_charge')),
  scope_type TEXT NOT NULL CHECK (scope_type IN ('global', 'operator_plan')),
  scope_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
  active_version_id TEXT NOT NULL DEFAULT '',
  lock_version BIGINT NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK ((scope_type = 'global' AND scope_id = '') OR (scope_type = 'operator_plan' AND scope_id <> '')),
  CHECK (model <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS pricing_rules_slot_idx ON pricing_rules(purpose, scope_type, scope_id, model);
CREATE INDEX IF NOT EXISTS pricing_rules_lookup_idx ON pricing_rules(purpose, scope_type, scope_id, model, status);

CREATE TABLE IF NOT EXISTS pricing_rule_versions (
  id TEXT PRIMARY KEY,
  rule_id TEXT NOT NULL REFERENCES pricing_rules(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL DEFAULT 0,
  engine_version INTEGER NOT NULL CHECK (engine_version = 1),
  currency TEXT NOT NULL CHECK (currency = 'USD'),
  expression TEXT NOT NULL CHECK (octet_length(expression) BETWEEN 1 AND 8192),
  expression_hash TEXT NOT NULL CHECK (expression_hash ~ '^[0-9a-f]{64}$'),
  analysis_json JSONB NOT NULL DEFAULT '{}',
  authoring_mode TEXT NOT NULL CHECK (authoring_mode IN ('visual', 'raw')),
  test_cases_json JSONB NOT NULL DEFAULT '[]',
  state TEXT NOT NULL CHECK (state IN ('draft', 'published')),
  created_by TEXT NOT NULL DEFAULT '',
  published_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  published_at TIMESTAMPTZ,
  UNIQUE(rule_id, revision)
);

CREATE UNIQUE INDEX IF NOT EXISTS pricing_rule_versions_one_draft_idx ON pricing_rule_versions(rule_id) WHERE state = 'draft';
CREATE INDEX IF NOT EXISTS pricing_rule_versions_rule_revision_idx ON pricing_rule_versions(rule_id, revision DESC);

CREATE OR REPLACE FUNCTION prevent_published_pricing_version_mutation()
RETURNS trigger AS $$
BEGIN
  IF OLD.state = 'published' THEN
    RAISE EXCEPTION 'published pricing rule versions are immutable';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS pricing_rule_versions_immutable ON pricing_rule_versions;
CREATE TRIGGER pricing_rule_versions_immutable
BEFORE UPDATE OR DELETE ON pricing_rule_versions
FOR EACH ROW EXECUTE FUNCTION prevent_published_pricing_version_mutation();

CREATE TABLE IF NOT EXISTS pricing_evaluations (
  id TEXT PRIMARY KEY,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost', 'customer_charge')),
  phase TEXT NOT NULL CHECK (phase IN ('estimate', 'settlement', 'replay')),
  operation_id TEXT NOT NULL DEFAULT '',
  attempt_id TEXT NOT NULL DEFAULT '',
  usage_record_id TEXT NOT NULL DEFAULT '',
  usage_version INTEGER NOT NULL DEFAULT 0,
  pricing_rule_id TEXT NOT NULL REFERENCES pricing_rules(id) ON DELETE RESTRICT,
  pricing_rule_version_id TEXT NOT NULL REFERENCES pricing_rule_versions(id) ON DELETE RESTRICT,
  engine_version INTEGER NOT NULL CHECK (engine_version = 1),
  expression_hash TEXT NOT NULL CHECK (expression_hash ~ '^[0-9a-f]{64}$'),
  facts_hash TEXT NOT NULL CHECK (facts_hash ~ '^[0-9a-f]{64}$'),
  facts_json JSONB NOT NULL DEFAULT '{}',
  amount_micros BIGINT,
  currency TEXT NOT NULL CHECK (currency = 'USD'),
  matched_tier TEXT NOT NULL DEFAULT '',
  line_items_json JSONB NOT NULL DEFAULT '[]',
  normalization_status TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed', 'disputed')),
  failure_code TEXT NOT NULL DEFAULT '',
  replay_of_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  CHECK ((status = 'succeeded' AND amount_micros IS NOT NULL AND amount_micros >= 0 AND failure_code = '') OR
         (status IN ('failed', 'disputed') AND amount_micros IS NULL AND failure_code <> ''))
);

CREATE UNIQUE INDEX IF NOT EXISTS pricing_evaluations_estimate_idx
  ON pricing_evaluations(operation_id, purpose, phase) WHERE phase = 'estimate';
CREATE UNIQUE INDEX IF NOT EXISTS pricing_evaluations_settlement_idx
  ON pricing_evaluations(operation_id, attempt_id, usage_version, purpose, phase) WHERE phase = 'settlement';
CREATE INDEX IF NOT EXISTS pricing_evaluations_usage_idx ON pricing_evaluations(usage_record_id, purpose);
