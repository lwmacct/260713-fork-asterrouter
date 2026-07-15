package controlplane

import "time"

const (
	ProviderStatusActive      = "active"
	ProviderStatusDisabled    = "disabled"
	ProviderStatusNeedsSecret = "needs_secret"

	APIKeyStatusActive   = "active"
	APIKeyStatusDisabled = "disabled"

	APIKeyLifecycleActive   = "active"
	APIKeyLifecycleRetiring = "retiring"
	APIKeyLifecycleRetired  = "retired"
	APIKeyLifecycleDisabled = "disabled"

	APIKeyTypeWorkspace = "workspace"
	APIKeyTypeUser      = "user"
	APIKeyTypeCustomer  = "customer"
	APIKeyTypeService   = "service"

	GatewayScopeInvoke          = "gateway:invoke"
	GatewayScopeModelsRead      = "models:read"
	GatewayScopeJobsRead        = "jobs:read"
	GatewayScopeJobsCancel      = "jobs:cancel"
	GatewayScopeJobsActions     = "jobs:actions"
	GatewayScopeArtifactsRead   = "artifacts:read"
	GatewayScopeArtifactsDelete = "artifacts:delete"

	GatewayOperationListModels      = "list_models"
	GatewayOperationChatCompletion  = "chat_completion"
	GatewayOperationImageGeneration = "image_generation"
	GatewayOperationVideoGeneration = "video_generation"
	GatewayOperationAudioGeneration = "audio_generation"

	GatewayModalityMetadata = "metadata"
	GatewayModalityText     = "text"
	GatewayModalityImage    = "image"
	GatewayModalityVideo    = "video"
	GatewayModalityAudio    = "audio"

	GatewayLanePolicyDirectOnly       = "direct_only"
	GatewayLanePolicyDurableOnly      = "durable_only"
	GatewayLanePolicyDirectAndDurable = "direct_and_durable"

	GatewayArtifactPolicyProxyOnly    = "proxy_only"
	GatewayArtifactPolicyTemporary    = "temporary"
	GatewayArtifactPolicyManaged      = "managed"
	GatewayArtifactPolicyCustomerSink = "customer_sink"
	GatewayArtifactPolicyMetadataOnly = "metadata_only"

	ProfileScopePlatform = "platform"

	PlatformTenantStatusActive   = "active"
	PlatformTenantStatusDisabled = "disabled"

	GatewayPrincipalStatusActive   = "active"
	GatewayPrincipalStatusDisabled = "disabled"

	GatewayPrincipalTypeService     = "service"
	GatewayPrincipalTypeDeveloper   = "developer"
	GatewayPrincipalTypeIntegration = "integration"

	ExternalAuthIntegrationStatusActive   = "active"
	ExternalAuthIntegrationStatusDisabled = "disabled"
	ExternalAuthIntegrationProtocolHMAC   = "hmac_signed_context"
	ExternalAuthIntegrationProtocolJWT    = "jwt_jwks"

	PlatformUsageSinkStatusActive   = "active"
	PlatformUsageSinkStatusDisabled = "disabled"

	PlatformUsageDeliveryStatusPending    = "pending"
	PlatformUsageDeliveryStatusDelivering = "delivering"
	PlatformUsageDeliveryStatusDelivered  = "delivered"
	PlatformUsageDeliveryStatusDeadLetter = "dead_letter"

	AccountStatusActive   = "active"
	AccountStatusError    = "error"
	AccountStatusDisabled = "disabled"

	CircuitStateClosed   = "closed"
	CircuitStateOpen     = "open"
	CircuitStateHalfOpen = "half_open"

	RoutingGroupStatusActive   = "active"
	RoutingGroupStatusDisabled = "disabled"

	RoutingGroupTypeStandard        = "standard"
	RoutingGroupTypeSubscription    = "subscription"
	RoutingGroupTypeExclusive       = "exclusive"
	RoutingGroupTypeImageGeneration = "image_generation"
	RoutingGroupTypeVideoGeneration = "video_generation"

	ModelPricingStatusActive   = "active"
	ModelPricingStatusDisabled = "disabled"
)

type ProviderConnection struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	BaseURL          string    `json:"base_url"`
	Status           string    `json:"status"`
	Models           []string  `json:"models"`
	Priority         int       `json:"priority"`
	SecretConfigured bool      `json:"secret_configured"`
	SecretHint       string    `json:"secret_hint"`
	SecretCiphertext string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProviderHealthCheck struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Status     string    `json:"status"`
	LatencyMS  int64     `json:"latency_ms"`
	Message    string    `json:"message"`
	Models     []string  `json:"models"`
	CheckedAt  time.Time `json:"checked_at"`
}

type ProviderRequest struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	BaseURL  string   `json:"base_url"`
	Status   string   `json:"status"`
	Models   []string `json:"models"`
	Priority int      `json:"priority"`
	APIKey   string   `json:"api_key"`
}

type RoutingGroup struct {
	ID                           string    `json:"id"`
	Name                         string    `json:"name"`
	Description                  string    `json:"description"`
	Platform                     string    `json:"platform"`
	GroupType                    string    `json:"group_type"`
	RateMultiplier               float64   `json:"rate_multiplier"`
	RPMLimit                     int       `json:"rpm_limit"`
	IsExclusive                  bool      `json:"is_exclusive"`
	DailyBudgetCents             int       `json:"daily_budget_cents"`
	WeeklyBudgetCents            int       `json:"weekly_budget_cents"`
	MonthlyBudgetCents           int       `json:"monthly_budget_cents"`
	ImageEnabled                 bool      `json:"image_enabled"`
	BatchImageEnabled            bool      `json:"batch_image_enabled"`
	ImageRateMultiplier          float64   `json:"image_rate_multiplier"`
	BatchImageDiscountMultiplier float64   `json:"batch_image_discount_multiplier"`
	ImagePrice1KCents            int       `json:"image_price_1k_cents"`
	ImagePrice2KCents            int       `json:"image_price_2k_cents"`
	ImagePrice4KCents            int       `json:"image_price_4k_cents"`
	VideoEnabled                 bool      `json:"video_enabled"`
	VideoRateMultiplier          float64   `json:"video_rate_multiplier"`
	VideoPrice480PCents          int       `json:"video_price_480p_cents"`
	VideoPrice720PCents          int       `json:"video_price_720p_cents"`
	VideoPrice1080PCents         int       `json:"video_price_1080p_cents"`
	PeakRateEnabled              bool      `json:"peak_rate_enabled"`
	PeakStart                    string    `json:"peak_start"`
	PeakEnd                      string    `json:"peak_end"`
	PeakRateMultiplier           float64   `json:"peak_rate_multiplier"`
	Status                       string    `json:"status"`
	SortOrder                    int       `json:"sort_order"`
	AccountCount                 int       `json:"account_count"`
	ActiveAccounts               int       `json:"active_account_count"`
	CreatedAt                    time.Time `json:"created_at"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

type RoutingGroupRequest struct {
	Name                         string  `json:"name"`
	Description                  string  `json:"description"`
	Platform                     string  `json:"platform"`
	GroupType                    string  `json:"group_type"`
	RateMultiplier               float64 `json:"rate_multiplier"`
	RPMLimit                     int     `json:"rpm_limit"`
	IsExclusive                  bool    `json:"is_exclusive"`
	DailyBudgetCents             int     `json:"daily_budget_cents"`
	WeeklyBudgetCents            int     `json:"weekly_budget_cents"`
	MonthlyBudgetCents           int     `json:"monthly_budget_cents"`
	ImageEnabled                 bool    `json:"image_enabled"`
	BatchImageEnabled            bool    `json:"batch_image_enabled"`
	ImageRateMultiplier          float64 `json:"image_rate_multiplier"`
	BatchImageDiscountMultiplier float64 `json:"batch_image_discount_multiplier"`
	ImagePrice1KCents            int     `json:"image_price_1k_cents"`
	ImagePrice2KCents            int     `json:"image_price_2k_cents"`
	ImagePrice4KCents            int     `json:"image_price_4k_cents"`
	VideoEnabled                 bool    `json:"video_enabled"`
	VideoRateMultiplier          float64 `json:"video_rate_multiplier"`
	VideoPrice480PCents          int     `json:"video_price_480p_cents"`
	VideoPrice720PCents          int     `json:"video_price_720p_cents"`
	VideoPrice1080PCents         int     `json:"video_price_1080p_cents"`
	PeakRateEnabled              bool    `json:"peak_rate_enabled"`
	PeakStart                    string  `json:"peak_start"`
	PeakEnd                      string  `json:"peak_end"`
	PeakRateMultiplier           float64 `json:"peak_rate_multiplier"`
	Status                       string  `json:"status"`
	SortOrder                    int     `json:"sort_order"`
}

type ProviderAccount struct {
	ID                      string                                 `json:"id"`
	ProviderID              string                                 `json:"provider_id"`
	Name                    string                                 `json:"name"`
	Platform                string                                 `json:"platform"`
	AuthType                string                                 `json:"auth_type"`
	Status                  string                                 `json:"status"`
	Schedulable             bool                                   `json:"schedulable"`
	Priority                int                                    `json:"priority"`
	Weight                  int                                    `json:"weight"`
	Concurrency             int                                    `json:"concurrency"`
	RPMLimit                int                                    `json:"rpm_limit"`
	TPMLimit                int                                    `json:"tpm_limit"`
	LoadFactor              *int                                   `json:"load_factor,omitempty"`
	RateMultiplier          float64                                `json:"rate_multiplier"`
	Models                  []string                               `json:"models"`
	GroupIDs                []string                               `json:"group_ids"`
	SecretConfigured        bool                                   `json:"secret_configured"`
	SecretHint              string                                 `json:"secret_hint"`
	SecretCiphertext        string                                 `json:"-"`
	ErrorMessage            string                                 `json:"error_message"`
	LastUsedAt              *time.Time                             `json:"last_used_at,omitempty"`
	ExpiresAt               *time.Time                             `json:"expires_at,omitempty"`
	CooldownUntil           *time.Time                             `json:"cooldown_until,omitempty"`
	CircuitState            string                                 `json:"circuit_state"`
	CircuitFailureThreshold int                                    `json:"circuit_failure_threshold"`
	CircuitOpenSeconds      int                                    `json:"circuit_open_seconds"`
	ConsecutiveFailures     int                                    `json:"consecutive_failures"`
	CircuitOpenedUntil      *time.Time                             `json:"circuit_opened_until,omitempty"`
	LastFailureAt           *time.Time                             `json:"last_failure_at,omitempty"`
	TempUnschedulableRules  []ProviderAccountTempUnschedulableRule `json:"temp_unschedulable_rules"`
	TempUnschedulableReason string                                 `json:"temp_unschedulable_reason"`
	CreatedAt               time.Time                              `json:"created_at"`
	UpdatedAt               time.Time                              `json:"updated_at"`
}

// ProviderAccountTempUnschedulableRule lets an admin configure a duration to
// cool an account down for when an upstream response matches a specific HTTP
// status code and contains any of a set of keywords (e.g. "insufficient
// balance", "key revoked"). This gives a more accurate cooldown than the
// fixed default applied by RecordProviderAccountFailure for unmatched
// failures.
type ProviderAccountTempUnschedulableRule struct {
	StatusCode      int      `json:"status_code"`
	Keywords        []string `json:"keywords"`
	DurationMinutes int      `json:"duration_minutes"`
}

// EffectiveLoadFactor returns the denominator used to compute an account's
// scheduling load ratio: LoadFactor when explicitly set to a positive value,
// otherwise Concurrency (floored at 1 to avoid division by zero).
func (a ProviderAccount) EffectiveLoadFactor() int {
	if a.LoadFactor != nil && *a.LoadFactor > 0 {
		return *a.LoadFactor
	}
	if a.Concurrency > 0 {
		return a.Concurrency
	}
	return 1
}

type ProviderAccountRequest struct {
	ProviderID              string                                 `json:"provider_id"`
	Name                    string                                 `json:"name"`
	Platform                string                                 `json:"platform"`
	AuthType                string                                 `json:"auth_type"`
	Status                  string                                 `json:"status"`
	Schedulable             *bool                                  `json:"schedulable"`
	Priority                int                                    `json:"priority"`
	Weight                  int                                    `json:"weight"`
	Concurrency             int                                    `json:"concurrency"`
	RPMLimit                int                                    `json:"rpm_limit"`
	TPMLimit                int                                    `json:"tpm_limit"`
	LoadFactor              *int                                   `json:"load_factor"`
	RateMultiplier          float64                                `json:"rate_multiplier"`
	Models                  []string                               `json:"models"`
	GroupIDs                []string                               `json:"group_ids"`
	Secret                  string                                 `json:"secret"`
	ExpiresAt               string                                 `json:"expires_at"`
	CircuitFailureThreshold int                                    `json:"circuit_failure_threshold"`
	CircuitOpenSeconds      int                                    `json:"circuit_open_seconds"`
	TempUnschedulableRules  []ProviderAccountTempUnschedulableRule `json:"temp_unschedulable_rules"`
}

type ProviderAccountHealthCheck struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	ProviderID string    `json:"provider_id"`
	Status     string    `json:"status"`
	LatencyMS  int64     `json:"latency_ms"`
	Message    string    `json:"message"`
	Models     []string  `json:"models"`
	CheckedAt  time.Time `json:"checked_at"`
}

type ModelPricing struct {
	ID                          string    `json:"id"`
	Model                       string    `json:"model"`
	Currency                    string    `json:"currency"`
	InputPriceCentsPer1MTokens  int       `json:"input_price_cents_per_1m_tokens"`
	OutputPriceCentsPer1MTokens int       `json:"output_price_cents_per_1m_tokens"`
	Status                      string    `json:"status"`
	CreatedAt                   time.Time `json:"created_at"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type ModelPricingRequest struct {
	Model                       string `json:"model"`
	Currency                    string `json:"currency"`
	InputPriceCentsPer1MTokens  int    `json:"input_price_cents_per_1m_tokens"`
	OutputPriceCentsPer1MTokens int    `json:"output_price_cents_per_1m_tokens"`
	Status                      string `json:"status"`
}

type APIKeyRecord struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	KeyHash                  string     `json:"-"`
	Fingerprint              string     `json:"fingerprint"`
	Prefix                   string     `json:"prefix"`
	Status                   string     `json:"status"`
	KeyType                  string     `json:"key_type"`
	CustomerID               string     `json:"customer_id"`
	OwnerUserID              string     `json:"owner_user_id"`
	ProfileScope             string     `json:"profile_scope"`
	PlatformTenantID         string     `json:"platform_tenant_id"`
	GatewayPrincipalID       string     `json:"gateway_principal_id"`
	TenantID                 string     `json:"tenant_id"`
	PrincipalType            string     `json:"principal_type"`
	PrincipalReference       string     `json:"principal_reference"`
	PolicyID                 string     `json:"policy_id"`
	Scopes                   []string   `json:"scopes"`
	ModelAllowlist           []string   `json:"model_allowlist"`
	AllowedModalities        []string   `json:"allowed_modalities"`
	AllowedOperations        []string   `json:"allowed_operations"`
	QPSLimit                 int        `json:"qps_limit"`
	RPMLimit                 int        `json:"rpm_limit"`
	TPMLimit                 int        `json:"tpm_limit"`
	ConcurrencyLimit         int        `json:"concurrency_limit"`
	MonthlyTokenLimit        int        `json:"monthly_token_limit"`
	MonthlyBudgetCents       int        `json:"monthly_budget_cents"`
	MonthlyImageLimit        int        `json:"monthly_image_limit"`
	MonthlyVideoSecondsLimit int        `json:"monthly_video_seconds_limit"`
	MonthlyAudioSecondsLimit int        `json:"monthly_audio_seconds_limit"`
	AllowedCIDRs             []string   `json:"allowed_cidrs"`
	LanePolicy               string     `json:"lane_policy"`
	ArtifactPolicy           string     `json:"artifact_policy"`
	ArtifactSinkID           string     `json:"artifact_sink_id,omitempty"`
	RotationFamilyID         string     `json:"rotation_family_id"`
	ReplacesKeyID            string     `json:"replaces_key_id"`
	ReplacedByKeyID          string     `json:"replaced_by_key_id"`
	RotationGraceExpiresAt   *time.Time `json:"rotation_grace_expires_at,omitempty"`
	LifecycleStatus          string     `json:"lifecycle_status"`
	ExpiresAt                *time.Time `json:"expires_at,omitempty"`
	LastUsedAt               *time.Time `json:"last_used_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type APIKeyCreateRequest struct {
	Name                     string   `json:"name"`
	PolicyID                 string   `json:"policy_id"`
	Scopes                   []string `json:"scopes"`
	ModelAllowlist           []string `json:"model_allowlist"`
	AllowedModalities        []string `json:"allowed_modalities"`
	AllowedOperations        []string `json:"allowed_operations"`
	QPSLimit                 int      `json:"qps_limit"`
	RPMLimit                 int      `json:"rpm_limit"`
	TPMLimit                 int      `json:"tpm_limit"`
	ConcurrencyLimit         int      `json:"concurrency_limit"`
	MonthlyTokenLimit        int      `json:"monthly_token_limit"`
	MonthlyBudgetCents       int      `json:"monthly_budget_cents"`
	MonthlyImageLimit        int      `json:"monthly_image_limit"`
	MonthlyVideoSecondsLimit int      `json:"monthly_video_seconds_limit"`
	MonthlyAudioSecondsLimit int      `json:"monthly_audio_seconds_limit"`
	AllowedCIDRs             []string `json:"allowed_cidrs"`
	LanePolicy               string   `json:"lane_policy"`
	ArtifactPolicy           string   `json:"artifact_policy"`
	ArtifactSinkID           string   `json:"artifact_sink_id"`
	ExpiresAt                string   `json:"expires_at"`
	KeyType                  string   `json:"key_type"`
	CustomerID               string   `json:"customer_id"`
	OwnerUserID              string   `json:"owner_user_id"`
	PlatformTenantID         string   `json:"platform_tenant_id"`
	GatewayPrincipalID       string   `json:"gateway_principal_id"`
}

type APIKeyUpdateRequest struct {
	Name                     string   `json:"name"`
	PolicyID                 string   `json:"policy_id"`
	Scopes                   []string `json:"scopes"`
	ModelAllowlist           []string `json:"model_allowlist"`
	AllowedModalities        []string `json:"allowed_modalities"`
	AllowedOperations        []string `json:"allowed_operations"`
	QPSLimit                 int      `json:"qps_limit"`
	RPMLimit                 int      `json:"rpm_limit"`
	TPMLimit                 int      `json:"tpm_limit"`
	ConcurrencyLimit         int      `json:"concurrency_limit"`
	MonthlyTokenLimit        int      `json:"monthly_token_limit"`
	MonthlyBudgetCents       int      `json:"monthly_budget_cents"`
	MonthlyImageLimit        int      `json:"monthly_image_limit"`
	MonthlyVideoSecondsLimit int      `json:"monthly_video_seconds_limit"`
	MonthlyAudioSecondsLimit int      `json:"monthly_audio_seconds_limit"`
	AllowedCIDRs             []string `json:"allowed_cidrs"`
	LanePolicy               string   `json:"lane_policy"`
	ArtifactPolicy           string   `json:"artifact_policy"`
	ArtifactSinkID           string   `json:"artifact_sink_id"`
	ExpiresAt                string   `json:"expires_at"`
	Status                   string   `json:"status"`
	KeyType                  string   `json:"key_type"`
	CustomerID               string   `json:"customer_id"`
	OwnerUserID              string   `json:"owner_user_id"`
	PlatformTenantID         string   `json:"platform_tenant_id"`
	GatewayPrincipalID       string   `json:"gateway_principal_id"`
}

type APIKeyRotateRequest struct {
	GracePeriodSeconds int `json:"grace_period_seconds"`
}

// PlatformTenant owns the gateway relationship with a product, partner, or
// API customer. It intentionally stores only an opaque entitlement reference;
// the connected product remains the source of truth for users and orders.
type PlatformTenant struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Slug                 string    `json:"slug"`
	EntitlementReference string    `json:"entitlement_reference"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type PlatformTenantRequest struct {
	Name                 string `json:"name"`
	Slug                 string `json:"slug"`
	EntitlementReference string `json:"entitlement_reference"`
	Status               string `json:"status"`
}

// GatewayPrincipal is a non-human caller identity inside a Platform Tenant.
// It is not an AsterRouter workspace user and never creates an external login
// account, session, subscription, or balance.
type GatewayPrincipal struct {
	ID                       string    `json:"id"`
	TenantID                 string    `json:"tenant_id"`
	Name                     string    `json:"name"`
	PrincipalType            string    `json:"principal_type"`
	ExternalSubjectReference string    `json:"external_subject_reference"`
	Status                   string    `json:"status"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type GatewayPrincipalRequest struct {
	TenantID                 string `json:"tenant_id"`
	Name                     string `json:"name"`
	PrincipalType            string `json:"principal_type"`
	ExternalSubjectReference string `json:"external_subject_reference"`
	Status                   string `json:"status"`
}

// ExternalAuthIntegration defines how a connected product delegates a
// bounded AI-access decision to AsterRouter. It contains no external user
// session, refresh token, subscription, or customer profile.
type ExternalAuthIntegration struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	GatewayPrincipalID string    `json:"gateway_principal_id"`
	Name               string    `json:"name"`
	Protocol           string    `json:"protocol"`
	KeyID              string    `json:"key_id"`
	SecretConfigured   bool      `json:"secret_configured"`
	SecretHint         string    `json:"secret_hint"`
	SecretCiphertext   string    `json:"-"`
	Issuer             string    `json:"issuer"`
	JWKSURL            string    `json:"jwks_url"`
	SubjectClaim       string    `json:"subject_claim"`
	ModelsClaim        string    `json:"models_claim"`
	QPSLimitClaim      string    `json:"qps_limit_claim"`
	MonthlyTokenClaim  string    `json:"monthly_token_limit_claim"`
	Audience           string    `json:"audience"`
	PolicyID           string    `json:"policy_id"`
	ModelAllowlist     []string  `json:"model_allowlist"`
	QPSLimit           int       `json:"qps_limit"`
	MonthlyTokenLimit  int       `json:"monthly_token_limit"`
	MaxTTLSeconds      int       `json:"max_ttl_seconds"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ExternalAuthIntegrationRequest struct {
	TenantID           string   `json:"tenant_id"`
	GatewayPrincipalID string   `json:"gateway_principal_id"`
	Name               string   `json:"name"`
	Protocol           string   `json:"protocol"`
	KeyID              string   `json:"key_id"`
	Secret             string   `json:"secret"`
	Issuer             string   `json:"issuer"`
	JWKSURL            string   `json:"jwks_url"`
	SubjectClaim       string   `json:"subject_claim"`
	ModelsClaim        string   `json:"models_claim"`
	QPSLimitClaim      string   `json:"qps_limit_claim"`
	MonthlyTokenClaim  string   `json:"monthly_token_limit_claim"`
	Audience           string   `json:"audience"`
	PolicyID           string   `json:"policy_id"`
	ModelAllowlist     []string `json:"model_allowlist"`
	QPSLimit           int      `json:"qps_limit"`
	MonthlyTokenLimit  int      `json:"monthly_token_limit"`
	MaxTTLSeconds      int      `json:"max_ttl_seconds"`
	Status             string   `json:"status"`
}

// ExternalAuthIntegrationCreateResponse returns a shared HMAC secret only at
// creation or rotation. JWT/JWKS integrations never receive a secret. The
// record never serializes its ciphertext.
type ExternalAuthIntegrationCreateResponse struct {
	Record ExternalAuthIntegration `json:"record"`
	Secret string                  `json:"secret"`
}

// PlatformUsageSink is a Platform-controlled destination for signed,
// metering-only usage events. It is bound to one External Auth Integration;
// it does not carry a product user, login token, subscription, or balance.
type PlatformUsageSink struct {
	ID                        string    `json:"id"`
	TenantID                  string    `json:"tenant_id"`
	ExternalAuthIntegrationID string    `json:"external_auth_integration_id"`
	Name                      string    `json:"name"`
	EndpointURLCiphertext     string    `json:"-"`
	EndpointURLHint           string    `json:"endpoint_url_hint"`
	SigningSecretCiphertext   string    `json:"-"`
	SigningSecretHint         string    `json:"signing_secret_hint"`
	Status                    string    `json:"status"`
	MaxAttempts               int       `json:"max_attempts"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type PlatformUsageSinkRequest struct {
	TenantID                  string `json:"tenant_id"`
	ExternalAuthIntegrationID string `json:"external_auth_integration_id"`
	Name                      string `json:"name"`
	EndpointURL               string `json:"endpoint_url"`
	SigningSecret             string `json:"signing_secret"`
	Status                    string `json:"status"`
	MaxAttempts               int    `json:"max_attempts"`
}

type PlatformUsageSinkCreateResponse struct {
	Record        PlatformUsageSink `json:"record"`
	SigningSecret string            `json:"signing_secret"`
}

// PlatformUsageDeliveryEvent is a durable, immutable usage snapshot. Payload
// stays internal so delivery history cannot be used to read metering details
// outside the authorized Platform observability surface.
type PlatformUsageDeliveryEvent struct {
	ID             string     `json:"id"`
	SinkID         string     `json:"sink_id"`
	UsageRecordID  string     `json:"usage_record_id"`
	EventID        string     `json:"event_id"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	MaxAttempts    int        `json:"max_attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	LastHTTPStatus int        `json:"last_http_status"`
	LastError      string     `json:"last_error,omitempty"`
	TargetHint     string     `json:"target_hint"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	PayloadJSON string `json:"-"`
	LeaseToken  string `json:"-"`
}

type PlatformUsageDeliveryQuery struct {
	SinkID     string
	DeliveryID string
	Status     string
	Limit      int
	Offset     int
}

type APIKeyCreateResponse struct {
	Record APIKeyRecord `json:"record"`
	Key    string       `json:"key"`
}

type AuditLog struct {
	ID                        string    `json:"id"`
	Actor                     string    `json:"actor"`
	Action                    string    `json:"action"`
	ResourceType              string    `json:"resource_type"`
	ResourceID                string    `json:"resource_id"`
	Summary                   string    `json:"summary"`
	ProfileScope              string    `json:"profile_scope"`
	PlatformTenantID          string    `json:"platform_tenant_id"`
	PlatformTenantName        string    `json:"platform_tenant_name"`
	GatewayPrincipalID        string    `json:"gateway_principal_id"`
	GatewayPrincipalName      string    `json:"gateway_principal_name"`
	ExternalAuthIntegrationID string    `json:"external_auth_integration_id"`
	ExternalSubjectReference  string    `json:"external_subject_reference"`
	CreatedAt                 time.Time `json:"created_at"`
}

type Dashboard struct {
	ProviderCount       int        `json:"provider_count"`
	ActiveProviderCount int        `json:"active_provider_count"`
	APIKeyCount         int        `json:"api_key_count"`
	ActiveAPIKeyCount   int        `json:"active_api_key_count"`
	Models              []string   `json:"models"`
	RecentAudit         []AuditLog `json:"recent_audit"`
}

type UsageRecord struct {
	ID                        string          `json:"id"`
	OperationID               string          `json:"operation_id"`
	AttemptID                 string          `json:"attempt_id"`
	UsageVersion              int             `json:"usage_version"`
	UsageSource               string          `json:"usage_source"`
	RequestFingerprint        string          `json:"request_fingerprint"`
	APIKeyID                  string          `json:"api_key_id"`
	CustomerID                string          `json:"customer_id"`
	ProfileScope              string          `json:"profile_scope"`
	PlatformTenantID          string          `json:"platform_tenant_id"`
	PlatformTenantName        string          `json:"platform_tenant_name"`
	GatewayPrincipalID        string          `json:"gateway_principal_id"`
	GatewayPrincipalName      string          `json:"gateway_principal_name"`
	ExternalAuthIntegrationID string          `json:"external_auth_integration_id"`
	ExternalSubjectReference  string          `json:"external_subject_reference"`
	APIFingerprint            string          `json:"api_fingerprint"`
	Model                     string          `json:"model"`
	UpstreamModel             string          `json:"upstream_model"`
	Protocol                  string          `json:"protocol"`
	ProviderID                string          `json:"provider_id"`
	ProviderAccountID         string          `json:"provider_account_id"`
	Status                    string          `json:"status"`
	ErrorType                 string          `json:"error_type"`
	LatencyMS                 int64           `json:"latency_ms"`
	TTFTMS                    *int64          `json:"ttft_ms,omitempty"`
	InputTokens               int             `json:"input_tokens"`
	OutputTokens              int             `json:"output_tokens"`
	TotalInputTokens          *int            `json:"total_input_tokens,omitempty"`
	UncachedInputTokens       *int            `json:"uncached_input_tokens,omitempty"`
	CacheReadTokens           *int            `json:"cache_read_tokens,omitempty"`
	CacheWrite5mTokens        *int            `json:"cache_write_5m_tokens,omitempty"`
	CacheWrite1hTokens        *int            `json:"cache_write_1h_tokens,omitempty"`
	CacheFieldsPresent        bool            `json:"cache_fields_present"`
	UsageDimensions           UsageDimensions `json:"usage_dimensions"`
	UsageNormalizationStatus  string          `json:"usage_normalization_status"`
	UpstreamRequestID         string          `json:"upstream_request_id"`
	ProcurementCostMicros     *int64          `json:"procurement_cost_micros,omitempty"`
	ProcurementCostCurrency   string          `json:"procurement_cost_currency"`
	ProcurementCostSource     string          `json:"procurement_cost_source"`
	ProcurementCostConfidence string          `json:"procurement_cost_confidence"`
	ProcurementPriceID        string          `json:"procurement_price_id"`
	ProviderBillingLineID     string          `json:"provider_billing_line_id"`
	CostCents                 int             `json:"cost_cents"`
	CreatedAt                 time.Time       `json:"created_at"`
}

type UsageModelSummary struct {
	Model             string `json:"model"`
	Requests          int    `json:"requests"`
	Errors            int    `json:"errors"`
	Tokens            int    `json:"tokens"`
	OutputImages      int64  `json:"output_images"`
	VideoMilliseconds int64  `json:"video_milliseconds"`
	AudioMilliseconds int64  `json:"audio_milliseconds"`
	CostCents         int    `json:"cost_cents"`
	AvgLatency        int64  `json:"avg_latency_ms"`
}

type UsageReport struct {
	TotalRequests      int                 `json:"total_requests"`
	ErrorRequests      int                 `json:"error_requests"`
	TotalTokens        int                 `json:"total_tokens"`
	TotalOutputImages  int64               `json:"total_output_images"`
	TotalVideoDuration int64               `json:"total_video_milliseconds"`
	TotalAudioDuration int64               `json:"total_audio_milliseconds"`
	TotalCostCents     int                 `json:"total_cost_cents"`
	AvgLatencyMS       int64               `json:"avg_latency_ms"`
	ByModel            []UsageModelSummary `json:"by_model"`
	Recent             []UsageRecord       `json:"recent"`
}

type UsageAggregate struct {
	TotalRequests      int
	ErrorRequests      int
	TotalTokens        int
	TotalOutputImages  int64
	TotalVideoDuration int64
	TotalAudioDuration int64
	TotalCostCents     int
	AvgLatencyMS       int64
	ByModel            []UsageModelSummary
}

type CostAllocationRollup struct {
	ResourceID     string
	APIKeyID       string
	APIFingerprint string
	Model          string
	Requests       int
	ErrorRequests  int
	TotalTokens    int
	TotalCostCents int
	AvgLatencyMS   int64
	LatencyTotal   int64
}

type CostAllocationRow struct {
	Dimension         string  `json:"dimension"`
	ResourceID        string  `json:"resource_id"`
	ResourceName      string  `json:"resource_name"`
	APIKeyID          string  `json:"api_key_id"`
	APIKeyName        string  `json:"api_key_name"`
	APIFingerprint    string  `json:"api_fingerprint"`
	Model             string  `json:"model"`
	Requests          int     `json:"requests"`
	ErrorRequests     int     `json:"error_requests"`
	TotalTokens       int     `json:"total_tokens"`
	TotalCostCents    int     `json:"total_cost_cents"`
	AvgLatencyMS      int64   `json:"avg_latency_ms"`
	BudgetCents       int     `json:"budget_cents"`
	BudgetUsedPercent float64 `json:"budget_used_percent"`
	CostSharePercent  float64 `json:"cost_share_percent"`
}

type CostAllocationReport struct {
	Dimension      string              `json:"dimension"`
	TotalRequests  int                 `json:"total_requests"`
	ErrorRequests  int                 `json:"error_requests"`
	TotalTokens    int                 `json:"total_tokens"`
	TotalCostCents int                 `json:"total_cost_cents"`
	AvgLatencyMS   int64               `json:"avg_latency_ms"`
	Rows           []CostAllocationRow `json:"rows"`
}

type UsageQuery struct {
	Limit                     int
	Offset                    int
	ID                        string
	Search                    string
	APIKeyID                  string
	APIKeyIDs                 []string
	CustomerID                string
	ProfileScope              string
	PlatformTenantID          string
	GatewayPrincipalID        string
	ExternalAuthIntegrationID string
	Model                     string
	ProviderID                string
	AccountID                 string
	UpstreamRequestID         string
	Status                    string
	CreatedFrom               time.Time
	CreatedTo                 time.Time
}

type GatewayTrace struct {
	ID                        string    `json:"id"`
	OperationID               string    `json:"operation_id"`
	AttemptID                 string    `json:"attempt_id"`
	RequestFingerprint        string    `json:"request_fingerprint"`
	APIKeyID                  string    `json:"api_key_id"`
	APIFingerprint            string    `json:"api_fingerprint"`
	ProfileScope              string    `json:"profile_scope"`
	PlatformTenantID          string    `json:"platform_tenant_id"`
	PlatformTenantName        string    `json:"platform_tenant_name"`
	GatewayPrincipalID        string    `json:"gateway_principal_id"`
	GatewayPrincipalName      string    `json:"gateway_principal_name"`
	ExternalAuthIntegrationID string    `json:"external_auth_integration_id"`
	ExternalSubjectReference  string    `json:"external_subject_reference"`
	Model                     string    `json:"model"`
	Stream                    bool      `json:"stream"`
	MessageCount              int       `json:"message_count"`
	ProviderID                string    `json:"provider_id"`
	ProviderAccountID         string    `json:"provider_account_id"`
	GatewayModelID            string    `json:"gateway_model_id"`
	RouteID                   string    `json:"route_id"`
	RouteGroup                string    `json:"route_group"`
	UpstreamModel             string    `json:"upstream_model"`
	RouteSource               string    `json:"route_source"`
	RouteReason               string    `json:"route_reason"`
	PolicyID                  string    `json:"policy_id"`
	PolicyName                string    `json:"policy_name"`
	PolicySource              string    `json:"policy_source"`
	PolicyVersion             int       `json:"policy_version"`
	PolicySnapshot            string    `json:"policy_snapshot"`
	Status                    string    `json:"status"`
	HTTPStatus                int       `json:"http_status"`
	ErrorType                 string    `json:"error_type"`
	LatencyMS                 int64     `json:"latency_ms"`
	InputTokens               int       `json:"input_tokens"`
	OutputTokens              int       `json:"output_tokens"`
	RequestSummary            string    `json:"request_summary"`
	ResponseSummary           string    `json:"response_summary"`
	RouteAttempts             string    `json:"route_attempts"`
	CreatedAt                 time.Time `json:"created_at"`
}

type GatewayTraceQuery struct {
	Limit                     int
	Offset                    int
	Search                    string
	APIKeyID                  string
	APIKeyIDs                 []string
	ProfileScope              string
	PlatformTenantID          string
	GatewayPrincipalID        string
	ExternalAuthIntegrationID string
	Model                     string
	Status                    string
	CreatedFrom               time.Time
	CreatedTo                 time.Time
}

type GatewayTraceSummary struct {
	Total        int   `json:"total"`
	Routed       int   `json:"routed"`
	Errors       int   `json:"errors"`
	Tokens       int   `json:"tokens"`
	AvgLatencyMS int64 `json:"avg_latency_ms"`
}

type AuditLogQuery struct {
	Limit                     int
	Offset                    int
	Search                    string
	Action                    string
	ResourceType              string
	ProfileScope              string
	PlatformTenantID          string
	GatewayPrincipalID        string
	ExternalAuthIntegrationID string
	CreatedFrom               time.Time
	CreatedTo                 time.Time
}

type AuditLogSummary struct {
	Total     int `json:"total"`
	Actors    int `json:"actors"`
	Resources int `json:"resources"`
	Actions   int `json:"actions"`
}

type PortalWorkspace struct {
	APIKeys       []APIKeyRecord `json:"api_keys"`
	Usage         UsageReport    `json:"usage"`
	RecentTraces  []GatewayTrace `json:"recent_traces"`
	Alerts        []AlertEvent   `json:"alerts"`
	Models        []string       `json:"models"`
	GatewayPath   string         `json:"gateway_path"`
	CanManageKeys bool           `json:"can_manage_keys"`
	Principal     string         `json:"principal"`
}

type GatewayAuthContext struct {
	APIKey                   APIKeyRecord             `json:"api_key"`
	Policy                   *GovernancePolicy        `json:"policy,omitempty"`
	PolicySource             string                   `json:"policy_source,omitempty"`
	PlatformTenant           *PlatformTenant          `json:"platform_tenant,omitempty"`
	GatewayPrincipal         *GatewayPrincipal        `json:"gateway_principal,omitempty"`
	ExternalAuthIntegration  *ExternalAuthIntegration `json:"external_auth_integration,omitempty"`
	ExternalSubjectReference string                   `json:"external_subject_reference,omitempty"`
}

type GatewayProvider struct {
	AttemptID        string
	ID               string
	Name             string
	Type             string
	BaseURL          string
	APIKey           string
	AdapterID        string
	AccountID        string
	AccountName      string
	Concurrency      int
	GatewayModelID   string
	RequestedModel   string
	UpstreamModel    string
	RouteID          string
	RouteGroup       string
	RoutePriority    int
	RouteWeight      int
	AccountWeight    int
	RPMLimit         int
	TPMLimit         int
	CircuitState     string
	CircuitProbe     bool
	Headroom         float64
	StickyEnabled    bool
	StickyTTLSeconds int
	Source           string
	SelectionReason  string
}
