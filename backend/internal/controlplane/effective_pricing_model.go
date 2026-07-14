package controlplane

import "time"

const defaultEffectivePricingPolicyID = "default"

const (
	ProcurementPriceStatusActive   = "active"
	ProcurementPriceStatusDisabled = "disabled"

	ProcurementCostConfidenceExact       = "exact"
	ProcurementCostConfidenceDerived     = "derived"
	ProcurementCostConfidenceEstimated   = "estimated"
	ProcurementCostConfidenceUnallocated = "unallocated"
	ProcurementCostConfidenceUnknown     = "unknown"

	CacheSupportUnknown        = "unknown"
	CacheSupportClaimed        = "claimed"
	CacheSupportAccepted       = "accepted"
	CacheSupportObserved       = "observed"
	CacheSupportBilledVerified = "billed_verified"
	CacheSupportDegraded       = "degraded"
	CacheSupportUnsupported    = "unsupported"

	PoolAffinityUnknown    = "unknown"
	PoolAffinityVerified   = "verified"
	PoolAffinityProbable   = "probable"
	PoolAffinityOpaque     = "opaque"
	PoolAffinityFragmented = "fragmented"

	AffinityTransportNone   = "none"
	AffinityTransportHeader = "header"
	AffinityTransportBody   = "body"

	EffectivePricingModeObserveOnly = "observe_only"
	EffectivePricingModeRecommend   = "recommend"
	EffectivePricingModeCanary      = "canary"
	EffectivePricingModeBalanced    = "balanced"
	EffectivePricingModeCostFirst   = "cost_first"
	EffectivePricingModeFixedRoute  = "fixed_route"

	EffectivePricingDecisionHold              = "hold"
	EffectivePricingDecisionRecommended       = "recommended"
	EffectivePricingDecisionCanary            = "canary"
	EffectivePricingDecisionActive            = "active"
	EffectivePricingDecisionRolledBack        = "rolled_back"
	EffectivePricingDecisionDegraded          = "degraded"
	EffectivePricingDecisionEmergencyFailover = "emergency_failover"

	CacheProbeStatusRunning   = "running"
	CacheProbeStatusSucceeded = "succeeded"
	CacheProbeStatusFailed    = "failed"
	CacheProbeStatusSkipped   = "skipped"

	BillingReconciliationPending     = "pending"
	BillingReconciliationMatched     = "matched"
	BillingReconciliationAmbiguous   = "ambiguous"
	BillingReconciliationUnallocated = "unallocated"

	AffinityBindingSupplier = "supplier"
	AffinityBindingAccount  = "account"
)

type ProcurementPrice struct {
	ID                               string     `json:"id"`
	ProviderID                       string     `json:"provider_id"`
	ProviderAccountID                string     `json:"provider_account_id"`
	UpstreamModel                    string     `json:"upstream_model"`
	Protocol                         string     `json:"protocol"`
	Currency                         string     `json:"currency"`
	UncachedInputMicrosPer1MTokens   int64      `json:"uncached_input_micros_per_1m_tokens"`
	CacheReadMicrosPer1MTokens       int64      `json:"cache_read_micros_per_1m_tokens"`
	CacheWrite5mMicrosPer1MTokens    int64      `json:"cache_write_5m_micros_per_1m_tokens"`
	CacheWrite1hMicrosPer1MTokens    int64      `json:"cache_write_1h_micros_per_1m_tokens"`
	OutputMicrosPer1MTokens          int64      `json:"output_micros_per_1m_tokens"`
	RequestMicros                    int64      `json:"request_micros"`
	ReferenceInputMicrosPer1MTokens  int64      `json:"reference_input_micros_per_1m_tokens"`
	ReferenceOutputMicrosPer1MTokens int64      `json:"reference_output_micros_per_1m_tokens"`
	QuotedMultiplier                 float64    `json:"quoted_multiplier"`
	RechargeMultiplier               float64    `json:"recharge_multiplier"`
	SourceKind                       string     `json:"source_kind"`
	SourceReference                  string     `json:"source_reference"`
	EvidenceHash                     string     `json:"evidence_hash"`
	Confidence                       string     `json:"confidence"`
	Status                           string     `json:"status"`
	EffectiveFrom                    time.Time  `json:"effective_from"`
	ExpiresAt                        *time.Time `json:"expires_at,omitempty"`
	CreatedAt                        time.Time  `json:"created_at"`
	UpdatedAt                        time.Time  `json:"updated_at"`
}

type ProcurementPriceRequest struct {
	ProviderID                       string  `json:"provider_id"`
	ProviderAccountID                string  `json:"provider_account_id"`
	UpstreamModel                    string  `json:"upstream_model"`
	Protocol                         string  `json:"protocol"`
	Currency                         string  `json:"currency"`
	UncachedInputMicrosPer1MTokens   int64   `json:"uncached_input_micros_per_1m_tokens"`
	CacheReadMicrosPer1MTokens       int64   `json:"cache_read_micros_per_1m_tokens"`
	CacheWrite5mMicrosPer1MTokens    int64   `json:"cache_write_5m_micros_per_1m_tokens"`
	CacheWrite1hMicrosPer1MTokens    int64   `json:"cache_write_1h_micros_per_1m_tokens"`
	OutputMicrosPer1MTokens          int64   `json:"output_micros_per_1m_tokens"`
	RequestMicros                    int64   `json:"request_micros"`
	ReferenceInputMicrosPer1MTokens  int64   `json:"reference_input_micros_per_1m_tokens"`
	ReferenceOutputMicrosPer1MTokens int64   `json:"reference_output_micros_per_1m_tokens"`
	QuotedMultiplier                 float64 `json:"quoted_multiplier"`
	RechargeMultiplier               float64 `json:"recharge_multiplier"`
	SourceKind                       string  `json:"source_kind"`
	SourceReference                  string  `json:"source_reference"`
	EvidenceHash                     string  `json:"evidence_hash"`
	Confidence                       string  `json:"confidence"`
	Status                           string  `json:"status"`
	EffectiveFrom                    string  `json:"effective_from"`
	ExpiresAt                        string  `json:"expires_at"`
}

type ProviderBillingLine struct {
	ID                   string     `json:"id"`
	ProviderID           string     `json:"provider_id"`
	ProviderAccountID    string     `json:"provider_account_id"`
	ExternalLineID       string     `json:"external_line_id"`
	ExternalRequestID    string     `json:"external_request_id"`
	UsageRecordID        string     `json:"usage_record_id"`
	UpstreamModel        string     `json:"upstream_model"`
	Currency             string     `json:"currency"`
	AmountMicros         int64      `json:"amount_micros"`
	InputCostMicros      *int64     `json:"input_cost_micros,omitempty"`
	OutputCostMicros     *int64     `json:"output_cost_micros,omitempty"`
	CacheReadCostMicros  *int64     `json:"cache_read_cost_micros,omitempty"`
	CacheWriteCostMicros *int64     `json:"cache_write_cost_micros,omitempty"`
	SourceKind           string     `json:"source_kind"`
	Confidence           string     `json:"confidence"`
	ReconciliationStatus string     `json:"reconciliation_status"`
	RawPayloadHash       string     `json:"raw_payload_hash"`
	UsageStartedAt       *time.Time `json:"usage_started_at,omitempty"`
	UsageEndedAt         *time.Time `json:"usage_ended_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ProviderBillingLineRequest struct {
	ProviderID           string `json:"provider_id"`
	ProviderAccountID    string `json:"provider_account_id"`
	ExternalLineID       string `json:"external_line_id"`
	ExternalRequestID    string `json:"external_request_id"`
	UsageRecordID        string `json:"usage_record_id"`
	UpstreamModel        string `json:"upstream_model"`
	Currency             string `json:"currency"`
	AmountMicros         int64  `json:"amount_micros"`
	InputCostMicros      *int64 `json:"input_cost_micros"`
	OutputCostMicros     *int64 `json:"output_cost_micros"`
	CacheReadCostMicros  *int64 `json:"cache_read_cost_micros"`
	CacheWriteCostMicros *int64 `json:"cache_write_cost_micros"`
	SourceKind           string `json:"source_kind"`
	Confidence           string `json:"confidence"`
	RawPayloadHash       string `json:"raw_payload_hash"`
	UsageStartedAt       string `json:"usage_started_at"`
	UsageEndedAt         string `json:"usage_ended_at"`
}

type ProviderCacheCapability struct {
	ID                      string     `json:"id"`
	ProviderAccountID       string     `json:"provider_account_id"`
	UpstreamModel           string     `json:"upstream_model"`
	Protocol                string     `json:"protocol"`
	SupportStatus           string     `json:"support_status"`
	PoolAffinityGrade       string     `json:"pool_affinity_grade"`
	AffinityTransport       string     `json:"affinity_transport"`
	AffinityField           string     `json:"affinity_field"`
	CacheControlMode        string     `json:"cache_control_mode"`
	UsageSchema             string     `json:"usage_schema"`
	MetricsCoverage         float64    `json:"metrics_coverage"`
	EligibleRequestHitRate  float64    `json:"eligible_request_hit_rate"`
	CacheTokenHitRate       float64    `json:"cache_token_hit_rate"`
	CacheWriteReadRatio     float64    `json:"cache_write_read_ratio"`
	AffinityConsistencyRate float64    `json:"affinity_consistency_rate"`
	BillingConsistencyRate  float64    `json:"billing_consistency_rate"`
	ProductionSampleCount   int64      `json:"production_sample_count"`
	ProbeSampleCount        int64      `json:"probe_sample_count"`
	DegradedReason          string     `json:"degraded_reason"`
	LastObservedAt          *time.Time `json:"last_observed_at,omitempty"`
	LastVerifiedAt          *time.Time `json:"last_verified_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type ProviderCacheCapabilityRequest struct {
	ProviderAccountID string `json:"provider_account_id"`
	UpstreamModel     string `json:"upstream_model"`
	Protocol          string `json:"protocol"`
	SupportStatus     string `json:"support_status"`
	PoolAffinityGrade string `json:"pool_affinity_grade"`
	AffinityTransport string `json:"affinity_transport"`
	AffinityField     string `json:"affinity_field"`
	CacheControlMode  string `json:"cache_control_mode"`
	UsageSchema       string `json:"usage_schema"`
}

type ProviderCacheProductionMetrics struct {
	ID                     string
	ProviderAccountID      string
	UpstreamModel          string
	Protocol               string
	MetricsCoverage        float64
	EligibleRequestHitRate float64
	CacheTokenHitRate      float64
	CacheWriteReadRatio    float64
	BillingConsistencyRate float64
	ProductionSampleCount  int64
	MetricsObserved        bool
	CacheActivityObserved  bool
	ObservedAt             time.Time
}

type ProviderCacheProbeRun struct {
	ID                       string    `json:"id"`
	ProviderID               string    `json:"provider_id"`
	ProviderAccountID        string    `json:"provider_account_id"`
	UpstreamModel            string    `json:"upstream_model"`
	Protocol                 string    `json:"protocol"`
	ProbeSeriesID            string    `json:"probe_series_id"`
	SessionHash              string    `json:"session_hash"`
	PrefixFingerprint        string    `json:"prefix_fingerprint"`
	PrefixTokens             int64     `json:"prefix_tokens"`
	WarmCacheReadTokens      int64     `json:"warm_cache_read_tokens"`
	WarmCacheWriteTokens     int64     `json:"warm_cache_write_tokens"`
	WarmTTFTMS               int64     `json:"warm_ttft_ms"`
	WarmUpstreamRequestID    string    `json:"warm_upstream_request_id"`
	ReuseCacheReadTokens     int64     `json:"reuse_cache_read_tokens"`
	ReuseCacheWriteTokens    int64     `json:"reuse_cache_write_tokens"`
	ReuseTTFTMS              int64     `json:"reuse_ttft_ms"`
	ReuseUpstreamRequestID   string    `json:"reuse_upstream_request_id"`
	ControlCacheReadTokens   int64     `json:"control_cache_read_tokens"`
	ControlCacheWriteTokens  int64     `json:"control_cache_write_tokens"`
	ControlTTFTMS            int64     `json:"control_ttft_ms"`
	ControlUpstreamRequestID string    `json:"control_upstream_request_id"`
	CacheFieldsPresent       bool      `json:"cache_fields_present"`
	EstimatedCostMicros      int64     `json:"estimated_cost_micros"`
	Status                   string    `json:"status"`
	FailureReason            string    `json:"failure_reason"`
	StartedAt                time.Time `json:"started_at"`
	FinishedAt               time.Time `json:"finished_at"`
}

type CacheProbeRequest struct {
	ProviderAccountID string `json:"provider_account_id"`
	UpstreamModel     string `json:"upstream_model"`
	Protocol          string `json:"protocol"`
	PrefixTokens      int64  `json:"prefix_tokens"`
	MaxCostMicros     int64  `json:"max_cost_micros"`
}

type CacheProbeReservationLimits struct {
	DayStart              time.Time
	Now                   time.Time
	Cooldown              time.Duration
	StaleAfter            time.Duration
	DailyTokenBudget      int64
	DailyCostBudgetMicros int64
}

type EffectivePricingPolicy struct {
	ID                             string    `json:"id"`
	Mode                           string    `json:"mode"`
	WindowHours                    int       `json:"window_hours"`
	MinSampleCount                 int64     `json:"min_sample_count"`
	MinMetricsCoverage             float64   `json:"min_metrics_coverage"`
	MinBillingConsistency          float64   `json:"min_billing_consistency"`
	MinCostImprovement             float64   `json:"min_cost_improvement"`
	MinCacheHitRateImprovement     float64   `json:"min_cache_hit_rate_improvement"`
	MinAffinityImprovement         float64   `json:"min_affinity_improvement"`
	MaxCacheTiebreakCostRegression float64   `json:"max_cache_tiebreak_cost_regression"`
	MaxErrorRateRegression         float64   `json:"max_error_rate_regression"`
	MaxP95LatencyRegression        float64   `json:"max_p95_latency_regression"`
	CanaryPercent                  int       `json:"canary_percent"`
	SupplierAffinityTTLSeconds     int       `json:"supplier_affinity_ttl_seconds"`
	AccountAffinityTTLSeconds      int       `json:"account_affinity_ttl_seconds"`
	ProbeEnabled                   bool      `json:"probe_enabled"`
	ProbeDailyTokenBudget          int64     `json:"probe_daily_token_budget"`
	ProbeDailyCostBudgetMicros     int64     `json:"probe_daily_cost_budget_micros"`
	ProbeCooldownSeconds           int       `json:"probe_cooldown_seconds"`
	UpdatedBy                      string    `json:"updated_by"`
	CreatedAt                      time.Time `json:"created_at"`
	UpdatedAt                      time.Time `json:"updated_at"`
}

type EffectivePricingPolicyRequest struct {
	Mode                           string  `json:"mode"`
	WindowHours                    int     `json:"window_hours"`
	MinSampleCount                 int64   `json:"min_sample_count"`
	MinMetricsCoverage             float64 `json:"min_metrics_coverage"`
	MinBillingConsistency          float64 `json:"min_billing_consistency"`
	MinCostImprovement             float64 `json:"min_cost_improvement"`
	MinCacheHitRateImprovement     float64 `json:"min_cache_hit_rate_improvement"`
	MinAffinityImprovement         float64 `json:"min_affinity_improvement"`
	MaxCacheTiebreakCostRegression float64 `json:"max_cache_tiebreak_cost_regression"`
	MaxErrorRateRegression         float64 `json:"max_error_rate_regression"`
	MaxP95LatencyRegression        float64 `json:"max_p95_latency_regression"`
	CanaryPercent                  int     `json:"canary_percent"`
	SupplierAffinityTTLSeconds     int     `json:"supplier_affinity_ttl_seconds"`
	AccountAffinityTTLSeconds      int     `json:"account_affinity_ttl_seconds"`
	ProbeEnabled                   bool    `json:"probe_enabled"`
	ProbeDailyTokenBudget          int64   `json:"probe_daily_token_budget"`
	ProbeDailyCostBudgetMicros     int64   `json:"probe_daily_cost_budget_micros"`
	ProbeCooldownSeconds           int     `json:"probe_cooldown_seconds"`
}

type EffectivePriceSnapshot struct {
	ID                       string    `json:"id"`
	ProviderID               string    `json:"provider_id"`
	ProviderAccountID        string    `json:"provider_account_id"`
	UpstreamModel            string    `json:"upstream_model"`
	Protocol                 string    `json:"protocol"`
	Currency                 string    `json:"currency"`
	EffectiveCostMicrosPer1M int64     `json:"effective_cost_micros_per_1m"`
	EffectiveMultiplier      float64   `json:"effective_multiplier"`
	QuotedMultiplier         float64   `json:"quoted_multiplier"`
	CacheTokenHitRate        float64   `json:"cache_token_hit_rate"`
	MetricsCoverage          float64   `json:"metrics_coverage"`
	BillingConsistencyRate   float64   `json:"billing_consistency_rate"`
	RequestCount             int64     `json:"request_count"`
	CostConfidence           string    `json:"cost_confidence"`
	PriceID                  string    `json:"price_id"`
	WindowStart              time.Time `json:"window_start"`
	WindowEnd                time.Time `json:"window_end"`
	ExpiresAt                time.Time `json:"expires_at"`
	CreatedAt                time.Time `json:"created_at"`
}

type EffectivePricingDecision struct {
	ID                         string    `json:"id"`
	Model                      string    `json:"model"`
	Protocol                   string    `json:"protocol"`
	CurrentProviderAccountID   string    `json:"current_provider_account_id"`
	CandidateProviderAccountID string    `json:"candidate_provider_account_id"`
	CurrentSnapshotID          string    `json:"current_snapshot_id"`
	CandidateSnapshotID        string    `json:"candidate_snapshot_id"`
	CurrentCostMicrosPer1M     int64     `json:"current_cost_micros_per_1m"`
	CandidateCostMicrosPer1M   int64     `json:"candidate_cost_micros_per_1m"`
	CostImprovement            float64   `json:"cost_improvement"`
	Status                     string    `json:"status"`
	ReasonCodes                []string  `json:"reason_codes"`
	CanaryPercent              int       `json:"canary_percent"`
	SampleCount                int64     `json:"sample_count"`
	Confidence                 string    `json:"confidence"`
	CreatedBy                  string    `json:"created_by"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type EffectivePricingDecisionActionRequest struct {
	Action        string `json:"action"`
	CanaryPercent int    `json:"canary_percent"`
}

type EffectivePricingDecisionEvaluationRequest struct {
	Model                      string `json:"model"`
	UpstreamModel              string `json:"upstream_model"`
	Protocol                   string `json:"protocol"`
	CurrentProviderAccountID   string `json:"current_provider_account_id"`
	CandidateProviderAccountID string `json:"candidate_provider_account_id"`
}

type RoutingAffinityBinding struct {
	ScopeKey          string    `json:"scope_key"`
	Kind              string    `json:"kind"`
	ProviderID        string    `json:"provider_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	RouteID           string    `json:"route_id"`
	Model             string    `json:"model"`
	Protocol          string    `json:"protocol"`
	PolicyVersion     int       `json:"policy_version"`
	CreatedAt         time.Time `json:"created_at"`
	LastReusedAt      time.Time `json:"last_reused_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type EffectivePricingUsageAggregate struct {
	ProviderID                 string
	ProviderAccountID          string
	UpstreamModel              string
	Protocol                   string
	RequestCount               int64
	SuccessfulRequestCount     int64
	ErrorCount                 int64
	CacheMetricsRequestCount   int64
	CacheHitRequestCount       int64
	TotalInputTokens           int64
	UncachedInputTokens        int64
	CacheReadTokens            int64
	CacheWrite5mTokens         int64
	CacheWrite1hTokens         int64
	OutputTokens               int64
	ProcurementCostMicros      int64
	ProcurementCostRecordCount int64
	LatencyTotalMS             int64
	LastCacheObservedAt        *time.Time
}

type EffectivePricingReport struct {
	WindowStart time.Time                   `json:"window_start"`
	WindowEnd   time.Time                   `json:"window_end"`
	Policy      EffectivePricingPolicy      `json:"policy"`
	Rows        []EffectivePricingReportRow `json:"rows"`
	Decisions   []EffectivePricingDecision  `json:"decisions"`
}

type EffectivePricingReportQuery struct {
	Model       string
	Protocol    string
	WindowHours int
}

type EffectivePricingReportRow struct {
	ProviderID               string   `json:"provider_id"`
	ProviderName             string   `json:"provider_name"`
	ProviderAccountID        string   `json:"provider_account_id"`
	ProviderAccountName      string   `json:"provider_account_name"`
	UpstreamModel            string   `json:"upstream_model"`
	Protocol                 string   `json:"protocol"`
	Currency                 string   `json:"currency"`
	QuotedMultiplier         float64  `json:"quoted_multiplier"`
	BilledMultiplier         float64  `json:"billed_multiplier"`
	EffectiveMultiplier      float64  `json:"effective_multiplier"`
	EffectiveCostMicrosPer1M int64    `json:"effective_cost_micros_per_1m"`
	RequestCount             int64    `json:"request_count"`
	ErrorRate                float64  `json:"error_rate"`
	MetricsCoverage          float64  `json:"metrics_coverage"`
	EligibleRequestHitRate   float64  `json:"eligible_request_hit_rate"`
	CacheTokenHitRate        float64  `json:"cache_token_hit_rate"`
	CacheWriteReadRatio      float64  `json:"cache_write_read_ratio"`
	BillingConsistencyRate   float64  `json:"billing_consistency_rate"`
	AffinityConsistencyRate  float64  `json:"affinity_consistency_rate"`
	CacheSupportStatus       string   `json:"cache_support_status"`
	PoolAffinityGrade        string   `json:"pool_affinity_grade"`
	CostConfidence           string   `json:"cost_confidence"`
	PriceID                  string   `json:"price_id"`
	Recommendation           string   `json:"recommendation"`
	ReasonCodes              []string `json:"reason_codes"`
}
