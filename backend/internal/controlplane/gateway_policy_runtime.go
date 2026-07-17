package controlplane

import (
	"context"
	"strings"
)

func (s *Service) validateGovernancePolicyReference(ctx context.Context, policyID string) error {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return nil
	}
	_, err := s.governancePolicyByID(ctx, policyID)
	return err
}

func (s *Service) effectiveGatewayPolicy(ctx context.Context, key APIKeyRecord) (*GovernancePolicy, string, error) {
	policies, err := s.repo.ListGovernancePolicies(ctx)
	if err != nil {
		return nil, "", err
	}
	policy, source := effectiveGatewayPolicyFromPolicies(policies, key)
	return policy, source, nil
}

func (s *Service) gatewayModelAllowed(auth GatewayAuthContext, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	baseModel := model
	if separator := strings.LastIndex(model, ":"); separator > 0 {
		baseModel = model[:separator]
	}
	if auth.Policy != nil {
		if contains(auth.Policy.ModelDenylist, model) || contains(auth.Policy.ModelDenylist, baseModel) {
			return false
		}
		if len(auth.Policy.ModelAllowlist) > 0 {
			if !contains(auth.Policy.ModelAllowlist, model) && !contains(auth.Policy.ModelAllowlist, baseModel) {
				return false
			}
		}
	}
	return contains(auth.APIKey.ModelAllowlist, model) || contains(auth.APIKey.ModelAllowlist, baseModel)
}

func (auth GatewayAuthContext) effectiveQPSLimit() int {
	limit := auth.APIKey.QPSLimit
	if auth.Policy != nil {
		limit = minimumPositive(limit, auth.Policy.QPSLimit)
	}
	return limit
}

func (auth GatewayAuthContext) effectiveMonthlyTokenLimit() int {
	limit := auth.APIKey.MonthlyTokenLimit
	if auth.Policy != nil {
		limit = minimumPositive(limit, auth.Policy.MonthlyTokenLimit)
	}
	return limit
}

func minimumPositive(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

func (auth GatewayAuthContext) effectiveMonthlyBudgetMicros() int64 {
	limit := auth.APIKey.MonthlyBudgetMicros
	if auth.Policy != nil {
		limit = minimumPositiveInt64(limit, auth.Policy.MonthlyBudgetMicros)
	}
	return limit
}

func minimumPositiveInt64(left, right int64) int64 {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

func (auth GatewayAuthContext) shouldBlockOverage() bool {
	if auth.Policy == nil {
		return true
	}
	return auth.Policy.OverageAction == "" || auth.Policy.OverageAction == GovernancePolicyOverageBlock
}

func activePolicyByID(policies []GovernancePolicy, id string) (GovernancePolicy, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return GovernancePolicy{}, false
	}
	for _, policy := range policies {
		if policy.ID == id && policy.Status == GovernancePolicyStatusActive {
			return policy, true
		}
	}
	return GovernancePolicy{}, false
}

func activePolicyByScope(policies []GovernancePolicy, scopeType string, scopeID string) (GovernancePolicy, bool) {
	for _, policy := range policies {
		if policy.Status == GovernancePolicyStatusActive && policy.ScopeType == scopeType && policy.ScopeID == scopeID {
			return policy, true
		}
	}
	return GovernancePolicy{}, false
}

func effectiveGatewayPolicyFromPolicies(policies []GovernancePolicy, key APIKeyRecord) (*GovernancePolicy, string) {
	explanation := explainGatewayPolicy(policies, key)
	if explanation.SelectedPolicyID == "" {
		return nil, ""
	}
	for _, policy := range policies {
		if policy.ID == explanation.SelectedPolicyID {
			selected := policy
			return &selected, explanation.SelectedSource
		}
	}
	return nil, ""
}

func explainGatewayPolicy(policies []GovernancePolicy, key APIKeyRecord) GatewayPolicyExplanation {
	explanation := GatewayPolicyExplanation{
		APIKeyID: key.ID,
	}
	sources := []gatewayPolicyCandidateSource{
		{Source: GatewayPolicySourceAPIKeyExplicit, ExplicitPolicyID: key.PolicyID, MissingReason: "api key explicit policy reference not found"},
		{Source: GatewayPolicySourceAPIKeyScope, ScopeType: GovernancePolicyScopeAPIKey, ScopeID: key.ID},
		{Source: GatewayPolicySourceGlobalScope, ScopeType: GovernancePolicyScopeGlobal, ScopeID: ""},
	}
	for _, source := range sources {
		candidates := gatewayPolicyCandidatesForSource(policies, source)
		if len(candidates) == 0 && strings.TrimSpace(source.ExplicitPolicyID) != "" {
			explanation.Candidates = append(explanation.Candidates, GatewayPolicyCandidate{
				PolicyID: strings.TrimSpace(source.ExplicitPolicyID),
				Source:   source.Source,
				Matched:  false,
				Reason:   source.MissingReason,
			})
			continue
		}
		for _, candidate := range candidates {
			if explanation.SelectedPolicyID == "" && candidate.Status == GovernancePolicyStatusActive {
				candidate.Selected = true
				candidate.Reason = "selected by " + strings.ReplaceAll(source.Source, "_", " ")
				explanation.SelectedPolicyID = candidate.PolicyID
				explanation.SelectedPolicyName = candidate.PolicyName
				explanation.SelectedPolicyVersion = candidate.PolicyVersion
				explanation.SelectedSource = candidate.Source
			} else if candidate.Status != GovernancePolicyStatusActive {
				candidate.Reason = "policy disabled"
			} else if explanation.SelectedSource == candidate.Source {
				candidate.Reason = "matched same source but another policy was selected first"
			} else {
				candidate.Reason = "matched but lower priority than " + strings.ReplaceAll(explanation.SelectedSource, "_", " ")
			}
			explanation.Candidates = append(explanation.Candidates, candidate)
		}
	}
	return explanation
}

type gatewayPolicyCandidateSource struct {
	Source           string
	ExplicitPolicyID string
	ScopeType        string
	ScopeID          string
	MissingReason    string
}

func gatewayPolicyCandidatesForSource(policies []GovernancePolicy, source gatewayPolicyCandidateSource) []GatewayPolicyCandidate {
	out := []GatewayPolicyCandidate{}
	explicitPolicyID := strings.TrimSpace(source.ExplicitPolicyID)
	for _, policy := range policies {
		if explicitPolicyID != "" {
			if policy.ID != explicitPolicyID {
				continue
			}
		} else if policy.ScopeType != source.ScopeType || policy.ScopeID != source.ScopeID {
			continue
		}
		out = append(out, GatewayPolicyCandidate{
			PolicyID:      policy.ID,
			PolicyName:    policy.Name,
			PolicyVersion: governancePolicyVersion(policy),
			Source:        source.Source,
			ScopeType:     policy.ScopeType,
			ScopeID:       policy.ScopeID,
			Status:        policy.Status,
			Matched:       true,
		})
	}
	return out
}

func governancePolicyVersion(policy GovernancePolicy) int {
	if policy.Version <= 0 {
		return 1
	}
	return policy.Version
}
