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
	for attemptNumber := 1; ; attemptNumber++ {
		candidates, err := s.durableAIJobCandidates(ctx, job, excluded)
		if err != nil {
			return job.Status, err
		}
		if len(candidates) == 0 {
			updated, transitionErr := s.RequeueAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, "capacity_unavailable", AIJobDefaultRetryAfter)
			if transitionErr != nil {
				return job.Status, transitionErr
			}
			return updated.Status, nil
		}
		provider := candidates[0]
		attempt, err := s.BeginAIAttempt(ctx, job.OperationID, attemptNumber, provider)
		if err != nil {
			return job.Status, err
		}
		executor := durableAIJobDispatchExecutor{adapter: adapter, provider: provider, job: job, attempt: attempt}
		updatedAttempt, _, dispatchErr := s.ExecuteAIAttemptDispatch(ctx, attempt.ID, []byte(job.RequestPayload), executor)
		switch updatedAttempt.DispatchState {
		case AIAttemptDispatchAccepted:
			updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusRunning, "")
			if transitionErr != nil {
				return job.Status, transitionErr
			}
			return updatedJob.Status, dispatchErrIfAccepted(dispatchErr)
		case AIAttemptDispatchProvenNotCreated:
			excluded[durableProviderCandidateKey(provider)] = struct{}{}
			continue
		case AIAttemptDispatchUnknown, AIAttemptDispatchSubmitted:
			updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusUnknown, "provider_status_unknown")
			if transitionErr != nil {
				return job.Status, errors.Join(dispatchErr, transitionErr)
			}
			return updatedJob.Status, errors.Join(ErrAIAttemptRequiresReconciliation, dispatchErr)
		default:
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
	attempts, err := s.AIAttemptsForReconciliation(ctx, limit)
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
	executor := durableAIJobReconcileExecutor{adapter: adapter, provider: provider, job: job, attempt: attempt}
	updatedAttempt, _, reconcileErr := s.ReconcileAIAttemptDispatch(ctx, attempt.ID, executor)
	if reconcileErr != nil && updatedAttempt.DispatchState != AIAttemptDispatchAccepted && updatedAttempt.DispatchState != AIAttemptDispatchProvenNotCreated {
		if job.Status != AIJobStatusUnknown && oneOf(job.Status, AIJobStatusDispatching, AIJobStatusRunning) {
			_, _ = s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusUnknown, "provider_status_unknown")
		}
		return reconcileErr
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
	if job.Status == AIJobStatusUnknown || job.Status == AIJobStatusDispatching {
		updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusRunning, "")
		if transitionErr != nil {
			return transitionErr
		}
		job = updatedJob
	}
	status := strings.ToLower(strings.TrimSpace(updatedAttempt.ProviderTaskStatus))
	if !isDurableProviderTerminalStatus(status) {
		return reconcileErr
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
	if !aiJobTerminalStatus(job.Status) {
		if !oneOf(job.Status, AIJobStatusRunning, AIJobStatusCanceling) {
			return errors.Join(reconcileErr, fmt.Errorf("ai job %q cannot apply provider terminal status from %q", job.ID, job.Status))
		}
		updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, terminal, status)
		if transitionErr != nil {
			return errors.Join(reconcileErr, transitionErr)
		}
		job = updatedJob
	}
	if job.Status != terminal {
		return errors.Join(reconcileErr, fmt.Errorf("provider terminal status %q conflicts with ai job status %q", status, job.Status))
	}
	if updatedAttempt.Status == AIAttemptStatusRunning {
		if completeErr := s.CompleteAIAttempt(ctx, updatedAttempt.ID, attemptStatus, status); completeErr != nil {
			return errors.Join(reconcileErr, completeErr)
		}
	}
	return reconcileErr
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
	candidates, err := s.durableAIJobCandidates(ctx, job, nil)
	if err != nil {
		return GatewayProvider{}, err
	}
	for _, candidate := range candidates {
		if candidate.AccountID == attempt.ProviderAccountID && candidate.RouteID == attempt.RouteID {
			return candidate, nil
		}
	}
	return GatewayProvider{}, fmt.Errorf("provider route %q/account %q is unavailable for reconciliation", attempt.RouteID, attempt.ProviderAccountID)
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
