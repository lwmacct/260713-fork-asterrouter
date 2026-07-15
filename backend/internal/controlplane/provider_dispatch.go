package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrAIAttemptRequiresReconciliation = errors.New("ai attempt requires provider reconciliation")

const (
	ProviderDispatchOutcomeAccepted         = "accepted"
	ProviderDispatchOutcomeUnknown          = "unknown"
	ProviderDispatchOutcomeProvenNotCreated = "proven_not_created"
	ProviderBillingStatusUnknown            = "unknown"
	ProviderBillingStatusPending            = "pending"
	ProviderBillingStatusFinal              = "final"
	ProviderBillingStatusNotCharged         = "not_charged"
	providerDispatchDefaultReconcileDelay   = time.Minute
)

type ProviderDispatchCommand struct {
	Intent  ProviderDispatchIntent `json:"intent"`
	Payload []byte                 `json:"payload"`
}

type ProviderDispatchResult struct {
	Outcome         string                       `json:"outcome"`
	Task            ProviderTaskReference        `json:"task"`
	Progress        *ProviderProgressObservation `json:"progress,omitempty"`
	Outputs         []ProviderOutputDescriptor   `json:"outputs,omitempty"`
	UsageDimensions UsageDimensions              `json:"usage_dimensions,omitempty"`
	Billing         ProviderBillingObservation   `json:"billing,omitempty"`
	ReconcileAfter  time.Time                    `json:"reconcile_after,omitempty"`
}

// ProviderBillingObservation contains provider-side billing facts only. Core
// remains responsible for customer pricing, quota enforcement, and ledgers.
type ProviderBillingObservation struct {
	Status                string `json:"status,omitempty"`
	ProcurementCostMicros *int64 `json:"procurement_cost_micros,omitempty"`
	Currency              string `json:"currency,omitempty"`
	Source                string `json:"source,omitempty"`
	Confidence            string `json:"confidence,omitempty"`
	PriceID               string `json:"price_id,omitempty"`
	ProviderBillingLineID string `json:"provider_billing_line_id,omitempty"`
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
	if result.Outcome == ProviderDispatchOutcomeAccepted && isDurableProviderTerminalStatus(strings.ToLower(strings.TrimSpace(result.Task.Status))) {
		next = s.nowUTC()
	}
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

// providerTaskStatusStale rejects a provider observation that would move a
// task backwards. Provider adapters may use different labels, so aliases are
// compared by their normalized lifecycle class rather than raw strings.
func providerTaskStatusStale(current, next string) bool {
	current = canonicalProviderTaskStatus(current)
	next = canonicalProviderTaskStatus(next)
	if current == "" || next == "" || current == next {
		return false
	}
	if current == "unknown" {
		return false
	}
	if next == "unknown" {
		return true
	}
	currentRank := providerTaskStatusRank(current)
	nextRank := providerTaskStatusRank(next)
	if currentRank == 4 {
		return true
	}
	return nextRank < currentRank
}

func canonicalProviderTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "succeeded"
	case "error":
		return "failed"
	case "cancelled":
		return "canceled"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func providerTaskStatusRank(status string) int {
	switch canonicalProviderTaskStatus(status) {
	case "succeeded", "failed", "canceled":
		return 4
	case "processing":
		return 3
	case "running":
		return 2
	case "queued", "accepted":
		return 1
	default:
		return 0
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
	if intent.ProviderAdapterID != attempt.ProviderAdapterID {
		return ProviderDispatchIntent{}, ErrAIAttemptDispatchConflict
	}
	return intent, nil
}
