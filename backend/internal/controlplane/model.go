package controlplane

import "time"

const (
	ProviderStatusActive      = "active"
	ProviderStatusDisabled    = "disabled"
	ProviderStatusNeedsSecret = "needs_secret"

	APIKeyStatusActive   = "active"
	APIKeyStatusDisabled = "disabled"

	APIKeyTypeWorkspace = "workspace"
	APIKeyTypeUser      = "user"
	APIKeyTypeCustomer  = "customer"
	APIKeyTypeService   = "service"

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
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	KeyHash           string     `json:"-"`
	Fingerprint       string     `json:"fingerprint"`
	Prefix            string     `json:"prefix"`
	Status            string     `json:"status"`
	KeyType           string     `json:"key_type"`
	CustomerID        string     `json:"customer_id"`
	OwnerUserID       string     `json:"owner_user_id"`
	PolicyID          string     `json:"policy_id"`
	ModelAllowlist    []string   `json:"model_allowlist"`
	QPSLimit          int        `json:"qps_limit"`
	MonthlyTokenLimit int        `json:"monthly_token_limit"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type APIKeyCreateRequest struct {
	Name              string   `json:"name"`
	PolicyID          string   `json:"policy_id"`
	ModelAllowlist    []string `json:"model_allowlist"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
	ExpiresAt         string   `json:"expires_at"`
	KeyType           string   `json:"key_type"`
	CustomerID        string   `json:"customer_id"`
	OwnerUserID       string   `json:"owner_user_id"`
}

type APIKeyUpdateRequest struct {
	Name              string   `json:"name"`
	PolicyID          string   `json:"policy_id"`
	ModelAllowlist    []string `json:"model_allowlist"`
	QPSLimit          int      `json:"qps_limit"`
	MonthlyTokenLimit int      `json:"monthly_token_limit"`
	ExpiresAt         string   `json:"expires_at"`
	Status            string   `json:"status"`
	KeyType           string   `json:"key_type"`
	CustomerID        string   `json:"customer_id"`
	OwnerUserID       string   `json:"owner_user_id"`
}

type APIKeyCreateResponse struct {
	Record APIKeyRecord `json:"record"`
	Key    string       `json:"key"`
}

type AuditLog struct {
	ID           string    `json:"id"`
	Actor        string    `json:"actor"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Summary      string    `json:"summary"`
	CreatedAt    time.Time `json:"created_at"`
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
	ID                string    `json:"id"`
	APIKeyID          string    `json:"api_key_id"`
	CustomerID        string    `json:"customer_id"`
	APIFingerprint    string    `json:"api_fingerprint"`
	Model             string    `json:"model"`
	UpstreamModel     string    `json:"upstream_model"`
	ProviderID        string    `json:"provider_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	Status            string    `json:"status"`
	ErrorType         string    `json:"error_type"`
	LatencyMS         int64     `json:"latency_ms"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	CostCents         int       `json:"cost_cents"`
	CreatedAt         time.Time `json:"created_at"`
}

type UsageModelSummary struct {
	Model      string `json:"model"`
	Requests   int    `json:"requests"`
	Errors     int    `json:"errors"`
	Tokens     int    `json:"tokens"`
	CostCents  int    `json:"cost_cents"`
	AvgLatency int64  `json:"avg_latency_ms"`
}

type UsageReport struct {
	TotalRequests  int                 `json:"total_requests"`
	ErrorRequests  int                 `json:"error_requests"`
	TotalTokens    int                 `json:"total_tokens"`
	TotalCostCents int                 `json:"total_cost_cents"`
	AvgLatencyMS   int64               `json:"avg_latency_ms"`
	ByModel        []UsageModelSummary `json:"by_model"`
	Recent         []UsageRecord       `json:"recent"`
}

type UsageAggregate struct {
	TotalRequests  int
	ErrorRequests  int
	TotalTokens    int
	TotalCostCents int
	AvgLatencyMS   int64
	ByModel        []UsageModelSummary
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
	Limit       int
	Offset      int
	Search      string
	APIKeyID    string
	APIKeyIDs   []string
	CustomerID  string
	Model       string
	ProviderID  string
	AccountID   string
	Status      string
	CreatedFrom time.Time
	CreatedTo   time.Time
}

type GatewayTrace struct {
	ID                string    `json:"id"`
	APIKeyID          string    `json:"api_key_id"`
	APIFingerprint    string    `json:"api_fingerprint"`
	Model             string    `json:"model"`
	Stream            bool      `json:"stream"`
	MessageCount      int       `json:"message_count"`
	ProviderID        string    `json:"provider_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	GatewayModelID    string    `json:"gateway_model_id"`
	RouteID           string    `json:"route_id"`
	RouteGroup        string    `json:"route_group"`
	UpstreamModel     string    `json:"upstream_model"`
	RouteSource       string    `json:"route_source"`
	RouteReason       string    `json:"route_reason"`
	PolicyID          string    `json:"policy_id"`
	PolicyName        string    `json:"policy_name"`
	PolicySource      string    `json:"policy_source"`
	PolicyVersion     int       `json:"policy_version"`
	PolicySnapshot    string    `json:"policy_snapshot"`
	Status            string    `json:"status"`
	HTTPStatus        int       `json:"http_status"`
	ErrorType         string    `json:"error_type"`
	LatencyMS         int64     `json:"latency_ms"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	RequestSummary    string    `json:"request_summary"`
	ResponseSummary   string    `json:"response_summary"`
	RouteAttempts     string    `json:"route_attempts"`
	CreatedAt         time.Time `json:"created_at"`
}

type GatewayTraceQuery struct {
	Limit       int
	Offset      int
	Search      string
	APIKeyID    string
	APIKeyIDs   []string
	Model       string
	Status      string
	CreatedFrom time.Time
	CreatedTo   time.Time
}

type GatewayTraceSummary struct {
	Total        int   `json:"total"`
	Routed       int   `json:"routed"`
	Errors       int   `json:"errors"`
	Tokens       int   `json:"tokens"`
	AvgLatencyMS int64 `json:"avg_latency_ms"`
}

type AuditLogQuery struct {
	Limit        int
	Offset       int
	Search       string
	Action       string
	ResourceType string
	CreatedFrom  time.Time
	CreatedTo    time.Time
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
	APIKey       APIKeyRecord      `json:"api_key"`
	Policy       *GovernancePolicy `json:"policy,omitempty"`
	PolicySource string            `json:"policy_source,omitempty"`
}

type GatewayProvider struct {
	ID               string
	Name             string
	BaseURL          string
	APIKey           string
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
