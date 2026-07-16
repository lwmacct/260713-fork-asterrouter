package controlplane

import (
	"context"
	"sort"
)

func (r *MemoryRepository) ListGovernancePolicies(context.Context) ([]GovernancePolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GovernancePolicy, 0, len(r.governancePolicies))
	for _, policy := range r.governancePolicies {
		out = append(out, policy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].Name < out[j].Name
		}
		return out[i].Status < out[j].Status
	})
	return out, nil
}

func (r *MemoryRepository) SaveGovernancePolicy(_ context.Context, policy GovernancePolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.governancePolicies[policy.ID] = policy
	return nil
}

func (r *PostgresRepository) ListGovernancePolicies(ctx context.Context) ([]GovernancePolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, description, scope_type, scope_id, model_allowlist, model_denylist, qps_limit,
  monthly_token_limit, monthly_budget_cents, overage_action, prompt_logging_mode, retention_days,
  tool_call_allowed, image_input_allowed, web_access_allowed, status, version, last_updated_by, created_at, updated_at
FROM governance_policies
ORDER BY status ASC, name ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GovernancePolicy, 0)
	for rows.Next() {
		var policy GovernancePolicy
		var allowlist, denylist string
		if err := rows.Scan(
			&policy.ID,
			&policy.Name,
			&policy.Description,
			&policy.ScopeType,
			&policy.ScopeID,
			&allowlist,
			&denylist,
			&policy.QPSLimit,
			&policy.MonthlyTokenLimit,
			&policy.MonthlyBudgetCents,
			&policy.OverageAction,
			&policy.PromptLoggingMode,
			&policy.RetentionDays,
			&policy.ToolCallAllowed,
			&policy.ImageInputAllowed,
			&policy.WebAccessAllowed,
			&policy.Status,
			&policy.Version,
			&policy.LastUpdatedBy,
			&policy.CreatedAt,
			&policy.UpdatedAt,
		); err != nil {
			return nil, err
		}
		policy.ModelAllowlist = parseStringList(allowlist)
		policy.ModelDenylist = parseStringList(denylist)
		out = append(out, policy)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveGovernancePolicy(ctx context.Context, policy GovernancePolicy) error {
	allowlist := marshalStringList(policy.ModelAllowlist)
	denylist := marshalStringList(policy.ModelDenylist)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO governance_policies(
  id, name, description, scope_type, scope_id, model_allowlist, model_denylist, qps_limit,
  monthly_token_limit, monthly_budget_cents, overage_action, prompt_logging_mode, retention_days,
  tool_call_allowed, image_input_allowed, web_access_allowed, status, version, last_updated_by, created_at, updated_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
ON CONFLICT(id) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  scope_type = EXCLUDED.scope_type,
  scope_id = EXCLUDED.scope_id,
  model_allowlist = EXCLUDED.model_allowlist,
  model_denylist = EXCLUDED.model_denylist,
  qps_limit = EXCLUDED.qps_limit,
  monthly_token_limit = EXCLUDED.monthly_token_limit,
  monthly_budget_cents = EXCLUDED.monthly_budget_cents,
  overage_action = EXCLUDED.overage_action,
  prompt_logging_mode = EXCLUDED.prompt_logging_mode,
  retention_days = EXCLUDED.retention_days,
  tool_call_allowed = EXCLUDED.tool_call_allowed,
  image_input_allowed = EXCLUDED.image_input_allowed,
  web_access_allowed = EXCLUDED.web_access_allowed,
  status = EXCLUDED.status,
  version = EXCLUDED.version,
  last_updated_by = EXCLUDED.last_updated_by,
  updated_at = EXCLUDED.updated_at
`, policy.ID, policy.Name, policy.Description, policy.ScopeType, policy.ScopeID, allowlist, denylist, policy.QPSLimit, policy.MonthlyTokenLimit, policy.MonthlyBudgetCents, policy.OverageAction, policy.PromptLoggingMode, policy.RetentionDays, policy.ToolCallAllowed, policy.ImageInputAllowed, policy.WebAccessAllowed, policy.Status, governancePolicyVersion(policy), policy.LastUpdatedBy, policy.CreatedAt, policy.UpdatedAt)
	return err
}
