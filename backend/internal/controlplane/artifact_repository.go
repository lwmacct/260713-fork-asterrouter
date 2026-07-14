package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const artifactSelectColumns = `id, operation_id, COALESCE(job_id, ''), COALESCE(attempt_id, ''), COALESCE(source_artifact_id, ''), profile_scope, tenant_id, integration_id,
principal_type, principal_id, external_subject_reference, role, policy, status, status_version, media_type, size_bytes,
sha256, store_driver, store_key, external_reference, error_type, retain_until, created_at, updated_at, ready_at, delivered_at, deleted_at`

type artifactScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(scanner artifactScanner) (Artifact, error) {
	var artifact Artifact
	if err := scanner.Scan(
		&artifact.ID, &artifact.OperationID, &artifact.JobID, &artifact.AttemptID, &artifact.SourceArtifactID,
		&artifact.ProfileScope, &artifact.TenantID, &artifact.IntegrationID, &artifact.PrincipalType, &artifact.PrincipalID,
		&artifact.ExternalSubjectReference, &artifact.Role, &artifact.Policy, &artifact.Status, &artifact.StatusVersion,
		&artifact.MediaType, &artifact.SizeBytes, &artifact.SHA256, &artifact.StoreDriver, &artifact.StoreKey,
		&artifact.ExternalReference, &artifact.ErrorType, &artifact.RetainUntil, &artifact.CreatedAt, &artifact.UpdatedAt,
		&artifact.ReadyAt, &artifact.DeliveredAt, &artifact.DeletedAt,
	); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func (r *MemoryRepository) CreateArtifact(_ context.Context, artifact Artifact, event ArtifactEvent, outbox TransactionalOutboxEvent) error {
	if err := validateArtifactAdmission(artifact, event, outbox); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateMemoryArtifactReferences(r, artifact); err != nil {
		return err
	}
	if _, exists := r.artifacts[artifact.ID]; exists {
		return fmt.Errorf("artifact %q already exists", artifact.ID)
	}
	if _, exists := r.artifactEvents[event.ID]; exists {
		return fmt.Errorf("artifact event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return err
	}
	r.artifacts[artifact.ID] = artifact
	r.artifactEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return nil
}

func (r *PostgresRepository) CreateArtifact(ctx context.Context, artifact Artifact, event ArtifactEvent, outbox TransactionalOutboxEvent) error {
	if err := validateArtifactAdmission(artifact, event, outbox); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validatePostgresArtifactReferences(ctx, tx, artifact); err != nil {
		return err
	}
	if err := insertArtifact(ctx, tx, artifact); err != nil {
		return err
	}
	if err := insertArtifactEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := insertTransactionalOutboxEvent(ctx, tx, outbox); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MemoryRepository) FindArtifact(_ context.Context, id string) (Artifact, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	artifact, found := r.artifacts[strings.TrimSpace(id)]
	return artifact, found, nil
}

func (r *PostgresRepository) FindArtifact(ctx context.Context, id string) (Artifact, bool, error) {
	artifact, err := scanArtifact(r.db.QueryRowContext(ctx, `SELECT `+artifactSelectColumns+` FROM artifacts WHERE id=$1`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	return artifact, err == nil, err
}

func (r *MemoryRepository) FindOwnedArtifact(_ context.Context, id string, owner ArtifactOwner) (Artifact, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	artifact, found := r.artifacts[strings.TrimSpace(id)]
	if !found || !artifactOwnerMatches(artifact, owner) {
		return Artifact{}, false, nil
	}
	return artifact, true, nil
}

func (r *PostgresRepository) FindOwnedArtifact(ctx context.Context, id string, owner ArtifactOwner) (Artifact, bool, error) {
	artifact, err := scanArtifact(r.db.QueryRowContext(ctx, `SELECT `+artifactSelectColumns+` FROM artifacts
WHERE id=$1 AND profile_scope=$2 AND tenant_id=$3 AND integration_id=$4 AND principal_type=$5 AND principal_id=$6 AND external_subject_reference=$7`,
		strings.TrimSpace(id), owner.ProfileScope, owner.TenantID, owner.IntegrationID, owner.PrincipalType, owner.PrincipalID, owner.ExternalSubjectReference))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	return artifact, err == nil, err
}

func (r *MemoryRepository) QueryArtifacts(_ context.Context, query ArtifactQuery) ([]Artifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Artifact, 0)
	for _, artifact := range r.artifacts {
		if artifactMatchesQuery(artifact, query) {
			out = append(out, artifact)
		}
	}
	sortArtifacts(out)
	return paginateArtifacts(out, query.Limit, query.Offset), nil
}

func (r *PostgresRepository) QueryArtifacts(ctx context.Context, query ArtifactQuery) ([]Artifact, error) {
	limit := artifactQueryLimit(query.Limit)
	owner := ArtifactOwner{}
	ownerScoped := query.Owner != nil
	if query.Owner != nil {
		owner = *query.Owner
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+artifactSelectColumns+` FROM artifacts
WHERE (NOT $1 OR (profile_scope=$2 AND tenant_id=$3 AND integration_id=$4 AND principal_type=$5 AND principal_id=$6 AND external_subject_reference=$7))
  AND ($8='' OR operation_id=$8) AND ($9='' OR job_id=$9) AND ($10='' OR status=$10)
  AND ($11::timestamptz IS NULL OR retain_until <= $11)
ORDER BY created_at DESC, id DESC LIMIT $12 OFFSET $13`,
		ownerScoped, strings.TrimSpace(owner.ProfileScope), strings.TrimSpace(owner.TenantID), strings.TrimSpace(owner.IntegrationID),
		strings.TrimSpace(owner.PrincipalType), strings.TrimSpace(owner.PrincipalID), strings.TrimSpace(owner.ExternalSubjectReference),
		strings.TrimSpace(query.OperationID), strings.TrimSpace(query.JobID), strings.TrimSpace(query.Status), query.RetainBefore, limit, nonNegative(query.Offset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Artifact, 0)
	for rows.Next() {
		artifact, scanErr := scanArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, artifact)
	}
	return out, rows.Err()
}

func (r *MemoryRepository) TransitionArtifact(_ context.Context, input ArtifactTransitionInput, transitionedAt time.Time) (Artifact, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	artifact, found := r.artifacts[strings.TrimSpace(input.ArtifactID)]
	if !found || artifact.StatusVersion != input.ExpectedVersion {
		return artifact, false, nil
	}
	updated, event, outbox, err := prepareArtifactRepositoryTransition(artifact, input, transitionedAt)
	if err != nil {
		return Artifact{}, false, err
	}
	if _, exists := r.artifactEvents[event.ID]; exists {
		return Artifact{}, false, fmt.Errorf("artifact event %q already exists", event.ID)
	}
	if err := validateMemoryOutboxInsert(r.transactionalOutboxEvents, outbox); err != nil {
		return Artifact{}, false, err
	}
	r.artifacts[updated.ID] = updated
	r.artifactEvents[event.ID] = event
	r.transactionalOutboxEvents[outbox.ID] = outbox
	return updated, true, nil
}

func (r *PostgresRepository) TransitionArtifact(ctx context.Context, input ArtifactTransitionInput, transitionedAt time.Time) (Artifact, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	artifact, err := scanArtifact(tx.QueryRowContext(ctx, `SELECT `+artifactSelectColumns+` FROM artifacts WHERE id=$1 FOR UPDATE`, strings.TrimSpace(input.ArtifactID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, err
	}
	if artifact.StatusVersion != input.ExpectedVersion {
		return artifact, false, nil
	}
	updated, event, outbox, err := prepareArtifactRepositoryTransition(artifact, input, transitionedAt)
	if err != nil {
		return Artifact{}, false, err
	}
	if err := updateArtifact(ctx, tx, updated, input.ExpectedVersion); err != nil {
		return Artifact{}, false, err
	}
	if err := insertArtifactEvent(ctx, tx, event); err != nil {
		return Artifact{}, false, err
	}
	if err := insertTransactionalOutboxEvent(ctx, tx, outbox); err != nil {
		return Artifact{}, false, err
	}
	return updated, true, tx.Commit()
}

func (r *MemoryRepository) ListArtifactEvents(_ context.Context, artifactID string) ([]ArtifactEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ArtifactEvent, 0)
	for _, event := range r.artifactEvents {
		if strings.TrimSpace(artifactID) == "" || event.ArtifactID == strings.TrimSpace(artifactID) {
			out = append(out, event)
		}
	}
	sortArtifactEvents(out)
	return out, nil
}

func (r *PostgresRepository) ListArtifactEvents(ctx context.Context, artifactID string) ([]ArtifactEvent, error) {
	statement := `SELECT id, artifact_id, version, event_type, from_status, to_status, reason, created_at FROM artifact_events`
	args := []any{}
	if strings.TrimSpace(artifactID) != "" {
		statement += ` WHERE artifact_id=$1`
		args = append(args, strings.TrimSpace(artifactID))
	}
	statement += ` ORDER BY artifact_id, version, created_at`
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ArtifactEvent, 0)
	for rows.Next() {
		var event ArtifactEvent
		if err := rows.Scan(&event.ID, &event.ArtifactID, &event.Version, &event.EventType, &event.FromStatus, &event.ToStatus, &event.Reason, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func insertArtifact(ctx context.Context, executor usageRecordExecutor, artifact Artifact) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO artifacts(
id, operation_id, job_id, attempt_id, source_artifact_id, profile_scope, tenant_id, integration_id, principal_type, principal_id,
external_subject_reference, role, policy, status, status_version, media_type, size_bytes, sha256, store_driver, store_key,
external_reference, error_type, retain_until, created_at, updated_at, ready_at, delivered_at, deleted_at)
VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)`,
		artifact.ID, artifact.OperationID, artifact.JobID, artifact.AttemptID, artifact.SourceArtifactID,
		artifact.ProfileScope, artifact.TenantID, artifact.IntegrationID, artifact.PrincipalType, artifact.PrincipalID,
		artifact.ExternalSubjectReference, artifact.Role, artifact.Policy, artifact.Status, artifact.StatusVersion, artifact.MediaType,
		artifact.SizeBytes, artifact.SHA256, artifact.StoreDriver, artifact.StoreKey, artifact.ExternalReference, artifact.ErrorType,
		artifact.RetainUntil, artifact.CreatedAt, artifact.UpdatedAt, artifact.ReadyAt, artifact.DeliveredAt, artifact.DeletedAt)
	return err
}

func updateArtifact(ctx context.Context, executor usageRecordExecutor, artifact Artifact, expectedVersion int) error {
	result, err := executor.ExecContext(ctx, `UPDATE artifacts SET
status=$2, status_version=$3, media_type=$4, size_bytes=$5, sha256=$6, store_driver=$7, store_key=$8,
external_reference=$9, error_type=$10, updated_at=$11, ready_at=$12, delivered_at=$13, deleted_at=$14
WHERE id=$1 AND status_version=$15`, artifact.ID, artifact.Status, artifact.StatusVersion, artifact.MediaType, artifact.SizeBytes,
		artifact.SHA256, artifact.StoreDriver, artifact.StoreKey, artifact.ExternalReference, artifact.ErrorType, artifact.UpdatedAt,
		artifact.ReadyAt, artifact.DeliveredAt, artifact.DeletedAt, expectedVersion)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrArtifactStateConflict
	}
	return nil
}

func insertArtifactEvent(ctx context.Context, executor usageRecordExecutor, event ArtifactEvent) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO artifact_events(id, artifact_id, version, event_type, from_status, to_status, reason, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, event.ID, event.ArtifactID, event.Version, event.EventType, event.FromStatus, event.ToStatus, event.Reason, event.CreatedAt)
	return err
}

func artifactMatchesQuery(artifact Artifact, query ArtifactQuery) bool {
	if query.Owner != nil && !artifactOwnerMatches(artifact, *query.Owner) {
		return false
	}
	if query.RetainBefore != nil && artifact.RetainUntil.After(*query.RetainBefore) {
		return false
	}
	return (strings.TrimSpace(query.OperationID) == "" || artifact.OperationID == strings.TrimSpace(query.OperationID)) &&
		(strings.TrimSpace(query.JobID) == "" || artifact.JobID == strings.TrimSpace(query.JobID)) &&
		(strings.TrimSpace(query.Status) == "" || artifact.Status == strings.TrimSpace(query.Status))
}

func sortArtifacts(artifacts []Artifact) {
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i].CreatedAt.Equal(artifacts[j].CreatedAt) {
			return artifacts[i].ID > artifacts[j].ID
		}
		return artifacts[i].CreatedAt.After(artifacts[j].CreatedAt)
	})
}

func paginateArtifacts(artifacts []Artifact, limit, offset int) []Artifact {
	offset = nonNegative(offset)
	if offset >= len(artifacts) {
		return []Artifact{}
	}
	limit = artifactQueryLimit(limit)
	end := offset + limit
	if end > len(artifacts) {
		end = len(artifacts)
	}
	return append([]Artifact(nil), artifacts[offset:end]...)
}

func artifactQueryLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 100
	}
	return limit
}

func sortArtifactEvents(events []ArtifactEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Version == events[j].Version {
			return events[i].CreatedAt.Before(events[j].CreatedAt)
		}
		return events[i].Version < events[j].Version
	})
}

func validateMemoryArtifactReferences(r *MemoryRepository, artifact Artifact) error {
	operation, found := r.aiOperations[artifact.OperationID]
	if !found || !artifactOwnerMatches(artifact, artifactOwnerFromOperation(operation)) {
		return errors.New("artifact operation ownership does not match")
	}
	if artifact.JobID != "" {
		job, found := r.aiJobs[artifact.JobID]
		if !found || job.OperationID != artifact.OperationID || !aiJobOwnerMatches(job, artifactOwnerFromOperation(operation)) {
			return errors.New("artifact job reference does not match")
		}
	}
	if artifact.AttemptID != "" {
		attempt, found := r.aiAttempts[artifact.AttemptID]
		if !found || attempt.OperationID != artifact.OperationID {
			return errors.New("artifact attempt reference does not match")
		}
	}
	if artifact.SourceArtifactID != "" {
		source, found := r.artifacts[artifact.SourceArtifactID]
		if !found || !artifactOwnerMatches(source, artifactOwnerFromOperation(operation)) {
			return errors.New("source artifact ownership does not match")
		}
	}
	return nil
}

func validatePostgresArtifactReferences(ctx context.Context, tx *sql.Tx, artifact Artifact) error {
	operation, err := scanAIOperation(tx.QueryRowContext(ctx, `SELECT `+aiOperationSelectColumns+` FROM ai_operations WHERE id=$1`, artifact.OperationID))
	if err != nil || !artifactOwnerMatches(artifact, artifactOwnerFromOperation(operation)) {
		return errors.New("artifact operation ownership does not match")
	}
	if artifact.JobID != "" {
		job, err := scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobSelectColumns+` FROM ai_jobs WHERE id=$1`, artifact.JobID))
		if err != nil || job.OperationID != artifact.OperationID || !aiJobOwnerMatches(job, artifactOwnerFromOperation(operation)) {
			return errors.New("artifact job reference does not match")
		}
	}
	if artifact.AttemptID != "" {
		attempt, err := scanAIAttempt(tx.QueryRowContext(ctx, `SELECT `+aiAttemptSelectColumns+` FROM ai_attempts WHERE id=$1`, artifact.AttemptID))
		if err != nil || attempt.OperationID != artifact.OperationID {
			return errors.New("artifact attempt reference does not match")
		}
	}
	if artifact.SourceArtifactID != "" {
		source, err := scanArtifact(tx.QueryRowContext(ctx, `SELECT `+artifactSelectColumns+` FROM artifacts WHERE id=$1`, artifact.SourceArtifactID))
		if err != nil || !artifactOwnerMatches(source, artifactOwnerFromOperation(operation)) {
			return errors.New("source artifact ownership does not match")
		}
	}
	return nil
}

func prepareArtifactRepositoryTransition(artifact Artifact, input ArtifactTransitionInput, transitionedAt time.Time) (Artifact, ArtifactEvent, TransactionalOutboxEvent, error) {
	if input.Content != nil {
		if !oneOf(strings.TrimSpace(input.ToStatus), ArtifactStatusUploading, ArtifactStatusReady, ArtifactStatusFailed) {
			return Artifact{}, ArtifactEvent{}, TransactionalOutboxEvent{}, errors.New("artifact content can only change during upload")
		}
		if err := applyArtifactContentUpdate(&artifact, *input.Content); err != nil {
			return Artifact{}, ArtifactEvent{}, TransactionalOutboxEvent{}, err
		}
	}
	return prepareArtifactTransition(artifact, input.ToStatus, input.Reason, transitionedAt)
}
