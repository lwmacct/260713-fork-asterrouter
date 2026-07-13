package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type RetentionCleanupResult struct {
	Before        time.Time `json:"before"`
	UsageRecords  int64     `json:"usage_records"`
	GatewayTraces int64     `json:"gateway_traces"`
	AlertEvents   int64     `json:"alert_events"`
	AuditLogs     int64     `json:"audit_logs"`
}

func (s *Service) CleanupRetainedData(ctx context.Context, actor string, before time.Time) (RetentionCleanupResult, error) {
	before = before.UTC()
	if before.IsZero() || before.After(time.Now().UTC()) {
		return RetentionCleanupResult{}, fmt.Errorf("retention cutoff must be in the past")
	}
	result, err := s.repo.CleanupRetainedData(ctx, before)
	if err != nil {
		return RetentionCleanupResult{}, err
	}
	err = s.audit(ctx, actor, "retention_cleanup", "system", "data_retention", fmt.Sprintf("Deleted usage=%d traces=%d alerts=%d audit=%d before %s", result.UsageRecords, result.GatewayTraces, result.AlertEvents, result.AuditLogs, before.Format(time.RFC3339)))
	return result, err
}

func (r *MemoryRepository) CleanupRetainedData(_ context.Context, before time.Time) (RetentionCleanupResult, error) {
	result := RetentionCleanupResult{Before: before.UTC()}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, record := range r.usageRecords {
		if record.CreatedAt.Before(before) {
			delete(r.usageRecords, id)
			result.UsageRecords++
		}
	}
	for id, trace := range r.gatewayTraces {
		if trace.CreatedAt.Before(before) {
			delete(r.gatewayTraces, id)
			result.GatewayTraces++
		}
	}
	for id, event := range r.alertEvents {
		if event.Status == AlertStatusResolved && event.LastSeenAt.Before(before) {
			delete(r.alertEvents, id)
			result.AlertEvents++
		}
	}
	for id, event := range r.auditLogs {
		if event.CreatedAt.Before(before) {
			delete(r.auditLogs, id)
			result.AuditLogs++
		}
	}
	return result, nil
}

func (r *PostgresRepository) CleanupRetainedData(ctx context.Context, before time.Time) (RetentionCleanupResult, error) {
	result := RetentionCleanupResult{Before: before.UTC()}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	operations := []struct {
		query string
		count *int64
	}{
		{`DELETE FROM usage_records WHERE created_at < $1`, &result.UsageRecords},
		{`DELETE FROM gateway_traces WHERE created_at < $1`, &result.GatewayTraces},
		{`DELETE FROM alert_events WHERE status = 'resolved' AND last_seen_at < $1`, &result.AlertEvents},
		{`DELETE FROM audit_logs WHERE created_at < $1`, &result.AuditLogs},
	}
	for _, operation := range operations {
		execution, err := tx.ExecContext(ctx, operation.query, before)
		if err != nil {
			return result, err
		}
		*operation.count, err = execution.RowsAffected()
		if err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
