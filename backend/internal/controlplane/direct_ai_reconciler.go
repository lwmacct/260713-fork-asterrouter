package controlplane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var ErrDirectAIProviderReconcilerRequired = errors.New("direct ai provider reconciler is required")

type DirectAIReconcileReport struct {
	Reconciled int
	Completed  int
	Pending    int
	Errors     int
}

func (s *Service) RunDirectAIReconcilerOnce(ctx context.Context, limit int, adapter DirectAIProviderReconciler) (DirectAIReconcileReport, error) {
	if adapter == nil {
		return DirectAIReconcileReport{}, ErrDirectAIProviderReconcilerRequired
	}
	attempts, err := s.DirectAIAttemptsForReconciliation(ctx, limit)
	if err != nil {
		return DirectAIReconcileReport{}, err
	}
	report := DirectAIReconcileReport{Reconciled: len(attempts)}
	var runErrs []error
	for _, attempt := range attempts {
		if reconcileErr := s.reconcileDirectAIAttempt(ctx, attempt, adapter); reconcileErr != nil {
			report.Errors++
			runErrs = append(runErrs, reconcileErr)
			continue
		}
		current, found, findErr := s.repo.FindAIAttempt(ctx, attempt.ID)
		if findErr != nil {
			report.Errors++
			runErrs = append(runErrs, findErr)
			continue
		}
		if !found || current.Status != AIAttemptStatusRunning {
			report.Completed++
		} else {
			report.Pending++
		}
	}
	return report, errors.Join(runErrs...)
}

func (s *Service) reconcileDirectAIAttempt(ctx context.Context, attempt AIAttempt, adapter DirectAIProviderReconciler) error {
	operation, found, err := s.repo.FindAIOperation(ctx, attempt.OperationID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("ai operation %q not found", attempt.OperationID)
	}
	if operation.Lane != string(gatewaycore.LaneDirect) {
		return nil
	}
	provider, err := s.providerForAttempt(ctx, operation.Model, attempt, "reconcile the provider task bound to the direct operation")
	if err != nil {
		return err
	}
	capacityErr := s.restoreProviderCapacityForAttempt(ctx, attempt)
	executor := directAIReconcileExecutor{adapter: adapter, provider: provider, operation: operation, attempt: attempt}
	updatedAttempt, result, reconcileErr := s.ReconcileAIAttemptDispatch(ctx, attempt.ID, executor)
	reconcileErr = errors.Join(reconcileErr, capacityErr, s.syncProviderCapacityForAttempt(ctx, updatedAttempt))
	if providerTaskStatusStale(updatedAttempt.ProviderTaskStatus, result.Task.Status) {
		return reconcileErr
	}
	if reconcileErr != nil && updatedAttempt.DispatchState != AIAttemptDispatchAccepted && updatedAttempt.DispatchState != AIAttemptDispatchProvenNotCreated {
		_ = s.DisputeBillingHold(ctx, operation.ID, "provider_status_unknown")
		return reconcileErr
	}
	if updatedAttempt.DispatchState == AIAttemptDispatchProvenNotCreated {
		if err := s.ReleaseBillingHold(ctx, operation.ID, "provider_proven_not_created"); err != nil {
			return errors.Join(reconcileErr, err)
		}
		return reconcileErr
	}
	if updatedAttempt.DispatchState != AIAttemptDispatchAccepted {
		return reconcileErr
	}
	status := strings.ToLower(strings.TrimSpace(updatedAttempt.ProviderTaskStatus))
	if !isDurableProviderTerminalStatus(status) {
		return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter))
	}
	terminalStatus := AIJobStatusFailed
	attemptStatus := AIAttemptStatusFailed
	if status == "canceled" || status == "cancelled" {
		terminalStatus = AIJobStatusCanceled
		attemptStatus = AIAttemptStatusCanceled
	}
	if status == "succeeded" || status == "completed" {
		request := directRequestFromOperation(operation)
		job := directJobFromOperation(operation)
		if len(result.Outputs) > 0 {
			if _, outputErr := s.ingestProviderOutputs(ctx, provider, job, updatedAttempt, result.Outputs, directAIReconcileOutputReader{adapter: adapter, operation: operation, request: request, result: result}); outputErr != nil {
				return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter), outputErr)
			}
		}
		artifacts, artifactErr := s.repo.QueryArtifacts(ctx, ArtifactQuery{OperationID: operation.ID, AttemptID: updatedAttempt.ID, Role: ArtifactRoleFinal, Limit: 100})
		if artifactErr != nil {
			return errors.Join(reconcileErr, artifactErr)
		}
		if deliverableErr := providerOutputsDeliverable(job, updatedAttempt.ID, artifacts); deliverableErr != nil {
			return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter), deliverableErr)
		}
		dimensions, dimensionsErr := durableProviderUsageDimensions(job, result, artifacts)
		if dimensionsErr != nil {
			return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter), dimensionsErr)
		}
		if usageErr := s.RecordDirectAIProviderUsage(ctx, operation, updatedAttempt, result, GatewayUsageInput{
			UsageVersion: 2, UsageSource: "provider_final", Status: "forwarded", UsageDimensions: dimensions,
			UsageNormalizationStatus: "normalized_provider_media", UpstreamRequestID: result.Task.ProviderRequestID,
		}); usageErr != nil {
			return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter), usageErr)
		}
	} else {
		resolved, billingErr := s.FinalizeDirectAIProviderTerminalBilling(ctx, operation, updatedAttempt, terminalStatus, result)
		if !resolved {
			_ = s.DisputeBillingHold(ctx, operation.ID, "provider_billing_unresolved")
			return errors.Join(reconcileErr, s.deferAIAttemptReconciliation(ctx, updatedAttempt, AIJobDefaultRetryAfter), billingErr)
		}
	}
	if completeErr := s.CompleteAIAttempt(ctx, updatedAttempt.ID, attemptStatus, status); completeErr != nil {
		return errors.Join(reconcileErr, completeErr)
	}
	return reconcileErr
}

type directAIReconcileExecutor struct {
	adapter   DirectAIProviderReconciler
	provider  GatewayProvider
	operation AIOperation
	attempt   AIAttempt
}

func (e directAIReconcileExecutor) ReconcileProviderTask(ctx context.Context, intent ProviderDispatchIntent, reference ProviderTaskReference) (ProviderDispatchResult, error) {
	return e.adapter.ReconcileDirectAI(ctx, e.provider, e.operation, e.attempt, intent, reference)
}

type directAIReconcileOutputReader struct {
	adapter   DirectAIProviderReconciler
	operation AIOperation
	request   gatewaycore.CanonicalRequest
	result    ProviderDispatchResult
}

func (directAIReconcileOutputReader) DispatchProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchCommand) (ProviderDispatchResult, error) {
	return ProviderDispatchResult{}, ErrDirectAIProviderReconcilerRequired
}

func (directAIReconcileOutputReader) ReconcileProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error) {
	return ProviderDispatchResult{}, ErrDirectAIProviderReconcilerRequired
}

func (a directAIReconcileOutputReader) OpenProviderOutput(ctx context.Context, provider GatewayProvider, _ AIJob, attempt AIAttempt, output ProviderOutputDescriptor) (io.ReadCloser, error) {
	return a.adapter.OpenDirectAIOutput(ctx, provider, a.operation, attempt, a.request, a.result, output)
}

func directRequestFromOperation(operation AIOperation) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ID: operation.ClientRequestID, ClientRequestID: operation.ClientRequestID, Fingerprint: operation.RequestFingerprint,
		IdempotencyKey: operation.IdempotencyKey, Protocol: gatewaycore.Protocol(operation.Protocol), Operation: operation.Operation,
		Modality: operation.Modality, Lane: gatewaycore.LaneDirect, Model: operation.Model, OutputCount: 1,
	}
}

func directJobFromOperation(operation AIOperation) AIJob {
	return AIJob{
		OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, IntegrationID: operation.IntegrationID,
		PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID, ExternalSubjectReference: operation.ExternalSubjectReference,
		Protocol: operation.Protocol, Operation: operation.Operation, Modality: operation.Modality, Model: operation.Model,
		ArtifactPolicy: operation.ArtifactPolicy, ArtifactSinkID: operation.ArtifactSinkID,
	}
}
