package controlplane

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrAIJobIdempotencyRequired = errors.New("durable job creation requires an idempotency key")
	ErrAIJobNotCancelable       = errors.New("ai job is not cancelable")
	ErrAIJobStateConflict       = errors.New("ai job state changed concurrently")
	ErrAIJobCapabilityMismatch  = errors.New("gateway model does not support the requested job capability")
)

const (
	AIJobStatusAccepted    = "accepted"
	AIJobStatusQueued      = "queued"
	AIJobStatusDispatching = "dispatching"
	AIJobStatusRunning     = "running"
	AIJobStatusCanceling   = "canceling"
	AIJobStatusCanceled    = "canceled"
	AIJobStatusSucceeded   = "succeeded"
	AIJobStatusFailed      = "failed"
	AIJobStatusUnknown     = "unknown"
	AIJobStatusExpired     = "expired"

	AIJobEventQueued          = "job.queued"
	AIJobEventScheduled       = "job.scheduled"
	AIJobEventRunning         = "job.running"
	AIJobEventCancelling      = "job.cancelling"
	AIJobEventCancelled       = "job.cancelled"
	AIJobEventCompleted       = "job.completed"
	AIJobEventFailed          = "job.failed"
	AIJobEventUnknown         = "job.unknown"
	AIJobEventExpired         = "job.expired"
	AIJobEventLeaseReassigned = "job.lease.reassigned"
	AIJobOutboxAggregate      = "ai_job"
	AIJobDefaultTTL           = 24 * time.Hour
	AIJobDefaultPollAfter     = 2
	AIJobDefaultRetryAfter    = 5 * time.Second
)

type AIJob struct {
	ID                       string     `json:"id"`
	OperationID              string     `json:"operation_id"`
	ProfileScope             string     `json:"-"`
	TenantID                 string     `json:"-"`
	CredentialID             string     `json:"-"`
	CredentialSource         string     `json:"-"`
	IntegrationID            string     `json:"-"`
	PrincipalType            string     `json:"-"`
	PrincipalID              string     `json:"-"`
	ExternalSubjectReference string     `json:"-"`
	RequestFingerprint       string     `json:"-"`
	IdempotencyKey           string     `json:"-"`
	Protocol                 string     `json:"protocol"`
	Operation                string     `json:"operation"`
	Modality                 string     `json:"modality"`
	Model                    string     `json:"model"`
	ArtifactPolicy           string     `json:"artifact_policy"`
	RequestPayload           string     `json:"-"`
	RequestPayloadCiphertext string     `json:"-"`
	Status                   string     `json:"status"`
	StatusVersion            int        `json:"status_version"`
	Priority                 int        `json:"priority"`
	NextEligibleAt           time.Time  `json:"next_eligible_at"`
	QueueLeaseUntil          *time.Time `json:"-"`
	QueueLeaseToken          string     `json:"-"`
	QueueWorkerID            string     `json:"-"`
	FenceToken               int64      `json:"-"`
	ErrorType                string     `json:"error_type,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
	ExpiresAt                time.Time  `json:"expires_at"`
}

type AIJobEvent struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	Version    int       `json:"version"`
	EventType  string    `json:"event_type"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// AIJobOwner is the current authorization identity for a job resource. The
// credential ID is intentionally absent so a rotated key for the same
// principal can continue to access work created before rotation.
type AIJobOwner struct {
	ProfileScope             string
	TenantID                 string
	IntegrationID            string
	PrincipalType            string
	PrincipalID              string
	ExternalSubjectReference string
}

func aiJobOwnerMatches(job AIJob, owner AIJobOwner) bool {
	return job.ProfileScope == owner.ProfileScope &&
		job.TenantID == owner.TenantID &&
		job.IntegrationID == owner.IntegrationID &&
		job.PrincipalType == owner.PrincipalType &&
		job.PrincipalID == owner.PrincipalID &&
		job.ExternalSubjectReference == owner.ExternalSubjectReference
}

func aiJobStatusTransitionAllowed(fromStatus, toStatus, reason string) bool {
	switch fromStatus {
	case AIJobStatusAccepted:
		return toStatus == AIJobStatusQueued || toStatus == AIJobStatusCanceled
	case AIJobStatusQueued:
		return toStatus == AIJobStatusDispatching || toStatus == AIJobStatusCanceled
	case AIJobStatusDispatching:
		return oneOf(toStatus, AIJobStatusQueued, AIJobStatusRunning, AIJobStatusUnknown, AIJobStatusCanceling)
	case AIJobStatusRunning:
		return oneOf(toStatus, AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusCanceling, AIJobStatusUnknown)
	case AIJobStatusCanceling:
		return oneOf(toStatus, AIJobStatusCanceled, AIJobStatusSucceeded)
	case AIJobStatusUnknown:
		if toStatus == AIJobStatusQueued {
			return strings.TrimSpace(reason) == "proven_not_created"
		}
		return oneOf(toStatus, AIJobStatusRunning, AIJobStatusSucceeded, AIJobStatusFailed)
	case AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusCanceled:
		return toStatus == AIJobStatusExpired
	default:
		return false
	}
}

func aiJobEventType(status string) string {
	switch status {
	case AIJobStatusQueued:
		return AIJobEventQueued
	case AIJobStatusDispatching:
		return AIJobEventScheduled
	case AIJobStatusRunning:
		return AIJobEventRunning
	case AIJobStatusCanceling:
		return AIJobEventCancelling
	case AIJobStatusCanceled:
		return AIJobEventCancelled
	case AIJobStatusSucceeded:
		return AIJobEventCompleted
	case AIJobStatusFailed:
		return AIJobEventFailed
	case AIJobStatusUnknown:
		return AIJobEventUnknown
	case AIJobStatusExpired:
		return AIJobEventExpired
	default:
		return ""
	}
}

func aiJobTerminalStatus(status string) bool {
	return oneOf(status, AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusCanceled, AIJobStatusExpired)
}
