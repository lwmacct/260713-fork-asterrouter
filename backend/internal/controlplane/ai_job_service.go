package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func (s *Service) BeginDurableAIJob(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (AIJob, bool, error) {
	if request.Lane != gatewaycore.LaneDurable || request.Protocol != gatewaycore.ProtocolAsterJobs || strings.TrimSpace(request.IdempotencyKey) == "" {
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
		Lane: string(request.Lane), Model: strings.TrimSpace(request.Model), Status: AIOperationStatusAccepted, CreatedAt: now, UpdatedAt: now,
	}
	job := AIJob{
		ID: "job_" + randomID(12), OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		RequestFingerprint: operation.RequestFingerprint, IdempotencyKey: operation.IdempotencyKey, Protocol: operation.Protocol,
		Operation: operation.Operation, Modality: operation.Modality, Model: operation.Model, ArtifactPolicy: strings.TrimSpace(auth.ArtifactPolicy),
		RequestPayloadCiphertext: requestPayloadCiphertext, Status: AIJobStatusQueued, StatusVersion: 1, NextEligibleAt: now,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(AIJobDefaultTTL),
	}
	event, outbox, err := newAIJobTransitionRecords(job, AIJobStatusAccepted, "", now)
	if err != nil {
		return AIJob{}, false, err
	}
	createdJob, created, err := s.repo.CreateDurableAIJob(ctx, operation, job, event, outbox)
	if err != nil {
		return AIJob{}, false, err
	}
	if !created && createdJob.RequestFingerprint != job.RequestFingerprint {
		return AIJob{}, false, ErrGatewayIdempotencyConflict
	}
	return createdJob, created, nil
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
	now := s.nowUTC()
	jobs, err := s.repo.ClaimQueuedAIJobs(ctx, now, now.Add(leaseDuration), strings.TrimSpace(workerID), "job_lease_"+randomID(12), limit)
	if err != nil {
		return nil, err
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
	return job, nil
}

func (s *Service) AIJobEvents(ctx context.Context, jobID string) ([]AIJobEvent, error) {
	return s.repo.ListAIJobEvents(ctx, strings.TrimSpace(jobID))
}

func aiJobOwnerFromAuth(auth gatewaycore.CanonicalAuthContext) AIJobOwner {
	return AIJobOwner{
		ProfileScope: strings.TrimSpace(auth.ProfileScope), TenantID: strings.TrimSpace(auth.TenantID),
		IntegrationID: strings.TrimSpace(auth.IntegrationID), PrincipalType: strings.TrimSpace(auth.PrincipalType),
		PrincipalID: strings.TrimSpace(auth.PrincipalID), ExternalSubjectReference: strings.TrimSpace(auth.ExternalSubjectReference),
	}
}
