package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrArtifactNotFound      = errors.New("artifact not found")
	ErrArtifactStateConflict = errors.New("artifact state changed concurrently")
	ErrArtifactStoreRequired = errors.New("artifact store is required")
	ErrArtifactTooLarge      = errors.New("artifact exceeds the configured size limit")
	ErrArtifactIntegrity     = errors.New("artifact integrity verification failed")
	ErrArtifactUnavailable   = errors.New("artifact content is unavailable")
)

const (
	ArtifactRoleInput             = "input"
	ArtifactRolePreview           = "preview"
	ArtifactRoleFinal             = "final"
	ArtifactRoleDerived           = "derived"
	ArtifactRoleProviderReference = "provider_reference"
	ArtifactRoleMetadata          = "metadata"

	ArtifactStatusPending         = "pending"
	ArtifactStatusUploading       = "uploading"
	ArtifactStatusReady           = "ready"
	ArtifactStatusFailed          = "failed"
	ArtifactStatusDelivering      = "delivering"
	ArtifactStatusDelivered       = "delivered"
	ArtifactStatusDeliveryFailed  = "delivery_failed"
	ArtifactStatusDeleteRequested = "delete_requested"
	ArtifactStatusDeleting        = "deleting"
	ArtifactStatusDeleted         = "deleted"
	ArtifactStatusDeleteFailed    = "delete_failed"
	ArtifactStatusExpired         = "expired"

	ArtifactStoreDriverNone   = "none"
	ArtifactStoreDriverMemory = "memory"
	ArtifactStoreDriverLocal  = "local"
	ArtifactStoreDriverS3     = "s3"
	ArtifactStoreDriverOSS    = "oss"

	ArtifactOutboxAggregate = "artifact"
	ArtifactDefaultMaxBytes = int64(10 << 30)
	ArtifactDefaultTTL      = 24 * time.Hour
)

type Artifact struct {
	ID                       string     `json:"id"`
	OperationID              string     `json:"operation_id"`
	JobID                    string     `json:"job_id,omitempty"`
	AttemptID                string     `json:"attempt_id,omitempty"`
	SourceArtifactID         string     `json:"source_artifact_id,omitempty"`
	ProfileScope             string     `json:"-"`
	TenantID                 string     `json:"-"`
	IntegrationID            string     `json:"-"`
	PrincipalType            string     `json:"-"`
	PrincipalID              string     `json:"-"`
	ExternalSubjectReference string     `json:"-"`
	Role                     string     `json:"role"`
	Policy                   string     `json:"policy"`
	Status                   string     `json:"status"`
	StatusVersion            int        `json:"status_version"`
	MediaType                string     `json:"media_type,omitempty"`
	SizeBytes                int64      `json:"size_bytes"`
	SHA256                   string     `json:"sha256,omitempty"`
	StoreDriver              string     `json:"store_driver"`
	StoreKey                 string     `json:"-"`
	ExternalReference        string     `json:"-"`
	ErrorType                string     `json:"error_type,omitempty"`
	RetainUntil              time.Time  `json:"retain_until"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	ReadyAt                  *time.Time `json:"ready_at,omitempty"`
	DeliveredAt              *time.Time `json:"delivered_at,omitempty"`
	DeletedAt                *time.Time `json:"deleted_at,omitempty"`
}

type ArtifactEvent struct {
	ID         string    `json:"id"`
	ArtifactID string    `json:"artifact_id"`
	Version    int       `json:"version"`
	EventType  string    `json:"event_type"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ArtifactQuery struct {
	Owner        *ArtifactOwner
	OperationID  string
	JobID        string
	Status       string
	RetainBefore *time.Time
	Limit        int
	Offset       int
}

type ArtifactContentUpdate struct {
	MediaType         string
	SizeBytes         int64
	SHA256            string
	StoreDriver       string
	StoreKey          string
	ExternalReference string
}

type ArtifactTransitionInput struct {
	ArtifactID      string
	ExpectedVersion int
	ToStatus        string
	Reason          string
	Content         *ArtifactContentUpdate
}

type ArtifactCreateInput struct {
	OperationID       string
	JobID             string
	AttemptID         string
	SourceArtifactID  string
	Role              string
	Policy            string
	MediaType         string
	StoreDriver       string
	ExternalReference string
	ExpectedSizeBytes int64
	ExpectedSHA256    string
	MaxBytes          int64
	RetainUntil       time.Time
}

type ArtifactOwner = AIJobOwner

func artifactOwnerFromOperation(operation AIOperation) ArtifactOwner {
	return ArtifactOwner{
		ProfileScope: operation.ProfileScope, TenantID: operation.TenantID, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID,
		ExternalSubjectReference: operation.ExternalSubjectReference,
	}
}

func artifactOwnerMatches(artifact Artifact, owner ArtifactOwner) bool {
	return artifact.ProfileScope == owner.ProfileScope && artifact.TenantID == owner.TenantID &&
		artifact.IntegrationID == owner.IntegrationID && artifact.PrincipalType == owner.PrincipalType &&
		artifact.PrincipalID == owner.PrincipalID && artifact.ExternalSubjectReference == owner.ExternalSubjectReference
}

func artifactOwnerFromAuth(authOwner AIJobOwner) ArtifactOwner {
	return ArtifactOwner(authOwner)
}

func validArtifactRole(role string) bool {
	return oneOf(role, ArtifactRoleInput, ArtifactRolePreview, ArtifactRoleFinal, ArtifactRoleDerived, ArtifactRoleProviderReference, ArtifactRoleMetadata)
}

func validArtifactPolicy(policy string) bool {
	return oneOf(policy, GatewayArtifactPolicyProxyOnly, GatewayArtifactPolicyTemporary, GatewayArtifactPolicyManaged, GatewayArtifactPolicyCustomerSink, GatewayArtifactPolicyMetadataOnly)
}

func artifactStatusTransitionAllowed(fromStatus, toStatus string) bool {
	switch fromStatus {
	case ArtifactStatusPending:
		return oneOf(toStatus, ArtifactStatusUploading, ArtifactStatusReady, ArtifactStatusFailed, ArtifactStatusDeleteRequested)
	case ArtifactStatusUploading:
		return oneOf(toStatus, ArtifactStatusReady, ArtifactStatusFailed, ArtifactStatusDeleteRequested)
	case ArtifactStatusReady:
		return oneOf(toStatus, ArtifactStatusDelivering, ArtifactStatusDeleteRequested, ArtifactStatusExpired)
	case ArtifactStatusDelivering:
		return oneOf(toStatus, ArtifactStatusDelivered, ArtifactStatusDeliveryFailed, ArtifactStatusDeleteRequested)
	case ArtifactStatusDeliveryFailed:
		return oneOf(toStatus, ArtifactStatusDelivering, ArtifactStatusDeleteRequested, ArtifactStatusExpired)
	case ArtifactStatusDelivered, ArtifactStatusFailed, ArtifactStatusExpired:
		return toStatus == ArtifactStatusDeleteRequested
	case ArtifactStatusDeleteRequested, ArtifactStatusDeleteFailed:
		return toStatus == ArtifactStatusDeleting
	case ArtifactStatusDeleting:
		return oneOf(toStatus, ArtifactStatusDeleted, ArtifactStatusDeleteFailed)
	default:
		return false
	}
}

func prepareArtifactTransition(artifact Artifact, toStatus, reason string, at time.Time) (Artifact, ArtifactEvent, TransactionalOutboxEvent, error) {
	toStatus = strings.TrimSpace(toStatus)
	if !artifactStatusTransitionAllowed(artifact.Status, toStatus) {
		return Artifact{}, ArtifactEvent{}, TransactionalOutboxEvent{}, fmt.Errorf("invalid artifact transition %s -> %s", artifact.Status, toStatus)
	}
	fromStatus := artifact.Status
	artifact.Status = toStatus
	artifact.StatusVersion++
	artifact.ErrorType = ""
	if oneOf(toStatus, ArtifactStatusFailed, ArtifactStatusDeliveryFailed, ArtifactStatusDeleteFailed) {
		artifact.ErrorType = strings.TrimSpace(reason)
	}
	artifact.UpdatedAt = at.UTC()
	if toStatus == ArtifactStatusReady {
		artifact.ReadyAt = timePointer(at.UTC())
	}
	if toStatus == ArtifactStatusDelivered {
		artifact.DeliveredAt = timePointer(at.UTC())
	}
	if toStatus == ArtifactStatusDeleted {
		artifact.DeletedAt = timePointer(at.UTC())
		artifact.StoreKey = ""
		artifact.ExternalReference = ""
	}
	event, outbox, err := newArtifactEventRecords(artifact, fromStatus, reason, at)
	return artifact, event, outbox, err
}

func applyArtifactContentUpdate(artifact *Artifact, update ArtifactContentUpdate) error {
	driver := strings.TrimSpace(update.StoreDriver)
	key := strings.TrimSpace(update.StoreKey)
	if update.SizeBytes < 0 || !oneOf(driver, ArtifactStoreDriverNone, ArtifactStoreDriverMemory, ArtifactStoreDriverLocal, ArtifactStoreDriverS3, ArtifactStoreDriverOSS) {
		return errors.New("invalid artifact content update")
	}
	if driver == ArtifactStoreDriverNone && key != "" {
		return errors.New("artifact without a store driver cannot have a store key")
	}
	if driver != ArtifactStoreDriverNone && !validArtifactStoreKey(key) {
		return errors.New("stored artifact requires a valid store key")
	}
	artifact.MediaType = strings.TrimSpace(update.MediaType)
	artifact.SizeBytes = update.SizeBytes
	artifact.SHA256 = strings.ToLower(strings.TrimSpace(update.SHA256))
	artifact.StoreDriver = driver
	artifact.StoreKey = key
	artifact.ExternalReference = strings.TrimSpace(update.ExternalReference)
	return nil
}

func newArtifactEventRecords(artifact Artifact, fromStatus, reason string, at time.Time) (ArtifactEvent, TransactionalOutboxEvent, error) {
	if strings.TrimSpace(artifact.ID) == "" || artifact.StatusVersion <= 0 || strings.TrimSpace(artifact.Status) == "" {
		return ArtifactEvent{}, TransactionalOutboxEvent{}, errors.New("invalid artifact event state")
	}
	eventType := "artifact." + strings.ReplaceAll(artifact.Status, "_", ".")
	event := ArtifactEvent{
		ID: "artifact_event_" + randomID(12), ArtifactID: artifact.ID, Version: artifact.StatusVersion,
		EventType: eventType, FromStatus: fromStatus, ToStatus: artifact.Status, Reason: strings.TrimSpace(reason), CreatedAt: at.UTC(),
	}
	payload, err := json.Marshal(map[string]any{
		"artifact_id": artifact.ID,
		"status":      artifact.Status,
		"version":     artifact.StatusVersion,
	})
	if err != nil {
		return ArtifactEvent{}, TransactionalOutboxEvent{}, fmt.Errorf("marshal artifact outbox payload: %w", err)
	}
	outbox := TransactionalOutboxEvent{
		ID: "outbox_" + randomID(12), AggregateType: ArtifactOutboxAggregate, AggregateID: artifact.ID,
		EventType: eventType, EventVersion: artifact.StatusVersion, PayloadJSON: string(payload), Status: OutboxStatusPending,
		AvailableAt: at.UTC(), MaxAttempts: OutboxDefaultMaxAttempts, CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}
	return event, outbox, nil
}

func validateArtifactAdmission(artifact Artifact, event ArtifactEvent, outbox TransactionalOutboxEvent) error {
	if strings.TrimSpace(artifact.ID) == "" || strings.TrimSpace(artifact.OperationID) == "" || !validArtifactRole(artifact.Role) ||
		!validArtifactPolicy(artifact.Policy) || artifact.Status != ArtifactStatusPending || artifact.StatusVersion != 1 ||
		artifact.SizeBytes < 0 || artifact.RetainUntil.IsZero() || artifact.CreatedAt.IsZero() || artifact.UpdatedAt.IsZero() ||
		event.ArtifactID != artifact.ID || event.Version != artifact.StatusVersion || event.ToStatus != artifact.Status ||
		outbox.AggregateType != ArtifactOutboxAggregate || outbox.AggregateID != artifact.ID || outbox.EventVersion != artifact.StatusVersion {
		return errors.New("invalid artifact admission records")
	}
	return nil
}

func artifactDownloadable(artifact Artifact, now time.Time) bool {
	return oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered) && artifact.RetainUntil.After(now) &&
		artifact.StoreDriver != ArtifactStoreDriverNone && strings.TrimSpace(artifact.StoreKey) != ""
}
