package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

const artifactIngestLease = 2 * time.Minute

func (s *Service) SetArtifactStore(store ArtifactStore) error {
	if store == nil {
		return errors.New("artifact store is required")
	}
	driver := strings.TrimSpace(store.Driver())
	if !oneOf(driver, ArtifactStoreDriverMemory, ArtifactStoreDriverLocal, ArtifactStoreDriverS3, ArtifactStoreDriverOSS) {
		return errors.New("artifact store driver is not supported")
	}
	s.artifactStoreMu.Lock()
	defer s.artifactStoreMu.Unlock()
	s.artifactStores[driver] = store
	s.artifactPrimaryDriver = driver
	return nil
}

func (s *Service) artifactStore(driver string) (ArtifactStore, bool) {
	s.artifactStoreMu.RLock()
	defer s.artifactStoreMu.RUnlock()
	store, found := s.artifactStores[strings.TrimSpace(driver)]
	return store, found
}

func (s *Service) primaryArtifactStoreDriver() (string, bool) {
	s.artifactStoreMu.RLock()
	defer s.artifactStoreMu.RUnlock()
	driver := strings.TrimSpace(s.artifactPrimaryDriver)
	_, found := s.artifactStores[driver]
	return driver, driver != "" && found
}

// PrimaryArtifactStoreDriver exposes the configured content backend to public
// upload admission without exposing the underlying store or credentials.
func (s *Service) PrimaryArtifactStoreDriver() (string, bool) {
	return s.primaryArtifactStoreDriver()
}

func (s *Service) CreateArtifactFromReader(ctx context.Context, input ArtifactCreateInput, body io.Reader) (Artifact, error) {
	operation, found, err := s.repo.FindAIOperation(ctx, strings.TrimSpace(input.OperationID))
	if err != nil {
		return Artifact{}, err
	}
	if !found {
		return Artifact{}, ErrArtifactNotFound
	}
	policy, err := s.resolveArtifactPolicyAndReferences(ctx, operation, input)
	if err != nil {
		return Artifact{}, err
	}
	input.Policy = policy
	if err := validateArtifactCreateInput(input, body, s.nowUTC()); err != nil {
		return Artifact{}, err
	}
	now := s.nowUTC()
	retainUntil := input.RetainUntil.UTC()
	if input.RetainUntil.IsZero() {
		retainUntil = now.Add(ArtifactDefaultTTL)
	}
	artifactID := strings.TrimSpace(input.ID)
	if artifactID == "" {
		artifactID = "artifact_" + randomID(12)
	}
	if !validArtifactID(artifactID) {
		return Artifact{}, errors.New("invalid artifact id")
	}
	artifact := Artifact{
		ID: artifactID, OperationID: operation.ID, JobID: strings.TrimSpace(input.JobID),
		AttemptID: strings.TrimSpace(input.AttemptID), SourceArtifactID: strings.TrimSpace(input.SourceArtifactID),
		ProfileScope: operation.ProfileScope, TenantID: operation.TenantID, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		Role: strings.TrimSpace(input.Role), Policy: policy, Status: ArtifactStatusPending, StatusVersion: 1,
		MediaType: strings.TrimSpace(input.MediaType), StoreDriver: ArtifactStoreDriverNone,
		ExternalReference: strings.TrimSpace(input.ExternalReference), RetainUntil: retainUntil, CreatedAt: now, UpdatedAt: now,
	}
	event, outbox, err := newArtifactEventRecords(artifact, "", "", now)
	if err != nil {
		return Artifact{}, err
	}
	if err := s.repo.CreateArtifact(ctx, artifact, event, outbox); err != nil {
		return Artifact{}, err
	}
	if body == nil {
		sizeBytes := input.ExpectedSizeBytes
		if sizeBytes < 0 {
			sizeBytes = 0
		}
		return s.transitionArtifact(ctx, artifact, ArtifactStatusReady, "", &ArtifactContentUpdate{
			MediaType: artifact.MediaType, SizeBytes: sizeBytes, SHA256: strings.ToLower(strings.TrimSpace(input.ExpectedSHA256)), StoreDriver: ArtifactStoreDriverNone,
			ExternalReference: artifact.ExternalReference,
		})
	}
	return s.storeArtifactContent(ctx, artifact, input, body)
}

// ValidateInputArtifactsForAuth verifies artifact references embedded in a
// canonical request before any Operation, Job, billing hold or provider
// dispatch is created. The provider never becomes an authorization oracle.
func (s *Service) ValidateInputArtifactsForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) error {
	if len(request.Payload) == 0 {
		return nil
	}
	var envelope struct {
		Input map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(request.Payload, &envelope); err != nil {
		return gatewaycore.ErrInvalidCanonicalRequest
	}
	ids := make([]string, 0, 4)
	appendID := func(raw json.RawMessage) error {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil || strings.TrimSpace(id) == "" {
			return gatewaycore.ErrInvalidCanonicalRequest
		}
		ids = append(ids, strings.TrimSpace(id))
		return nil
	}
	for _, key := range []string{"artifact_id", "input_artifact_id"} {
		if raw, exists := envelope.Input[key]; exists {
			if err := appendID(raw); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"artifact_ids", "input_artifact_ids"} {
		raw, exists := envelope.Input[key]
		if !exists {
			continue
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil || len(values) > 32 {
			return gatewaycore.ErrInvalidCanonicalRequest
		}
		for _, value := range values {
			if err := appendID(value); err != nil {
				return err
			}
		}
	}
	owner := ArtifactOwner(aiJobOwnerFromAuth(auth))
	for _, id := range ids {
		artifact, found, err := s.repo.FindOwnedArtifact(ctx, id, owner)
		if err != nil {
			return err
		}
		if !found || artifact.Role != ArtifactRoleInput || !oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered) || !artifactDownloadable(artifact, s.nowUTC()) {
			return ErrArtifactNotFound
		}
	}
	return nil
}

func (s *Service) resolveArtifactPolicyAndReferences(ctx context.Context, operation AIOperation, input ArtifactCreateInput) (string, error) {
	policy := artifactPolicySnapshot(operation.ArtifactPolicy)
	requestedPolicy := strings.TrimSpace(input.Policy)
	if requestedPolicy != "" && requestedPolicy != policy {
		return "", errors.New("artifact policy must match the operation policy snapshot")
	}
	if strings.TrimSpace(input.JobID) != "" {
		job, found, err := s.repo.FindAIJob(ctx, strings.TrimSpace(input.JobID))
		if err != nil {
			return "", err
		}
		if !found || job.OperationID != operation.ID || !aiJobOwnerMatches(job, artifactOwnerFromOperation(operation)) {
			return "", ErrArtifactNotFound
		}
		if job.ArtifactPolicy != policy {
			return "", errors.New("artifact policy snapshots do not match")
		}
	}
	if strings.TrimSpace(input.AttemptID) != "" {
		attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(input.AttemptID))
		if err != nil {
			return "", err
		}
		if !found || attempt.OperationID != operation.ID {
			return "", ErrArtifactNotFound
		}
	}
	if strings.TrimSpace(input.SourceArtifactID) != "" {
		source, found, err := s.repo.FindArtifact(ctx, strings.TrimSpace(input.SourceArtifactID))
		if err != nil {
			return "", err
		}
		if !found || !artifactOwnerMatches(source, artifactOwnerFromOperation(operation)) {
			return "", ErrArtifactNotFound
		}
	}
	if !validArtifactPolicy(policy) {
		return "", errors.New("artifact policy is required")
	}
	return policy, nil
}

func validateArtifactCreateInput(input ArtifactCreateInput, body io.Reader, now time.Time) error {
	if !validArtifactRole(strings.TrimSpace(input.Role)) || input.ExpectedSizeBytes < -1 || input.MaxBytes < 0 {
		return errors.New("invalid artifact create input")
	}
	if !input.RetainUntil.IsZero() && !input.RetainUntil.After(now) {
		return errors.New("artifact retain_until must be in the future")
	}
	driver := strings.TrimSpace(input.StoreDriver)
	if body == nil {
		if driver != "" && driver != ArtifactStoreDriverNone {
			return errors.New("metadata artifact cannot select a content store")
		}
		if !oneOf(strings.TrimSpace(input.Role), ArtifactRoleMetadata, ArtifactRoleProviderReference) &&
			!oneOf(input.Policy, GatewayArtifactPolicyMetadataOnly, GatewayArtifactPolicyProxyOnly, GatewayArtifactPolicyCustomerSink) {
			return ErrArtifactStoreRequired
		}
		if input.Role == ArtifactRoleProviderReference && strings.TrimSpace(input.ExternalReference) == "" {
			return errors.New("provider reference artifact requires an external reference")
		}
		return nil
	}
	if oneOf(input.Policy, GatewayArtifactPolicyMetadataOnly, GatewayArtifactPolicyProxyOnly) {
		return errors.New("artifact policy does not permit retained content")
	}
	if !oneOf(driver, ArtifactStoreDriverMemory, ArtifactStoreDriverLocal, ArtifactStoreDriverS3, ArtifactStoreDriverOSS) {
		return ErrArtifactStoreRequired
	}
	return nil
}

func (s *Service) storeArtifactContent(ctx context.Context, artifact Artifact, input ArtifactCreateInput, body io.Reader) (Artifact, error) {
	driver := strings.TrimSpace(input.StoreDriver)
	store, found := s.artifactStore(driver)
	if !found {
		failed, _ := s.transitionArtifact(ctx, artifact, ArtifactStatusFailed, "store_unavailable", nil)
		return failed, ErrArtifactStoreRequired
	}
	storeKey := artifact.ID + "/content"
	uploading, err := s.transitionArtifact(ctx, artifact, ArtifactStatusUploading, "", &ArtifactContentUpdate{
		MediaType: strings.TrimSpace(input.MediaType), SizeBytes: 0, StoreDriver: driver, StoreKey: storeKey,
		ExternalReference: strings.TrimSpace(input.ExternalReference),
	})
	if err != nil {
		return Artifact{}, err
	}
	return s.writeArtifactContent(ctx, uploading, input, body, store)
}

func (s *Service) resumeArtifactContent(ctx context.Context, artifact Artifact, input ArtifactCreateInput, body io.Reader) (Artifact, error) {
	if body == nil {
		return Artifact{}, errors.New("artifact content reader is required")
	}
	if oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered) {
		return artifact, nil
	}
	if !oneOf(artifact.Status, ArtifactStatusPending, ArtifactStatusUploading, ArtifactStatusFailed) {
		return Artifact{}, ErrArtifactUnavailable
	}
	if artifact.Status == ArtifactStatusUploading && artifact.UpdatedAt.After(s.nowUTC().Add(-artifactIngestLease)) {
		return Artifact{}, ErrArtifactIngestInProgress
	}
	driver := strings.TrimSpace(input.StoreDriver)
	if artifact.StoreDriver != ArtifactStoreDriverNone && strings.TrimSpace(artifact.StoreDriver) != "" {
		driver = artifact.StoreDriver
	}
	store, found := s.artifactStore(driver)
	if !found {
		return Artifact{}, ErrArtifactStoreRequired
	}
	storeKey := artifact.ID + "/content"
	uploading, err := s.transitionArtifact(ctx, artifact, ArtifactStatusUploading, "ingest_claimed", &ArtifactContentUpdate{
		MediaType: strings.TrimSpace(input.MediaType), SizeBytes: 0, StoreDriver: driver, StoreKey: storeKey,
		ExternalReference: strings.TrimSpace(input.ExternalReference),
	})
	if err != nil {
		return Artifact{}, err
	}
	input.StoreDriver = driver
	return s.writeArtifactContent(ctx, uploading, input, body, store)
}

func (s *Service) writeArtifactContent(ctx context.Context, uploading Artifact, input ArtifactCreateInput, body io.Reader, store ArtifactStore) (Artifact, error) {
	storeKey := uploading.StoreKey
	driver := uploading.StoreDriver
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = ArtifactDefaultMaxBytes
	}
	hasher := sha256.New()
	counter := &artifactByteCounter{}
	limited := io.LimitReader(body, maxBytes+1)
	reader := io.TeeReader(limited, io.MultiWriter(hasher, counter))
	heartbeat := s.startArtifactIngestHeartbeat(ctx, uploading)
	_, storeErr := store.Put(heartbeat.Context(), storeKey, reader, -1, strings.TrimSpace(input.MediaType))
	uploading, heartbeatErr := heartbeat.Stop()
	if heartbeatErr != nil {
		return Artifact{}, errors.Join(storeErr, heartbeatErr)
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	content := &ArtifactContentUpdate{
		MediaType: strings.TrimSpace(input.MediaType), SizeBytes: counter.total, SHA256: actualSHA,
		StoreDriver: driver, StoreKey: storeKey, ExternalReference: strings.TrimSpace(input.ExternalReference),
	}
	verificationErr := verifyArtifactContent(input, counter.total, actualSHA, maxBytes)
	if storeErr != nil || verificationErr != nil {
		_ = store.Delete(ctx, storeKey)
		reason := artifactFailureReason(storeErr, verificationErr)
		failed, transitionErr := s.transitionArtifact(ctx, uploading, ArtifactStatusFailed, reason, content)
		if transitionErr != nil {
			return Artifact{}, transitionErr
		}
		if verificationErr != nil {
			return failed, verificationErr
		}
		return failed, storeErr
	}
	return s.transitionArtifact(ctx, uploading, ArtifactStatusReady, "", content)
}

type artifactStatusHeartbeat struct {
	service *Service
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	status  string
	reason  string
	lease   time.Duration

	mu       sync.Mutex
	artifact Artifact
	err      error
}

func (s *Service) startArtifactIngestHeartbeat(ctx context.Context, artifact Artifact) *artifactStatusHeartbeat {
	return s.startArtifactStatusHeartbeat(ctx, artifact, ArtifactStatusUploading, "ingest_heartbeat", artifactIngestLease)
}

func (s *Service) startArtifactStatusHeartbeat(ctx context.Context, artifact Artifact, status, reason string, lease time.Duration) *artifactStatusHeartbeat {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	if lease <= 0 {
		lease = artifactIngestLease
	}
	heartbeat := &artifactStatusHeartbeat{
		service: s, ctx: heartbeatCtx, cancel: cancel, done: make(chan struct{}), artifact: artifact,
		status: strings.TrimSpace(status), reason: strings.TrimSpace(reason), lease: lease,
	}
	go heartbeat.run()
	return heartbeat
}

func (h *artifactStatusHeartbeat) Context() context.Context { return h.ctx }

func (h *artifactStatusHeartbeat) run() {
	defer close(h.done)
	ticker := time.NewTicker(h.lease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			current := h.artifact
			h.mu.Unlock()
			updated, changed, err := h.service.repo.TransitionArtifact(h.ctx, ArtifactTransitionInput{
				ArtifactID: current.ID, ExpectedVersion: current.StatusVersion, ToStatus: h.status, Reason: h.reason,
			}, h.service.nowUTC())
			if err != nil || !changed {
				if err == nil {
					err = ErrArtifactStateConflict
				}
				h.mu.Lock()
				h.err = err
				h.mu.Unlock()
				h.cancel()
				return
			}
			h.mu.Lock()
			h.artifact = updated
			h.mu.Unlock()
		}
	}
}

func (h *artifactStatusHeartbeat) Stop() (Artifact, error) {
	h.cancel()
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.artifact, h.err
}

func validArtifactID(id string) bool {
	if !strings.HasPrefix(id, "artifact_") || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

type artifactByteCounter struct {
	total int64
}

func (c *artifactByteCounter) Write(p []byte) (int, error) {
	c.total += int64(len(p))
	return len(p), nil
}

func verifyArtifactContent(input ArtifactCreateInput, actualSize int64, actualSHA string, maxBytes int64) error {
	if actualSize > maxBytes {
		return ErrArtifactTooLarge
	}
	if input.ExpectedSizeBytes > 0 && actualSize != input.ExpectedSizeBytes {
		return ErrArtifactIntegrity
	}
	expectedSHA := strings.ToLower(strings.TrimSpace(input.ExpectedSHA256))
	if expectedSHA != "" && expectedSHA != actualSHA {
		return ErrArtifactIntegrity
	}
	return nil
}

func artifactFailureReason(storeErr, verificationErr error) string {
	switch {
	case errors.Is(verificationErr, ErrArtifactTooLarge):
		return "too_large"
	case verificationErr != nil:
		return "integrity_failed"
	case storeErr != nil:
		return "store_write_failed"
	default:
		return "artifact_failed"
	}
}

func (s *Service) transitionArtifact(ctx context.Context, artifact Artifact, toStatus, reason string, content *ArtifactContentUpdate) (Artifact, error) {
	updated, changed, err := s.repo.TransitionArtifact(ctx, ArtifactTransitionInput{
		ArtifactID: artifact.ID, ExpectedVersion: artifact.StatusVersion, ToStatus: toStatus, Reason: reason, Content: content,
	}, s.nowUTC())
	if err != nil {
		return Artifact{}, err
	}
	if !changed {
		return updated, ErrArtifactStateConflict
	}
	return updated, nil
}

func (s *Service) ArtifactForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string) (Artifact, bool, error) {
	return s.repo.FindOwnedArtifact(ctx, strings.TrimSpace(id), ArtifactOwner(aiJobOwnerFromAuth(auth)))
}

func (s *Service) ArtifactsForJobAndAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, jobID string) ([]Artifact, bool, error) {
	owner := ArtifactOwner(aiJobOwnerFromAuth(auth))
	if _, found, err := s.repo.FindOwnedAIJob(ctx, strings.TrimSpace(jobID), owner); err != nil || !found {
		return nil, found, err
	}
	artifacts, err := s.repo.QueryArtifacts(ctx, ArtifactQuery{Owner: &owner, JobID: strings.TrimSpace(jobID), Limit: 100})
	return artifacts, true, err
}

func (s *Service) OpenArtifactForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string, byteRange *ArtifactByteRange) (Artifact, ArtifactRead, bool, error) {
	artifact, found, err := s.ArtifactForAuth(ctx, auth, id)
	if err != nil || !found {
		return Artifact{}, ArtifactRead{}, found, err
	}
	if artifact.Policy == GatewayArtifactPolicyProxyOnly {
		opened, proxyErr := s.openProxiedArtifact(ctx, artifact, byteRange)
		return artifact, opened, true, proxyErr
	}
	if !artifactDownloadable(artifact, s.nowUTC()) {
		return artifact, ArtifactRead{}, true, ErrArtifactUnavailable
	}
	store, found := s.artifactStore(artifact.StoreDriver)
	if !found {
		return artifact, ArtifactRead{}, true, ErrArtifactStoreRequired
	}
	opened, err := store.Open(ctx, artifact.StoreKey, byteRange)
	return artifact, opened, true, err
}

func (s *Service) RequestArtifactDeletionForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string) (Artifact, bool, error) {
	artifact, found, err := s.ArtifactForAuth(ctx, auth, id)
	if err != nil || !found {
		return Artifact{}, found, err
	}
	if oneOf(artifact.Status, ArtifactStatusDeleteRequested, ArtifactStatusDeleting, ArtifactStatusDeleted, ArtifactStatusDeleteFailed) {
		return artifact, true, nil
	}
	updated, err := s.transitionArtifact(ctx, artifact, ArtifactStatusDeleteRequested, "client_requested", nil)
	return updated, true, err
}

func (s *Service) RunArtifactRetentionOnce(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	now := s.nowUTC()
	processed := 0
	for _, status := range []string{
		ArtifactStatusReady, ArtifactStatusDelivered, ArtifactStatusDeliveryFailed,
		ArtifactStatusPending, ArtifactStatusUploading, ArtifactStatusFailed,
	} {
		artifacts, err := s.repo.QueryArtifacts(ctx, ArtifactQuery{Status: status, RetainBefore: &now, Limit: limit - processed})
		if err != nil {
			return processed, err
		}
		for _, artifact := range artifacts {
			toStatus := ArtifactStatusExpired
			if oneOf(artifact.Status, ArtifactStatusPending, ArtifactStatusUploading, ArtifactStatusFailed) {
				toStatus = ArtifactStatusDeleteRequested
			}
			if _, err := s.transitionArtifact(ctx, artifact, toStatus, "retention_expired", nil); err == nil {
				processed++
			} else if !errors.Is(err, ErrArtifactStateConflict) {
				return processed, err
			}
			if processed >= limit {
				return processed, nil
			}
		}
	}
	return processed, nil
}

func (s *Service) RunArtifactDeletionWorkerOnce(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	processed := 0
	for _, status := range []string{ArtifactStatusDeleteRequested, ArtifactStatusDeleteFailed, ArtifactStatusExpired} {
		artifacts, err := s.repo.QueryArtifacts(ctx, ArtifactQuery{Status: status, Limit: limit - processed})
		if err != nil {
			return processed, err
		}
		for _, candidate := range artifacts {
			if candidate.Status == ArtifactStatusExpired {
				candidate, err = s.transitionArtifact(ctx, candidate, ArtifactStatusDeleteRequested, "retention_cleanup", nil)
				if errors.Is(err, ErrArtifactStateConflict) {
					continue
				}
				if err != nil {
					return processed, err
				}
			}
			deleting, err := s.transitionArtifact(ctx, candidate, ArtifactStatusDeleting, "worker_claimed", nil)
			if errors.Is(err, ErrArtifactStateConflict) {
				continue
			}
			if err != nil {
				return processed, err
			}
			deleteErr := s.deleteArtifactContent(ctx, deleting)
			toStatus, reason := ArtifactStatusDeleted, ""
			if deleteErr != nil {
				toStatus, reason = ArtifactStatusDeleteFailed, "store_delete_failed"
			}
			if _, err := s.transitionArtifact(ctx, deleting, toStatus, reason, nil); err != nil && !errors.Is(err, ErrArtifactStateConflict) {
				return processed, err
			}
			processed++
			if deleteErr != nil {
				return processed, fmt.Errorf("delete artifact %s: %w", deleting.ID, deleteErr)
			}
			if processed >= limit {
				return processed, nil
			}
		}
	}
	return processed, nil
}

func (s *Service) deleteArtifactContent(ctx context.Context, artifact Artifact) error {
	if artifact.StoreDriver == ArtifactStoreDriverNone || strings.TrimSpace(artifact.StoreKey) == "" {
		return nil
	}
	store, found := s.artifactStore(artifact.StoreDriver)
	if !found {
		return ErrArtifactStoreRequired
	}
	return store.Delete(ctx, artifact.StoreKey)
}

func (s *Service) ArtifactEvents(ctx context.Context, artifactID string) ([]ArtifactEvent, error) {
	return s.repo.ListArtifactEvents(ctx, strings.TrimSpace(artifactID))
}

func (s *Service) RunArtifactLifecycleScheduler(ctx context.Context, interval time.Duration, batchSize int, onError func(error)) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunArtifactRetentionOnce(ctx, batchSize); err != nil && onError != nil {
				onError(err)
			}
			if _, err := s.RunArtifactDeletionWorkerOnce(ctx, batchSize); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
