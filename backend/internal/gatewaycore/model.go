package gatewaycore

import "encoding/json"

type Protocol string

const (
	ProtocolOpenAIModels              Protocol = "openai_models"
	ProtocolOpenAIChat                Protocol = "openai_chat_completions"
	ProtocolOpenAIImages              Protocol = "openai_images_generations"
	ProtocolOpenAIMedia               Protocol = "openai_media_generations"
	ProtocolOpenAIAudioTranscriptions Protocol = "openai_audio_transcriptions"
	ProtocolOpenAIAudioTranslations   Protocol = "openai_audio_translations"
	ProtocolOpenAIAudioSpeech         Protocol = "openai_audio_speech"
	ProtocolOpenAIResponses           Protocol = "openai_responses"
	ProtocolAnthropicMessages         Protocol = "anthropic_messages"
	ProtocolGeminiGenerate            Protocol = "gemini_generate_content"
	ProtocolRealtime                  Protocol = "realtime"
	ProtocolAsterJobs                 Protocol = "aster_jobs"
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
	ID                   string                  `json:"id"`
	ClientRequestID      string                  `json:"client_request_id"`
	Fingerprint          string                  `json:"fingerprint"`
	Protocol             Protocol                `json:"protocol"`
	Operation            string                  `json:"operation"`
	Modality             string                  `json:"modality"`
	Lane                 Lane                    `json:"lane"`
	Model                string                  `json:"model"`
	Stream               bool                    `json:"stream"`
	MessageCount         int                     `json:"message_count"`
	IdempotencyKey       string                  `json:"idempotency_key,omitempty"`
	StickyKey            string                  `json:"sticky_key,omitempty"`
	ResponseMode         string                  `json:"response_mode,omitempty"`
	PreviewMode          string                  `json:"preview_mode,omitempty"`
	DeliveryMode         string                  `json:"delivery_mode,omitempty"`
	OutputCount          int                     `json:"output_count,omitempty"`
	VideoDurationMS      int64                   `json:"video_duration_ms,omitempty"`
	AudioDurationMS      int64                   `json:"audio_duration_ms,omitempty"`
	InputAudioDurationMS int64                   `json:"input_audio_duration_ms,omitempty"`
	InputCharacters      int64                   `json:"input_characters,omitempty"`
	SourceIP             string                  `json:"-"`
	Payload              json.RawMessage         `json:"-"`
	TransportBody        []byte                  `json:"-"`
	TransportContentType string                  `json:"-"`
	InputArtifact        *CanonicalInputArtifact `json:"-"`
}

// CanonicalInputArtifact carries bounded request content only for the lifetime
// of the HTTP request. Persistent operation records contain its digest and
// stable request fields through Fingerprint, never the raw bytes.
type CanonicalInputArtifact struct {
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Content   []byte `json:"-"`
}

type CanonicalLimits struct {
	QPSLimit                 int   `json:"qps_limit"`
	RPMLimit                 int   `json:"rpm_limit"`
	TPMLimit                 int   `json:"tpm_limit"`
	ConcurrencyLimit         int   `json:"concurrency_limit"`
	MonthlyTokenLimit        int   `json:"monthly_token_limit"`
	MonthlyBudgetMicros      int64 `json:"monthly_budget_micros"`
	MonthlyImageLimit        int   `json:"monthly_image_limit"`
	MonthlyVideoSecondsLimit int   `json:"monthly_video_seconds_limit"`
	MonthlyAudioSecondsLimit int   `json:"monthly_audio_seconds_limit"`
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
	Scopes                   []string         `json:"scopes"`
	AllowedModels            []string         `json:"allowed_models"`
	AllowedModalities        []string         `json:"allowed_modalities"`
	AllowedOperations        []string         `json:"allowed_operations"`
	AllowedCIDRs             []string         `json:"allowed_cidrs"`
	Limits                   CanonicalLimits  `json:"limits"`
	LanePolicy               string           `json:"lane_policy"`
	ArtifactPolicy           string           `json:"artifact_policy"`
	ArtifactSinkID           string           `json:"artifact_sink_id,omitempty"`
}
