package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

func (s *Service) BeginCanonicalOperation(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (AIOperation, bool, error) {
	if strings.TrimSpace(auth.CredentialID) == "" || strings.TrimSpace(request.Fingerprint) == "" || request.Protocol == "" || request.Operation == "" || request.Lane == "" {
		return AIOperation{}, false, gatewaycore.ErrInvalidCanonicalRequest
	}
	now := s.nowUTC()
	operation := AIOperation{
		ID:                       "aio_" + randomID(12),
		ProfileScope:             strings.TrimSpace(auth.ProfileScope),
		TenantID:                 strings.TrimSpace(auth.TenantID),
		CredentialID:             strings.TrimSpace(auth.CredentialID),
		CredentialSource:         string(auth.CredentialSource),
		IntegrationID:            strings.TrimSpace(auth.IntegrationID),
		PrincipalType:            strings.TrimSpace(auth.PrincipalType),
		PrincipalID:              strings.TrimSpace(auth.PrincipalID),
		ExternalSubjectReference: strings.TrimSpace(auth.ExternalSubjectReference),
		ClientRequestID:          strings.TrimSpace(request.ClientRequestID),
		RequestFingerprint:       strings.TrimSpace(request.Fingerprint),
		IdempotencyKey:           strings.TrimSpace(request.IdempotencyKey),
		Protocol:                 string(request.Protocol),
		Operation:                strings.TrimSpace(request.Operation),
		Modality:                 strings.TrimSpace(request.Modality),
		Lane:                     string(request.Lane),
		Model:                    strings.TrimSpace(request.Model),
		ArtifactPolicy:           artifactPolicySnapshot(auth.ArtifactPolicy),
		ArtifactSinkID:           artifactSinkSnapshot(auth.ArtifactPolicy, auth.ArtifactSinkID),
		Status:                   AIOperationStatusAccepted,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if !validArtifactSinkBinding(operation.ArtifactPolicy, operation.ArtifactSinkID) {
		return AIOperation{}, false, ErrArtifactSinkRequired
	}
	if err := s.ValidateInputArtifactsForAuth(ctx, auth, request); err != nil {
		return AIOperation{}, false, err
	}
	billing, err := s.newBillingHoldAdmission(ctx, operation, auth, request)
	if err != nil {
		return AIOperation{}, false, err
	}
	createdOperation, created, err := s.repo.CreateAIOperationWithBillingHold(ctx, operation, billing)
	if err != nil {
		return AIOperation{}, false, err
	}
	if !created && createdOperation.RequestFingerprint != operation.RequestFingerprint {
		return AIOperation{}, false, ErrGatewayIdempotencyConflict
	}
	return createdOperation, created, nil
}

func (s *Service) MarkAIOperationRunning(ctx context.Context, operationID string) error {
	updated, err := s.repo.MarkAIOperationRunning(ctx, strings.TrimSpace(operationID), s.nowUTC())
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("ai operation is not accepted")
	}
	return nil
}

func (s *Service) CompleteAIOperation(ctx context.Context, operationID, status, errorType string) error {
	if !oneOf(status, AIOperationStatusSucceeded, AIOperationStatusFailed, AIOperationStatusCanceled) {
		return errors.New("invalid ai operation terminal status")
	}
	updated, err := s.repo.CompleteAIOperation(ctx, strings.TrimSpace(operationID), status, strings.TrimSpace(errorType), s.nowUTC())
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("ai operation is not active")
	}
	return nil
}

func (s *Service) BeginAIAttempt(ctx context.Context, operationID string, attemptNumber int, provider GatewayProvider) (AIAttempt, error) {
	if strings.TrimSpace(operationID) == "" || attemptNumber <= 0 {
		return AIAttempt{}, errors.New("operation id and positive attempt number are required")
	}
	now := s.nowUTC()
	attempt := AIAttempt{
		ID: "attempt_" + randomID(12), OperationID: strings.TrimSpace(operationID), AttemptNumber: attemptNumber,
		ProviderID: provider.ID, ProviderAccountID: provider.AccountID, ProviderAdapterID: provider.AdapterID,
		RouteID: provider.RouteID, UpstreamModel: provider.UpstreamModel,
		Status: AIAttemptStatusRunning, DispatchState: AIAttemptDispatchPending, CreatedAt: now, UpdatedAt: now,
	}
	attempt.DispatchKey = attempt.ID
	stored, created, err := s.repo.CreateOrGetAIAttempt(ctx, attempt)
	if err != nil {
		return AIAttempt{}, err
	}
	if !created && !sameAIAttemptProvider(stored, attempt) {
		return AIAttempt{}, ErrAIAttemptDispatchConflict
	}
	return stored, nil
}

func sameAIAttemptProvider(left, right AIAttempt) bool {
	return left.OperationID == right.OperationID && left.AttemptNumber == right.AttemptNumber && left.ProviderID == right.ProviderID &&
		left.ProviderAccountID == right.ProviderAccountID && left.RouteID == right.RouteID && left.UpstreamModel == right.UpstreamModel
}

func (s *Service) AIAttempt(ctx context.Context, id string) (AIAttempt, bool, error) {
	return s.repo.FindAIAttempt(ctx, strings.TrimSpace(id))
}

func (s *Service) PrepareAIAttemptDispatch(ctx context.Context, attemptID string) (AIAttempt, bool, error) {
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	operation, found, err := s.repo.FindAIOperation(ctx, attempt.OperationID)
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, fmt.Errorf("ai operation %q not found", attempt.OperationID)
	}
	intent := ProviderDispatchIntent{
		Version: 1, AttemptID: attempt.ID, OperationID: attempt.OperationID, DispatchKey: attempt.DispatchKey,
		RequestFingerprint: operation.RequestFingerprint, ProviderID: attempt.ProviderID, ProviderAccountID: attempt.ProviderAccountID,
		ProviderAdapterID: attempt.ProviderAdapterID, RouteID: attempt.RouteID, UpstreamModel: attempt.UpstreamModel,
	}
	payload, err := json.Marshal(intent)
	if err != nil {
		return AIAttempt{}, false, err
	}
	if attempt.DispatchState != AIAttemptDispatchPending || attempt.DispatchIntentJSON != "" {
		if attempt.DispatchIntentJSON == string(payload) && attempt.DispatchKey == intent.DispatchKey {
			return attempt, false, nil
		}
		return AIAttempt{}, false, ErrAIAttemptDispatchConflict
	}
	if attempt.Status != AIAttemptStatusRunning {
		return AIAttempt{}, false, ErrAIAttemptDispatchState
	}
	requested := attempt
	requested.DispatchState = AIAttemptDispatchPrepared
	requested.DispatchIntentJSON = string(payload)
	requested.UpdatedAt = s.nowUTC()
	updated, changed, err := s.repo.UpdateAIAttemptDispatch(ctx, requested, attempt.DispatchVersion)
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !changed {
		if updated.DispatchIntentJSON == string(payload) && updated.DispatchKey == intent.DispatchKey && updated.DispatchState != AIAttemptDispatchPending {
			return updated, false, nil
		}
		return updated, false, ErrAIAttemptDispatchState
	}
	return updated, true, nil
}

func (s *Service) MarkAIAttemptDispatchSubmitted(ctx context.Context, attemptID string, expectedVersion int, reconcileAfter time.Time) (AIAttempt, bool, error) {
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	if oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) && attempt.DispatchSubmittedAt != nil {
		return attempt, false, nil
	}
	if attempt.DispatchState != AIAttemptDispatchPrepared || attempt.DispatchVersion != expectedVersion {
		return attempt, false, ErrAIAttemptDispatchState
	}
	now := s.nowUTC()
	if reconcileAfter.IsZero() {
		reconcileAfter = now
	}
	requested := attempt
	requested.DispatchState = AIAttemptDispatchSubmitted
	requested.DispatchSubmittedAt = &now
	requested.ReconcileAfter = &reconcileAfter
	requested.UpdatedAt = now
	return s.updateAIAttemptDispatch(ctx, requested, expectedVersion)
}

func (s *Service) BindAIAttemptProviderTask(ctx context.Context, attemptID string, expectedVersion int, reference ProviderTaskReference, reconcileAfter time.Time) (AIAttempt, bool, error) {
	reference.ProviderTaskID = strings.TrimSpace(reference.ProviderTaskID)
	reference.ProviderRequestID = strings.TrimSpace(reference.ProviderRequestID)
	reference.Status = strings.TrimSpace(reference.Status)
	if reference.ProviderTaskID == "" {
		return AIAttempt{}, false, errors.New("provider task id is required")
	}
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	if attempt.ProviderTaskID != "" {
		if attempt.ProviderTaskID != reference.ProviderTaskID || attempt.ProviderRequestID != reference.ProviderRequestID {
			return attempt, false, ErrAIAttemptDispatchConflict
		}
		if providerTaskStatusStale(attempt.ProviderTaskStatus, reference.Status) {
			return attempt, false, nil
		}
		if attempt.DispatchState != AIAttemptDispatchUnknown {
			return attempt, false, nil
		}
	}
	if !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchUnknown) || attempt.DispatchVersion != expectedVersion {
		return attempt, false, ErrAIAttemptDispatchState
	}
	now := s.nowUTC()
	if reference.AcceptedAt.IsZero() {
		reference.AcceptedAt = now
	}
	if reference.Status == "" {
		reference.Status = "accepted"
	}
	if reconcileAfter.IsZero() {
		reconcileAfter = now
	}
	requested := attempt
	requested.DispatchState = AIAttemptDispatchAccepted
	requested.ProviderTaskID = reference.ProviderTaskID
	requested.ProviderRequestID = reference.ProviderRequestID
	requested.ProviderTaskStatus = reference.Status
	requested.ProviderAcceptedAt = &reference.AcceptedAt
	requested.LastReconciledAt = &now
	requested.ReconcileAfter = &reconcileAfter
	requested.UpdatedAt = now
	return s.updateAIAttemptDispatch(ctx, requested, expectedVersion)
}

func (s *Service) MarkAIAttemptDispatchUnknown(ctx context.Context, attemptID string, expectedVersion int, reconcileAfter time.Time) (AIAttempt, bool, error) {
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	if attempt.DispatchState == AIAttemptDispatchUnknown {
		return attempt, false, nil
	}
	if !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted) || attempt.DispatchVersion != expectedVersion {
		return attempt, false, ErrAIAttemptDispatchState
	}
	now := s.nowUTC()
	if reconcileAfter.IsZero() {
		reconcileAfter = now
	}
	requested := attempt
	requested.DispatchState = AIAttemptDispatchUnknown
	requested.ProviderTaskStatus = "unknown"
	requested.LastReconciledAt = &now
	requested.ReconcileAfter = &reconcileAfter
	requested.UpdatedAt = now
	return s.updateAIAttemptDispatch(ctx, requested, expectedVersion)
}

func (s *Service) RecordAIAttemptReconciliation(ctx context.Context, attemptID string, expectedVersion int, providerStatus string, reconcileAfter time.Time) (AIAttempt, bool, error) {
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	if !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) || attempt.DispatchVersion != expectedVersion {
		return attempt, false, ErrAIAttemptDispatchState
	}
	now := s.nowUTC()
	if reconcileAfter.IsZero() {
		reconcileAfter = now
	}
	if providerTaskStatusStale(attempt.ProviderTaskStatus, providerStatus) {
		return attempt, false, nil
	}
	requested := attempt
	requested.ProviderTaskStatus = strings.TrimSpace(providerStatus)
	requested.LastReconciledAt = &now
	requested.ReconcileAfter = &reconcileAfter
	requested.UpdatedAt = now
	return s.updateAIAttemptDispatch(ctx, requested, expectedVersion)
}

func (s *Service) ProveAIAttemptNotCreated(ctx context.Context, attemptID string, expectedVersion int) (AIAttempt, bool, error) {
	attempt, found, err := s.repo.FindAIAttempt(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !found {
		return AIAttempt{}, false, ErrAIAttemptNotFound
	}
	if attempt.ProviderTaskID != "" {
		return attempt, false, ErrAIAttemptDispatchConflict
	}
	changed := false
	if attempt.DispatchState != AIAttemptDispatchProvenNotCreated {
		if !oneOf(attempt.DispatchState, AIAttemptDispatchPrepared, AIAttemptDispatchSubmitted, AIAttemptDispatchUnknown) || attempt.DispatchVersion != expectedVersion {
			return attempt, false, ErrAIAttemptDispatchState
		}
		now := s.nowUTC()
		requested := attempt
		requested.DispatchState = AIAttemptDispatchProvenNotCreated
		requested.ProviderTaskStatus = AIAttemptDispatchProvenNotCreated
		requested.LastReconciledAt = &now
		requested.ReconcileAfter = nil
		requested.UpdatedAt = now
		attempt, changed, err = s.updateAIAttemptDispatch(ctx, requested, expectedVersion)
		if err != nil {
			return attempt, false, err
		}
	}
	if attempt.Status == AIAttemptStatusRunning {
		if err := s.CompleteAIAttempt(ctx, attempt.ID, AIAttemptStatusSkipped, AIAttemptDispatchProvenNotCreated); err != nil {
			return attempt, changed, err
		}
		attempt, _, err = s.repo.FindAIAttempt(ctx, attempt.ID)
		if err != nil {
			return AIAttempt{}, changed, err
		}
	}
	return attempt, changed, nil
}

func (s *Service) AIAttemptsForReconciliation(ctx context.Context, limit int) ([]AIAttempt, error) {
	return s.repo.ListAIAttemptsForReconciliation(ctx, s.nowUTC(), limit)
}

func (s *Service) DirectAIAttemptsForReconciliation(ctx context.Context, limit int) ([]AIAttempt, error) {
	return s.repo.ListDirectAIAttemptsForReconciliation(ctx, s.nowUTC(), limit)
}

func (s *Service) DurableAIAttemptsForReconciliation(ctx context.Context, limit int) ([]AIAttempt, error) {
	return s.repo.ListDurableAIAttemptsForReconciliation(ctx, s.nowUTC(), limit)
}

func (s *Service) updateAIAttemptDispatch(ctx context.Context, requested AIAttempt, expectedVersion int) (AIAttempt, bool, error) {
	updated, changed, err := s.repo.UpdateAIAttemptDispatch(ctx, requested, expectedVersion)
	if err != nil {
		return AIAttempt{}, false, err
	}
	if !changed {
		return updated, false, ErrAIAttemptDispatchState
	}
	return updated, true, nil
}

func (s *Service) CompleteAIAttempt(ctx context.Context, attemptID, status, errorType string) error {
	if !oneOf(status, AIAttemptStatusSucceeded, AIAttemptStatusFailed, AIAttemptStatusSkipped, AIAttemptStatusCanceled) {
		return errors.New("invalid ai attempt terminal status")
	}
	updated, err := s.repo.CompleteAIAttempt(ctx, strings.TrimSpace(attemptID), status, strings.TrimSpace(errorType), s.nowUTC())
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("ai attempt is not running")
	}
	return nil
}

func (s *Service) AIOperation(ctx context.Context, id string) (AIOperation, bool, error) {
	return s.repo.FindAIOperation(ctx, strings.TrimSpace(id))
}

func (s *Service) BillingLedgerEntries(ctx context.Context, operationID string) ([]BillingLedgerEntry, error) {
	return s.repo.ListBillingLedgerEntries(ctx, strings.TrimSpace(operationID))
}

func (s *Service) TransactionalOutboxEvents(ctx context.Context, aggregateID string) ([]TransactionalOutboxEvent, error) {
	return s.repo.ListTransactionalOutboxEvents(ctx, strings.TrimSpace(aggregateID))
}

func (s *Service) buildUsageSettlement(ctx context.Context, record UsageRecord) (UsageSettlement, error) {
	operation, found, err := s.repo.FindAIOperation(ctx, record.OperationID)
	if err != nil {
		return UsageSettlement{}, err
	}
	if !found || operation.RequestFingerprint != record.RequestFingerprint {
		return UsageSettlement{}, ErrUsageLedgerConflict
	}
	hold, found, err := s.repo.FindBillingHoldByOperationID(ctx, record.OperationID)
	if err != nil {
		return UsageSettlement{}, err
	}
	if !found {
		return UsageSettlement{}, errors.New("billing hold not found for usage settlement")
	}
	versions, err := s.repo.ListBillingHoldPricingVersions(ctx, hold.ID)
	if err != nil {
		return UsageSettlement{}, err
	}
	facts := pricingFactsFromUsage(operation, record)
	settlement := UsageSettlement{Record: record}
	for _, snapshot := range versions {
		rule, version, findErr := s.PricingRuleVersionDetail(ctx, snapshot.PricingRuleVersionID)
		if findErr != nil || version.State != PricingVersionStatePublished {
			return UsageSettlement{}, ErrUsageLedgerConflict
		}
		compiled, compileErr := s.pricingEngine.CompileByHash(version.Expression, version.ExpressionHash)
		result, evaluateErr := pricing.Result{}, compileErr
		if evaluateErr == nil {
			result, evaluateErr = s.pricingEngine.Evaluate(compiled, facts)
		}
		evaluationID := deterministicPricingEvaluationID(record, snapshot.Purpose)
		if evaluateErr != nil {
			factsHash, _ := facts.Hash()
			evaluation := PricingEvaluation{
				ID: evaluationID, Purpose: snapshot.Purpose, Phase: pricing.PhaseSettlement, OperationID: record.OperationID,
				AttemptID: record.AttemptID, UsageRecordID: record.ID, UsageVersion: record.UsageVersion,
				PricingRuleID: rule.ID, PricingRuleVersionID: version.ID, EngineVersion: version.EngineVersion,
				ExpressionHash: version.ExpressionHash, FactsHash: factsHash, Facts: facts, Currency: pricing.CurrencyUSD,
				NormalizationStatus: facts.NormalizationStatus, Status: PricingEvaluationStatusDisputed,
				FailureCode: pricingFailureCode(evaluateErr), CreatedAt: record.CreatedAt,
			}
			settlement.Evaluations = append(settlement.Evaluations, evaluation)
			if snapshot.Purpose == PricingPurposeUsageCost {
				settlement.Record.PricingStatus = "disputed"
				settlement.Record.UsageCostMicros = nil
			}
			continue
		}
		amount := result.AmountMicros
		evaluation := PricingEvaluation{
			ID: evaluationID, Purpose: snapshot.Purpose, Phase: pricing.PhaseSettlement, OperationID: record.OperationID,
			AttemptID: record.AttemptID, UsageRecordID: record.ID, UsageVersion: record.UsageVersion,
			PricingRuleID: rule.ID, PricingRuleVersionID: version.ID, EngineVersion: result.EngineVersion,
			ExpressionHash: result.ExpressionHash, FactsHash: result.FactsHash, Facts: facts, AmountMicros: &amount,
			Currency: result.Currency, MatchedTier: result.MatchedTier, Lines: result.Lines,
			NormalizationStatus: facts.NormalizationStatus, Status: PricingEvaluationStatusSuccess, CreatedAt: record.CreatedAt,
		}
		settlement.Evaluations = append(settlement.Evaluations, evaluation)
		ledger := BillingLedgerEntry{
			ID: deterministicBillingLedgerID(record, snapshot.Purpose), OperationID: record.OperationID, AttemptID: record.AttemptID,
			UsageVersion: record.UsageVersion, UsageRecordID: record.ID, RequestFingerprint: record.RequestFingerprint,
			Purpose: snapshot.Purpose, AmountMicros: amount, Currency: result.Currency, PricingEvaluationID: evaluation.ID,
			PricingRuleVersionID: version.ID, Status: BillingLedgerStatusApplied, CreatedAt: record.CreatedAt,
		}
		settlement.Ledgers = append(settlement.Ledgers, ledger)
		if snapshot.Purpose == PricingPurposeUsageCost {
			settlement.Record.UsageCostMicros = &amount
			settlement.Record.UsageCostCurrency = result.Currency
			settlement.Record.UsagePricingEvaluationID = evaluation.ID
			if amount == 0 {
				settlement.Record.PricingStatus = "free"
			} else {
				settlement.Record.PricingStatus = "priced"
			}
		} else if snapshot.Purpose == PricingPurposeCustomerCharge && record.CustomerID != "" {
			outbox, outboxErr := customerChargeOutbox(record, ledger)
			if outboxErr != nil {
				return UsageSettlement{}, outboxErr
			}
			settlement.OutboxEvents = append(settlement.OutboxEvents, outbox)
		}
	}
	riskOutbox, err := usageRecordedOutbox(settlement.Record)
	if err != nil {
		return UsageSettlement{}, err
	}
	settlement.OutboxEvents = append(settlement.OutboxEvents, riskOutbox)
	return settlement, nil
}

func pricingFactsFromUsage(operation AIOperation, record UsageRecord) pricing.Facts {
	totalInput := record.InputTokens
	if record.TotalInputTokens != nil {
		totalInput = *record.TotalInputTokens
	}
	uncachedInput := totalInput
	if record.UncachedInputTokens != nil {
		uncachedInput = *record.UncachedInputTokens
	}
	dimensions := record.UsageDimensions
	return pricing.Facts{
		TotalInputTokens: int64(totalInput), UncachedInputTokens: int64(uncachedInput), CacheReadTokens: int64Value(record.CacheReadTokens),
		CacheWrite5mTokens: int64Value(record.CacheWrite5mTokens), CacheWrite1hTokens: int64Value(record.CacheWrite1hTokens),
		OutputTokens: int64(record.OutputTokens), CacheFieldsPresent: record.CacheFieldsPresent,
		InputImages: usageDimensionQuantity(dimensions, UsageDimensionInputImages), OutputImages: usageDimensionQuantity(dimensions, UsageDimensionOutputImages),
		PartialImages: usageDimensionQuantity(dimensions, UsageDimensionPartialImages), InputVideoMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionInputVideoMilliseconds),
		OutputVideoMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionOutputVideoMilliseconds), InputAudioMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionInputAudioMilliseconds),
		OutputAudioMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionOutputAudioMilliseconds), RealtimeAudioMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionRealtimeAudioMilliseconds),
		InputCharacters: usageDimensionQuantity(dimensions, UsageDimensionInputCharacters), Actions: usageDimensionQuantity(dimensions, UsageDimensionActions),
		BatchItems: usageDimensionQuantity(dimensions, UsageDimensionBatchItems), InputBytes: usageDimensionQuantity(dimensions, UsageDimensionInputBytes),
		OutputBytes: usageDimensionQuantity(dimensions, UsageDimensionOutputBytes), TransferBytes: usageDimensionQuantity(dimensions, UsageDimensionTransferBytes),
		SessionMilliseconds: usageDimensionQuantity(dimensions, UsageDimensionSessionMilliseconds), Protocol: record.Protocol,
		Operation: operation.Operation, Modality: operation.Modality, Lane: operation.Lane,
		NormalizationStatus: record.UsageNormalizationStatus, Phase: pricing.PhaseSettlement, ObservedAt: record.CreatedAt,
	}
}

func int64Value(value *int) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
}

func deterministicPricingEvaluationID(record UsageRecord, purpose string) string {
	return "peval_" + prefix(hashAPIKey(usageLedgerDigest(record)+"\x00"+purpose), 24)
}

func deterministicBillingLedgerID(record UsageRecord, purpose string) string {
	return "billing_" + prefix(hashAPIKey(usageLedgerDigest(record)+"\x00"+purpose), 24)
}

func pricingFailureCode(err error) string {
	var pricingErr *pricing.Error
	if errors.As(err, &pricingErr) && pricingErr.Code != "" {
		return pricingErr.Code
	}
	return "pricing_evaluation_failed"
}

func usageRecordedOutbox(record UsageRecord) (TransactionalOutboxEvent, error) {
	payload, err := json.Marshal(UsageRecordedEvent{
		UsageRecordID: record.ID, OperationID: record.OperationID, AttemptID: record.AttemptID, UsageVersion: record.UsageVersion,
		APIKeyID: record.APIKeyID, CustomerID: record.CustomerID, InputTokens: record.InputTokens, OutputTokens: record.OutputTokens,
		UsageDimensions: record.UsageDimensions, UsageCostMicros: record.UsageCostMicros, PricingStatus: record.PricingStatus, Status: record.Status,
	})
	if err != nil {
		return TransactionalOutboxEvent{}, fmt.Errorf("marshal usage recorded event: %w", err)
	}
	digest := usageLedgerDigest(record)
	return TransactionalOutboxEvent{
		ID: "outbox_" + prefix(hashAPIKey(digest+"\x00"+OutboxEventUsageRecorded), 24), AggregateType: "usage", AggregateID: record.OperationID + ":" + record.AttemptID,
		EventType: OutboxEventUsageRecorded, EventVersion: record.UsageVersion, PayloadJSON: string(payload), Status: OutboxStatusPending,
		AvailableAt: record.CreatedAt, MaxAttempts: OutboxDefaultMaxAttempts, CreatedAt: record.CreatedAt, UpdatedAt: record.CreatedAt,
	}, nil
}

func customerChargeOutbox(record UsageRecord, ledger BillingLedgerEntry) (TransactionalOutboxEvent, error) {
	idempotencyKey := "customer_charge:" + ledger.ID
	payload, err := json.Marshal(CustomerChargePostedEvent{BillingLedgerID: ledger.ID, CustomerID: record.CustomerID, AmountMicros: ledger.AmountMicros, Currency: ledger.Currency, IdempotencyKey: idempotencyKey})
	if err != nil {
		return TransactionalOutboxEvent{}, fmt.Errorf("marshal customer charge event: %w", err)
	}
	return TransactionalOutboxEvent{
		ID: "outbox_" + prefix(hashAPIKey(ledger.ID+"\x00"+OutboxEventCustomerChargePosted), 24), AggregateType: "customer_charge", AggregateID: record.OperationID + ":" + record.AttemptID,
		EventType: OutboxEventCustomerChargePosted, EventVersion: record.UsageVersion, PayloadJSON: string(payload), Status: OutboxStatusPending,
		AvailableAt: record.CreatedAt, MaxAttempts: OutboxDefaultMaxAttempts, CreatedAt: record.CreatedAt, UpdatedAt: record.CreatedAt,
	}, nil
}

func usageLedgerDigest(record UsageRecord) string {
	identity := record.OperationID + "\x00" + record.AttemptID + "\x00" + strconv.Itoa(record.UsageVersion)
	return prefix(hashAPIKey(identity), 24)
}

func normalizeUsageLedgerInput(in *GatewayUsageInput) {
	if in.OperationID == "" {
		return
	}
	if in.UsageVersion <= 0 {
		in.UsageVersion = 1
	}
	if strings.TrimSpace(in.UsageSource) == "" {
		in.UsageSource = "gateway_final"
	}
}
