package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

const pricingSchema = `
CREATE TABLE IF NOT EXISTS pricing_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost','customer_charge')),
  scope_type TEXT NOT NULL CHECK (scope_type IN ('global','operator_plan')),
  scope_id TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active','disabled')),
  active_version_id TEXT NOT NULL DEFAULT '',
  lock_version BIGINT NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK ((scope_type = 'global' AND scope_id = '') OR (scope_type = 'operator_plan' AND scope_id <> '')),
  CHECK (model <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS pricing_rules_slot_idx
  ON pricing_rules(purpose, scope_type, scope_id, model);
CREATE INDEX IF NOT EXISTS pricing_rules_lookup_idx
  ON pricing_rules(purpose, scope_type, scope_id, model, status);

CREATE TABLE IF NOT EXISTS pricing_rule_versions (
  id TEXT PRIMARY KEY,
  rule_id TEXT NOT NULL REFERENCES pricing_rules(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL DEFAULT 0,
  engine_version INTEGER NOT NULL CHECK (engine_version = 1),
  currency TEXT NOT NULL CHECK (currency = 'USD'),
  expression TEXT NOT NULL CHECK (octet_length(expression) BETWEEN 1 AND 8192),
  expression_hash TEXT NOT NULL CHECK (expression_hash ~ '^[0-9a-f]{64}$'),
  analysis_json JSONB NOT NULL DEFAULT '{}',
  authoring_mode TEXT NOT NULL CHECK (authoring_mode IN ('visual','raw')),
  test_cases_json JSONB NOT NULL DEFAULT '[]',
  state TEXT NOT NULL CHECK (state IN ('draft','published')),
  created_by TEXT NOT NULL DEFAULT '',
  published_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  published_at TIMESTAMPTZ,
  UNIQUE(rule_id, revision)
);
CREATE UNIQUE INDEX IF NOT EXISTS pricing_rule_versions_one_draft_idx
  ON pricing_rule_versions(rule_id) WHERE state = 'draft';
CREATE INDEX IF NOT EXISTS pricing_rule_versions_rule_revision_idx
  ON pricing_rule_versions(rule_id, revision DESC);

CREATE TABLE IF NOT EXISTS pricing_evaluations (
  id TEXT PRIMARY KEY,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost','customer_charge')),
  phase TEXT NOT NULL CHECK (phase IN ('estimate','settlement','replay')),
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
  status TEXT NOT NULL CHECK (status IN ('succeeded','failed','disputed')),
  failure_code TEXT NOT NULL DEFAULT '',
  replay_of_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  CHECK ((status = 'succeeded' AND amount_micros IS NOT NULL AND amount_micros >= 0 AND failure_code = '') OR
         (status IN ('failed','disputed') AND amount_micros IS NULL AND failure_code <> ''))
);
CREATE UNIQUE INDEX IF NOT EXISTS pricing_evaluations_estimate_idx
  ON pricing_evaluations(operation_id, purpose, phase) WHERE phase = 'estimate';
CREATE UNIQUE INDEX IF NOT EXISTS pricing_evaluations_settlement_idx
  ON pricing_evaluations(operation_id, attempt_id, usage_version, purpose, phase) WHERE phase = 'settlement';
CREATE INDEX IF NOT EXISTS pricing_evaluations_usage_idx
  ON pricing_evaluations(usage_record_id, purpose);

CREATE TABLE IF NOT EXISTS billing_hold_pricing_versions (
  hold_id TEXT NOT NULL REFERENCES billing_holds(id) ON DELETE RESTRICT,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost','customer_charge')),
  pricing_rule_version_id TEXT NOT NULL REFERENCES pricing_rule_versions(id) ON DELETE RESTRICT,
  estimate_evaluation_id TEXT NOT NULL REFERENCES pricing_evaluations(id) ON DELETE RESTRICT,
  settlement_evaluation_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (hold_id, purpose)
);

CREATE TABLE IF NOT EXISTS billing_ledger_entries (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES ai_operations(id) ON DELETE RESTRICT,
  attempt_id TEXT NOT NULL DEFAULT '',
  usage_version INTEGER NOT NULL,
  usage_record_id TEXT NOT NULL,
  request_fingerprint TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('usage_cost','customer_charge')),
  amount_micros BIGINT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
  pricing_evaluation_id TEXT NOT NULL REFERENCES pricing_evaluations(id) ON DELETE RESTRICT,
  pricing_rule_version_id TEXT NOT NULL REFERENCES pricing_rule_versions(id) ON DELETE RESTRICT,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(operation_id, attempt_id, usage_version, purpose)
);
CREATE INDEX IF NOT EXISTS billing_ledger_operation_created_idx ON billing_ledger_entries(operation_id, created_at);

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
`

func (r *MemoryRepository) CreatePricingRule(_ context.Context, rule PricingRule, draft PricingRuleVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pricingRules[rule.ID]; exists {
		return errors.New("pricing rule already exists")
	}
	for _, existing := range r.pricingRules {
		if pricingRuleSameSlot(existing, rule) {
			return errors.New("pricing rule slot already exists")
		}
	}
	if draft.RuleID != rule.ID || draft.State != PricingVersionStateDraft {
		return errors.New("pricing draft does not belong to rule")
	}
	r.pricingRules[rule.ID] = rule
	r.pricingRuleVersions[draft.ID] = clonePricingRuleVersion(draft)
	return nil
}

func (r *MemoryRepository) ListPricingRules(context.Context) ([]PricingRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PricingRule, 0, len(r.pricingRules))
	for _, rule := range r.pricingRules {
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Purpose == out[j].Purpose {
			return out[i].Model < out[j].Model
		}
		return out[i].Purpose < out[j].Purpose
	})
	return out, nil
}

func (r *MemoryRepository) FindPricingRule(_ context.Context, id string) (PricingRule, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rule, ok := r.pricingRules[strings.TrimSpace(id)]
	return rule, ok, nil
}

func (r *MemoryRepository) ListPricingRuleVersions(_ context.Context, ruleID string) ([]PricingRuleVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PricingRuleVersion, 0)
	for _, version := range r.pricingRuleVersions {
		if version.RuleID == strings.TrimSpace(ruleID) {
			out = append(out, clonePricingRuleVersion(version))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}

func (r *MemoryRepository) FindPricingRuleVersion(_ context.Context, id string) (PricingRuleVersion, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version, ok := r.pricingRuleVersions[strings.TrimSpace(id)]
	return clonePricingRuleVersion(version), ok, nil
}

func (r *MemoryRepository) SavePricingRuleDraft(_ context.Context, rule PricingRule, draft PricingRuleVersion, expectedLockVersion int64) (PricingRule, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.pricingRules[rule.ID]
	if !ok || current.LockVersion != expectedLockVersion {
		return PricingRule{}, false, nil
	}
	if current.Purpose != rule.Purpose || current.ScopeType != rule.ScopeType || current.ScopeID != rule.ScopeID || current.Model != rule.Model {
		return PricingRule{}, false, errors.New("pricing rule selection slot is immutable")
	}
	if draft.RuleID != rule.ID || draft.State != PricingVersionStateDraft {
		return PricingRule{}, false, errors.New("pricing draft is invalid")
	}
	for id, version := range r.pricingRuleVersions {
		if version.RuleID == rule.ID && version.State == PricingVersionStateDraft && id != draft.ID {
			delete(r.pricingRuleVersions, id)
		}
	}
	rule.LockVersion = current.LockVersion + 1
	rule.ActiveVersionID = current.ActiveVersionID
	rule.CreatedAt = current.CreatedAt
	rule.UpdatedAt = rule.UpdatedAt.UTC()
	r.pricingRules[rule.ID] = rule
	r.pricingRuleVersions[draft.ID] = clonePricingRuleVersion(draft)
	return rule, true, nil
}

func (r *MemoryRepository) PublishPricingRuleVersion(_ context.Context, version PricingRuleVersion, expectedLockVersion int64, expectedActiveVersionID string) (PricingRule, PricingRuleVersion, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rule, ok := r.pricingRules[version.RuleID]
	if !ok || rule.LockVersion != expectedLockVersion || (expectedActiveVersionID != "" && rule.ActiveVersionID != expectedActiveVersionID) {
		return PricingRule{}, PricingRuleVersion{}, false, nil
	}
	draft, ok := r.pricingRuleVersions[version.ID]
	if !ok || draft.RuleID != rule.ID || draft.State != PricingVersionStateDraft {
		return PricingRule{}, PricingRuleVersion{}, false, errors.New("pricing draft not found")
	}
	maxRevision := 0
	for _, item := range r.pricingRuleVersions {
		if item.RuleID == rule.ID && item.State == PricingVersionStatePublished && item.Revision > maxRevision {
			maxRevision = item.Revision
		}
	}
	version.Revision = maxRevision + 1
	version.State = PricingVersionStatePublished
	version.RuleID = rule.ID
	version.UpdatedAt = version.UpdatedAt.UTC()
	version.CreatedAt = draft.CreatedAt
	version.PublishedAt = timePointer(version.UpdatedAt)
	r.pricingRuleVersions[version.ID] = clonePricingRuleVersion(version)
	rule.ActiveVersionID = version.ID
	rule.LockVersion++
	rule.UpdatedAt = version.UpdatedAt
	rule.UpdatedBy = version.PublishedBy
	r.pricingRules[rule.ID] = rule
	return rule, version, true, nil
}

func (r *MemoryRepository) ActivatePricingRuleVersion(_ context.Context, ruleID, versionID string, expectedLockVersion int64, actor string, updatedAt time.Time) (PricingRule, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rule, ok := r.pricingRules[ruleID]
	version, versionOK := r.pricingRuleVersions[versionID]
	if !ok || !versionOK || version.RuleID != ruleID || version.State != PricingVersionStatePublished || rule.LockVersion != expectedLockVersion {
		return PricingRule{}, false, nil
	}
	rule.ActiveVersionID = versionID
	rule.LockVersion++
	rule.UpdatedBy = actor
	rule.UpdatedAt = updatedAt.UTC()
	r.pricingRules[ruleID] = rule
	return rule, true, nil
}

func (r *MemoryRepository) SetPricingRuleStatus(_ context.Context, ruleID, status string, expectedLockVersion int64, actor string, updatedAt time.Time) (PricingRule, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rule, ok := r.pricingRules[ruleID]
	if !ok || rule.LockVersion != expectedLockVersion {
		return PricingRule{}, false, nil
	}
	rule.Status = status
	rule.LockVersion++
	rule.UpdatedBy = actor
	rule.UpdatedAt = updatedAt.UTC()
	r.pricingRules[ruleID] = rule
	return rule, true, nil
}

func (r *MemoryRepository) SavePricingEvaluation(_ context.Context, evaluation PricingEvaluation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pricingEvaluations[evaluation.ID]; exists {
		return errors.New("pricing evaluation already exists")
	}
	r.pricingEvaluations[evaluation.ID] = clonePricingEvaluation(evaluation)
	return nil
}

func (r *MemoryRepository) FindPricingEvaluation(_ context.Context, id string) (PricingEvaluation, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	evaluation, ok := r.pricingEvaluations[strings.TrimSpace(id)]
	return clonePricingEvaluation(evaluation), ok, nil
}

func clonePricingRuleVersion(value PricingRuleVersion) PricingRuleVersion {
	value.TestCases = append([]PricingRuleTestCase(nil), value.TestCases...)
	return value
}

func clonePricingEvaluation(value PricingEvaluation) PricingEvaluation {
	value.Lines = append([]pricing.PricingLine(nil), value.Lines...)
	return value
}

func pricingRuleSameSlot(left, right PricingRule) bool {
	return left.Purpose == right.Purpose && left.ScopeType == right.ScopeType && left.ScopeID == right.ScopeID && left.Model == right.Model
}

func (r *PostgresRepository) CreatePricingRule(ctx context.Context, rule PricingRule, draft PricingRuleVersion) error {
	analysis, err := json.Marshal(draft.Analysis)
	if err != nil {
		return err
	}
	tests, err := json.Marshal(draft.TestCases)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `
INSERT INTO pricing_rules(id,name,purpose,scope_type,scope_id,model,status,active_version_id,lock_version,created_by,updated_by,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,'',1,$8,$8,$9,$9)`, rule.ID, rule.Name, rule.Purpose, rule.ScopeType, rule.ScopeID, rule.Model, rule.Status, rule.CreatedBy, rule.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO pricing_rule_versions(id,rule_id,revision,engine_version,currency,expression,expression_hash,analysis_json,authoring_mode,test_cases_json,state,created_by,created_at,updated_at)
VALUES($1,$2,0,$3,$4,$5,$6,$7,$8,$9,'draft',$10,$11,$11)`, draft.ID, draft.RuleID, draft.EngineVersion, draft.Currency, draft.Expression, draft.ExpressionHash, analysis, draft.AuthoringMode, tests, draft.CreatedBy, draft.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRepository) ListPricingRules(ctx context.Context) ([]PricingRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,purpose,scope_type,scope_id,model,status,active_version_id,lock_version,created_by,updated_by,created_at,updated_at FROM pricing_rules ORDER BY purpose, model, scope_type, scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PricingRule, 0)
	for rows.Next() {
		item, err := scanPricingRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindPricingRule(ctx context.Context, id string) (PricingRule, bool, error) {
	item, err := scanPricingRule(r.db.QueryRowContext(ctx, `SELECT id,name,purpose,scope_type,scope_id,model,status,active_version_id,lock_version,created_by,updated_by,created_at,updated_at FROM pricing_rules WHERE id=$1`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return PricingRule{}, false, nil
	}
	return item, err == nil, err
}

func (r *PostgresRepository) ListPricingRuleVersions(ctx context.Context, ruleID string) ([]PricingRuleVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,rule_id,revision,engine_version,currency,expression,expression_hash,analysis_json,authoring_mode,test_cases_json,state,created_by,published_by,created_at,updated_at,published_at FROM pricing_rule_versions WHERE rule_id=$1 ORDER BY revision DESC, created_at DESC`, strings.TrimSpace(ruleID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PricingRuleVersion, 0)
	for rows.Next() {
		item, err := scanPricingRuleVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindPricingRuleVersion(ctx context.Context, id string) (PricingRuleVersion, bool, error) {
	item, err := scanPricingRuleVersion(r.db.QueryRowContext(ctx, `SELECT id,rule_id,revision,engine_version,currency,expression,expression_hash,analysis_json,authoring_mode,test_cases_json,state,created_by,published_by,created_at,updated_at,published_at FROM pricing_rule_versions WHERE id=$1`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return PricingRuleVersion{}, false, nil
	}
	return item, err == nil, err
}

func (r *PostgresRepository) SavePricingRuleDraft(ctx context.Context, rule PricingRule, draft PricingRuleVersion, expectedLockVersion int64) (PricingRule, bool, error) {
	analysis, err := json.Marshal(draft.Analysis)
	if err != nil {
		return PricingRule{}, false, err
	}
	tests, err := json.Marshal(draft.TestCases)
	if err != nil {
		return PricingRule{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PricingRule{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var current PricingRule
	if _, err = scanPricingRule(tx.QueryRowContext(ctx, `SELECT id,name,purpose,scope_type,scope_id,model,status,active_version_id,lock_version,created_by,updated_by,created_at,updated_at FROM pricing_rules WHERE id=$1 FOR UPDATE`, rule.ID), &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PricingRule{}, false, nil
		}
		return PricingRule{}, false, err
	}
	if current.LockVersion != expectedLockVersion {
		return PricingRule{}, false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM pricing_rule_versions WHERE rule_id=$1 AND state='draft' AND id<>$2`, rule.ID, draft.ID); err != nil {
		return PricingRule{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO pricing_rule_versions(id,rule_id,revision,engine_version,currency,expression,expression_hash,analysis_json,authoring_mode,test_cases_json,state,created_by,created_at,updated_at)
VALUES($1,$2,0,$3,$4,$5,$6,$7,$8,$9,'draft',$10,$11,$11)
ON CONFLICT(id) DO UPDATE SET expression=EXCLUDED.expression, expression_hash=EXCLUDED.expression_hash, analysis_json=EXCLUDED.analysis_json, authoring_mode=EXCLUDED.authoring_mode, test_cases_json=EXCLUDED.test_cases_json, updated_at=EXCLUDED.updated_at
`, draft.ID, draft.RuleID, draft.EngineVersion, draft.Currency, draft.Expression, draft.ExpressionHash, analysis, draft.AuthoringMode, tests, draft.CreatedBy, draft.CreatedAt); err != nil {
		return PricingRule{}, false, err
	}
	now := rule.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pricing_rules SET name=$1,status=$2,updated_by=$3,updated_at=$4,lock_version=lock_version+1 WHERE id=$5 AND lock_version=$6`, rule.Name, rule.Status, rule.UpdatedBy, now, rule.ID, expectedLockVersion); err != nil {
		return PricingRule{}, false, err
	}
	current.Name, current.Status, current.UpdatedBy, current.UpdatedAt, current.LockVersion = rule.Name, rule.Status, rule.UpdatedBy, now, expectedLockVersion+1
	if err = tx.Commit(); err != nil {
		return PricingRule{}, false, err
	}
	return current, true, nil
}

func (r *PostgresRepository) PublishPricingRuleVersion(ctx context.Context, version PricingRuleVersion, expectedLockVersion int64, expectedActiveVersionID string) (PricingRule, PricingRuleVersion, bool, error) {
	analysis, err := json.Marshal(version.Analysis)
	if err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	tests, err := json.Marshal(version.TestCases)
	if err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var rule PricingRule
	if _, err = scanPricingRule(tx.QueryRowContext(ctx, `SELECT id,name,purpose,scope_type,scope_id,model,status,active_version_id,lock_version,created_by,updated_by,created_at,updated_at FROM pricing_rules WHERE id=$1 FOR UPDATE`, version.RuleID), &rule); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PricingRule{}, PricingRuleVersion{}, false, nil
		}
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	if rule.LockVersion != expectedLockVersion || (expectedActiveVersionID != "" && rule.ActiveVersionID != expectedActiveVersionID) {
		return PricingRule{}, PricingRuleVersion{}, false, nil
	}
	var draftCreatedAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT created_at FROM pricing_rule_versions WHERE id=$1 AND rule_id=$2 AND state='draft'`, version.ID, version.RuleID).Scan(&draftCreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PricingRule{}, PricingRuleVersion{}, false, errors.New("pricing draft not found")
		}
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM pricing_rule_versions WHERE rule_id=$1 AND state='published'`, version.RuleID).Scan(&revision); err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	version.Revision = revision
	version.State = PricingVersionStatePublished
	version.CreatedAt = draftCreatedAt
	version.PublishedAt = timePointer(version.UpdatedAt.UTC())
	if _, err = tx.ExecContext(ctx, `UPDATE pricing_rule_versions SET revision=$1,engine_version=$2,currency=$3,expression=$4,expression_hash=$5,analysis_json=$6,authoring_mode=$7,test_cases_json=$8,state='published',published_by=$9,updated_at=$10,published_at=$10 WHERE id=$11 AND state='draft'`, version.Revision, version.EngineVersion, version.Currency, version.Expression, version.ExpressionHash, analysis, version.AuthoringMode, tests, version.PublishedBy, version.UpdatedAt, version.ID); err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pricing_rules SET active_version_id=$1,lock_version=lock_version+1,updated_by=$2,updated_at=$3 WHERE id=$4 AND lock_version=$5`, version.ID, version.PublishedBy, version.UpdatedAt, version.RuleID, expectedLockVersion); err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	rule.ActiveVersionID, rule.LockVersion, rule.UpdatedBy, rule.UpdatedAt = version.ID, expectedLockVersion+1, version.PublishedBy, version.UpdatedAt
	if err = tx.Commit(); err != nil {
		return PricingRule{}, PricingRuleVersion{}, false, err
	}
	return rule, version, true, nil
}

func (r *PostgresRepository) ActivatePricingRuleVersion(ctx context.Context, ruleID, versionID string, expectedLockVersion int64, actor string, updatedAt time.Time) (PricingRule, bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE pricing_rules SET active_version_id=$1,lock_version=lock_version+1,updated_by=$2,updated_at=$3 WHERE id=$4 AND lock_version=$5 AND EXISTS (SELECT 1 FROM pricing_rule_versions WHERE id=$1 AND rule_id=$4 AND state='published')`, versionID, actor, updatedAt.UTC(), ruleID, expectedLockVersion)
	if err != nil {
		return PricingRule{}, false, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return PricingRule{}, false, nil
	}
	return r.FindPricingRule(ctx, ruleID)
}

func (r *PostgresRepository) SetPricingRuleStatus(ctx context.Context, ruleID, status string, expectedLockVersion int64, actor string, updatedAt time.Time) (PricingRule, bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE pricing_rules SET status=$1,lock_version=lock_version+1,updated_by=$2,updated_at=$3 WHERE id=$4 AND lock_version=$5`, status, actor, updatedAt.UTC(), ruleID, expectedLockVersion)
	if err != nil {
		return PricingRule{}, false, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return PricingRule{}, false, nil
	}
	return r.FindPricingRule(ctx, ruleID)
}

func (r *PostgresRepository) SavePricingEvaluation(ctx context.Context, evaluation PricingEvaluation) error {
	return insertPricingEvaluation(ctx, r.db, evaluation)
}

type pricingEvaluationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertPricingEvaluation(ctx context.Context, executor pricingEvaluationExecutor, evaluation PricingEvaluation) error {
	facts, err := json.Marshal(evaluation.Facts)
	if err != nil {
		return err
	}
	lines, err := json.Marshal(evaluation.Lines)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
INSERT INTO pricing_evaluations(id,purpose,phase,operation_id,attempt_id,usage_record_id,usage_version,pricing_rule_id,pricing_rule_version_id,engine_version,expression_hash,facts_hash,facts_json,amount_micros,currency,matched_tier,line_items_json,normalization_status,status,failure_code,replay_of_id,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`, evaluation.ID, evaluation.Purpose, evaluation.Phase, evaluation.OperationID, evaluation.AttemptID, evaluation.UsageRecordID, evaluation.UsageVersion, evaluation.PricingRuleID, evaluation.PricingRuleVersionID, evaluation.EngineVersion, evaluation.ExpressionHash, evaluation.FactsHash, facts, evaluation.AmountMicros, evaluation.Currency, evaluation.MatchedTier, lines, evaluation.NormalizationStatus, evaluation.Status, evaluation.FailureCode, evaluation.ReplayOfID, evaluation.CreatedAt)
	return err
}

func (r *PostgresRepository) FindPricingEvaluation(ctx context.Context, id string) (PricingEvaluation, bool, error) {
	var evaluation PricingEvaluation
	var factsJSON, linesJSON []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,purpose,phase,operation_id,attempt_id,usage_record_id,usage_version,pricing_rule_id,pricing_rule_version_id,engine_version,expression_hash,facts_hash,facts_json,amount_micros,currency,matched_tier,line_items_json,normalization_status,status,failure_code,replay_of_id,created_at FROM pricing_evaluations WHERE id=$1`, id).Scan(&evaluation.ID, &evaluation.Purpose, &evaluation.Phase, &evaluation.OperationID, &evaluation.AttemptID, &evaluation.UsageRecordID, &evaluation.UsageVersion, &evaluation.PricingRuleID, &evaluation.PricingRuleVersionID, &evaluation.EngineVersion, &evaluation.ExpressionHash, &evaluation.FactsHash, &factsJSON, &evaluation.AmountMicros, &evaluation.Currency, &evaluation.MatchedTier, &linesJSON, &evaluation.NormalizationStatus, &evaluation.Status, &evaluation.FailureCode, &evaluation.ReplayOfID, &evaluation.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PricingEvaluation{}, false, nil
	}
	if err != nil {
		return PricingEvaluation{}, false, err
	}
	if err := json.Unmarshal(factsJSON, &evaluation.Facts); err != nil {
		return PricingEvaluation{}, false, err
	}
	if err := json.Unmarshal(linesJSON, &evaluation.Lines); err != nil {
		return PricingEvaluation{}, false, err
	}
	return evaluation, true, nil
}

type pricingScanner interface {
	Scan(dest ...any) error
}

func scanPricingRule(scanner pricingScanner, target ...*PricingRule) (PricingRule, error) {
	var item PricingRule
	err := scanner.Scan(&item.ID, &item.Name, &item.Purpose, &item.ScopeType, &item.ScopeID, &item.Model, &item.Status, &item.ActiveVersionID, &item.LockVersion, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if len(target) > 0 {
		*target[0] = item
	}
	return item, err
}

func scanPricingRuleVersion(scanner pricingScanner) (PricingRuleVersion, error) {
	var item PricingRuleVersion
	var analysisJSON, testsJSON []byte
	err := scanner.Scan(&item.ID, &item.RuleID, &item.Revision, &item.EngineVersion, &item.Currency, &item.Expression, &item.ExpressionHash, &analysisJSON, &item.AuthoringMode, &testsJSON, &item.State, &item.CreatedBy, &item.PublishedBy, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt)
	if err != nil {
		return PricingRuleVersion{}, err
	}
	if err := json.Unmarshal(analysisJSON, &item.Analysis); err != nil {
		return PricingRuleVersion{}, err
	}
	if err := json.Unmarshal(testsJSON, &item.TestCases); err != nil {
		return PricingRuleVersion{}, err
	}
	return item, nil
}
