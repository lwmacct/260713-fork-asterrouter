package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Service) ListGovernancePolicies(ctx context.Context) ([]GovernancePolicy, error) {
	return s.repo.ListGovernancePolicies(ctx)
}

func (s *Service) CreateGovernancePolicy(ctx context.Context, actor string, req GovernancePolicyRequest) (GovernancePolicy, error) {
	now := time.Now().UTC()
	policy, err := governancePolicyFromRequest(req, now)
	if err != nil {
		return GovernancePolicy{}, err
	}
	if err := s.validateGovernancePolicyScope(ctx, policy.ScopeType, policy.ScopeID); err != nil {
		return GovernancePolicy{}, err
	}
	policy.ID = "pol_" + randomID(10)
	policy.Version = 1
	policy.LastUpdatedBy = actorOrSystem(actor)
	if err := s.repo.SaveGovernancePolicy(ctx, policy); err != nil {
		return GovernancePolicy{}, err
	}
	if err := s.audit(ctx, actor, "create", "governance_policy", policy.ID, fmt.Sprintf("Created governance policy %s", policy.Name)); err != nil {
		return GovernancePolicy{}, err
	}
	return policy, nil
}

func (s *Service) UpdateGovernancePolicy(ctx context.Context, actor string, id string, req GovernancePolicyRequest) (GovernancePolicy, error) {
	existing, err := s.governancePolicyByID(ctx, id)
	if err != nil {
		return GovernancePolicy{}, err
	}
	policy, err := governancePolicyFromRequest(req, existing.CreatedAt)
	if err != nil {
		return GovernancePolicy{}, err
	}
	if err := s.validateGovernancePolicyScope(ctx, policy.ScopeType, policy.ScopeID); err != nil {
		return GovernancePolicy{}, err
	}
	policy.ID = existing.ID
	policy.Version = nonNegative(existing.Version) + 1
	policy.LastUpdatedBy = actorOrSystem(actor)
	policy.CreatedAt = existing.CreatedAt
	policy.UpdatedAt = time.Now().UTC()
	if err := s.repo.SaveGovernancePolicy(ctx, policy); err != nil {
		return GovernancePolicy{}, err
	}
	if err := s.audit(ctx, actor, "update", "governance_policy", policy.ID, fmt.Sprintf("Updated governance policy %s", policy.Name)); err != nil {
		return GovernancePolicy{}, err
	}
	return policy, nil
}

func (s *Service) ExplainGatewayPolicyForAPIKey(ctx context.Context, apiKeyID string) (GatewayPolicyExplanation, error) {
	key, err := s.apiKeyByID(ctx, apiKeyID)
	if err != nil {
		return GatewayPolicyExplanation{}, err
	}
	policies, err := s.repo.ListGovernancePolicies(ctx)
	if err != nil {
		return GatewayPolicyExplanation{}, err
	}
	explanation := explainGatewayPolicy(policies, key)
	explanation.APIKeyID = key.ID
	return explanation, nil
}

func governancePolicyFromRequest(req GovernancePolicyRequest, createdAt time.Time) (GovernancePolicy, error) {
	now := time.Now().UTC()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return GovernancePolicy{}, errors.New("name is required")
	}
	scopeType := strings.TrimSpace(req.ScopeType)
	if scopeType == "" {
		scopeType = GovernancePolicyScopeGlobal
	}
	if !oneOf(scopeType, GovernancePolicyScopeGlobal, GovernancePolicyScopeAPIKey) {
		return GovernancePolicy{}, errors.New("scope_type must be global or api_key")
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeType == GovernancePolicyScopeGlobal {
		scopeID = ""
	} else if scopeID == "" {
		return GovernancePolicy{}, errors.New("scope_id is required for scoped policies")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = GovernancePolicyStatusActive
	}
	if !oneOf(status, GovernancePolicyStatusActive, GovernancePolicyStatusDisabled) {
		return GovernancePolicy{}, errors.New("status must be active or disabled")
	}
	overageAction := strings.TrimSpace(req.OverageAction)
	if overageAction == "" {
		overageAction = GovernancePolicyOverageBlock
	}
	if !oneOf(overageAction, GovernancePolicyOverageBlock, GovernancePolicyOverageWarn, GovernancePolicyOverageFallback) {
		return GovernancePolicy{}, errors.New("overage_action must be block, warn, or fallback")
	}
	promptLoggingMode := strings.TrimSpace(req.PromptLoggingMode)
	if promptLoggingMode == "" {
		promptLoggingMode = GovernancePolicyPromptLoggingMetadataOnly
	}
	if !oneOf(promptLoggingMode, GovernancePolicyPromptLoggingDisabled, GovernancePolicyPromptLoggingMetadataOnly, GovernancePolicyPromptLoggingRedacted) {
		return GovernancePolicy{}, errors.New("prompt_logging_mode must be disabled, metadata_only, or redacted")
	}
	if req.QPSLimit < 0 {
		return GovernancePolicy{}, errors.New("qps_limit must be greater than or equal to 0")
	}
	if req.MonthlyTokenLimit < 0 {
		return GovernancePolicy{}, errors.New("monthly_token_limit must be greater than or equal to 0")
	}
	if req.MonthlyBudgetMicros < 0 {
		return GovernancePolicy{}, errors.New("monthly_budget_micros must be greater than or equal to 0")
	}
	if req.RetentionDays < 0 {
		return GovernancePolicy{}, errors.New("retention_days must be greater than or equal to 0")
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	return GovernancePolicy{
		Name:                name,
		Description:         strings.TrimSpace(req.Description),
		ScopeType:           scopeType,
		ScopeID:             scopeID,
		ModelAllowlist:      normalizeStringSet(req.ModelAllowlist),
		ModelDenylist:       normalizeStringSet(req.ModelDenylist),
		QPSLimit:            req.QPSLimit,
		MonthlyTokenLimit:   req.MonthlyTokenLimit,
		MonthlyBudgetMicros: req.MonthlyBudgetMicros,
		OverageAction:       overageAction,
		PromptLoggingMode:   promptLoggingMode,
		RetentionDays:       req.RetentionDays,
		ToolCallAllowed:     req.ToolCallAllowed,
		ImageInputAllowed:   req.ImageInputAllowed,
		WebAccessAllowed:    req.WebAccessAllowed,
		Status:              status,
		CreatedAt:           createdAt,
		UpdatedAt:           now,
	}, nil
}

func (s *Service) governancePolicyByID(ctx context.Context, id string) (GovernancePolicy, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GovernancePolicy{}, errors.New("governance policy id is required")
	}
	policies, err := s.repo.ListGovernancePolicies(ctx)
	if err != nil {
		return GovernancePolicy{}, err
	}
	for _, policy := range policies {
		if policy.ID == id {
			return policy, nil
		}
	}
	return GovernancePolicy{}, fmt.Errorf("governance policy %s not found", id)
}

func (s *Service) validateGovernancePolicyScope(ctx context.Context, scopeType string, scopeID string) error {
	switch scopeType {
	case GovernancePolicyScopeGlobal:
		return nil
	case GovernancePolicyScopeAPIKey:
		_, err := s.apiKeyByID(ctx, scopeID)
		return err
	default:
		return fmt.Errorf("unsupported governance policy scope %s", scopeType)
	}
}

func normalizeStringSet(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func actorOrSystem(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return systemActor
	}
	return actor
}
