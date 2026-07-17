package controlplane

import "time"

const (
	GovernancePolicyStatusActive   = "active"
	GovernancePolicyStatusDisabled = "disabled"

	GovernancePolicyScopeGlobal = "global"
	GovernancePolicyScopeAPIKey = "api_key"

	GovernancePolicyOverageBlock    = "block"
	GovernancePolicyOverageWarn     = "warn"
	GovernancePolicyOverageFallback = "fallback"

	GovernancePolicyPromptLoggingDisabled     = "disabled"
	GovernancePolicyPromptLoggingMetadataOnly = "metadata_only"
	GovernancePolicyPromptLoggingRedacted     = "redacted"

	GatewayPolicySourceAPIKeyExplicit          = "api_key_explicit"
	GatewayPolicySourceAPIKeyScope             = "api_key_scope"
	GatewayPolicySourceGlobalScope             = "global_scope"
	GatewayPolicySourceExternalAuthIntegration = "external_auth_integration"
)

type GovernancePolicy struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Version             int       `json:"version"`
	LastUpdatedBy       string    `json:"last_updated_by"`
	ScopeType           string    `json:"scope_type"`
	ScopeID             string    `json:"scope_id"`
	ModelAllowlist      []string  `json:"model_allowlist"`
	ModelDenylist       []string  `json:"model_denylist"`
	QPSLimit            int       `json:"qps_limit"`
	MonthlyTokenLimit   int       `json:"monthly_token_limit"`
	MonthlyBudgetMicros int64     `json:"monthly_budget_micros"`
	OverageAction       string    `json:"overage_action"`
	PromptLoggingMode   string    `json:"prompt_logging_mode"`
	RetentionDays       int       `json:"retention_days"`
	ToolCallAllowed     bool      `json:"tool_call_allowed"`
	ImageInputAllowed   bool      `json:"image_input_allowed"`
	WebAccessAllowed    bool      `json:"web_access_allowed"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type GatewayPolicyExplanation struct {
	APIKeyID              string                   `json:"api_key_id"`
	SelectedPolicyID      string                   `json:"selected_policy_id"`
	SelectedPolicyName    string                   `json:"selected_policy_name"`
	SelectedPolicyVersion int                      `json:"selected_policy_version"`
	SelectedSource        string                   `json:"selected_source"`
	Candidates            []GatewayPolicyCandidate `json:"candidates"`
}

type GatewayPolicyCandidate struct {
	PolicyID      string `json:"policy_id"`
	PolicyName    string `json:"policy_name"`
	PolicyVersion int    `json:"policy_version"`
	Source        string `json:"source"`
	ScopeType     string `json:"scope_type"`
	ScopeID       string `json:"scope_id"`
	Status        string `json:"status"`
	Matched       bool   `json:"matched"`
	Selected      bool   `json:"selected"`
	Reason        string `json:"reason"`
}

type GovernancePolicyRequest struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	ScopeType           string   `json:"scope_type"`
	ScopeID             string   `json:"scope_id"`
	ModelAllowlist      []string `json:"model_allowlist"`
	ModelDenylist       []string `json:"model_denylist"`
	QPSLimit            int      `json:"qps_limit"`
	MonthlyTokenLimit   int      `json:"monthly_token_limit"`
	MonthlyBudgetMicros int64    `json:"monthly_budget_micros"`
	OverageAction       string   `json:"overage_action"`
	PromptLoggingMode   string   `json:"prompt_logging_mode"`
	RetentionDays       int      `json:"retention_days"`
	ToolCallAllowed     bool     `json:"tool_call_allowed"`
	ImageInputAllowed   bool     `json:"image_input_allowed"`
	WebAccessAllowed    bool     `json:"web_access_allowed"`
	Status              string   `json:"status"`
}
