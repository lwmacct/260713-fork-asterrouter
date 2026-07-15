package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrDurableAIJobAdapterRequired     = errors.New("durable ai job adapter is required")
	ErrDurableAIJobProviderUnavailable = errors.New("durable ai job provider is unavailable")
	ErrDurableAIJobCapacityUnavailable = errors.New("durable ai job provider capacity is unavailable")
)

type DurableAIJobAdapter interface {
	DispatchProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchCommand) (ProviderDispatchResult, error)
	ReconcileProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error)
}

// DurableAIJobAdapterSelector is implemented by composite adapter hosts. The
// selected ID is snapshotted on the Attempt so reconciliation cannot drift to
// a different protocol plugin after a restart or plugin lifecycle change.
type DurableAIJobAdapterSelector interface {
	SelectDurableAIJobAdapter(context.Context, GatewayProvider, AIJob) (adapterID string, supported bool, err error)
}

type DurableAIJobWorkerReport struct {
	Claimed    int
	Accepted   int
	Requeued   int
	Unknown    int
	Reconciled int
	Completed  int
	Errors     int
}

func (s *Service) RunDurableAIJobWorkerOnce(ctx context.Context, workerID string, leaseDuration time.Duration, limit int, adapter DurableAIJobAdapter) (DurableAIJobWorkerReport, error) {
	if adapter == nil {
		return DurableAIJobWorkerReport{}, ErrDurableAIJobAdapterRequired
	}
	jobs, err := s.ClaimReadyAIJobs(ctx, workerID, leaseDuration, limit)
	if err != nil {
		return DurableAIJobWorkerReport{}, err
	}
	report := DurableAIJobWorkerReport{Claimed: len(jobs)}
	var runErrs []error
	for _, job := range jobs {
		outcome, processErr := s.dispatchClaimedAIJob(ctx, job, adapter)
		switch outcome {
		case AIJobStatusRunning:
			report.Accepted++
		case AIJobStatusQueued:
			report.Requeued++
		case AIJobStatusUnknown:
			report.Unknown++
		}
		if processErr != nil {
			report.Errors++
			runErrs = append(runErrs, processErr)
		}
	}
	return report, errors.Join(runErrs...)
}

func (s *Service) dispatchClaimedAIJob(ctx context.Context, job AIJob, adapter DurableAIJobAdapter) (string, error) {
	excluded := map[string]struct{}{}
	adapterUnavailable := false
	for attemptNumber := 1; ; attemptNumber++ {
		candidates, err := s.durableAIJobCandidates(ctx, job, excluded)
		if err != nil {
			return job.Status, err
		}
		if len(candidates) == 0 {
			reason := "capacity_unavailable"
			if adapterUnavailable {
				reason = "adapter_unavailable"
			}
			updated, transitionErr := s.RequeueAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, reason, AIJobDefaultRetryAfter)
			if transitionErr != nil {
				return job.Status, transitionErr
			}
			return updated.Status, nil
		}
		provider := candidates[0]
		adapterID, supported, selectErr := selectDurableAIJobAdapter(ctx, adapter, provider, job)
		if selectErr != nil {
			return job.Status, selectErr
		}
		if !supported {
			adapterUnavailable = true
			excluded[durableProviderCandidateKey(provider)] = struct{}{}
			continue
		}
		provider.AdapterID = adapterID
		permit, _, capacityAcquired, capacityErr := s.TryAcquireProviderAccountPermitContext(
			ctx, provider, estimateDurableAIJobTokens(job), providerCapacityLeaseID(job.OperationID, attemptNumber),
		)
		if capacityErr != nil {
			return job.Status, capacityErr
		}
		if !capacityAcquired {
			excluded[durableProviderCandidateKey(provider)] = struct{}{}
			continue
		}
		attempt, err := s.BeginAIAttempt(ctx, job.OperationID, attemptNumber, provider)
		if err != nil {
			permit.Release()
			return job.Status, err
		}
		provider.AdapterID = attempt.ProviderAdapterID
		executor := durableAIJobDispatchExecutor{adapter: adapter, provider: provider, job: job, attempt: attempt}
		updatedAttempt, dispatchResult, dispatchErr := s.ExecuteAIAttemptDispatch(ctx, attempt.ID, []byte(job.RequestPayload), executor)
		switch updatedAttempt.DispatchState {
		case AIAttemptDispatchAccepted:
			capacityErr = permit.Retain(ctx, providerCapacityRetentionDuration(s.nowUTC(), updatedAttempt.ReconcileAfter))
			billingErr := s.CommitBillingHold(ctx, job.OperationID, "provider_task_accepted")
			currentJob, found, findErr := s.repo.FindAIJob(ctx, job.ID)
			if findErr != nil || !found {
				if findErr == nil {
					findErr = fmt.Errorf("ai job %q not found after provider acceptance", job.ID)
				}
				return job.Status, errors.Join(capacityErr, billingErr, findErr)
			}
			if currentJob.Status == AIJobStatusCanceling {
				job = currentJob
			} else {
				updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, currentJob.StatusVersion, currentJob.FenceToken, AIJobStatusRunning, "")
				if transitionErr != nil {
					return job.Status, errors.Join(capacityErr, billingErr, transitionErr)
				}
				job = updatedJob
			}
			progressedJob, progressErr := s.applyAcceptedProviderProgress(ctx, provider, job, updatedAttempt, dispatchResult, adapter)
			capacitySyncErr := s.syncProviderCapacityForAttempt(ctx, updatedAttempt)
			return progressedJob.Status, errors.Join(dispatchErrIfAccepted(dispatchErr), capacityErr, capacitySyncErr, billingErr, progressErr)
		case AIAttemptDispatchProvenNotCreated:
			permit.Release()
			excluded[durableProviderCandidateKey(provider)] = struct{}{}
			continue
		case AIAttemptDispatchUnknown, AIAttemptDispatchSubmitted:
			capacityErr = permit.Retain(ctx, providerCapacityRetentionDuration(s.nowUTC(), updatedAttempt.ReconcileAfter))
			billingErr := s.DisputeBillingHold(ctx, job.OperationID, "provider_status_unknown")
			updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusUnknown, "provider_status_unknown")
			if transitionErr != nil {
				return job.Status, errors.Join(dispatchErr, capacityErr, billingErr, transitionErr)
			}
			return updatedJob.Status, errors.Join(ErrAIAttemptRequiresReconciliation, dispatchErr, capacityErr, billingErr)
		default:
			permit.Release()
			return job.Status, errors.Join(dispatchErr, fmt.Errorf("unexpected durable attempt dispatch state %q", updatedAttempt.DispatchState))
		}
	}
}

func dispatchErrIfAccepted(err error) error {
	if err == nil || errors.Is(err, ErrAIAttemptRequiresReconciliation) {
		return nil
	}
	return err
}

func (s *Service) RunDurableAIJobReconcilerOnce(ctx context.Context, limit int, adapter DurableAIJobAdapter) (DurableAIJobWorkerReport, error) {
	if adapter == nil {
		return DurableAIJobWorkerReport{}, ErrDurableAIJobAdapterRequired
	}
	attempts, err := s.DurableAIAttemptsForReconciliation(ctx, limit)
	if err != nil {
		return DurableAIJobWorkerReport{}, err
	}
	report := DurableAIJobWorkerReport{Reconciled: len(attempts)}
	var runErrs []error
	for _, attempt := range attempts {
		if reconcileErr := s.reconcileAIAttempt(ctx, attempt, adapter); reconcileErr != nil {
			report.Errors++
			runErrs = append(runErrs, reconcileErr)
			continue
		}
		current, _, findErr := s.AIAttempt(ctx, attempt.ID)
		if findErr != nil {
			report.Errors++
			runErrs = append(runErrs, findErr)
			continue
		}
		switch current.DispatchState {
		case AIAttemptDispatchAccepted:
			report.Accepted++
		case AIAttemptDispatchUnknown, AIAttemptDispatchSubmitted:
			report.Unknown++
		case AIAttemptDispatchProvenNotCreated:
			report.Requeued++
		}
		if job, found, jobErr := s.repo.FindAIJobByOperationID(ctx, current.OperationID); jobErr != nil {
			report.Errors++
			runErrs = append(runErrs, jobErr)
		} else if found && aiJobTerminalStatus(job.Status) {
			report.Completed++
		}
	}
	return report, errors.Join(runErrs...)
}

func (s *Service) reconcileAIAttempt(ctx context.Context, attempt AIAttempt, adapter DurableAIJobAdapter) error {
	operation, found, err := s.repo.FindAIOperation(ctx, attempt.OperationID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("ai operation %q not found", attempt.OperationID)
	}
	job, found, err := s.repo.FindAIJobByOperationID(ctx, operation.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("ai job for operation %q not found", operation.ID)
	}
	provider, err := s.durableProviderForAttempt(ctx, job, attempt)
	if err != nil {
		return err
	}
	capacityErr := s.restoreProviderCapacityForAttempt(ctx, attempt)
	var executor ProviderTaskReconciler = durableAIJobReconcileExecutor{adapter: adapter, provider: provider, job: job, attempt: attempt}
	if job.Status == AIJobStatusCanceling {
		if canceler, ok := adapter.(DurableAIJobAdapterCanceler); ok {
			if supported, selectErr := selectDurableAIJobCancellation(ctx, adapter, provider, job, attempt); selectErr == nil && supported {
				executor = durableAIJobCancelExecutor{adapter: canceler, provider: provider, job: job, attempt: attempt}
			}
		}
	}
	updatedAttempt, dispatchResult, reconcileErr := s.ReconcileAIAttemptDispatch(ctx, attempt.ID, executor)
	reconcileErr = errors.Join(reconcileErr, capacityErr, s.syncProviderCapacityForAttempt(ctx, updatedAttempt))
	if providerTaskStatusStale(updatedAttempt.ProviderTaskStatus, dispatchResult.Task.Status) {
		return reconcileErr
	}
	if reconcileErr != nil && updatedAttempt.DispatchState != AIAttemptDispatchAccepted && updatedAttempt.DispatchState != AIAttemptDispatchProvenNotCreated {
		billingErr := s.DisputeBillingHold(ctx, operation.ID, "provider_status_unknown")
		if job.Status != AIJobStatusUnknown && oneOf(job.Status, AIJobStatusDispatching, AIJobStatusRunning) {
			_, _ = s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusUnknown, "provider_status_unknown")
		}
		return errors.Join(reconcileErr, billingErr)
	}
	if updatedAttempt.DispatchState == AIAttemptDispatchProvenNotCreated {
		if oneOf(job.Status, AIJobStatusUnknown, AIJobStatusDispatching) {
			_, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusQueued, "proven_not_created")
			return transitionErr
		}
		return reconcileErr
	}
	if updatedAttempt.DispatchState != AIAttemptDispatchAccepted {
		return reconcileErr
	}
	if billingErr := s.CommitBillingHold(ctx, operation.ID, "provider_task_accepted"); billingErr != nil {
		return errors.Join(reconcileErr, billingErr)
	}
	if job.Status == AIJobStatusUnknown || job.Status == AIJobStatusDispatching {
		updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusRunning, "")
		if transitionErr != nil {
			return transitionErr
		}
		job = updatedJob
	}
	_, progressErr := s.applyAcceptedProviderProgress(ctx, provider, job, updatedAttempt, dispatchResult, adapter)
	return errors.Join(reconcileErr, progressErr)
}

func (s *Service) applyAcceptedProviderProgress(ctx context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, result ProviderDispatchResult, adapter DurableAIJobAdapter) (returnedJob AIJob, returnedErr error) {
	status := strings.ToLower(strings.TrimSpace(attempt.ProviderTaskStatus))
	providerTerminal := isDurableProviderTerminalStatus(status)
	providerSucceeded := oneOf(status, "succeeded", "completed")
	var progressErr error
	if result.Progress != nil {
		_, _, progressErr = s.RecordAIJobProgress(ctx, job, attempt, *result.Progress)
	}
	defer func() { returnedErr = errors.Join(returnedErr, progressErr) }()
	if len(result.Outputs) > 0 && (!providerTerminal || providerSucceeded) {
		if _, outputErr := s.ingestProviderOutputs(ctx, provider, job, attempt, result.Outputs, adapter); outputErr != nil {
			deferErr := s.deferAIAttemptReconciliation(ctx, attempt, AIJobDefaultRetryAfter)
			return job, errors.Join(outputErr, deferErr)
		}
	}
	if !providerTerminal {
		return job, nil
	}
	terminal := AIJobStatusSucceeded
	attemptStatus := AIAttemptStatusSucceeded
	if status == "failed" || status == "error" {
		terminal = AIJobStatusFailed
		attemptStatus = AIAttemptStatusFailed
	}
	if status == "canceled" || status == "cancelled" {
		terminal = AIJobStatusCanceled
		attemptStatus = AIAttemptStatusCanceled
	}
	if terminal == AIJobStatusSucceeded {
		artifacts, artifactErr := s.repo.QueryArtifacts(ctx, ArtifactQuery{JobID: job.ID, AttemptID: attempt.ID, Role: ArtifactRoleFinal, Limit: 100})
		if artifactErr != nil {
			return job, artifactErr
		}
		if deliverableErr := providerOutputsDeliverable(job, attempt.ID, artifacts); deliverableErr != nil {
			deferErr := s.deferAIAttemptReconciliation(ctx, attempt, AIJobDefaultRetryAfter)
			return job, errors.Join(deliverableErr, deferErr)
		}
		usageDimensions, usageErr := durableProviderUsageDimensions(job, result, artifacts)
		var billing ProviderBillingObservation
		if usageErr == nil {
			billing, usageErr = normalizeProviderSuccessBilling(result.Billing, usageDimensions)
		}
		if usageErr == nil {
			operation, found, operationErr := s.repo.FindAIOperation(ctx, job.OperationID)
			if operationErr != nil {
				usageErr = operationErr
			} else if !found {
				usageErr = fmt.Errorf("ai operation %q not found", job.OperationID)
			} else {
				input := GatewayUsageInput{
					UsageSource: "provider_final", Status: "forwarded", UsageDimensions: usageDimensions,
					UsageNormalizationStatus: "normalized_provider_media", UpstreamRequestID: result.Task.ProviderRequestID,
				}
				applyProviderBillingUsageFields(&input, billing)
				usageErr = s.recordAIOperationUsage(ctx, operation, attempt, input)
			}
		}
		if usageErr != nil {
			deferErr := s.deferAIAttemptReconciliation(ctx, attempt, AIJobDefaultRetryAfter)
			return job, errors.Join(usageErr, deferErr)
		}
	}
	billingResolved := true
	var billingErr error
	if terminal != AIJobStatusSucceeded {
		operation, found, operationErr := s.repo.FindAIOperation(ctx, job.OperationID)
		if operationErr != nil {
			billingErr = operationErr
		} else if !found {
			billingErr = fmt.Errorf("ai operation %q not found", job.OperationID)
		} else {
			billingResolved, billingErr = s.finalizeAIOperationTerminalBilling(ctx, operation, attempt, terminal, result, 1)
		}
		if !billingResolved {
			if disputeErr := s.DisputeBillingHold(ctx, job.OperationID, "provider_billing_unresolved"); disputeErr != nil {
				return job, errors.Join(billingErr, disputeErr)
			}
		}
	}
	if !aiJobTerminalStatus(job.Status) {
		if !oneOf(job.Status, AIJobStatusRunning, AIJobStatusCanceling) {
			return job, fmt.Errorf("ai job %q cannot apply provider terminal status from %q", job.ID, job.Status)
		}
		updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, terminal, status)
		if transitionErr != nil {
			return job, transitionErr
		}
		job = updatedJob
	}
	if job.Status != terminal {
		return job, fmt.Errorf("provider terminal status %q conflicts with ai job status %q", status, job.Status)
	}
	if !billingResolved {
		deferErr := s.deferAIAttemptReconciliation(ctx, attempt, AIJobDefaultRetryAfter)
		return job, errors.Join(billingErr, deferErr)
	}
	if attempt.Status == AIAttemptStatusRunning {
		if completeErr := s.CompleteAIAttempt(ctx, attempt.ID, attemptStatus, status); completeErr != nil {
			return job, completeErr
		}
	}
	return job, nil
}

func (s *Service) deferAIAttemptReconciliation(ctx context.Context, attempt AIAttempt, delay time.Duration) error {
	current, found, err := s.repo.FindAIAttempt(ctx, attempt.ID)
	if err != nil || !found || current.Status != AIAttemptStatusRunning {
		return err
	}
	if delay <= 0 {
		delay = AIJobDefaultRetryAfter
	}
	_, _, err = s.RecordAIAttemptReconciliation(ctx, current.ID, current.DispatchVersion, current.ProviderTaskStatus, s.nowUTC().Add(delay))
	if errors.Is(err, ErrAIAttemptDispatchState) {
		return nil
	}
	return err
}

func estimateDurableAIJobTokens(job AIJob) int {
	payloadBytes := len(job.RequestPayload)
	if payloadBytes == 0 {
		payloadBytes = len(job.RequestPayloadCiphertext)
	}
	return max(1, payloadBytes/4)
}

func (s *Service) restoreProviderCapacityForAttempt(ctx context.Context, attempt AIAttempt) error {
	store := s.currentProviderCapacityStore()
	if store == nil || strings.TrimSpace(attempt.ProviderAccountID) == "" {
		return nil
	}
	_, err := store.Restore(ctx, providerCapacityLeaseForAttempt(attempt), providerCapacityRetentionDuration(s.nowUTC(), attempt.ReconcileAfter))
	return err
}

func (s *Service) syncProviderCapacityForAttempt(ctx context.Context, attempt AIAttempt) error {
	store := s.currentProviderCapacityStore()
	if store == nil || strings.TrimSpace(attempt.ProviderAccountID) == "" {
		return nil
	}
	lease := providerCapacityLeaseForAttempt(attempt)
	if attempt.DispatchState == AIAttemptDispatchProvenNotCreated || attempt.Status != AIAttemptStatusRunning || isDurableProviderTerminalStatus(strings.ToLower(strings.TrimSpace(attempt.ProviderTaskStatus))) {
		return store.Release(ctx, lease)
	}
	_, err := store.Restore(ctx, lease, providerCapacityRetentionDuration(s.nowUTC(), attempt.ReconcileAfter))
	return err
}

func isDurableProviderTerminalStatus(status string) bool {
	return oneOf(status, "succeeded", "completed", "failed", "error", "canceled", "cancelled")
}

func (s *Service) durableAIJobCandidates(ctx context.Context, job AIJob, excluded map[string]struct{}) ([]GatewayProvider, error) {
	candidates, _, err := s.GatewayProviderCandidatesForModel(ctx, job.Model)
	if err != nil {
		return nil, err
	}
	filtered := make([]GatewayProvider, 0, len(candidates))
	for _, candidate := range candidates {
		if _, skip := excluded[durableProviderCandidateKey(candidate)]; !skip {
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

func (s *Service) durableProviderForAttempt(ctx context.Context, job AIJob, attempt AIAttempt) (GatewayProvider, error) {
	return s.providerForAttempt(ctx, job.Model, attempt, "reconcile the provider task bound to the durable attempt")
}

func (s *Service) providerForAttempt(ctx context.Context, requestedModel string, attempt AIAttempt, selectionReason string) (GatewayProvider, error) {
	accounts, err := s.repo.ListProviderAccounts(ctx)
	if err != nil {
		return GatewayProvider{}, err
	}
	var account ProviderAccount
	accountFound := false
	for _, candidate := range accounts {
		if candidate.ID == attempt.ProviderAccountID && candidate.ProviderID == attempt.ProviderID {
			account = candidate
			accountFound = true
			break
		}
	}
	if !accountFound {
		return GatewayProvider{}, fmt.Errorf("provider account %q is unavailable for reconciliation", attempt.ProviderAccountID)
	}
	providers, err := s.repo.ListProviders(ctx)
	if err != nil {
		return GatewayProvider{}, err
	}
	provider, providerFound := providerByIDMap(providers)[attempt.ProviderID]
	if !providerFound || !validHTTPURL(provider.BaseURL) {
		return GatewayProvider{}, fmt.Errorf("provider %q is unavailable for reconciliation", attempt.ProviderID)
	}
	secret, err := decryptSecret(s.secretKey, account.SecretCiphertext)
	if err != nil {
		return GatewayProvider{}, err
	}
	route := ModelRoute{ID: attempt.RouteID, ProviderAccountID: attempt.ProviderAccountID, UpstreamModel: attempt.UpstreamModel}
	if routes, listErr := s.repo.ListModelRoutes(ctx); listErr == nil {
		for _, candidate := range routes {
			if candidate.ID == attempt.RouteID && candidate.ProviderAccountID == attempt.ProviderAccountID && candidate.UpstreamModel == attempt.UpstreamModel {
				route = candidate
				break
			}
		}
	}
	return GatewayProvider{
		ID: provider.ID, Name: provider.Name, Type: provider.Type, BaseURL: provider.BaseURL, APIKey: secret,
		AdapterID: attempt.ProviderAdapterID, AccountID: account.ID, AccountName: account.Name, Concurrency: account.Concurrency,
		GatewayModelID: route.GatewayModelID, RequestedModel: requestedModel, UpstreamModel: attempt.UpstreamModel,
		RouteID: attempt.RouteID, RouteGroup: route.RouteGroup, RoutePriority: route.Priority, RouteWeight: route.Weight,
		AccountWeight: account.Weight, RPMLimit: account.RPMLimit, TPMLimit: account.TPMLimit, CircuitState: account.CircuitState,
		Source: "attempt_snapshot", SelectionReason: selectionReason,
	}, nil
}

func selectDurableAIJobAdapter(ctx context.Context, adapter DurableAIJobAdapter, provider GatewayProvider, job AIJob) (string, bool, error) {
	adapterID, supported, _, err := selectDurableAIJobAdapterWithEvidence(ctx, adapter, provider, job)
	return adapterID, supported, err
}

func durableProviderCandidateKey(provider GatewayProvider) string {
	return strings.TrimSpace(provider.RouteID) + "\x00" + strings.TrimSpace(provider.AccountID)
}

type durableAIJobDispatchExecutor struct {
	adapter  DurableAIJobAdapter
	provider GatewayProvider
	job      AIJob
	attempt  AIAttempt
}

func (e durableAIJobDispatchExecutor) DispatchProviderTask(ctx context.Context, command ProviderDispatchCommand) (ProviderDispatchResult, error) {
	return e.adapter.DispatchProviderTask(ctx, e.provider, e.job, e.attempt, command)
}

type durableAIJobReconcileExecutor struct {
	adapter  DurableAIJobAdapter
	provider GatewayProvider
	job      AIJob
	attempt  AIAttempt
}

func (e durableAIJobReconcileExecutor) ReconcileProviderTask(ctx context.Context, intent ProviderDispatchIntent, reference ProviderTaskReference) (ProviderDispatchResult, error) {
	return e.adapter.ReconcileProviderTask(ctx, e.provider, e.job, e.attempt, intent, reference)
}
