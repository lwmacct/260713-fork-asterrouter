package controlplane

import "time"

const (
	ProviderBillingSourceObserveOnly = "observe_only"
	ProviderBillingSourceActive      = "active"
	ProviderBillingSourceDisabled    = "disabled"

	ProviderBillingSyncTriggerManual    = "manual"
	ProviderBillingSyncTriggerScheduled = "scheduled"

	ProviderBillingSyncRunning      = "running"
	ProviderBillingSyncSucceeded    = "succeeded"
	ProviderBillingSyncFailed       = "failed"
	ProviderBillingSyncLeaseExpired = "lease_expired"
)

type ProviderBillingSource struct {
	ID                   string                            `json:"id"`
	ProviderID           string                            `json:"provider_id"`
	ProviderAccountID    string                            `json:"provider_account_id"`
	AdapterID            string                            `json:"adapter_id"`
	Status               string                            `json:"status"`
	AutomaticSyncEnabled bool                              `json:"automatic_sync_enabled"`
	SyncIntervalSeconds  int                               `json:"sync_interval_seconds"`
	Cursor               string                            `json:"-"`
	Capabilities         ProviderBillingSourceCapabilities `json:"capabilities"`
	DetectionStatus      string                            `json:"detection_status"`
	ContractVersion      string                            `json:"contract_version"`
	EvidenceHash         string                            `json:"evidence_hash"`
	Warnings             []string                          `json:"warnings"`
	NextSyncAt           *time.Time                        `json:"next_sync_at,omitempty"`
	LastSyncStartedAt    *time.Time                        `json:"last_sync_started_at,omitempty"`
	LastSyncCompletedAt  *time.Time                        `json:"last_sync_completed_at,omitempty"`
	LastSuccessAt        *time.Time                        `json:"last_success_at,omitempty"`
	ConsecutiveFailures  int                               `json:"consecutive_failures"`
	LastErrorCode        string                            `json:"last_error_code"`
	LeaseToken           string                            `json:"-"`
	LeaseExpiresAt       *time.Time                        `json:"-"`
	Version              int64                             `json:"version"`
	CreatedBy            string                            `json:"created_by"`
	UpdatedBy            string                            `json:"updated_by"`
	CreatedAt            time.Time                         `json:"created_at"`
	UpdatedAt            time.Time                         `json:"updated_at"`
	RoutingHealth        *ProviderBillingRoutingHealth     `json:"routing_health,omitempty"`
}

const (
	ProviderBillingRoutingHealthHealthy     = "healthy"
	ProviderBillingRoutingHealthDegraded    = "degraded"
	ProviderBillingRoutingHealthBlocked     = "blocked"
	ProviderBillingRoutingHealthObserveOnly = "observe_only"
	ProviderBillingRoutingHealthDisabled    = "disabled"
)

type ProviderBillingRoutingHealth struct {
	SourceStatus              string     `json:"source_status"`
	Status                    string     `json:"status"`
	HardBlocked               bool       `json:"hard_blocked"`
	EconomicSwitchEligible    bool       `json:"economic_switch_eligible"`
	ReasonCodes               []string   `json:"reason_codes"`
	EvaluatedAt               time.Time  `json:"evaluated_at"`
	EvidenceObservedAt        *time.Time `json:"evidence_observed_at,omitempty"`
	EvidenceStaleAfterSeconds int        `json:"evidence_stale_after_seconds"`
}

type ProviderBillingSourceRequest struct {
	ProviderAccountID    string `json:"provider_account_id"`
	AdapterID            string `json:"adapter_id"`
	Status               string `json:"status"`
	AutomaticSyncEnabled bool   `json:"automatic_sync_enabled"`
	SyncIntervalSeconds  int    `json:"sync_interval_seconds"`
	Version              *int64 `json:"version,omitempty"`
}

type ProviderBillingSyncRun struct {
	ID                string                            `json:"id"`
	SourceID          string                            `json:"source_id"`
	ProviderID        string                            `json:"provider_id"`
	ProviderAccountID string                            `json:"provider_account_id"`
	Trigger           string                            `json:"trigger"`
	TriggeredBy       string                            `json:"triggered_by"`
	AdapterID         string                            `json:"adapter_id"`
	Status            string                            `json:"status"`
	Capabilities      ProviderBillingSourceCapabilities `json:"capabilities"`
	DetectionStatus   string                            `json:"detection_status"`
	ContractVersion   string                            `json:"contract_version"`
	DiscoveredLines   int                               `json:"discovered_lines"`
	ImportedLines     int                               `json:"imported_lines"`
	SkippedLines      int                               `json:"skipped_lines"`
	EvidenceHash      string                            `json:"evidence_hash"`
	Warnings          []string                          `json:"warnings"`
	ErrorCode         string                            `json:"error_code"`
	StartedAt         time.Time                         `json:"started_at"`
	FinishedAt        *time.Time                        `json:"finished_at,omitempty"`
	CreatedAt         time.Time                         `json:"created_at"`
}

type ProviderBalanceSnapshotRecord struct {
	ID                string    `json:"id"`
	SourceID          string    `json:"source_id"`
	SyncRunID         string    `json:"sync_run_id"`
	ProviderAccountID string    `json:"provider_account_id"`
	Kind              string    `json:"kind"`
	AmountMicros      int64     `json:"amount_micros"`
	Unlimited         bool      `json:"unlimited"`
	Currency          string    `json:"currency"`
	EvidenceHash      string    `json:"evidence_hash"`
	ObservedAt        time.Time `json:"observed_at"`
	CreatedAt         time.Time `json:"created_at"`
}

type ProviderUsageAggregateSnapshot struct {
	ID                  string    `json:"id"`
	SourceID            string    `json:"source_id"`
	SyncRunID           string    `json:"sync_run_id"`
	ProviderAccountID   string    `json:"provider_account_id"`
	Scope               string    `json:"scope"`
	Model               string    `json:"model"`
	RequestCount        int64     `json:"request_count"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	ListCostMicros      *int64    `json:"list_cost_micros,omitempty"`
	ActualCostMicros    *int64    `json:"actual_cost_micros,omitempty"`
	Currency            string    `json:"currency"`
	EvidenceHash        string    `json:"evidence_hash"`
	ObservedAt          time.Time `json:"observed_at"`
	CreatedAt           time.Time `json:"created_at"`
}

type ProviderBillingSourceClaimRequest struct {
	SourceID      string
	Trigger       string
	TriggeredBy   string
	Now           time.Time
	LeaseDuration time.Duration
	Limit         int
}

type ProviderBillingSourceClaim struct {
	Source ProviderBillingSource
	Run    ProviderBillingSyncRun
}

type ProviderBillingSyncCommit struct {
	SourceID    string
	LeaseToken  string
	Run         ProviderBillingSyncRun
	Balance     *ProviderBalanceSnapshotRecord
	Aggregates  []ProviderUsageAggregateSnapshot
	Cursor      string
	NextSyncAt  *time.Time
	CompletedAt time.Time
}

type ProviderBillingSourceEvidence struct {
	Source     ProviderBillingSource            `json:"source"`
	Runs       []ProviderBillingSyncRun         `json:"runs"`
	Balances   []ProviderBalanceSnapshotRecord  `json:"balances"`
	Aggregates []ProviderUsageAggregateSnapshot `json:"aggregates"`
}

type ProviderBillingSyncResult struct {
	Source     ProviderBillingSource            `json:"source"`
	Run        ProviderBillingSyncRun           `json:"run"`
	Balance    *ProviderBalanceSnapshotRecord   `json:"balance,omitempty"`
	Aggregates []ProviderUsageAggregateSnapshot `json:"aggregates"`
}

type ProviderBillingSyncBatchReport struct {
	Claimed   int                         `json:"claimed"`
	Succeeded int                         `json:"succeeded"`
	Failed    int                         `json:"failed"`
	Results   []ProviderBillingSyncResult `json:"results"`
}
