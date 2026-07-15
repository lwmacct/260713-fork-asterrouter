package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func (s *Service) BeginDurableAIJob(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (AIJob, bool, error) {
	if request.Lane != gatewaycore.LaneDurable || !durableAIJobProtocolSupported(request.Protocol) || strings.TrimSpace(request.IdempotencyKey) == "" {
		if strings.TrimSpace(request.IdempotencyKey) == "" {
			return AIJob{}, false, ErrAIJobIdempotencyRequired
		}
		return AIJob{}, false, gatewaycore.ErrInvalidCanonicalRequest
	}
	if strings.TrimSpace(auth.CredentialID) == "" || strings.TrimSpace(auth.TenantID) == "" || strings.TrimSpace(auth.PrincipalID) == "" || strings.TrimSpace(request.Fingerprint) == "" {
		return AIJob{}, false, gatewaycore.ErrInvalidCanonicalRequest
	}
	resolved, found, err := s.ResolveGatewayModel(ctx, request.Model)
	if err != nil {
		return AIJob{}, false, err
	}
	if !found || (resolved.GatewayModel.Modality != request.Modality && resolved.GatewayModel.Modality != "multimodal") {
		return AIJob{}, false, ErrAIJobCapabilityMismatch
	}
	if err := s.ValidateInputArtifactsForAuth(ctx, auth, request); err != nil {
		return AIJob{}, false, err
	}
	requestPayloadCiphertext, err := encryptSecret(s.secretKey, string(request.Payload))
	if err != nil {
		return AIJob{}, false, err
	}
	now := s.nowUTC()
	operation := AIOperation{
		ID: "aio_" + randomID(12), ProfileScope: strings.TrimSpace(auth.ProfileScope), TenantID: strings.TrimSpace(auth.TenantID),
		CredentialID: strings.TrimSpace(auth.CredentialID), CredentialSource: string(auth.CredentialSource), IntegrationID: strings.TrimSpace(auth.IntegrationID),
		PrincipalType: strings.TrimSpace(auth.PrincipalType), PrincipalID: strings.TrimSpace(auth.PrincipalID),
		ExternalSubjectReference: strings.TrimSpace(auth.ExternalSubjectReference), ClientRequestID: strings.TrimSpace(request.ClientRequestID),
		RequestFingerprint: strings.TrimSpace(request.Fingerprint), IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		Protocol: string(request.Protocol), Operation: strings.TrimSpace(request.Operation), Modality: strings.TrimSpace(request.Modality),
		Lane: string(request.Lane), Model: strings.TrimSpace(request.Model), ArtifactPolicy: artifactPolicySnapshot(auth.ArtifactPolicy),
		ArtifactSinkID: artifactSinkSnapshot(auth.ArtifactPolicy, auth.ArtifactSinkID),
		Status:         AIOperationStatusAccepted, CreatedAt: now, UpdatedAt: now,
	}
	if !validArtifactSinkBinding(operation.ArtifactPolicy, operation.ArtifactSinkID) {
		return AIJob{}, false, ErrArtifactSinkRequired
	}
	job := AIJob{
		ID: "job_" + randomID(12), OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		RequestFingerprint: operation.RequestFingerprint, IdempotencyKey: operation.IdempotencyKey, Protocol: operation.Protocol,
		Operation: operation.Operation, Modality: operation.Modality, Model: operation.Model, ArtifactPolicy: operation.ArtifactPolicy,
		ArtifactSinkID:           operation.ArtifactSinkID,
		RequestPayloadCiphertext: requestPayloadCiphertext, Status: AIJobStatusQueued, StatusVersion: 1, NextEligibleAt: now,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(AIJobDefaultTTL),
	}
	event, outbox, err := newAIJobTransitionRecords(job, AIJobStatusAccepted, "", now)
	if err != nil {
		return AIJob{}, false, err
	}
	billing, err := s.newBillingHoldAdmission(ctx, operation, auth, request)
	if err != nil {
		return AIJob{}, false, err
	}
	createdJob, created, err := s.repo.CreateDurableAIJob(ctx, operation, job, event, outbox, s.currentAIJobAdmissionLimits(), billing)
	if err != nil {
		return AIJob{}, false, err
	}
	if !created && createdJob.RequestFingerprint != job.RequestFingerprint {
		return AIJob{}, false, ErrGatewayIdempotencyConflict
	}
	s.registerAIJobReadyState(ctx, createdJob)
	return createdJob, created, nil
}

func durableAIJobProtocolSupported(protocol gatewaycore.Protocol) bool {
	return protocol == gatewaycore.ProtocolAsterJobs || protocol == gatewaycore.ProtocolOpenAIImages || protocol == gatewaycore.ProtocolOpenAIMedia
}

func (s *Service) SetAIJobReadyIndex(index AIJobReadyIndex) {
	s.aiJobRuntimeMu.Lock()
	defer s.aiJobRuntimeMu.Unlock()
	s.aiJobReadyIndex = index
}

func (s *Service) SetAIJobAdmissionLimits(limits AIJobAdmissionLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	s.aiJobRuntimeMu.Lock()
	defer s.aiJobRuntimeMu.Unlock()
	s.aiJobAdmissionLimits = limits
	return nil
}

func (s *Service) currentAIJobReadyIndex() AIJobReadyIndex {
	s.aiJobRuntimeMu.RLock()
	defer s.aiJobRuntimeMu.RUnlock()
	return s.aiJobReadyIndex
}

func (s *Service) currentAIJobAdmissionLimits() AIJobAdmissionLimits {
	s.aiJobRuntimeMu.RLock()
	defer s.aiJobRuntimeMu.RUnlock()
	return s.aiJobAdmissionLimits
}

func (s *Service) AuthorizeGatewayCredentialScope(ctx context.Context, credential gatewaycore.CredentialEnvelope, sourceIP, requiredScope string) (gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeGatewayCredentialScope(ctx, credential, sourceIP, requiredScope, true)
}

// RevalidateGatewayCredentialScope repeats the complete credential and policy
// check for a live connection without turning each check into a LastUsed write.
func (s *Service) RevalidateGatewayCredentialScope(ctx context.Context, credential gatewaycore.CredentialEnvelope, sourceIP, requiredScope string) (gatewaycore.CanonicalAuthContext, error) {
	return s.authorizeGatewayCredentialScope(ctx, credential, sourceIP, requiredScope, false)
}

func (s *Service) authorizeGatewayCredentialScope(ctx context.Context, credential gatewaycore.CredentialEnvelope, sourceIP, requiredScope string, recordLastUsed bool) (gatewaycore.CanonicalAuthContext, error) {
	auth, err := s.authenticateGatewayCredential(ctx, credential.BearerToken, credential.SignedContext, recordLastUsed)
	if err != nil {
		return gatewaycore.CanonicalAuthContext{}, err
	}
	policy := effectiveAPIKeyPolicy(auth.APIKey)
	if !contains(policy.scopes, strings.TrimSpace(requiredScope)) || !apiKeyAllowsSourceIP(policy, sourceIP) {
		return gatewaycore.CanonicalAuthContext{}, ErrGatewayPolicyForbidden
	}
	return s.canonicalAuthContext(auth), nil
}

func (s *Service) AIJobForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string) (AIJob, bool, error) {
	return s.repo.FindOwnedAIJob(ctx, strings.TrimSpace(id), aiJobOwnerFromAuth(auth))
}

func (s *Service) CancelAIJobForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string) (AIJob, bool, error) {
	job, _, found, err := s.repo.RequestAIJobCancellation(ctx, strings.TrimSpace(id), aiJobOwnerFromAuth(auth), s.nowUTC())
	if err == nil && found && job.StatusVersion > 1 {
		s.removeAIJobReadyReference(ctx, AIJobReadyReference{JobID: job.ID, StatusVersion: job.StatusVersion - 1})
		s.registerAIJobReadyState(ctx, job)
	}
	return job, found, err
}

func (s *Service) ClaimReadyAIJobs(ctx context.Context, workerID string, leaseDuration time.Duration, limit int) ([]AIJob, error) {
	return s.claimReadyAIJobs(ctx, workerID, leaseDuration, limit, true)
}

func (s *Service) claimReadyAIJobMetadata(ctx context.Context, workerID string, leaseDuration time.Duration, limit int) ([]AIJob, error) {
	return s.claimReadyAIJobs(ctx, workerID, leaseDuration, limit, false)
}

func (s *Service) claimReadyAIJobs(ctx context.Context, workerID string, leaseDuration time.Duration, limit int, includePayload bool) ([]AIJob, error) {
	if leaseDuration <= 0 {
		return nil, errors.New("positive ai job lease duration is required")
	}
	if limit <= 0 {
		return []AIJob{}, nil
	}
	now := s.nowUTC()
	leaseUntil := now.Add(leaseDuration)
	workerID = strings.TrimSpace(workerID)
	leaseToken := "job_lease_" + randomID(12)
	jobs := make([]AIJob, 0, limit)
	index := s.currentAIJobReadyIndex()
	if index != nil {
		candidateLimit := aiJobReadyCandidateLimit(limit)
		indexed, candidateErr := index.Candidates(ctx, AIJobReadyQuery{ReadyAt: now, Limit: candidateLimit})
		if candidateErr == nil && len(indexed) > 0 {
			references := make([]AIJobReadyReference, 0, len(indexed))
			for _, entry := range indexed {
				references = append(references, entry.reference())
			}
			claimed, claimErr := s.repo.ClaimAIJobsByReadyReferences(ctx, references, now, leaseUntil, workerID, leaseToken, limit)
			if claimErr != nil {
				return nil, claimErr
			}
			jobs = append(jobs, claimed...)
			s.reconcileAIJobReadyCandidates(ctx, indexed, claimed, now)
		}
	}
	if len(jobs) < limit {
		claimed, err := s.repo.ClaimQueuedAIJobs(ctx, now, leaseUntil, workerID, leaseToken, limit-len(jobs))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, claimed...)
	}
	for _, job := range jobs {
		s.registerAIJobReadyState(ctx, job)
	}
	if !includePayload {
		return jobs, nil
	}
	for index := range jobs {
		payload, decryptErr := decryptSecret(s.secretKey, jobs[index].RequestPayloadCiphertext)
		if decryptErr != nil {
			return nil, decryptErr
		}
		jobs[index].RequestPayload = payload
		jobs[index].RequestPayloadCiphertext = ""
	}
	return jobs, nil
}

func aiJobReadyCandidateLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > 64 {
		return 1024
	}
	return limit * 16
}

func (s *Service) ExtendAIJobQueueLease(ctx context.Context, envelope AIJobDeliveryEnvelope, leaseDuration time.Duration) (AIJob, error) {
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return AIJob{}, err
	}
	if leaseDuration <= 0 {
		return AIJob{}, errors.New("positive ai job lease duration is required")
	}
	now := s.nowUTC()
	job, extended, err := s.repo.ExtendAIJobQueueLease(
		ctx, envelope.JobID, envelope.StatusVersion, envelope.FenceToken, envelope.QueueLeaseToken, now.Add(leaseDuration), now,
	)
	if err != nil {
		return AIJob{}, err
	}
	if !extended {
		return job, ErrAIJobStateConflict
	}
	s.removeAIJobReadyReference(ctx, envelope.reference())
	s.registerAIJobReadyState(ctx, job)
	return job, nil
}

func (s *Service) TransitionAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, toStatus, reason string) (AIJob, error) {
	job, updated, err := s.repo.TransitionAIJob(ctx, strings.TrimSpace(id), expectedVersion, fenceToken, strings.TrimSpace(toStatus), strings.TrimSpace(reason), s.nowUTC())
	if err != nil {
		return AIJob{}, err
	}
	if !updated {
		return job, ErrAIJobStateConflict
	}
	s.removeAIJobReadyReference(ctx, AIJobReadyReference{JobID: strings.TrimSpace(id), StatusVersion: expectedVersion})
	s.registerAIJobReadyState(ctx, job)
	return job, nil
}

func (s *Service) RequeueAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, reason string, delay time.Duration) (AIJob, error) {
	if delay < 0 {
		return AIJob{}, errors.New("ai job retry delay must be non-negative")
	}
	now := s.nowUTC()
	job, updated, err := s.repo.RequeueAIJob(ctx, strings.TrimSpace(id), expectedVersion, fenceToken, strings.TrimSpace(reason), now.Add(delay), now)
	if err != nil {
		return AIJob{}, err
	}
	if !updated {
		return job, ErrAIJobStateConflict
	}
	s.removeAIJobReadyReference(ctx, AIJobReadyReference{JobID: strings.TrimSpace(id), StatusVersion: expectedVersion})
	s.registerAIJobReadyState(ctx, job)
	return job, nil
}

func (s *Service) registerAIJobReadyState(ctx context.Context, job AIJob) {
	index := s.currentAIJobReadyIndex()
	if index == nil || (job.Status != AIJobStatusQueued && job.Status != AIJobStatusDispatching) {
		return
	}
	entry, err := newAIJobReadyEntry(job)
	if err == nil {
		_ = index.Register(ctx, entry)
	}
}

func (s *Service) removeAIJobReadyReference(ctx context.Context, reference AIJobReadyReference) {
	index := s.currentAIJobReadyIndex()
	if index != nil && strings.TrimSpace(reference.JobID) != "" && reference.StatusVersion > 0 {
		_ = index.Remove(ctx, reference)
	}
}

func (s *Service) reconcileAIJobReadyCandidates(ctx context.Context, candidates []AIJobReadyEntry, claimed []AIJob, now time.Time) {
	claimedIDs := make(map[string]bool, len(claimed))
	for _, job := range claimed {
		claimedIDs[job.ID] = true
	}
	for _, candidate := range candidates {
		if claimedIDs[candidate.JobID] {
			s.removeAIJobReadyReference(ctx, candidate.reference())
			continue
		}
		job, found, err := s.repo.FindAIJob(ctx, candidate.JobID)
		if err != nil || (!found && err == nil) {
			if err == nil {
				s.removeAIJobReadyReference(ctx, candidate.reference())
			}
			continue
		}
		if job.StatusVersion != candidate.StatusVersion || !oneOf(job.Status, AIJobStatusQueued, AIJobStatusDispatching) ||
			!aiJobReadyForClaim(job, now) || !aiJobReadyAt(job).Equal(candidate.ReadyAt) {
			s.removeAIJobReadyReference(ctx, candidate.reference())
			s.registerAIJobReadyState(ctx, job)
		}
	}
}

func (s *Service) AIJobEvents(ctx context.Context, jobID string) ([]AIJobEvent, error) {
	return s.repo.ListAIJobEvents(ctx, strings.TrimSpace(jobID))
}

func (s *Service) RecordAIJobProgress(ctx context.Context, job AIJob, attempt AIAttempt, observation ProviderProgressObservation) (AIJobProgressEvent, bool, error) {
	stage := strings.ToLower(strings.TrimSpace(observation.Stage))
	event := AIJobProgressEvent{
		ID: aiJobProgressEventID(attempt.ID, observation.Sequence), JobID: job.ID, AttemptID: attempt.ID,
		ProviderTaskID: strings.TrimSpace(attempt.ProviderTaskID), ProviderSequence: observation.Sequence,
		Percent: observation.Percent, Stage: stage, CreatedAt: s.nowUTC(),
	}
	return s.repo.AppendAIJobProgressEvent(ctx, event)
}

func (s *Service) AIJobProgressEvents(ctx context.Context, jobID string) ([]AIJobProgressEvent, error) {
	return s.repo.ListAIJobProgressEvents(ctx, strings.TrimSpace(jobID))
}

func (s *Service) AIJobProgressEventsForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, jobID string) ([]AIJobProgressEvent, bool, error) {
	job, found, err := s.repo.FindOwnedAIJob(ctx, strings.TrimSpace(jobID), aiJobOwnerFromAuth(auth))
	if err != nil || !found {
		return nil, found, err
	}
	events, err := s.repo.ListAIJobProgressEvents(ctx, job.ID)
	return events, err == nil, err
}

func aiJobOwnerFromAuth(auth gatewaycore.CanonicalAuthContext) AIJobOwner {
	return AIJobOwner{
		ProfileScope: strings.TrimSpace(auth.ProfileScope), TenantID: strings.TrimSpace(auth.TenantID),
		IntegrationID: strings.TrimSpace(auth.IntegrationID), PrincipalType: strings.TrimSpace(auth.PrincipalType),
		PrincipalID: strings.TrimSpace(auth.PrincipalID), ExternalSubjectReference: strings.TrimSpace(auth.ExternalSubjectReference),
	}
}
