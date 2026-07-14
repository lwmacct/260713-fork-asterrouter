package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const aiJobSelectColumns = `id, operation_id, profile_scope, tenant_id, credential_id, credential_source,
integration_id, principal_type, principal_id, external_subject_reference, request_fingerprint, idempotency_key,
protocol, operation, modality, model, artifact_policy, request_payload_ciphertext, status, status_version, priority,
next_eligible_at, queue_lease_until, queue_lease_token, queue_worker_id, fence_token, error_type,
created_at, updated_at, completed_at, expires_at`

type aiJobExecutor interface {
	usageRecordExecutor
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *MemoryRepository) CreateDurableAIJob(_ context.Context, operation AIOperation, job AIJob, event AIJobEvent, outbox TransactionalOutboxEvent) (AIJob, bool, error) {
	if err := validateDurableAIJobAdmission(operation, job, event, outbox); err != nil {
		return AIJob{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.aiJobs {
		if sameAIJobIdempotencyScope(current, job) {
			return current, false, nil
		}
	}
	if _, exists := r.aiOperations[operation.ID]; exists {
		return AIJob{}, false, fmt.Errorf("ai operation %q already exists", operation.ID)
	}
	if _, exists := r.aiJobs[job.ID]; exists {
		return AIJob{}, false, fmt.Errorf("ai job %q already exists", job.ID)
	}
	if _, exists := r.aiJobEvents[event.ID]; exists {
		return AIJob{}, false, fmt.Errorf("ai job event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return AIJob{}, false, err
	}
	r.aiOperations[operation.ID] = operation
	r.aiJobs[job.ID] = job
	r.aiJobEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return job, true, nil
}

func (r *MemoryRepository) FindAIJob(_ context.Context, id string) (AIJob, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, found := r.aiJobs[id]
	return job, found, nil
}

func (r *MemoryRepository) FindAIJobByOperationID(_ context.Context, operationID string) (AIJob, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, job := range r.aiJobs {
		if job.OperationID == strings.TrimSpace(operationID) {
			return job, true, nil
		}
	}
	return AIJob{}, false, nil
}

func (r *MemoryRepository) FindOwnedAIJob(_ context.Context, id string, owner AIJobOwner) (AIJob, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, found := r.aiJobs[id]
	if !found || !aiJobOwnerMatches(job, owner) {
		return AIJob{}, false, nil
	}
	return job, true, nil
}

func (r *MemoryRepository) RequestAIJobCancellation(_ context.Context, id string, owner AIJobOwner, requestedAt time.Time) (AIJob, bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.aiJobs[id]
	if !found || !aiJobOwnerMatches(job, owner) {
		return AIJob{}, false, false, nil
	}
	switch job.Status {
	case AIJobStatusCanceling, AIJobStatusCanceled:
		return job, false, true, nil
	case AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusUnknown, AIJobStatusExpired:
		return job, false, true, ErrAIJobNotCancelable
	}
	toStatus := AIJobStatusCanceling
	if job.Status == AIJobStatusAccepted || job.Status == AIJobStatusQueued {
		toStatus = AIJobStatusCanceled
	}
	updated, event, outbox, err := prepareAIJobTransition(job, toStatus, "client_requested", requestedAt)
	if err != nil {
		return AIJob{}, false, true, err
	}
	if job.Status == AIJobStatusDispatching {
		updated.FenceToken++
	}
	if _, exists := r.aiJobEvents[event.ID]; exists {
		return AIJob{}, false, true, fmt.Errorf("ai job event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return AIJob{}, false, true, err
	}
	operation, ok := r.aiOperations[job.OperationID]
	if !ok {
		return AIJob{}, false, true, fmt.Errorf("ai operation %q not found", job.OperationID)
	}
	applyMemoryOperationJobStatus(&operation, updated.Status, updated.ErrorType, requestedAt)
	r.aiOperations[operation.ID] = operation
	r.aiJobs[id] = updated
	r.aiJobEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return updated, true, true, nil
}

func (r *MemoryRepository) ClaimQueuedAIJobs(_ context.Context, now, leaseUntil time.Time, workerID, leaseToken string, limit int) ([]AIJob, error) {
	if limit <= 0 || strings.TrimSpace(workerID) == "" || strings.TrimSpace(leaseToken) == "" || !leaseUntil.After(now) {
		return []AIJob{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	candidates := make([]AIJob, 0, len(r.aiJobs))
	jobs := make([]AIJob, 0, len(r.aiJobs))
	for _, job := range r.aiJobs {
		jobs = append(jobs, job)
		if aiJobReadyForClaim(job, now) {
			candidates = append(candidates, job)
		}
	}
	activities := make([]aiJobDispatchActivity, 0)
	for _, event := range r.aiJobEvents {
		if event.EventType != AIJobEventScheduled {
			continue
		}
		if job, found := r.aiJobs[event.JobID]; found {
			activities = append(activities, aiJobDispatchActivity{Job: job, DispatchedAt: event.CreatedAt})
		}
	}
	candidates = rankAIJobFairCandidates(candidates, jobs, activities, now, limit)
	claimed := make([]AIJob, 0, len(candidates))
	for _, candidate := range candidates {
		updated, event, outbox, err := prepareAIJobClaim(candidate, now)
		if err != nil {
			return nil, err
		}
		updated.FenceToken++
		updated.QueueLeaseUntil = timePointer(leaseUntil)
		updated.QueueLeaseToken = leaseToken
		updated.QueueWorkerID = workerID
		if _, exists := r.aiJobEvents[event.ID]; exists {
			return nil, fmt.Errorf("ai job event %q already exists", event.ID)
		}
		if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
			return nil, err
		}
		operation := r.aiOperations[candidate.OperationID]
		applyMemoryOperationJobStatus(&operation, updated.Status, updated.ErrorType, now)
		r.aiOperations[operation.ID] = operation
		r.aiJobs[updated.ID] = updated
		r.aiJobEvents[event.ID] = event
		r.transactionalOutboxEvents[outbox.ID] = outbox
		claimed = append(claimed, updated)
	}
	return claimed, nil
}

func (r *MemoryRepository) ListAIJobsForDeliveryRebuild(_ context.Context, now time.Time, limit int) ([]AIJob, error) {
	if limit <= 0 {
		return []AIJob{}, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	jobs := make([]AIJob, 0)
	for _, job := range r.aiJobs {
		if job.Status == AIJobStatusDispatching && job.QueueLeaseUntil != nil && job.QueueLeaseUntil.After(now) && strings.TrimSpace(job.QueueLeaseToken) != "" {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if !jobs[i].UpdatedAt.Equal(jobs[j].UpdatedAt) {
			return jobs[i].UpdatedAt.Before(jobs[j].UpdatedAt)
		}
		return jobs[i].ID < jobs[j].ID
	})
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (r *MemoryRepository) ExtendAIJobQueueLease(_ context.Context, id string, expectedVersion int, fenceToken int64, leaseToken string, leaseUntil, extendedAt time.Time) (AIJob, bool, error) {
	if !leaseUntil.After(extendedAt) || strings.TrimSpace(leaseToken) == "" {
		return AIJob{}, false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.aiJobs[id]
	if !found || job.Status != AIJobStatusDispatching || job.StatusVersion != expectedVersion || job.FenceToken != fenceToken ||
		job.QueueLeaseUntil == nil || !job.QueueLeaseUntil.After(extendedAt) || job.QueueLeaseToken != leaseToken {
		return job, false, nil
	}
	if !leaseUntil.After(*job.QueueLeaseUntil) {
		return job, true, nil
	}
	job.QueueLeaseUntil = timePointer(leaseUntil)
	if extendedAt.After(job.UpdatedAt) {
		job.UpdatedAt = extendedAt
	}
	r.aiJobs[id] = job
	return job, true, nil
}

func (r *MemoryRepository) TransitionAIJob(_ context.Context, id string, expectedVersion int, fenceToken int64, toStatus, reason string, transitionedAt time.Time) (AIJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.aiJobs[id]
	if !found || job.StatusVersion != expectedVersion || (job.FenceToken > 0 && job.FenceToken != fenceToken) {
		return job, false, nil
	}
	updated, event, outbox, err := prepareAIJobTransition(job, toStatus, reason, transitionedAt)
	if err != nil {
		return AIJob{}, false, err
	}
	if _, exists := r.aiJobEvents[event.ID]; exists {
		return AIJob{}, false, fmt.Errorf("ai job event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return AIJob{}, false, err
	}
	operation, ok := r.aiOperations[job.OperationID]
	if !ok {
		return AIJob{}, false, fmt.Errorf("ai operation %q not found", job.OperationID)
	}
	applyMemoryOperationJobStatus(&operation, updated.Status, updated.ErrorType, transitionedAt)
	r.aiOperations[operation.ID] = operation
	r.aiJobs[id] = updated
	r.aiJobEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return updated, true, nil
}

func (r *MemoryRepository) RequeueAIJob(_ context.Context, id string, expectedVersion int, fenceToken int64, reason string, nextEligibleAt, transitionedAt time.Time) (AIJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, found := r.aiJobs[id]
	if !found || job.StatusVersion != expectedVersion || (job.FenceToken > 0 && job.FenceToken != fenceToken) {
		return job, false, nil
	}
	updated, event, outbox, err := prepareAIJobTransition(job, AIJobStatusQueued, reason, transitionedAt)
	if err != nil {
		return AIJob{}, false, err
	}
	if nextEligibleAt.After(transitionedAt) {
		updated.NextEligibleAt = nextEligibleAt
	} else {
		updated.NextEligibleAt = transitionedAt
	}
	if _, exists := r.aiJobEvents[event.ID]; exists {
		return AIJob{}, false, fmt.Errorf("ai job event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return AIJob{}, false, err
	}
	operation, ok := r.aiOperations[job.OperationID]
	if !ok {
		return AIJob{}, false, fmt.Errorf("ai operation %q not found", job.OperationID)
	}
	applyMemoryOperationJobStatus(&operation, updated.Status, updated.ErrorType, transitionedAt)
	r.aiOperations[operation.ID] = operation
	r.aiJobs[id] = updated
	r.aiJobEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return updated, true, nil
}

func (r *MemoryRepository) ListAIJobEvents(_ context.Context, jobID string) ([]AIJobEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AIJobEvent, 0)
	for _, event := range r.aiJobEvents {
		if jobID == "" || event.JobID == jobID {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].JobID == out[j].JobID {
			return out[i].Version < out[j].Version
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *PostgresRepository) CreateDurableAIJob(ctx context.Context, operation AIOperation, job AIJob, event AIJobEvent, outbox TransactionalOutboxEvent) (AIJob, bool, error) {
	if err := validateDurableAIJobAdmission(operation, job, event, outbox); err != nil {
		return AIJob{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := insertAIOperation(ctx, tx, operation)
	if err != nil {
		return AIJob{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIJob{}, false, err
	}
	if rows == 0 {
		existing, found, findErr := findAIJobByIdempotencyScope(ctx, tx, job)
		if findErr != nil {
			return AIJob{}, false, findErr
		}
		if !found {
			return AIJob{}, false, ErrGatewayIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return AIJob{}, false, err
		}
		return existing, false, nil
	}
	if err := insertAIJob(ctx, tx, job); err != nil {
		return AIJob{}, false, err
	}
	if err := insertAIJobEvent(ctx, tx, event); err != nil {
		return AIJob{}, false, err
	}
	if err := insertTransactionalOutboxEvent(ctx, tx, outbox); err != nil {
		return AIJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIJob{}, false, err
	}
	return job, true, nil
}

func (r *PostgresRepository) FindAIJob(ctx context.Context, id string) (AIJob, bool, error) {
	job, err := scanAIJob(r.db.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	return job, err == nil, err
}

func (r *PostgresRepository) FindAIJobByOperationID(ctx context.Context, operationID string) (AIJob, bool, error) {
	job, err := scanAIJob(r.db.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE operation_id=$1`, strings.TrimSpace(operationID)))
	if err == sql.ErrNoRows {
		return AIJob{}, false, nil
	}
	return job, err == nil, err
}

func (r *PostgresRepository) FindOwnedAIJob(ctx context.Context, id string, owner AIJobOwner) (AIJob, bool, error) {
	job, err := scanAIJob(r.db.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs
WHERE id=$1 AND profile_scope=$2 AND tenant_id=$3 AND integration_id=$4 AND principal_type=$5 AND principal_id=$6 AND external_subject_reference=$7`,
		id, owner.ProfileScope, owner.TenantID, owner.IntegrationID, owner.PrincipalType, owner.PrincipalID, owner.ExternalSubjectReference))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	return job, err == nil, err
}

func (r *PostgresRepository) RequestAIJobCancellation(ctx context.Context, id string, owner AIJobOwner, requestedAt time.Time) (AIJob, bool, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIJob{}, false, false, err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs
WHERE id=$1 AND profile_scope=$2 AND tenant_id=$3 AND integration_id=$4 AND principal_type=$5 AND principal_id=$6 AND external_subject_reference=$7 FOR UPDATE`,
		id, owner.ProfileScope, owner.TenantID, owner.IntegrationID, owner.PrincipalType, owner.PrincipalID, owner.ExternalSubjectReference))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, false, nil
	}
	if err != nil {
		return AIJob{}, false, false, err
	}
	switch job.Status {
	case AIJobStatusCanceling, AIJobStatusCanceled:
		if err := tx.Commit(); err != nil {
			return AIJob{}, false, true, err
		}
		return job, false, true, nil
	case AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusUnknown, AIJobStatusExpired:
		return job, false, true, ErrAIJobNotCancelable
	}
	toStatus := AIJobStatusCanceling
	if job.Status == AIJobStatusAccepted || job.Status == AIJobStatusQueued {
		toStatus = AIJobStatusCanceled
	}
	updated, event, outbox, err := prepareAIJobTransition(job, toStatus, "client_requested", requestedAt)
	if err != nil {
		return AIJob{}, false, true, err
	}
	if job.Status == AIJobStatusDispatching {
		updated.FenceToken++
	}
	if err := persistAIJobTransition(ctx, tx, job, updated, event, outbox); err != nil {
		return AIJob{}, false, true, err
	}
	if err := tx.Commit(); err != nil {
		return AIJob{}, false, true, err
	}
	return updated, true, true, nil
}

func (r *PostgresRepository) ClaimQueuedAIJobs(ctx context.Context, now, leaseUntil time.Time, workerID, leaseToken string, limit int) ([]AIJob, error) {
	if limit <= 0 || strings.TrimSpace(workerID) == "" || strings.TrimSpace(leaseToken) == "" || !leaseUntil.After(now) {
		return []AIJob{}, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	candidates, err := listPostgresAIJobFairCandidates(ctx, tx, now, limit)
	if err != nil {
		return nil, err
	}
	inFlight, err := listPostgresAIJobInFlight(ctx, tx)
	if err != nil {
		return nil, err
	}
	activities, err := listPostgresAIJobDispatchActivity(ctx, tx)
	if err != nil {
		return nil, err
	}
	candidates = rankAIJobFairCandidates(candidates, inFlight, activities, now, len(candidates))
	claimed := make([]AIJob, 0, len(candidates))
	for _, candidate := range candidates {
		if len(claimed) >= limit {
			break
		}
		current, found, lockErr := lockPostgresAIJobForClaim(ctx, tx, candidate.ID, now)
		if lockErr != nil {
			return nil, lockErr
		}
		if !found {
			continue
		}
		updated, event, outbox, transitionErr := prepareAIJobClaim(current, now)
		if transitionErr != nil {
			return nil, transitionErr
		}
		updated.FenceToken++
		updated.QueueLeaseUntil = timePointer(leaseUntil)
		updated.QueueLeaseToken = leaseToken
		updated.QueueWorkerID = workerID
		if err := persistAIJobTransition(ctx, tx, current, updated, event, outbox); err != nil {
			return nil, err
		}
		claimed = append(claimed, updated)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *PostgresRepository) ListAIJobsForDeliveryRebuild(ctx context.Context, now time.Time, limit int) ([]AIJob, error) {
	if limit <= 0 {
		return []AIJob{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs
WHERE status=$1 AND queue_lease_until>$2 AND queue_lease_token<>''
ORDER BY updated_at, id LIMIT $3`, AIJobStatusDispatching, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]AIJob, 0)
	for rows.Next() {
		job, scanErr := scanAIJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *PostgresRepository) ExtendAIJobQueueLease(ctx context.Context, id string, expectedVersion int, fenceToken int64, leaseToken string, leaseUntil, extendedAt time.Time) (AIJob, bool, error) {
	if !leaseUntil.After(extendedAt) || strings.TrimSpace(leaseToken) == "" {
		return AIJob{}, false, nil
	}
	job, err := scanAIJob(r.db.QueryRowContext(ctx, `UPDATE ai_jobs
SET queue_lease_until=GREATEST(queue_lease_until,$1), updated_at=GREATEST(updated_at,$2)
WHERE id=$3 AND status=$4 AND status_version=$5 AND fence_token=$6 AND queue_lease_token=$7 AND queue_lease_until>$2
RETURNING `+aiJobSelectColumns, leaseUntil, extendedAt, id, AIJobStatusDispatching, expectedVersion, fenceToken, leaseToken))
	if err == nil {
		return job, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, err
	}
	current, _, findErr := r.FindAIJob(ctx, id)
	if findErr != nil {
		return AIJob{}, false, findErr
	}
	return current, false, nil
}

func (r *PostgresRepository) TransitionAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, toStatus, reason string, transitionedAt time.Time) (AIJob, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	if err != nil {
		return AIJob{}, false, err
	}
	if job.StatusVersion != expectedVersion || (job.FenceToken > 0 && job.FenceToken != fenceToken) {
		return job, false, nil
	}
	updated, event, outbox, err := prepareAIJobTransition(job, toStatus, reason, transitionedAt)
	if err != nil {
		return AIJob{}, false, err
	}
	if err := persistAIJobTransition(ctx, tx, job, updated, event, outbox); err != nil {
		return AIJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIJob{}, false, err
	}
	return updated, true, nil
}

func (r *PostgresRepository) RequeueAIJob(ctx context.Context, id string, expectedVersion int, fenceToken int64, reason string, nextEligibleAt, transitionedAt time.Time) (AIJob, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AIJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	if err != nil {
		return AIJob{}, false, err
	}
	if job.StatusVersion != expectedVersion || (job.FenceToken > 0 && job.FenceToken != fenceToken) {
		return job, false, nil
	}
	updated, event, outbox, err := prepareAIJobTransition(job, AIJobStatusQueued, reason, transitionedAt)
	if err != nil {
		return AIJob{}, false, err
	}
	if nextEligibleAt.After(transitionedAt) {
		updated.NextEligibleAt = nextEligibleAt
	} else {
		updated.NextEligibleAt = transitionedAt
	}
	if err := persistAIJobTransition(ctx, tx, job, updated, event, outbox); err != nil {
		return AIJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AIJob{}, false, err
	}
	return updated, true, nil
}

func (r *PostgresRepository) ListAIJobEvents(ctx context.Context, jobID string) ([]AIJobEvent, error) {
	query := `SELECT id, job_id, version, event_type, from_status, to_status, reason, created_at FROM ai_job_events`
	args := []any{}
	if strings.TrimSpace(jobID) != "" {
		query += ` WHERE job_id=$1`
		args = append(args, strings.TrimSpace(jobID))
	}
	query += ` ORDER BY job_id, version`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AIJobEvent, 0)
	for rows.Next() {
		var event AIJobEvent
		if err := rows.Scan(&event.ID, &event.JobID, &event.Version, &event.EventType, &event.FromStatus, &event.ToStatus, &event.Reason, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func validateDurableAIJobAdmission(operation AIOperation, job AIJob, event AIJobEvent, outbox TransactionalOutboxEvent) error {
	if operation.ID == "" || operation.ID != job.OperationID || operation.Lane != "durable" || operation.IdempotencyKey == "" ||
		job.ID == "" || job.Status != AIJobStatusQueued || job.StatusVersion != 1 || job.RequestFingerprint == "" ||
		job.RequestFingerprint != operation.RequestFingerprint || job.IdempotencyKey != operation.IdempotencyKey || job.RequestPayloadCiphertext == "" ||
		event.JobID != job.ID || event.Version != job.StatusVersion || event.EventType != AIJobEventQueued ||
		outbox.AggregateType != AIJobOutboxAggregate || outbox.AggregateID != job.ID || outbox.EventVersion != job.StatusVersion || outbox.EventType != event.EventType {
		return errors.New("invalid durable ai job admission records")
	}
	normalizeTransactionalOutboxEvent(&outbox)
	return nil
}

func sameAIJobIdempotencyScope(left, right AIJob) bool {
	return left.IdempotencyKey != "" && left.ProfileScope == right.ProfileScope && left.TenantID == right.TenantID &&
		left.CredentialSource == right.CredentialSource && left.CredentialID == right.CredentialID && left.IntegrationID == right.IntegrationID &&
		left.PrincipalType == right.PrincipalType && left.PrincipalID == right.PrincipalID &&
		left.ExternalSubjectReference == right.ExternalSubjectReference && left.Operation == right.Operation && left.IdempotencyKey == right.IdempotencyKey
}

func validateMemoryOutboxInsert(events map[string]TransactionalOutboxEvent, candidate TransactionalOutboxEvent) error {
	if _, exists := events[candidate.ID]; exists {
		return fmt.Errorf("transactional outbox event %q already exists", candidate.ID)
	}
	for _, event := range events {
		if event.AggregateType == candidate.AggregateType && event.AggregateID == candidate.AggregateID && event.EventType == candidate.EventType && event.EventVersion == candidate.EventVersion {
			return fmt.Errorf("transactional outbox event is not unique")
		}
	}
	return nil
}

func prepareAIJobTransition(job AIJob, toStatus, reason string, transitionedAt time.Time) (AIJob, AIJobEvent, TransactionalOutboxEvent, error) {
	if !aiJobStatusTransitionAllowed(job.Status, toStatus, reason) {
		return AIJob{}, AIJobEvent{}, TransactionalOutboxEvent{}, fmt.Errorf("invalid ai job transition %s -> %s", job.Status, toStatus)
	}
	fromStatus := job.Status
	job.Status = toStatus
	job.StatusVersion++
	job.UpdatedAt = transitionedAt
	if toStatus != AIJobStatusDispatching {
		job.QueueLeaseUntil = nil
		job.QueueLeaseToken = ""
		job.QueueWorkerID = ""
	}
	if toStatus == AIJobStatusQueued {
		job.NextEligibleAt = transitionedAt
	}
	job.ErrorType = ""
	if toStatus == AIJobStatusFailed || toStatus == AIJobStatusUnknown {
		job.ErrorType = strings.TrimSpace(reason)
	}
	if aiJobTerminalStatus(toStatus) && toStatus != AIJobStatusExpired {
		job.CompletedAt = timePointer(transitionedAt)
	}
	event, outbox, err := newAIJobTransitionRecords(job, fromStatus, reason, transitionedAt)
	return job, event, outbox, err
}

func prepareAIJobClaim(job AIJob, claimedAt time.Time) (AIJob, AIJobEvent, TransactionalOutboxEvent, error) {
	if job.Status == AIJobStatusDispatching {
		return prepareAIJobLeaseReassignment(job, claimedAt)
	}
	return prepareAIJobTransition(job, AIJobStatusDispatching, "", claimedAt)
}

func prepareAIJobLeaseReassignment(job AIJob, transitionedAt time.Time) (AIJob, AIJobEvent, TransactionalOutboxEvent, error) {
	if job.Status != AIJobStatusDispatching || job.QueueLeaseUntil == nil || job.QueueLeaseUntil.After(transitionedAt) {
		return AIJob{}, AIJobEvent{}, TransactionalOutboxEvent{}, errors.New("ai job dispatch lease is not reclaimable")
	}
	job.StatusVersion++
	job.UpdatedAt = transitionedAt
	event, outbox, err := newAIJobEventRecords(job, AIJobStatusDispatching, "lease_expired", AIJobEventLeaseReassigned, transitionedAt)
	return job, event, outbox, err
}

func newAIJobTransitionRecords(job AIJob, fromStatus, reason string, occurredAt time.Time) (AIJobEvent, TransactionalOutboxEvent, error) {
	eventType := aiJobEventType(job.Status)
	return newAIJobEventRecords(job, fromStatus, reason, eventType, occurredAt)
}

func newAIJobEventRecords(job AIJob, fromStatus, reason, eventType string, occurredAt time.Time) (AIJobEvent, TransactionalOutboxEvent, error) {
	if eventType == "" || job.StatusVersion <= 0 {
		return AIJobEvent{}, TransactionalOutboxEvent{}, errors.New("invalid ai job event state")
	}
	identity := job.ID + "\x00" + fmt.Sprintf("%d", job.StatusVersion)
	digest := prefix(hashAPIKey(identity), 24)
	event := AIJobEvent{
		ID: "job_event_" + digest, JobID: job.ID, Version: job.StatusVersion, EventType: eventType,
		FromStatus: fromStatus, ToStatus: job.Status, Reason: strings.TrimSpace(reason), CreatedAt: occurredAt,
	}
	payload, err := json.Marshal(struct {
		EventID     string `json:"event_id"`
		JobID       string `json:"job_id"`
		OperationID string `json:"operation_id"`
		Version     int    `json:"version"`
		EventType   string `json:"event_type"`
		Status      string `json:"status"`
	}{event.ID, job.ID, job.OperationID, job.StatusVersion, event.EventType, job.Status})
	if err != nil {
		return AIJobEvent{}, TransactionalOutboxEvent{}, err
	}
	outbox := TransactionalOutboxEvent{
		ID: "outbox_" + digest, AggregateType: AIJobOutboxAggregate, AggregateID: job.ID,
		EventType: event.EventType, EventVersion: event.Version, PayloadJSON: string(payload), Status: OutboxStatusPending,
		AvailableAt: occurredAt, MaxAttempts: OutboxDefaultMaxAttempts, CreatedAt: occurredAt, UpdatedAt: occurredAt,
	}
	return event, outbox, nil
}

func applyMemoryOperationJobStatus(operation *AIOperation, jobStatus, errorType string, at time.Time) {
	if operation == nil || operation.ID == "" {
		return
	}
	switch jobStatus {
	case AIJobStatusDispatching, AIJobStatusRunning, AIJobStatusCanceling, AIJobStatusUnknown:
		if operation.Status == AIOperationStatusAccepted {
			operation.Status = AIOperationStatusRunning
			operation.UpdatedAt = at
		}
	case AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusCanceled:
		if operation.Status != AIOperationStatusAccepted && operation.Status != AIOperationStatusRunning {
			return
		}
		operation.Status = mapAIJobToOperationStatus(jobStatus)
		operation.ErrorType = errorType
		operation.UpdatedAt = at
		operation.CompletedAt = timePointer(at)
	}
}

func mapAIJobToOperationStatus(status string) string {
	switch status {
	case AIJobStatusSucceeded:
		return AIOperationStatusSucceeded
	case AIJobStatusCanceled:
		return AIOperationStatusCanceled
	default:
		return AIOperationStatusFailed
	}
}

func scanAIJob(scanner apiKeyScanner) (AIJob, error) {
	var job AIJob
	var leaseUntil, completedAt sql.NullTime
	if err := scanner.Scan(
		&job.ID, &job.OperationID, &job.ProfileScope, &job.TenantID, &job.CredentialID, &job.CredentialSource,
		&job.IntegrationID, &job.PrincipalType, &job.PrincipalID, &job.ExternalSubjectReference, &job.RequestFingerprint, &job.IdempotencyKey,
		&job.Protocol, &job.Operation, &job.Modality, &job.Model, &job.ArtifactPolicy, &job.RequestPayloadCiphertext, &job.Status, &job.StatusVersion,
		&job.Priority, &job.NextEligibleAt, &leaseUntil, &job.QueueLeaseToken, &job.QueueWorkerID, &job.FenceToken, &job.ErrorType,
		&job.CreatedAt, &job.UpdatedAt, &completedAt, &job.ExpiresAt,
	); err != nil {
		return AIJob{}, err
	}
	if leaseUntil.Valid {
		job.QueueLeaseUntil = timePointer(leaseUntil.Time)
	}
	if completedAt.Valid {
		job.CompletedAt = timePointer(completedAt.Time)
	}
	return job, nil
}

func insertAIJob(ctx context.Context, executor usageRecordExecutor, job AIJob) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO ai_jobs(
id, operation_id, profile_scope, tenant_id, credential_id, credential_source, integration_id, principal_type, principal_id,
external_subject_reference, request_fingerprint, idempotency_key, protocol, operation, modality, model, artifact_policy,
request_payload_ciphertext, status, status_version, priority, next_eligible_at, queue_lease_until, queue_lease_token, queue_worker_id,
fence_token, error_type, created_at, updated_at, completed_at, expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,NULL,'','',$23,$24,$25,$26,NULL,$27)`,
		job.ID, job.OperationID, job.ProfileScope, job.TenantID, job.CredentialID, job.CredentialSource, job.IntegrationID,
		job.PrincipalType, job.PrincipalID, job.ExternalSubjectReference, job.RequestFingerprint, job.IdempotencyKey,
		job.Protocol, job.Operation, job.Modality, job.Model, job.ArtifactPolicy, job.RequestPayloadCiphertext, job.Status, job.StatusVersion,
		job.Priority, job.NextEligibleAt, job.FenceToken, job.ErrorType, job.CreatedAt, job.UpdatedAt, job.ExpiresAt)
	return err
}

func insertAIJobEvent(ctx context.Context, executor usageRecordExecutor, event AIJobEvent) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO ai_job_events(id, job_id, version, event_type, from_status, to_status, reason, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, event.ID, event.JobID, event.Version, event.EventType, event.FromStatus, event.ToStatus, event.Reason, event.CreatedAt)
	return err
}

func insertTransactionalOutboxEvent(ctx context.Context, executor usageRecordExecutor, outbox TransactionalOutboxEvent) error {
	normalizeTransactionalOutboxEvent(&outbox)
	_, err := executor.ExecContext(ctx, `INSERT INTO transactional_outbox(
id, aggregate_type, aggregate_id, event_type, event_version, payload_json, status, available_at, attempt_count, max_attempts,
lease_until, lease_token, last_error, published_at, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,'','',NULL,$11,$12)`,
		outbox.ID, outbox.AggregateType, outbox.AggregateID, outbox.EventType, outbox.EventVersion, outbox.PayloadJSON,
		outbox.Status, outbox.AvailableAt, outbox.AttemptCount, outbox.MaxAttempts, outbox.CreatedAt, outbox.UpdatedAt)
	return err
}

func findAIJobByIdempotencyScope(ctx context.Context, executor aiJobExecutor, job AIJob) (AIJob, bool, error) {
	row := executor.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE
profile_scope=$1 AND tenant_id=$2 AND credential_source=$3 AND credential_id=$4 AND integration_id=$5 AND
principal_type=$6 AND principal_id=$7 AND external_subject_reference=$8 AND operation=$9 AND idempotency_key=$10`,
		job.ProfileScope, job.TenantID, job.CredentialSource, job.CredentialID, job.IntegrationID,
		job.PrincipalType, job.PrincipalID, job.ExternalSubjectReference, job.Operation, job.IdempotencyKey)
	found, err := scanAIJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AIJob{}, false, nil
	}
	return found, err == nil, err
}

func persistAIJobTransition(ctx context.Context, tx *sql.Tx, previous, updated AIJob, event AIJobEvent, outbox TransactionalOutboxEvent) error {
	result, err := tx.ExecContext(ctx, `UPDATE ai_jobs SET status=$1, status_version=$2, next_eligible_at=$3,
queue_lease_until=$4, queue_lease_token=$5, queue_worker_id=$6, fence_token=$7, error_type=$8,
updated_at=$9, completed_at=$10 WHERE id=$11 AND status_version=$12`,
		updated.Status, updated.StatusVersion, updated.NextEligibleAt, updated.QueueLeaseUntil, updated.QueueLeaseToken,
		updated.QueueWorkerID, updated.FenceToken, updated.ErrorType, updated.UpdatedAt, updated.CompletedAt, updated.ID, previous.StatusVersion)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrAIJobStateConflict
	}
	if err := updateOperationForAIJobStatus(ctx, tx, updated.OperationID, updated.Status, updated.ErrorType, updated.UpdatedAt); err != nil {
		return err
	}
	if err := insertAIJobEvent(ctx, tx, event); err != nil {
		return err
	}
	return insertTransactionalOutboxEvent(ctx, tx, outbox)
}

func updateOperationForAIJobStatus(ctx context.Context, tx *sql.Tx, operationID, jobStatus, errorType string, at time.Time) error {
	switch jobStatus {
	case AIJobStatusDispatching, AIJobStatusRunning, AIJobStatusCanceling, AIJobStatusUnknown:
		_, err := tx.ExecContext(ctx, `UPDATE ai_operations SET status=$1, updated_at=$2 WHERE id=$3 AND status=$4`,
			AIOperationStatusRunning, at, operationID, AIOperationStatusAccepted)
		return err
	case AIJobStatusSucceeded, AIJobStatusFailed, AIJobStatusCanceled:
		result, err := tx.ExecContext(ctx, `UPDATE ai_operations SET status=$1, error_type=$2, updated_at=$3, completed_at=$3
WHERE id=$4 AND status IN ($5,$6)`, mapAIJobToOperationStatus(jobStatus), errorType, at, operationID, AIOperationStatusAccepted, AIOperationStatusRunning)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return fmt.Errorf("ai operation %q is not active", operationID)
		}
	}
	return nil
}
