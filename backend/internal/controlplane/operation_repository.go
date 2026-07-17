package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/pricing"
)

const aiOperationSelectColumns = `id, profile_scope, tenant_id, credential_id, credential_source, integration_id,
principal_type, principal_id, external_subject_reference, client_request_id, request_fingerprint, idempotency_key,
protocol, operation, modality, lane, model, artifact_policy, artifact_sink_id, status, error_type, created_at, updated_at, completed_at`

const aiAttemptSelectColumns = `id, operation_id, attempt_number, provider_id, provider_account_id, provider_adapter_id, route_id,
upstream_model, status, error_type, dispatch_state, dispatch_version, dispatch_key, dispatch_intent_json,
dispatch_submitted_at, provider_task_id, provider_request_id, provider_task_status, provider_accepted_at,
last_reconciled_at, reconcile_after, created_at, updated_at, completed_at`

func normalizeAIAttempt(attempt *AIAttempt) {
	if attempt.DispatchState == "" {
		attempt.DispatchState = AIAttemptDispatchPending
	}
	if attempt.DispatchKey == "" {
		attempt.DispatchKey = attempt.ID
	}
}

func normalizeAIOperation(operation *AIOperation) {
	operation.ArtifactPolicy = artifactPolicySnapshot(operation.ArtifactPolicy)
	operation.ArtifactSinkID = artifactSinkSnapshot(operation.ArtifactPolicy, operation.ArtifactSinkID)
}

func applyAIAttemptDispatchUpdate(current *AIAttempt, requested AIAttempt) {
	current.DispatchState = requested.DispatchState
	current.DispatchVersion++
	current.DispatchKey = requested.DispatchKey
	current.DispatchIntentJSON = requested.DispatchIntentJSON
	current.DispatchSubmittedAt = cloneTimePointer(requested.DispatchSubmittedAt)
	current.ProviderTaskID = requested.ProviderTaskID
	current.ProviderRequestID = requested.ProviderRequestID
	current.ProviderTaskStatus = requested.ProviderTaskStatus
	current.ProviderAcceptedAt = cloneTimePointer(requested.ProviderAcceptedAt)
	current.LastReconciledAt = cloneTimePointer(requested.LastReconciledAt)
	current.ReconcileAfter = cloneTimePointer(requested.ReconcileAfter)
	current.UpdatedAt = requested.UpdatedAt
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func aiAttemptReconciliationTime(attempt AIAttempt) time.Time {
	if attempt.ReconcileAfter != nil {
		return *attempt.ReconcileAfter
	}
	return attempt.UpdatedAt
}

func aiAttemptDueForReconciliation(attempt AIAttempt, now time.Time) bool {
	if attempt.Status != AIAttemptStatusRunning || !oneOf(attempt.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) {
		return false
	}
	return attempt.ReconcileAfter == nil || !attempt.ReconcileAfter.After(now)
}

type aiAttemptScanner interface {
	Scan(dest ...any) error
}

func scanAIAttempt(scanner aiAttemptScanner) (AIAttempt, error) {
	var attempt AIAttempt
	err := scanner.Scan(
		&attempt.ID, &attempt.OperationID, &attempt.AttemptNumber, &attempt.ProviderID, &attempt.ProviderAccountID, &attempt.ProviderAdapterID, &attempt.RouteID,
		&attempt.UpstreamModel, &attempt.Status, &attempt.ErrorType, &attempt.DispatchState, &attempt.DispatchVersion, &attempt.DispatchKey,
		&attempt.DispatchIntentJSON, &attempt.DispatchSubmittedAt, &attempt.ProviderTaskID, &attempt.ProviderRequestID, &attempt.ProviderTaskStatus,
		&attempt.ProviderAcceptedAt, &attempt.LastReconciledAt, &attempt.ReconcileAfter, &attempt.CreatedAt, &attempt.UpdatedAt, &attempt.CompletedAt,
	)
	if err == nil {
		normalizeAIAttempt(&attempt)
	}
	return attempt, err
}

func (r *MemoryRepository) CreateAIOperation(_ context.Context, operation AIOperation) (AIOperation, bool, error) {
	normalizeAIOperation(&operation)
	r.mu.Lock()
	defer r.mu.Unlock()
	if operation.IdempotencyKey != "" {
		for _, current := range r.aiOperations {
			if sameAIOperationIdempotencyScope(current, operation) {
				return current, false, nil
			}
		}
	}
	if _, exists := r.aiOperations[operation.ID]; exists {
		return AIOperation{}, false, fmt.Errorf("ai operation %q already exists", operation.ID)
	}
	r.aiOperations[operation.ID] = operation
	return operation, true, nil
}

func sameAIOperationIdempotencyScope(left, right AIOperation) bool {
	return left.IdempotencyKey != "" && left.ProfileScope == right.ProfileScope && left.TenantID == right.TenantID &&
		left.CredentialSource == right.CredentialSource && left.CredentialID == right.CredentialID && left.IntegrationID == right.IntegrationID &&
		left.PrincipalType == right.PrincipalType && left.PrincipalID == right.PrincipalID &&
		left.ExternalSubjectReference == right.ExternalSubjectReference && left.Operation == right.Operation && left.IdempotencyKey == right.IdempotencyKey
}

func (r *MemoryRepository) FindAIOperation(_ context.Context, id string) (AIOperation, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	operation, found := r.aiOperations[id]
	return operation, found, nil
}

func (r *MemoryRepository) MarkAIOperationRunning(_ context.Context, id string, updatedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	operation, found := r.aiOperations[id]
	if !found || operation.Status != AIOperationStatusAccepted {
		return false, nil
	}
	operation.Status = AIOperationStatusRunning
	operation.UpdatedAt = updatedAt
	r.aiOperations[id] = operation
	return true, nil
}

func (r *MemoryRepository) CompleteAIOperation(_ context.Context, id, status, errorType string, completedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	operation, found := r.aiOperations[id]
	if !found || (operation.Status != AIOperationStatusAccepted && operation.Status != AIOperationStatusRunning) {
		return false, nil
	}
	operation.Status = status
	operation.ErrorType = errorType
	operation.UpdatedAt = completedAt
	operation.CompletedAt = &completedAt
	r.aiOperations[id] = operation
	return true, nil
}

func (r *MemoryRepository) CreateAIAttempt(ctx context.Context, attempt AIAttempt) error {
	_, created, err := r.CreateOrGetAIAttempt(ctx, attempt)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("ai attempt number already exists")
	}
	return nil
}

func (r *MemoryRepository) CreateOrGetAIAttempt(_ context.Context, attempt AIAttempt) (AIAttempt, bool, error) {
	normalizeAIAttempt(&attempt)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.aiOperations[attempt.OperationID]; !found {
		return AIAttempt{}, false, fmt.Errorf("ai operation %q not found", attempt.OperationID)
	}
	for _, current := range r.aiAttempts {
		if current.OperationID == attempt.OperationID && current.AttemptNumber == attempt.AttemptNumber {
			return current, false, nil
		}
	}
	if _, exists := r.aiAttempts[attempt.ID]; exists {
		return AIAttempt{}, false, fmt.Errorf("ai attempt %q already exists", attempt.ID)
	}
	r.aiAttempts[attempt.ID] = attempt
	return attempt, true, nil
}

func (r *MemoryRepository) FindAIAttempt(_ context.Context, id string) (AIAttempt, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	attempt, found := r.aiAttempts[strings.TrimSpace(id)]
	return attempt, found, nil
}

func (r *MemoryRepository) ListAIAttemptsByOperationID(_ context.Context, operationID string) ([]AIAttempt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	attempts := make([]AIAttempt, 0)
	for _, attempt := range r.aiAttempts {
		if attempt.OperationID == strings.TrimSpace(operationID) {
			attempts = append(attempts, attempt)
		}
	}
	sort.SliceStable(attempts, func(i, j int) bool {
		if attempts[i].AttemptNumber == attempts[j].AttemptNumber {
			return attempts[i].ID < attempts[j].ID
		}
		return attempts[i].AttemptNumber < attempts[j].AttemptNumber
	})
	return attempts, nil
}

func (r *MemoryRepository) UpdateAIAttemptDispatch(_ context.Context, requested AIAttempt, expectedVersion int) (AIAttempt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.aiAttempts[requested.ID]
	if !found || current.Status != AIAttemptStatusRunning || current.DispatchVersion != expectedVersion {
		return current, false, nil
	}
	if requested.ProviderTaskID != "" {
		for _, other := range r.aiAttempts {
			if other.ID != current.ID && other.ProviderTaskID == requested.ProviderTaskID && other.ProviderAccountID == requested.ProviderAccountID {
				return current, false, ErrAIAttemptDispatchConflict
			}
		}
	}
	applyAIAttemptDispatchUpdate(&current, requested)
	r.aiAttempts[current.ID] = current
	return current, true, nil
}

func (r *MemoryRepository) ScheduleAIAttemptReconciliation(_ context.Context, id string, expectedVersion int, scheduledAt time.Time, audit AuditLog) (AIAttempt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, found := r.aiAttempts[strings.TrimSpace(id)]
	if !found || current.Status != AIAttemptStatusRunning || current.DispatchVersion != expectedVersion ||
		!oneOf(current.DispatchState, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown) {
		return current, false, nil
	}
	if _, exists := r.auditLogs[audit.ID]; exists {
		return current, false, fmt.Errorf("audit log %q already exists", audit.ID)
	}
	requested := current
	requested.ReconcileAfter = timePointer(scheduledAt)
	requested.UpdatedAt = scheduledAt
	applyAIAttemptDispatchUpdate(&current, requested)
	r.aiAttempts[current.ID] = current
	r.auditLogs[audit.ID] = audit
	return current, true, nil
}

func (r *MemoryRepository) ScheduleArtifactDeliveryRetry(_ context.Context, artifactID string, requested AIAttempt, expectedVersion int, audit AuditLog) (AIAttempt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	artifact, artifactFound := r.artifacts[strings.TrimSpace(artifactID)]
	current, attemptFound := r.aiAttempts[requested.ID]
	if !artifactFound || artifact.Status != ArtifactStatusDeliveryFailed || artifact.Policy != GatewayArtifactPolicyCustomerSink || artifact.AttemptID != requested.ID ||
		!attemptFound || current.Status != AIAttemptStatusRunning || current.DispatchVersion != expectedVersion {
		return current, false, nil
	}
	if _, exists := r.auditLogs[audit.ID]; exists {
		return current, false, fmt.Errorf("audit log %q already exists", audit.ID)
	}
	applyAIAttemptDispatchUpdate(&current, requested)
	r.aiAttempts[current.ID] = current
	r.auditLogs[audit.ID] = audit
	return current, true, nil
}

func (r *MemoryRepository) ListAIAttemptsForReconciliation(_ context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AIAttempt, 0, limit)
	for _, attempt := range r.aiAttempts {
		if !aiAttemptDueForReconciliation(attempt, now) {
			continue
		}
		result = append(result, attempt)
	}
	sort.Slice(result, func(i, j int) bool {
		return aiAttemptReconciliationTime(result[i]).Before(aiAttemptReconciliationTime(result[j]))
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *MemoryRepository) ListDirectAIAttemptsForReconciliation(_ context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AIAttempt, 0, limit)
	for _, attempt := range r.aiAttempts {
		if !aiAttemptDueForReconciliation(attempt, now) {
			continue
		}
		operation, found := r.aiOperations[attempt.OperationID]
		if !found || operation.Lane != string(gatewaycore.LaneDirect) {
			continue
		}
		result = append(result, attempt)
	}
	sort.Slice(result, func(i, j int) bool {
		return aiAttemptReconciliationTime(result[i]).Before(aiAttemptReconciliationTime(result[j]))
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *MemoryRepository) ListDurableAIAttemptsForReconciliation(_ context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]AIAttempt, 0, limit)
	for _, attempt := range r.aiAttempts {
		if !aiAttemptDueForReconciliation(attempt, now) {
			continue
		}
		if _, found := memoryAIJobByOperationID(r.aiJobs, attempt.OperationID); !found {
			continue
		}
		result = append(result, attempt)
	}
	sort.Slice(result, func(i, j int) bool {
		return aiAttemptReconciliationTime(result[i]).Before(aiAttemptReconciliationTime(result[j]))
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *MemoryRepository) CompleteAIAttempt(_ context.Context, id, status, errorType string, completedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	attempt, found := r.aiAttempts[id]
	if !found || attempt.Status != AIAttemptStatusRunning {
		return false, nil
	}
	attempt.Status = status
	attempt.ErrorType = errorType
	attempt.UpdatedAt = completedAt
	attempt.CompletedAt = &completedAt
	r.aiAttempts[id] = attempt
	return true, nil
}

func (r *MemoryRepository) ApplyUsageSettlement(_ context.Context, settlement UsageSettlement) (bool, error) {
	record := settlement.Record
	usageDimensions, err := NormalizeUsageDimensions(record.UsageDimensions)
	if err != nil {
		return false, err
	}
	record.UsageDimensions = usageDimensions
	settlement.Record = record
	if err := validateUsageSettlement(settlement); err != nil {
		return false, err
	}
	for index := range settlement.OutboxEvents {
		normalizeTransactionalOutboxEvent(&settlement.OutboxEvents[index])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.aiOperations[record.OperationID]; !found {
		return false, fmt.Errorf("ai operation %q not found", record.OperationID)
	}
	if record.AttemptID != "" {
		attempt, found := r.aiAttempts[record.AttemptID]
		if !found || attempt.OperationID != record.OperationID {
			return false, fmt.Errorf("ai attempt %q not found for operation", record.AttemptID)
		}
	}
	existingCount := 0
	for _, billing := range settlement.Ledgers {
		for _, current := range r.billingLedgerEntries {
			if sameBillingLedgerIdentity(current, billing) {
				existingCount++
				if !sameBillingLedgerContent(current, billing) {
					return false, ErrUsageLedgerConflict
				}
			}
		}
	}
	if existingCount > 0 {
		if existingCount != len(settlement.Ledgers) {
			return false, ErrUsageLedgerConflict
		}
		existing, found := r.usageRecords[record.ID]
		if !found || !usageDimensionsEqual(existing.UsageDimensions, record.UsageDimensions) || existing.PricingStatus != record.PricingStatus || !optionalMoneyEqual(existing.UsageCostMicros, record.UsageCostMicros) {
			return false, ErrUsageLedgerConflict
		}
		return false, nil
	}
	if len(settlement.Ledgers) == 0 {
		if existing, found := r.usageRecords[record.ID]; found {
			if usageDimensionsEqual(existing.UsageDimensions, record.UsageDimensions) && existing.PricingStatus == record.PricingStatus && optionalMoneyEqual(existing.UsageCostMicros, record.UsageCostMicros) {
				return false, nil
			}
			return false, ErrUsageLedgerConflict
		}
	}
	if _, exists := r.usageRecords[record.ID]; exists {
		return false, fmt.Errorf("usage record %q already exists", record.ID)
	}
	for _, evaluation := range settlement.Evaluations {
		if _, exists := r.pricingEvaluations[evaluation.ID]; exists {
			return false, fmt.Errorf("pricing evaluation %q already exists", evaluation.ID)
		}
	}
	for _, billing := range settlement.Ledgers {
		if _, exists := r.billingLedgerEntries[billing.ID]; exists {
			return false, fmt.Errorf("billing ledger entry %q already exists", billing.ID)
		}
	}
	for _, outbox := range settlement.OutboxEvents {
		if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
			return false, err
		}
	}
	for _, event := range settlement.PlatformEvents {
		if _, exists := r.platformUsageDeliveryEvents[event.ID]; exists {
			return false, fmt.Errorf("platform usage delivery event %q already exists", event.ID)
		}
		for _, current := range r.platformUsageDeliveryEvents {
			if current.EventID == event.EventID || (current.SinkID == event.SinkID && current.UsageRecordID == event.UsageRecordID) {
				return false, fmt.Errorf("platform usage delivery event is not unique")
			}
		}
	}
	for _, billing := range settlement.Ledgers {
		if billing.Purpose == PricingPurposeUsageCost {
			if err := settleMemoryBillingHoldForUsage(r, record, billing); err != nil {
				return false, err
			}
		}
	}
	for _, evaluation := range settlement.Evaluations {
		key := memoryBillingHoldPricingVersionKeyForEvaluation(r, evaluation.PricingRuleVersionID, evaluation.Purpose, record.OperationID)
		if key != "" {
			version := r.billingHoldPricingVersions[key]
			version.SettlementEvaluationID = evaluation.ID
			r.billingHoldPricingVersions[key] = version
		}
		if evaluation.Purpose == PricingPurposeUsageCost && evaluation.Status == PricingEvaluationStatusDisputed && billingHoldUsageIsFinal(record) {
			hold, found := memoryBillingHoldForOperation(r.billingHolds, record.OperationID)
			if found && !oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased, BillingHoldStatusDisputed) {
				updated, transitionErr := prepareBillingHoldTransition(hold, BillingHoldStatusDisputed, 0, "pricing_evaluation_failed", record.CreatedAt)
				if transitionErr != nil {
					return false, transitionErr
				}
				r.billingHolds[hold.ID] = updated
			}
		}
	}
	if record.PricingStatus == "unpriced" && billingHoldUsageIsFinal(record) {
		hold, found := memoryBillingHoldForOperation(r.billingHolds, record.OperationID)
		if found && !oneOf(hold.Status, BillingHoldStatusSettled, BillingHoldStatusReleased) {
			updated, transitionErr := prepareBillingHoldTransition(hold, BillingHoldStatusSettled, 0, "usage_unpriced", record.CreatedAt)
			if transitionErr != nil {
				return false, transitionErr
			}
			r.billingHolds[hold.ID] = updated
		}
	}
	r.usageRecords[record.ID] = record
	for _, evaluation := range settlement.Evaluations {
		r.pricingEvaluations[evaluation.ID] = clonePricingEvaluation(evaluation)
	}
	for _, billing := range settlement.Ledgers {
		r.billingLedgerEntries[billing.ID] = billing
	}
	for _, outbox := range settlement.OutboxEvents {
		r.transactionalOutboxEvents[outbox.ID] = outbox
	}
	for _, event := range settlement.PlatformEvents {
		r.platformUsageDeliveryEvents[event.ID] = event
	}
	return true, nil
}

func memoryBillingHoldPricingVersionKeyForEvaluation(r *MemoryRepository, versionID, purpose, operationID string) string {
	hold, found := memoryBillingHoldForOperation(r.billingHolds, operationID)
	if !found {
		return ""
	}
	key := hold.ID + "\n" + purpose
	version, found := r.billingHoldPricingVersions[key]
	if !found || version.PricingRuleVersionID != versionID {
		return ""
	}
	return key
}

func (r *MemoryRepository) ListBillingLedgerEntries(_ context.Context, operationID string) ([]BillingLedgerEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BillingLedgerEntry, 0)
	for _, entry := range r.billingLedgerEntries {
		if operationID == "" || entry.OperationID == operationID {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) ListTransactionalOutboxEvents(_ context.Context, aggregateID string) ([]TransactionalOutboxEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TransactionalOutboxEvent, 0)
	for _, event := range r.transactionalOutboxEvents {
		if aggregateID == "" || event.AggregateID == aggregateID {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) ClaimDueTransactionalOutboxEvents(_ context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]TransactionalOutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || strings.TrimSpace(leaseToken) == "" {
		return []TransactionalOutboxEvent{}, nil
	}
	candidates := make([]TransactionalOutboxEvent, 0, len(r.transactionalOutboxEvents))
	for _, event := range r.transactionalOutboxEvents {
		if event.AvailableAt.After(now) || event.Status == OutboxStatusPublished || event.Status == OutboxStatusDeadLetter {
			continue
		}
		if event.Status == OutboxStatusPublishing && event.LeaseUntil != nil && event.LeaseUntil.After(now) {
			continue
		}
		if event.Status != OutboxStatusPending && event.Status != OutboxStatusPublishing {
			continue
		}
		if hasEarlierUnpublishedOutboxEvent(r.transactionalOutboxEvents, event) {
			continue
		}
		candidates = append(candidates, event)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].AvailableAt.Equal(candidates[j].AvailableAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].AvailableAt.Before(candidates[j].AvailableAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	for index := range candidates {
		event := candidates[index]
		normalizeTransactionalOutboxEvent(&event)
		event.Status = OutboxStatusPublishing
		event.AttemptCount++
		event.LeaseUntil = &leaseUntil
		event.LeaseToken = leaseToken
		event.UpdatedAt = now
		r.transactionalOutboxEvents[event.ID] = event
		candidates[index] = event
	}
	return candidates, nil
}

func (r *MemoryRepository) CompleteTransactionalOutboxEvent(_ context.Context, id, leaseToken string, publishedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.transactionalOutboxEvents[id]
	if !found || event.Status != OutboxStatusPublishing || event.LeaseToken != leaseToken {
		return fmt.Errorf("transactional outbox event is not leased")
	}
	event.Status = OutboxStatusPublished
	event.PublishedAt = &publishedAt
	event.LeaseUntil = nil
	event.LeaseToken = ""
	event.LastError = ""
	event.UpdatedAt = publishedAt
	r.transactionalOutboxEvents[id] = event
	return nil
}

func (r *MemoryRepository) RescheduleTransactionalOutboxEvent(_ context.Context, id, leaseToken string, nextAttemptAt time.Time, lastError string, deadLetter bool, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.transactionalOutboxEvents[id]
	if !found || event.Status != OutboxStatusPublishing || event.LeaseToken != leaseToken {
		return fmt.Errorf("transactional outbox event is not leased")
	}
	event.Status = OutboxStatusPending
	if deadLetter {
		event.Status = OutboxStatusDeadLetter
	}
	event.AvailableAt = nextAttemptAt
	event.LeaseUntil = nil
	event.LeaseToken = ""
	event.LastError = trimTransactionalOutboxError(lastError)
	event.UpdatedAt = updatedAt
	r.transactionalOutboxEvents[id] = event
	return nil
}

func (r *MemoryRepository) RequeueTransactionalOutboxEvent(_ context.Context, id string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, found := r.transactionalOutboxEvents[id]
	if !found || event.Status != OutboxStatusDeadLetter {
		return fmt.Errorf("transactional outbox event not found or not dead-lettered")
	}
	event.Status = OutboxStatusPending
	event.AvailableAt = nextAttemptAt
	event.AttemptCount = 0
	event.LeaseUntil = nil
	event.LeaseToken = ""
	event.LastError = ""
	event.UpdatedAt = nextAttemptAt
	r.transactionalOutboxEvents[id] = event
	return nil
}

func hasEarlierUnpublishedOutboxEvent(events map[string]TransactionalOutboxEvent, candidate TransactionalOutboxEvent) bool {
	for _, event := range events {
		if event.AggregateType == candidate.AggregateType && event.AggregateID == candidate.AggregateID && event.EventVersion < candidate.EventVersion && event.Status != OutboxStatusPublished && event.Status != OutboxStatusDeadLetter {
			return true
		}
	}
	return false
}

func scanAIOperation(scanner apiKeyScanner) (AIOperation, error) {
	var operation AIOperation
	var completedAt sql.NullTime
	err := scanner.Scan(
		&operation.ID, &operation.ProfileScope, &operation.TenantID, &operation.CredentialID, &operation.CredentialSource, &operation.IntegrationID,
		&operation.PrincipalType, &operation.PrincipalID, &operation.ExternalSubjectReference, &operation.ClientRequestID,
		&operation.RequestFingerprint, &operation.IdempotencyKey, &operation.Protocol, &operation.Operation, &operation.Modality,
		&operation.Lane, &operation.Model, &operation.ArtifactPolicy, &operation.ArtifactSinkID, &operation.Status, &operation.ErrorType, &operation.CreatedAt, &operation.UpdatedAt, &completedAt,
	)
	if err != nil {
		return AIOperation{}, err
	}
	if completedAt.Valid {
		operation.CompletedAt = &completedAt.Time
	}
	return operation, nil
}

func insertAIOperation(ctx context.Context, executor usageRecordExecutor, operation AIOperation) (sql.Result, error) {
	normalizeAIOperation(&operation)
	return executor.ExecContext(ctx, `
INSERT INTO ai_operations(
  id, profile_scope, tenant_id, credential_id, credential_source, integration_id,
  principal_type, principal_id, external_subject_reference, client_request_id, request_fingerprint, idempotency_key,
  protocol, operation, modality, lane, model, artifact_policy, artifact_sink_id, status, error_type, created_at, updated_at, completed_at
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,NULL)
ON CONFLICT (profile_scope, tenant_id, credential_source, credential_id, integration_id, principal_type, principal_id, external_subject_reference, operation, idempotency_key) WHERE idempotency_key <> '' DO NOTHING
`, operation.ID, operation.ProfileScope, operation.TenantID, operation.CredentialID, operation.CredentialSource, operation.IntegrationID,
		operation.PrincipalType, operation.PrincipalID, operation.ExternalSubjectReference, operation.ClientRequestID,
		operation.RequestFingerprint, operation.IdempotencyKey, operation.Protocol, operation.Operation, operation.Modality,
		operation.Lane, operation.Model, operation.ArtifactPolicy, operation.ArtifactSinkID, operation.Status, operation.ErrorType, operation.CreatedAt, operation.UpdatedAt)
}

func (r *PostgresRepository) CreateAIOperation(ctx context.Context, operation AIOperation) (AIOperation, bool, error) {
	normalizeAIOperation(&operation)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIOperation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := insertAIOperation(ctx, tx, operation)
	if err != nil {
		return AIOperation{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIOperation{}, false, err
	}
	if rows == 0 {
		row := tx.QueryRowContext(ctx, `SELECT `+aiOperationSelectColumns+` FROM ai_operations WHERE
profile_scope=$1 AND tenant_id=$2 AND credential_source=$3 AND credential_id=$4 AND integration_id=$5 AND principal_type=$6 AND principal_id=$7 AND external_subject_reference=$8 AND operation=$9 AND idempotency_key=$10`,
			operation.ProfileScope, operation.TenantID, operation.CredentialSource, operation.CredentialID, operation.IntegrationID,
			operation.PrincipalType, operation.PrincipalID, operation.ExternalSubjectReference, operation.Operation, operation.IdempotencyKey)
		existing, scanErr := scanAIOperation(row)
		if scanErr != nil {
			return AIOperation{}, false, scanErr
		}
		if err := tx.Commit(); err != nil {
			return AIOperation{}, false, err
		}
		return existing, false, nil
	}
	if err := tx.Commit(); err != nil {
		return AIOperation{}, false, err
	}
	return operation, true, nil
}

func (r *PostgresRepository) FindAIOperation(ctx context.Context, id string) (AIOperation, bool, error) {
	operation, err := scanAIOperation(r.db.QueryRowContext(ctx, `SELECT `+aiOperationSelectColumns+` FROM ai_operations WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return AIOperation{}, false, nil
	}
	return operation, err == nil, err
}

func (r *PostgresRepository) MarkAIOperationRunning(ctx context.Context, id string, updatedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_operations SET status=$1, updated_at=$2 WHERE id=$3 AND status=$4`, AIOperationStatusRunning, updatedAt, id, AIOperationStatusAccepted)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *PostgresRepository) CompleteAIOperation(ctx context.Context, id, status, errorType string, completedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_operations SET status=$1, error_type=$2, completed_at=$3, updated_at=$3 WHERE id=$4 AND status IN ($5,$6)`, status, errorType, completedAt, id, AIOperationStatusAccepted, AIOperationStatusRunning)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *PostgresRepository) CreateAIAttempt(ctx context.Context, attempt AIAttempt) error {
	_, created, err := r.CreateOrGetAIAttempt(ctx, attempt)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("ai attempt number already exists")
	}
	return nil
}

func (r *PostgresRepository) CreateOrGetAIAttempt(ctx context.Context, attempt AIAttempt) (AIAttempt, bool, error) {
	normalizeAIAttempt(&attempt)
	result, err := r.db.ExecContext(ctx, `
INSERT INTO ai_attempts(id, operation_id, attempt_number, provider_id, provider_account_id, provider_adapter_id, route_id, upstream_model, status, error_type,
dispatch_state, dispatch_version, dispatch_key, dispatch_intent_json, dispatch_submitted_at, provider_task_id, provider_request_id,
provider_task_status, provider_accepted_at, last_reconciled_at, reconcile_after, created_at, updated_at, completed_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULL,'','','',NULL,NULL,NULL,$15,$16,NULL)
ON CONFLICT(operation_id, attempt_number) DO NOTHING
`, attempt.ID, attempt.OperationID, attempt.AttemptNumber, attempt.ProviderID, attempt.ProviderAccountID, attempt.ProviderAdapterID, attempt.RouteID, attempt.UpstreamModel, attempt.Status, attempt.ErrorType,
		attempt.DispatchState, attempt.DispatchVersion, attempt.DispatchKey, attempt.DispatchIntentJSON, attempt.CreatedAt, attempt.UpdatedAt)
	if err != nil {
		return AIAttempt{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIAttempt{}, false, err
	}
	found, _, err := r.FindAIAttemptByOperationNumber(ctx, attempt.OperationID, attempt.AttemptNumber)
	return found, rows == 1, err
}

func (r *PostgresRepository) FindAIAttempt(ctx context.Context, id string) (AIAttempt, bool, error) {
	attempt, err := scanAIAttempt(r.db.QueryRowContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE id=$1`, strings.TrimSpace(id)))
	if err == sql.ErrNoRows {
		return AIAttempt{}, false, nil
	}
	return attempt, err == nil, err
}

func (r *PostgresRepository) ListAIAttemptsByOperationID(ctx context.Context, operationID string) ([]AIAttempt, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE operation_id=$1 ORDER BY attempt_number, id`, strings.TrimSpace(operationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attempts := make([]AIAttempt, 0)
	for rows.Next() {
		attempt, scanErr := scanAIAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (r *PostgresRepository) FindAIAttemptByOperationNumber(ctx context.Context, operationID string, attemptNumber int) (AIAttempt, bool, error) {
	attempt, err := scanAIAttempt(r.db.QueryRowContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE operation_id=$1 AND attempt_number=$2`, operationID, attemptNumber))
	if err == sql.ErrNoRows {
		return AIAttempt{}, false, nil
	}
	return attempt, err == nil, err
}

func (r *PostgresRepository) UpdateAIAttemptDispatch(ctx context.Context, requested AIAttempt, expectedVersion int) (AIAttempt, bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_attempts SET dispatch_state=$1, dispatch_version=dispatch_version+1,
dispatch_key=$2, dispatch_intent_json=$3, dispatch_submitted_at=$4, provider_task_id=$5, provider_request_id=$6,
provider_task_status=$7, provider_accepted_at=$8, last_reconciled_at=$9, reconcile_after=$10, updated_at=$11
WHERE id=$12 AND status=$13 AND dispatch_version=$14`, requested.DispatchState, requested.DispatchKey, requested.DispatchIntentJSON,
		requested.DispatchSubmittedAt, requested.ProviderTaskID, requested.ProviderRequestID, requested.ProviderTaskStatus, requested.ProviderAcceptedAt,
		requested.LastReconciledAt, requested.ReconcileAfter, requested.UpdatedAt, requested.ID, AIAttemptStatusRunning, expectedVersion)
	if err != nil {
		return AIAttempt{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIAttempt{}, false, err
	}
	current, found, findErr := r.FindAIAttempt(ctx, requested.ID)
	if findErr != nil || !found {
		return current, false, findErr
	}
	return current, rows == 1, nil
}

func (r *PostgresRepository) ScheduleAIAttemptReconciliation(ctx context.Context, id string, expectedVersion int, scheduledAt time.Time, audit AuditLog) (AIAttempt, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIAttempt{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := scanAIAttempt(tx.QueryRowContext(ctx, `UPDATE ai_attempts
SET dispatch_version=dispatch_version+1, reconcile_after=$1, updated_at=$1
WHERE id=$2 AND status=$3 AND dispatch_version=$4 AND dispatch_state IN ($5,$6,$7)
RETURNING `+aiAttemptSelectColumns, scheduledAt, strings.TrimSpace(id), AIAttemptStatusRunning, expectedVersion,
		AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown))
	if errors.Is(err, sql.ErrNoRows) {
		current, findErr := scanAIAttempt(tx.QueryRowContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE id=$1`, strings.TrimSpace(id)))
		if errors.Is(findErr, sql.ErrNoRows) {
			return AIAttempt{}, false, nil
		}
		return current, false, findErr
	}
	if err != nil {
		return AIAttempt{}, false, err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return AIAttempt{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIAttempt{}, false, err
	}
	return updated, true, nil
}

func (r *PostgresRepository) ScheduleArtifactDeliveryRetry(ctx context.Context, artifactID string, requested AIAttempt, expectedVersion int, audit AuditLog) (AIAttempt, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIAttempt{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := scanAIAttempt(tx.QueryRowContext(ctx, `UPDATE ai_attempts SET dispatch_state=$1, dispatch_version=dispatch_version+1,
dispatch_key=$2, dispatch_intent_json=$3, dispatch_submitted_at=$4, provider_task_id=$5, provider_request_id=$6,
provider_task_status=$7, provider_accepted_at=$8, last_reconciled_at=$9, reconcile_after=$10, updated_at=$11
WHERE id=$12 AND status=$13 AND dispatch_version=$14 AND EXISTS (
	SELECT 1 FROM artifacts WHERE artifacts.id=$15 AND artifacts.attempt_id=ai_attempts.id AND artifacts.status=$16 AND artifacts.policy=$17
)
RETURNING `+aiAttemptSelectColumns, requested.DispatchState, requested.DispatchKey, requested.DispatchIntentJSON,
		requested.DispatchSubmittedAt, requested.ProviderTaskID, requested.ProviderRequestID, requested.ProviderTaskStatus, requested.ProviderAcceptedAt,
		requested.LastReconciledAt, requested.ReconcileAfter, requested.UpdatedAt, requested.ID, AIAttemptStatusRunning, expectedVersion,
		strings.TrimSpace(artifactID), ArtifactStatusDeliveryFailed, GatewayArtifactPolicyCustomerSink))
	if errors.Is(err, sql.ErrNoRows) {
		current, findErr := scanAIAttempt(tx.QueryRowContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE id=$1`, requested.ID))
		if errors.Is(findErr, sql.ErrNoRows) {
			return AIAttempt{}, false, nil
		}
		return current, false, findErr
	}
	if err != nil {
		return AIAttempt{}, false, err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return AIAttempt{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIAttempt{}, false, err
	}
	return updated, true, nil
}

func (r *PostgresRepository) ListAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts
WHERE status=$1 AND dispatch_state IN ($2,$3,$4) AND (reconcile_after IS NULL OR reconcile_after <= $5)
ORDER BY COALESCE(reconcile_after, updated_at), updated_at LIMIT $6`, AIAttemptStatusRunning, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIAttempt, 0, limit)
	for rows.Next() {
		attempt, scanErr := scanAIAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, attempt)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) ListDirectAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts
WHERE status=$1 AND dispatch_state IN ($2,$3,$4) AND (reconcile_after IS NULL OR reconcile_after <= $5)
AND EXISTS (SELECT 1 FROM ai_operations WHERE ai_operations.id=ai_attempts.operation_id AND ai_operations.lane=$6)
ORDER BY COALESCE(reconcile_after, updated_at), updated_at LIMIT $7`, AIAttemptStatusRunning, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown, now, string(gatewaycore.LaneDirect), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIAttempt, 0, limit)
	for rows.Next() {
		attempt, scanErr := scanAIAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, attempt)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) ListDurableAIAttemptsForReconciliation(ctx context.Context, now time.Time, limit int) ([]AIAttempt, error) {
	if limit <= 0 {
		return []AIAttempt{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts
WHERE status=$1 AND dispatch_state IN ($2,$3,$4) AND (reconcile_after IS NULL OR reconcile_after <= $5)
AND EXISTS (SELECT 1 FROM ai_jobs WHERE ai_jobs.operation_id=ai_attempts.operation_id)
ORDER BY COALESCE(reconcile_after, updated_at), updated_at LIMIT $6`, AIAttemptStatusRunning, AIAttemptDispatchSubmitted, AIAttemptDispatchAccepted, AIAttemptDispatchUnknown, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIAttempt, 0, limit)
	for rows.Next() {
		attempt, scanErr := scanAIAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, attempt)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) CompleteAIAttempt(ctx context.Context, id, status, errorType string, completedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_attempts SET status=$1, error_type=$2, completed_at=$3, updated_at=$3 WHERE id=$4 AND status=$5`, status, errorType, completedAt, id, AIAttemptStatusRunning)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *PostgresRepository) ApplyUsageSettlement(ctx context.Context, settlement UsageSettlement) (bool, error) {
	record := settlement.Record
	usageDimensions, err := NormalizeUsageDimensions(record.UsageDimensions)
	if err != nil {
		return false, err
	}
	record.UsageDimensions = usageDimensions
	settlement.Record = record
	if err := validateUsageSettlement(settlement); err != nil {
		return false, err
	}
	for index := range settlement.OutboxEvents {
		normalizeTransactionalOutboxEvent(&settlement.OutboxEvents[index])
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if record.AttemptID != "" {
		var attemptExists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_attempts WHERE id=$1 AND operation_id=$2)`, record.AttemptID, record.OperationID).Scan(&attemptExists); err != nil {
			return false, err
		}
		if !attemptExists {
			return false, fmt.Errorf("ai attempt %q not found for operation", record.AttemptID)
		}
	}
	existingCount := 0
	for _, billing := range settlement.Ledgers {
		var current BillingLedgerEntry
		err = tx.QueryRowContext(ctx, `SELECT id,operation_id,attempt_id,usage_version,usage_record_id,request_fingerprint,purpose,amount_micros,currency,pricing_evaluation_id,pricing_rule_version_id,status,created_at FROM billing_ledger_entries WHERE operation_id=$1 AND attempt_id=$2 AND usage_version=$3 AND purpose=$4`, billing.OperationID, billing.AttemptID, billing.UsageVersion, billing.Purpose).Scan(
			&current.ID, &current.OperationID, &current.AttemptID, &current.UsageVersion, &current.UsageRecordID, &current.RequestFingerprint,
			&current.Purpose, &current.AmountMicros, &current.Currency, &current.PricingEvaluationID, &current.PricingRuleVersionID, &current.Status, &current.CreatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		existingCount++
		if !sameBillingLedgerContent(current, billing) {
			return false, ErrUsageLedgerConflict
		}
	}
	if existingCount > 0 {
		if existingCount != len(settlement.Ledgers) {
			return false, ErrUsageLedgerConflict
		}
		var existingUsageJSON []byte
		var existingStatus string
		var existingAmount *int64
		if err := tx.QueryRowContext(ctx, `SELECT usage_dimensions,pricing_status,usage_cost_micros FROM usage_records WHERE id=$1`, record.ID).Scan(&existingUsageJSON, &existingStatus, &existingAmount); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, ErrUsageLedgerConflict
			}
			return false, err
		}
		existingDimensions, err := ParseUsageDimensionsJSON(string(existingUsageJSON))
		if err != nil || !usageDimensionsEqual(existingDimensions, record.UsageDimensions) || existingStatus != record.PricingStatus || !optionalMoneyEqual(existingAmount, record.UsageCostMicros) {
			return false, ErrUsageLedgerConflict
		}
		return false, nil
	}
	if len(settlement.Ledgers) == 0 {
		var existingUsageJSON []byte
		var existingStatus string
		var existingAmount *int64
		err := tx.QueryRowContext(ctx, `SELECT usage_dimensions,pricing_status,usage_cost_micros FROM usage_records WHERE id=$1`, record.ID).Scan(&existingUsageJSON, &existingStatus, &existingAmount)
		if err == nil {
			existingDimensions, parseErr := ParseUsageDimensionsJSON(string(existingUsageJSON))
			if parseErr == nil && usageDimensionsEqual(existingDimensions, record.UsageDimensions) && existingStatus == record.PricingStatus && optionalMoneyEqual(existingAmount, record.UsageCostMicros) {
				return false, nil
			}
			return false, ErrUsageLedgerConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	for _, evaluation := range settlement.Evaluations {
		if err := insertPricingEvaluation(ctx, tx, evaluation); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE billing_hold_pricing_versions SET settlement_evaluation_id=$1 WHERE hold_id=(SELECT id FROM billing_holds WHERE operation_id=$2) AND purpose=$3 AND pricing_rule_version_id=$4`, evaluation.ID, evaluation.OperationID, evaluation.Purpose, evaluation.PricingRuleVersionID); err != nil {
			return false, err
		}
		if evaluation.Purpose == PricingPurposeUsageCost && evaluation.Status == PricingEvaluationStatusDisputed {
			if err := disputePostgresBillingHoldForUsage(ctx, tx, record, evaluation.FailureCode); err != nil {
				return false, err
			}
		}
	}
	for _, billing := range settlement.Ledgers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO billing_ledger_entries(id,operation_id,attempt_id,usage_version,usage_record_id,request_fingerprint,purpose,amount_micros,currency,pricing_evaluation_id,pricing_rule_version_id,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, billing.ID, billing.OperationID, billing.AttemptID, billing.UsageVersion, billing.UsageRecordID, billing.RequestFingerprint, billing.Purpose, billing.AmountMicros, billing.Currency, billing.PricingEvaluationID, billing.PricingRuleVersionID, billing.Status, billing.CreatedAt); err != nil {
			return false, err
		}
		if billing.Purpose == PricingPurposeUsageCost {
			if err := settlePostgresBillingHoldForUsage(ctx, tx, record, billing); err != nil {
				return false, err
			}
		}
	}
	if record.PricingStatus == "unpriced" {
		if err := settlePostgresUnpricedUsage(ctx, tx, record); err != nil {
			return false, err
		}
	}
	if err := saveUsageRecord(ctx, tx, record); err != nil {
		return false, err
	}
	for _, outbox := range settlement.OutboxEvents {
		if err := insertTransactionalOutboxEvent(ctx, tx, outbox); err != nil {
			return false, err
		}
	}
	for _, event := range settlement.PlatformEvents {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO platform_usage_delivery_events(id, sink_id, usage_record_id, event_id, payload_json, status, attempt_count, max_attempts, next_attempt_at, lease_until, lease_token, delivered_at, last_http_status, last_error, target_hint, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL,'',NULL,0,'',$10,$11,$12)
`, event.ID, event.SinkID, event.UsageRecordID, event.EventID, event.PayloadJSON, event.Status, event.AttemptCount, event.MaxAttempts, event.NextAttemptAt, event.TargetHint, event.CreatedAt, event.UpdatedAt); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func validateUsageSettlement(settlement UsageSettlement) error {
	record := settlement.Record
	if strings.TrimSpace(record.OperationID) == "" || record.UsageVersion <= 0 || record.ID == "" || record.RequestFingerprint == "" {
		return ErrUsageLedgerConflict
	}
	evaluations := make(map[string]PricingEvaluation, len(settlement.Evaluations))
	for _, evaluation := range settlement.Evaluations {
		if evaluation.ID == "" || evaluation.OperationID != record.OperationID || evaluation.AttemptID != record.AttemptID || evaluation.UsageRecordID != record.ID || evaluation.UsageVersion != record.UsageVersion || evaluation.Phase != pricing.PhaseSettlement {
			return ErrUsageLedgerConflict
		}
		evaluations[evaluation.ID] = evaluation
	}
	purposes := make(map[string]struct{}, len(settlement.Ledgers))
	for _, billing := range settlement.Ledgers {
		if record.OperationID != billing.OperationID || record.AttemptID != billing.AttemptID || record.UsageVersion != billing.UsageVersion || record.ID != billing.UsageRecordID || record.RequestFingerprint != billing.RequestFingerprint || billing.Status != BillingLedgerStatusApplied || billing.Currency != pricing.CurrencyUSD || billing.AmountMicros < 0 {
			return ErrUsageLedgerConflict
		}
		evaluation, found := evaluations[billing.PricingEvaluationID]
		if !found || evaluation.Status != PricingEvaluationStatusSuccess || evaluation.AmountMicros == nil || *evaluation.AmountMicros != billing.AmountMicros || evaluation.Purpose != billing.Purpose || evaluation.PricingRuleVersionID != billing.PricingRuleVersionID {
			return ErrUsageLedgerConflict
		}
		if _, duplicate := purposes[billing.Purpose]; duplicate {
			return ErrUsageLedgerConflict
		}
		purposes[billing.Purpose] = struct{}{}
	}
	return nil
}

func sameBillingLedgerIdentity(left, right BillingLedgerEntry) bool {
	return left.OperationID == right.OperationID && left.AttemptID == right.AttemptID && left.UsageVersion == right.UsageVersion && left.Purpose == right.Purpose
}

func sameBillingLedgerContent(left, right BillingLedgerEntry) bool {
	return sameBillingLedgerIdentity(left, right) && left.UsageRecordID == right.UsageRecordID && left.RequestFingerprint == right.RequestFingerprint && left.AmountMicros == right.AmountMicros && left.Currency == right.Currency && left.PricingEvaluationID == right.PricingEvaluationID && left.PricingRuleVersionID == right.PricingRuleVersionID
}

func optionalMoneyEqual(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (r *PostgresRepository) ListBillingLedgerEntries(ctx context.Context, operationID string) ([]BillingLedgerEntry, error) {
	query := `SELECT id,operation_id,attempt_id,usage_version,usage_record_id,request_fingerprint,purpose,amount_micros,currency,pricing_evaluation_id,pricing_rule_version_id,status,created_at FROM billing_ledger_entries`
	args := []any{}
	if strings.TrimSpace(operationID) != "" {
		query += ` WHERE operation_id=$1`
		args = append(args, strings.TrimSpace(operationID))
	}
	query += ` ORDER BY created_at, id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BillingLedgerEntry, 0)
	for rows.Next() {
		var entry BillingLedgerEntry
		if err := rows.Scan(&entry.ID, &entry.OperationID, &entry.AttemptID, &entry.UsageVersion, &entry.UsageRecordID, &entry.RequestFingerprint, &entry.Purpose, &entry.AmountMicros, &entry.Currency, &entry.PricingEvaluationID, &entry.PricingRuleVersionID, &entry.Status, &entry.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListTransactionalOutboxEvents(ctx context.Context, aggregateID string) ([]TransactionalOutboxEvent, error) {
	query := `SELECT id, aggregate_type, aggregate_id, event_type, event_version, payload_json, status, available_at, attempt_count, max_attempts, lease_until, lease_token, last_error, published_at, created_at, updated_at FROM transactional_outbox`
	args := []any{}
	if strings.TrimSpace(aggregateID) != "" {
		query += ` WHERE aggregate_id=$1`
		args = append(args, strings.TrimSpace(aggregateID))
	}
	query += ` ORDER BY created_at, id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TransactionalOutboxEvent, 0)
	for rows.Next() {
		event, err := scanTransactionalOutboxEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ClaimDueTransactionalOutboxEvents(ctx context.Context, now, leaseUntil time.Time, leaseToken string, limit int) ([]TransactionalOutboxEvent, error) {
	if limit <= 0 || strings.TrimSpace(leaseToken) == "" {
		return []TransactionalOutboxEvent{}, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT event.id, event.aggregate_type, event.aggregate_id, event.event_type, event.event_version, event.payload_json,
       event.status, event.available_at, event.attempt_count, event.max_attempts, event.lease_until, event.lease_token,
       event.last_error, event.published_at, event.created_at, event.updated_at
FROM transactional_outbox event
WHERE event.status IN ($1, $2)
  AND event.available_at <= $3
  AND (event.status = $1 OR event.lease_until IS NULL OR event.lease_until <= $3)
  AND NOT EXISTS (
    SELECT 1 FROM transactional_outbox earlier
    WHERE earlier.aggregate_type = event.aggregate_type
      AND earlier.aggregate_id = event.aggregate_id
      AND earlier.event_version < event.event_version
      AND earlier.status NOT IN ($4, $5)
  )
ORDER BY event.available_at ASC, event.created_at ASC
FOR UPDATE SKIP LOCKED
LIMIT $6`, OutboxStatusPending, OutboxStatusPublishing, now, OutboxStatusPublished, OutboxStatusDeadLetter, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed := []TransactionalOutboxEvent{}
	for rows.Next() {
		event, scanErr := scanTransactionalOutboxEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		claimed = append(claimed, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range claimed {
		event := &claimed[index]
		if _, err := tx.ExecContext(ctx, `UPDATE transactional_outbox SET status=$1, attempt_count=attempt_count+1, lease_until=$2, lease_token=$3, updated_at=$4 WHERE id=$5`, OutboxStatusPublishing, leaseUntil, leaseToken, now, event.ID); err != nil {
			return nil, err
		}
		event.Status = OutboxStatusPublishing
		event.AttemptCount++
		event.LeaseUntil = &leaseUntil
		event.LeaseToken = leaseToken
		event.UpdatedAt = now
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *PostgresRepository) CompleteTransactionalOutboxEvent(ctx context.Context, id, leaseToken string, publishedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE transactional_outbox SET status=$1, published_at=$2, lease_until=NULL, lease_token='', last_error='', updated_at=$2 WHERE id=$3 AND status=$4 AND lease_token=$5`, OutboxStatusPublished, publishedAt, id, OutboxStatusPublishing, leaseToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("transactional outbox event is not leased")
	}
	return nil
}

func (r *PostgresRepository) RescheduleTransactionalOutboxEvent(ctx context.Context, id, leaseToken string, nextAttemptAt time.Time, lastError string, deadLetter bool, updatedAt time.Time) error {
	status := OutboxStatusPending
	if deadLetter {
		status = OutboxStatusDeadLetter
	}
	result, err := r.db.ExecContext(ctx, `UPDATE transactional_outbox SET status=$1, available_at=$2, lease_until=NULL, lease_token='', last_error=$3, updated_at=$4 WHERE id=$5 AND status=$6 AND lease_token=$7`, status, nextAttemptAt, trimTransactionalOutboxError(lastError), updatedAt, id, OutboxStatusPublishing, leaseToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("transactional outbox event is not leased")
	}
	return nil
}

func (r *PostgresRepository) RequeueTransactionalOutboxEvent(ctx context.Context, id string, nextAttemptAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE transactional_outbox SET status=$1, available_at=$2, attempt_count=0, lease_until=NULL, lease_token='', last_error='', updated_at=$2 WHERE id=$3 AND status=$4`, OutboxStatusPending, nextAttemptAt, id, OutboxStatusDeadLetter)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("transactional outbox event not found or not dead-lettered")
	}
	return nil
}

func scanTransactionalOutboxEvent(scanner apiKeyScanner) (TransactionalOutboxEvent, error) {
	var event TransactionalOutboxEvent
	var leaseUntil, publishedAt sql.NullTime
	if err := scanner.Scan(
		&event.ID, &event.AggregateType, &event.AggregateID, &event.EventType, &event.EventVersion, &event.PayloadJSON,
		&event.Status, &event.AvailableAt, &event.AttemptCount, &event.MaxAttempts, &leaseUntil, &event.LeaseToken,
		&event.LastError, &publishedAt, &event.CreatedAt, &event.UpdatedAt,
	); err != nil {
		return TransactionalOutboxEvent{}, err
	}
	if leaseUntil.Valid {
		event.LeaseUntil = &leaseUntil.Time
	}
	if publishedAt.Valid {
		event.PublishedAt = &publishedAt.Time
	}
	normalizeTransactionalOutboxEvent(&event)
	return event, nil
}

func normalizeTransactionalOutboxEvent(event *TransactionalOutboxEvent) {
	if event.MaxAttempts <= 0 {
		event.MaxAttempts = OutboxDefaultMaxAttempts
	}
}

func trimTransactionalOutboxError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1024 {
		return value[:1024]
	}
	return value
}
