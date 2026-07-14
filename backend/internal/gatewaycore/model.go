package gatewaycore

import "encoding/json"

type Protocol string

const (
	ProtocolOpenAIModels      Protocol = "openai_models"
	ProtocolOpenAIChat        Protocol = "openai_chat_completions"
	ProtocolOpenAIResponses   Protocol = "openai_responses"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
	ProtocolGeminiGenerate    Protocol = "gemini_generate_content"
	ProtocolRealtime          Protocol = "realtime"
)

type Lane string

const (
	LaneDirect  Lane = "direct"
	LaneDurable Lane = "durable"
)

type CredentialSource string

const (
	CredentialSourceAPIKey      CredentialSource = "api_key"
	CredentialSourceHMACContext CredentialSource = "hmac_context"
	CredentialSourceJWTJWKS     CredentialSource = "jwt_jwks"
)

type CredentialEnvelope struct {
	BearerToken   string `json:"-"`
	SignedContext string `json:"-"`
	Transport     string `json:"transport"`
}

type CanonicalRequest struct {
	ID              string          `json:"id"`
	ClientRequestID string          `json:"client_request_id"`
	Fingerprint     string          `json:"fingerprint"`
	Protocol        Protocol        `json:"protocol"`
	Operation       string          `json:"operation"`
	Modality        string          `json:"modality"`
	Lane            Lane            `json:"lane"`
	Model           string          `json:"model"`
	Stream          bool            `json:"stream"`
	MessageCount    int             `json:"message_count"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	StickyKey       string          `json:"sticky_key,omitempty"`
	Payload         json.RawMessage `json:"-"`
}

type CanonicalLimits struct {
	QPSLimit           int `json:"qps_limit"`
	RPMLimit           int `json:"rpm_limit"`
	TPMLimit           int `json:"tpm_limit"`
	ConcurrencyLimit   int `json:"concurrency_limit"`
	MonthlyTokenLimit  int `json:"monthly_token_limit"`
	MonthlyBudgetCents int `json:"monthly_budget_cents"`
}

type CanonicalAuthContext struct {
	CredentialSource         CredentialSource `json:"credential_source"`
	CredentialID             string           `json:"credential_id"`
	CredentialFingerprint    string           `json:"credential_fingerprint"`
	IntegrationID            string           `json:"integration_id,omitempty"`
	ProfileScope             string           `json:"profile_scope"`
	TenantID                 string           `json:"tenant_id,omitempty"`
	PrincipalType            string           `json:"principal_type"`
	PrincipalID              string           `json:"principal_id"`
	ExternalSubjectReference string           `json:"external_subject_reference,omitempty"`
	PolicyID                 string           `json:"policy_id,omitempty"`
	PolicyVersion            int              `json:"policy_version,omitempty"`
	AllowedModels            []string         `json:"allowed_models"`
	Limits                   CanonicalLimits  `json:"limits"`
	LanePolicy               string           `json:"lane_policy"`
	ArtifactPolicy           string           `json:"artifact_policy"`
}
