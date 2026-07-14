package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrAIAttemptRequiresReconciliation = errors.New("ai attempt requires provider reconciliation")

const (
	ProviderDispatchOutcomeAccepted         = "accepted"
	ProviderDispatchOutcomeUnknown          = "unknown"
	ProviderDispatchOutcomeProvenNotCreated = "proven_not_created"
	providerDispatchDefaultReconcileDelay   = time.Minute
)

type ProviderDispatchCommand struct {
	Intent  ProviderDispatchIntent
	Payload []byte
}

type ProviderDispatchResult struct {
	Outcome        string
	Task           ProviderTaskReference
	ReconcileAfter time.Time
}

type ProviderDispatchExecutor interface {
	DispatchProviderTask(context.Context, ProviderDispatchCommand) (ProviderDispatchResult, error)
}

type ProviderTaskReconciler interface {
	ReconcileProviderTask(context.Context, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error)
}

func (s *Service) ExecuteAIAttemptDispatch(ctx context.Context, attemptID string, payload []byte, executor ProviderDispatchExecutor) (AIAttempt, ProviderDispatchResult, error) {
	if executor == nil {
		return AIAttempt{}, ProviderDispatchResult{}, errors.New("provider dispatch executor is required")
	}
	prepared, _, err := s.PrepareAIAttemptDispatch(ctx, attemptID)
	if err != nil {
		return AIAttempt{}, ProviderDispatchResult{}, err
	}
	intent, err := providerDispatchIntentFromAttempt(prepared)
	if err != nil {
		return prepared, ProviderDispatchResult{}, err
	}
	submitted, changed, err := s.MarkAIAttemptDispatchSubmitted(ctx, prepared.ID, prepared.DispatchVersion, s.nowUTC().Add(providerDispatchDefaultReconcileDelay))
	if err != nil {
		return submitted, ProviderDispatchResult{}, err
	}
	if !changed {
		return submitted, ProviderDispatchResult{}, ErrAIAttemptRequiresReconciliation
	}
	result, dispatchErr := executor.DispatchProviderTask(ctx, ProviderDispatchCommand{Intent: intent, Payload: append([]byte(nil), payload...)})
	updated, resolveErr := s.resolveProviderDispatchResult(ctx, submitted, result)
	if dispatchErr != nil {
		return updated, result, errors.Join(ErrAIAttemptRequiresReconciliation, dispatchErr, resolveErr)
	}
	if resolveErr != nil {
		return updated, result, resolveErr
	}
	return updated, result, nil
}

func (s *Service) ReconcileAIAttemptDispatch(ctx context.Context, attemptID string, reconciler ProviderTaskReconciler) (AIAttempt, ProviderDispatchResult, error) {
	if reconciler == nil {
		return AIAttempt{}, ProviderDispatchResult{}, errors.New("provider task reconciler is required")
	}
	attempt, found, err := s.AIAttempt(ctx, attemptID)
	if err != nil {
		return AIAttempt{}, ProviderDispatchResult{}, err
	}
	if !found {
		return AIAttempt{}, ProviderDispatchResult{}, ErrAIAttemptNotFound
	}
	if !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) {
		return attempt, ProviderDispatchResult{}, ErrAIAttemptDispatchState
	}
	intent, err := providerDispatchIntentFromAttempt(attempt)
	if err != nil {
		return attempt, ProviderDispatchResult{}, err
	}
	reference := ProviderTaskReference{
		ProviderTaskID: attempt.ProviderTaskID, ProviderRequestID: attempt.ProviderRequestID,
		Status: attempt.ProviderTaskStatus,
	}
	if attempt.ProviderAcceptedAt != nil {
		reference.AcceptedAt = *attempt.ProviderAcceptedAt
	}
	result, reconcileErr := reconciler.ReconcileProviderTask(ctx, intent, reference)
	updated, resolveErr := s.resolveProviderDispatchResult(ctx, attempt, result)
	if reconcileErr != nil {
		return updated, result, errors.Join(ErrAIAttemptRequiresReconciliation, reconcileErr, resolveErr)
	}
	if resolveErr != nil {
		return updated, result, resolveErr
	}
	return updated, result, nil
}

func (s *Service) resolveProviderDispatchResult(ctx context.Context, attempt AIAttempt, result ProviderDispatchResult) (AIAttempt, error) {
	next := result.ReconcileAfter
	if next.IsZero() {
		next = s.nowUTC().Add(providerDispatchDefaultReconcileDelay)
	}
	switch result.Outcome {
	case ProviderDispatchOutcomeAccepted:
		if result.Task.ProviderTaskID == "" {
			updated, _, err := s.MarkAIAttemptDispatchUnknown(ctx, attempt.ID, attempt.DispatchVersion, next)
			return updated, errors.Join(ErrAIAttemptRequiresReconciliation, errors.New("accepted provider dispatch result requires a task id"), err)
		}
		updated, changed, err := s.BindAIAttemptProviderTask(ctx, attempt.ID, attempt.DispatchVersion, result.Task, next)
		if err != nil || changed {
			return updated, err
		}
		updated, _, err = s.RecordAIAttemptReconciliation(ctx, attempt.ID, updated.DispatchVersion, result.Task.Status, next)
		return updated, err
	case ProviderDispatchOutcomeProvenNotCreated:
		updated, _, err := s.ProveAIAttemptNotCreated(ctx, attempt.ID, attempt.DispatchVersion)
		return updated, err
	case ProviderDispatchOutcomeUnknown, "":
		if attempt.DispatchState == AIAttemptDispatchUnknown {
			updated, _, err := s.RecordAIAttemptReconciliation(ctx, attempt.ID, attempt.DispatchVersion, "unknown", next)
			return updated, errors.Join(ErrAIAttemptRequiresReconciliation, err)
		}
		updated, _, err := s.MarkAIAttemptDispatchUnknown(ctx, attempt.ID, attempt.DispatchVersion, next)
		return updated, errors.Join(ErrAIAttemptRequiresReconciliation, err)
	default:
		updated, _, err := s.MarkAIAttemptDispatchUnknown(ctx, attempt.ID, attempt.DispatchVersion, next)
		return updated, errors.Join(ErrAIAttemptRequiresReconciliation, fmt.Errorf("invalid provider dispatch outcome %q", result.Outcome), err)
	}
}

func providerDispatchIntentFromAttempt(attempt AIAttempt) (ProviderDispatchIntent, error) {
	var intent ProviderDispatchIntent
	if err := json.Unmarshal([]byte(attempt.DispatchIntentJSON), &intent); err != nil {
		return ProviderDispatchIntent{}, fmt.Errorf("decode provider dispatch intent: %w", err)
	}
	if intent.Version != 1 || intent.AttemptID != attempt.ID || intent.OperationID != attempt.OperationID || intent.DispatchKey != attempt.DispatchKey ||
		intent.ProviderID != attempt.ProviderID || intent.ProviderAccountID != attempt.ProviderAccountID || intent.RouteID != attempt.RouteID || intent.UpstreamModel != attempt.UpstreamModel || intent.RequestFingerprint == "" {
		return ProviderDispatchIntent{}, ErrAIAttemptDispatchConflict
	}
	return intent, nil
}
