package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrProviderCallbackInvalid        = errors.New("provider callback is invalid")
	ErrProviderCallbackUnauthorized   = errors.New("provider callback is not authorized")
	ErrProviderCallbackBinding        = errors.New("provider callback is not bound to the attempt")
	ErrProviderCallbackReplayConflict = errors.New("provider callback event conflicts with an existing receipt")
)

const (
	ProviderCallbackReceiptProcessing = "processing"
	ProviderCallbackReceiptApplied    = "applied"
	ProviderCallbackReceiptRejected   = "rejected"
)

// ProviderCallback is the normalized, provider-agnostic contract emitted by a
// provider adapter after it has verified the vendor webhook signature. Core
// never receives vendor headers, secrets, or provider-specific payloads.
type ProviderCallback struct {
	EventID           string                       `json:"event_id"`
	AdapterID         string                       `json:"adapter_id"`
	AttemptID         string                       `json:"attempt_id"`
	ProviderID        string                       `json:"provider_id"`
	ProviderAccountID string                       `json:"provider_account_id"`
	ProviderTaskID    string                       `json:"provider_task_id"`
	ProviderRequestID string                       `json:"provider_request_id,omitempty"`
	Status            string                       `json:"status"`
	Progress          *ProviderProgressObservation `json:"progress,omitempty"`
	Outputs           []ProviderOutputDescriptor   `json:"outputs,omitempty"`
	UsageDimensions   UsageDimensions              `json:"usage_dimensions,omitempty"`
	Billing           ProviderBillingObservation   `json:"billing,omitempty"`
	ReconcileAfter    time.Time                    `json:"reconcile_after,omitempty"`
}

type ProviderCallbackReceipt struct {
	EventID           string     `json:"event_id"`
	AdapterID         string     `json:"adapter_id"`
	AttemptID         string     `json:"attempt_id"`
	ProviderID        string     `json:"provider_id"`
	ProviderAccountID string     `json:"provider_account_id"`
	ProviderTaskID    string     `json:"provider_task_id"`
	PayloadHash       string     `json:"payload_hash"`
	Status            string     `json:"status"`
	ErrorType         string     `json:"error_type,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
}

type ProviderCallbackResult struct {
	EventID   string `json:"event_id"`
	AttemptID string `json:"attempt_id"`
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

func (c ProviderCallback) normalized() (ProviderCallback, error) {
	c.EventID = strings.TrimSpace(c.EventID)
	c.AdapterID = strings.TrimSpace(c.AdapterID)
	c.AttemptID = strings.TrimSpace(c.AttemptID)
	c.ProviderID = strings.TrimSpace(c.ProviderID)
	c.ProviderAccountID = strings.TrimSpace(c.ProviderAccountID)
	c.ProviderTaskID = strings.TrimSpace(c.ProviderTaskID)
	c.ProviderRequestID = strings.TrimSpace(c.ProviderRequestID)
	c.Status = strings.ToLower(strings.TrimSpace(c.Status))
	if c.EventID == "" || len(c.EventID) > 256 || c.AdapterID == "" || c.AttemptID == "" ||
		c.ProviderID == "" || c.ProviderAccountID == "" || c.ProviderTaskID == "" || !validProviderCallbackStatus(c.Status) {
		return ProviderCallback{}, ErrProviderCallbackInvalid
	}
	if c.Progress != nil {
		progress := *c.Progress
		progress.Stage = strings.ToLower(strings.TrimSpace(progress.Stage))
		if progress.Sequence <= 0 || (progress.Percent != nil && (*progress.Percent < 0 || *progress.Percent > 100)) || (progress.Percent == nil && progress.Stage == "") {
			return ProviderCallback{}, ErrProviderCallbackInvalid
		}
		c.Progress = &progress
	}
	if len(c.Outputs) > 0 {
		normalized, err := normalizeProviderOutputs(c.Outputs)
		if err != nil {
			return ProviderCallback{}, errors.Join(ErrProviderCallbackInvalid, err)
		}
		c.Outputs = normalized
	}
	if c.UsageDimensions != nil {
		normalized, err := NormalizeUsageDimensions(c.UsageDimensions)
		if err != nil {
			return ProviderCallback{}, errors.Join(ErrProviderCallbackInvalid, err)
		}
		c.UsageDimensions = normalized
	}
	return c, nil
}

func validProviderCallbackStatus(status string) bool {
	return oneOf(status, "queued", "accepted", "running", "processing", "succeeded", "completed", "failed", "error", "canceled", "cancelled")
}

func providerCallbackHash(callback ProviderCallback) (string, error) {
	payload, err := json.Marshal(callback)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// ProcessProviderCallback applies a trusted adapter callback through the same
// dispatch resolver and terminal settlement path used by reconciliation.
func (s *Service) ProcessProviderCallback(ctx context.Context, callback ProviderCallback, adapter DurableAIJobAdapter) (ProviderCallbackResult, error) {
	if s == nil || adapter == nil {
		return ProviderCallbackResult{}, ErrProviderCallbackUnauthorized
	}
	callback, err := callback.normalized()
	if err != nil {
		return ProviderCallbackResult{}, err
	}
	if strings.TrimSpace(callback.AdapterID) == "" {
		return ProviderCallbackResult{}, ErrProviderCallbackUnauthorized
	}
	payloadHash, err := providerCallbackHash(callback)
	if err != nil {
		return ProviderCallbackResult{}, err
	}
	if existing, found, findErr := s.repo.FindProviderCallbackReceipt(ctx, callback.EventID); findErr != nil {
		return ProviderCallbackResult{}, findErr
	} else if found {
		if existing.PayloadHash != payloadHash || existing.AttemptID != callback.AttemptID || existing.AdapterID != callback.AdapterID {
			return ProviderCallbackResult{}, ErrProviderCallbackReplayConflict
		}
		return ProviderCallbackResult{EventID: callback.EventID, AttemptID: callback.AttemptID, Status: existing.Status, Duplicate: true}, nil
	}
	attempt, found, err := s.repo.FindAIAttempt(ctx, callback.AttemptID)
	if err != nil {
		return ProviderCallbackResult{}, err
	}
	if !found {
		return ProviderCallbackResult{}, ErrProviderCallbackBinding
	}
	if attempt.ProviderAdapterID != callback.AdapterID || attempt.ProviderID != callback.ProviderID ||
		attempt.ProviderAccountID != callback.ProviderAccountID || attempt.ProviderTaskID != callback.ProviderTaskID ||
		(callback.ProviderRequestID != "" && attempt.ProviderRequestID != callback.ProviderRequestID) {
		return ProviderCallbackResult{}, ErrProviderCallbackBinding
	}
	if !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) || attempt.Status != AIAttemptStatusRunning {
		return ProviderCallbackResult{}, ErrProviderCallbackBinding
	}
	receipt := ProviderCallbackReceipt{
		EventID: callback.EventID, AdapterID: callback.AdapterID, AttemptID: callback.AttemptID,
		ProviderID: callback.ProviderID, ProviderAccountID: callback.ProviderAccountID, ProviderTaskID: callback.ProviderTaskID,
		PayloadHash: payloadHash, Status: ProviderCallbackReceiptProcessing, CreatedAt: s.nowUTC(),
	}
	existing, created, err := s.repo.CreateOrGetProviderCallbackReceipt(ctx, receipt)
	if err != nil {
		return ProviderCallbackResult{}, err
	}
	if !created {
		if existing.PayloadHash != payloadHash || existing.AttemptID != callback.AttemptID || existing.AdapterID != callback.AdapterID {
			return ProviderCallbackResult{}, ErrProviderCallbackReplayConflict
		}
		return ProviderCallbackResult{EventID: callback.EventID, AttemptID: callback.AttemptID, Status: existing.Status, Duplicate: true}, nil
	}

	provider, err := s.durableProviderForAttempt(ctx, AIJob{Model: "", OperationID: attempt.OperationID}, attempt)
	if err != nil {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "provider_unavailable", s.nowUTC())
		return ProviderCallbackResult{}, err
	}
	job, found, err := s.repo.FindAIJobByOperationID(ctx, attempt.OperationID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("ai job for operation %q not found", attempt.OperationID)
		}
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "job_unavailable", s.nowUTC())
		return ProviderCallbackResult{}, err
	}
	provider.AdapterID = callback.AdapterID
	result := ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task: ProviderTaskReference{
			ProviderTaskID: callback.ProviderTaskID, ProviderRequestID: callback.ProviderRequestID, Status: callback.Status,
		},
		Progress: callback.Progress, Outputs: callback.Outputs, UsageDimensions: callback.UsageDimensions,
		Billing: callback.Billing, ReconcileAfter: callback.ReconcileAfter,
	}
	if result.Task.ProviderRequestID == "" {
		result.Task.ProviderRequestID = attempt.ProviderRequestID
	}
	reconciler := providerCallbackReconciler{result: result}
	updatedAttempt, resolved, resolveErr := s.ReconcileAIAttemptDispatch(ctx, attempt.ID, reconciler)
	if resolveErr != nil {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "dispatch_state", s.nowUTC())
		return ProviderCallbackResult{}, resolveErr
	}
	if updatedAttempt.ProviderTaskID != callback.ProviderTaskID {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "task_binding", s.nowUTC())
		return ProviderCallbackResult{}, ErrProviderCallbackBinding
	}
	if providerTaskStatusStale(updatedAttempt.ProviderTaskStatus, resolved.Task.Status) {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptApplied, "", s.nowUTC())
		return ProviderCallbackResult{EventID: callback.EventID, AttemptID: callback.AttemptID, Status: updatedAttempt.ProviderTaskStatus}, nil
	}
	if billingErr := s.CommitBillingHold(ctx, job.OperationID, "provider_task_accepted"); billingErr != nil {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "billing_hold", s.nowUTC())
		return ProviderCallbackResult{}, billingErr
	}
	if job.Status == AIJobStatusUnknown || job.Status == AIJobStatusDispatching {
		updatedJob, transitionErr := s.TransitionAIJob(ctx, job.ID, job.StatusVersion, job.FenceToken, AIJobStatusRunning, "")
		if transitionErr != nil {
			_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "job_state", s.nowUTC())
			return ProviderCallbackResult{}, transitionErr
		}
		job = updatedJob
	}
	_, progressErr := s.applyAcceptedProviderProgress(ctx, provider, job, updatedAttempt, resolved, adapter)
	capacityErr := s.syncProviderCapacityForAttempt(ctx, updatedAttempt)
	if err := errors.Join(progressErr, capacityErr); err != nil {
		_ = s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptRejected, "processing", s.nowUTC())
		return ProviderCallbackResult{}, err
	}
	if err := s.repo.CompleteProviderCallbackReceipt(ctx, callback.EventID, ProviderCallbackReceiptApplied, "", s.nowUTC()); err != nil {
		return ProviderCallbackResult{}, err
	}
	return ProviderCallbackResult{EventID: callback.EventID, AttemptID: callback.AttemptID, Status: callback.Status}, nil
}

type providerCallbackReconciler struct {
	result ProviderDispatchResult
}

func (r providerCallbackReconciler) ReconcileProviderTask(context.Context, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error) {
	return r.result, nil
}
