package controlplane

import (
	"errors"
	"time"
)

var (
	ErrGatewayIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrGatewayIdempotencyReplay   = errors.New("direct request with this idempotency key was already accepted")
	ErrUsageLedgerConflict        = errors.New("usage ledger identity conflicts with an existing entry")
	ErrAIAttemptNotFound          = errors.New("ai attempt not found")
	ErrAIAttemptDispatchConflict  = errors.New("ai attempt dispatch intent conflicts with existing state")
	ErrAIAttemptDispatchState     = errors.New("ai attempt dispatch state changed concurrently")
)

const (
	AIOperationStatusAccepted  = "accepted"
	AIOperationStatusRunning   = "running"
	AIOperationStatusSucceeded = "succeeded"
	AIOperationStatusFailed    = "failed"
	AIOperationStatusCanceled  = "canceled"

	AIAttemptStatusRunning   = "running"
	AIAttemptStatusSucceeded = "succeeded"
	AIAttemptStatusFailed    = "failed"
	AIAttemptStatusSkipped   = "skipped"
	AIAttemptStatusCanceled  = "canceled"

	AIAttemptDispatchPending          = "pending"
	AIAttemptDispatchPrepared         = "prepared"
	AIAttemptDispatchSubmitted        = "submitted"
	AIAttemptDispatchAccepted         = "accepted"
	AIAttemptDispatchUnknown          = "unknown"
	AIAttemptDispatchProvenNotCreated = "proven_not_created"

	BillingLedgerEntryTypeUsage = "usage"
	BillingLedgerStatusApplied  = "applied"

	OutboxStatusPending    = "pending"
	OutboxStatusPublishing = "publishing"
	OutboxStatusPublished  = "published"
	OutboxStatusDeadLetter = "dead_letter"
	OutboxEventUsage       = "usage.applied"

	OutboxDefaultMaxAttempts = 20
)

type AIOperation struct {
	ID                       string     `json:"id"`
	ProfileScope             string     `json:"profile_scope"`
	TenantID                 string     `json:"tenant_id"`
	CredentialID             string     `json:"credential_id"`
	CredentialSource         string     `json:"credential_source"`
	IntegrationID            string     `json:"integration_id"`
	PrincipalType            string     `json:"principal_type"`
	PrincipalID              string     `json:"principal_id"`
	ExternalSubjectReference string     `json:"external_subject_reference"`
	ClientRequestID          string     `json:"client_request_id"`
	RequestFingerprint       string     `json:"request_fingerprint"`
	IdempotencyKey           string     `json:"idempotency_key,omitempty"`
	Protocol                 string     `json:"protocol"`
	Operation                string     `json:"operation"`
	Modality                 string     `json:"modality"`
	Lane                     string     `json:"lane"`
	Model                    string     `json:"model"`
	Status                   string     `json:"status"`
	ErrorType                string     `json:"error_type,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
}

type AIAttempt struct {
	ID                  string     `json:"id"`
	OperationID         string     `json:"operation_id"`
	AttemptNumber       int        `json:"attempt_number"`
	ProviderID          string     `json:"provider_id"`
	ProviderAccountID   string     `json:"provider_account_id"`
	RouteID             string     `json:"route_id"`
	UpstreamModel       string     `json:"upstream_model"`
	Status              string     `json:"status"`
	ErrorType           string     `json:"error_type,omitempty"`
	DispatchState       string     `json:"dispatch_state"`
	DispatchVersion     int        `json:"dispatch_version"`
	DispatchKey         string     `json:"-"`
	DispatchIntentJSON  string     `json:"-"`
	DispatchSubmittedAt *time.Time `json:"dispatch_submitted_at,omitempty"`
	ProviderTaskID      string     `json:"-"`
	ProviderRequestID   string     `json:"-"`
	ProviderTaskStatus  string     `json:"provider_task_status,omitempty"`
	ProviderAcceptedAt  *time.Time `json:"provider_accepted_at,omitempty"`
	LastReconciledAt    *time.Time `json:"last_reconciled_at,omitempty"`
	ReconcileAfter      *time.Time `json:"reconcile_after,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

// ProviderDispatchIntent contains only replay-safe routing facts. It must not
// contain provider secrets, prompts, media bytes, or client access tokens.
type ProviderDispatchIntent struct {
	Version            int    `json:"version"`
	AttemptID          string `json:"attempt_id"`
	OperationID        string `json:"operation_id"`
	DispatchKey        string `json:"dispatch_key"`
	RequestFingerprint string `json:"request_fingerprint"`
	ProviderID         string `json:"provider_id"`
	ProviderAccountID  string `json:"provider_account_id"`
	RouteID            string `json:"route_id"`
	UpstreamModel      string `json:"upstream_model"`
}

type ProviderTaskReference struct {
	ProviderTaskID    string
	ProviderRequestID string
	Status            string
	AcceptedAt        time.Time
}

type BillingLedgerEntry struct {
	ID                 string    `json:"id"`
	OperationID        string    `json:"operation_id"`
	AttemptID          string    `json:"attempt_id"`
	UsageVersion       int       `json:"usage_version"`
	UsageRecordID      string    `json:"usage_record_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	EntryType          string    `json:"entry_type"`
	AmountCents        int       `json:"amount_cents"`
	Currency           string    `json:"currency"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
}

type TransactionalOutboxEvent struct {
	ID            string     `json:"id"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   string     `json:"aggregate_id"`
	EventType     string     `json:"event_type"`
	EventVersion  int        `json:"event_version"`
	Status        string     `json:"status"`
	AvailableAt   time.Time  `json:"available_at"`
	AttemptCount  int        `json:"attempt_count"`
	MaxAttempts   int        `json:"max_attempts"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	LeaseToken    string     `json:"-"`
	LastError     string     `json:"last_error,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	PayloadJSON string `json:"-"`
}
