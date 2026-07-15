package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (r *MemoryRepository) CreateOrGetProviderCallbackReceipt(_ context.Context, receipt ProviderCallbackReceipt) (ProviderCallbackReceipt, bool, error) {
	receipt.EventID = strings.TrimSpace(receipt.EventID)
	if receipt.EventID == "" || receipt.PayloadHash == "" || receipt.AttemptID == "" || receipt.AdapterID == "" ||
		(receipt.Status != "" && !oneOf(receipt.Status, ProviderCallbackReceiptProcessing, ProviderCallbackReceiptApplied, ProviderCallbackReceiptRejected)) {
		return ProviderCallbackReceipt{}, false, ErrProviderCallbackInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, found := r.providerCallbackReceipts[receipt.EventID]; found {
		return current, false, nil
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = time.Now().UTC()
	}
	if receipt.Status == "" {
		receipt.Status = ProviderCallbackReceiptProcessing
	}
	r.providerCallbackReceipts[receipt.EventID] = receipt
	return receipt, true, nil
}

func (r *MemoryRepository) FindProviderCallbackReceipt(_ context.Context, eventID string) (ProviderCallbackReceipt, bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ProviderCallbackReceipt{}, false, ErrProviderCallbackInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	receipt, found := r.providerCallbackReceipts[eventID]
	return receipt, found, nil
}

func (r *MemoryRepository) CompleteProviderCallbackReceipt(_ context.Context, eventID, status, errorType string, processedAt time.Time) error {
	eventID = strings.TrimSpace(eventID)
	status = strings.TrimSpace(status)
	if eventID == "" || !oneOf(status, ProviderCallbackReceiptApplied, ProviderCallbackReceiptRejected) {
		return ErrProviderCallbackInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt, found := r.providerCallbackReceipts[eventID]
	if !found {
		return sql.ErrNoRows
	}
	if receipt.Status != ProviderCallbackReceiptProcessing {
		return nil
	}
	receipt.Status = status
	receipt.ErrorType = strings.TrimSpace(errorType)
	if processedAt.IsZero() {
		processedAt = time.Now().UTC()
	}
	receipt.ProcessedAt = &processedAt
	r.providerCallbackReceipts[eventID] = receipt
	return nil
}

func (r *PostgresRepository) CreateOrGetProviderCallbackReceipt(ctx context.Context, receipt ProviderCallbackReceipt) (ProviderCallbackReceipt, bool, error) {
	receipt.EventID = strings.TrimSpace(receipt.EventID)
	if receipt.EventID == "" || receipt.PayloadHash == "" || receipt.AttemptID == "" || receipt.AdapterID == "" ||
		(receipt.Status != "" && !oneOf(receipt.Status, ProviderCallbackReceiptProcessing, ProviderCallbackReceiptApplied, ProviderCallbackReceiptRejected)) {
		return ProviderCallbackReceipt{}, false, ErrProviderCallbackInvalid
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = time.Now().UTC()
	}
	if receipt.Status == "" {
		receipt.Status = ProviderCallbackReceiptProcessing
	}
	const columns = `event_id, adapter_id, attempt_id, provider_id, provider_account_id, provider_task_id, payload_hash, status, error_type, created_at, processed_at`
	query := `INSERT INTO provider_callback_receipts (` + columns + `)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(event_id) DO NOTHING
RETURNING ` + columns
	created, err := scanProviderCallbackReceipt(r.db.QueryRowContext(ctx, query,
		receipt.EventID, receipt.AdapterID, receipt.AttemptID, receipt.ProviderID, receipt.ProviderAccountID,
		receipt.ProviderTaskID, receipt.PayloadHash, receipt.Status, receipt.ErrorType, receipt.CreatedAt, receipt.ProcessedAt))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProviderCallbackReceipt{}, false, err
	}
	existing, err := scanProviderCallbackReceipt(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM provider_callback_receipts WHERE event_id=$1`, receipt.EventID))
	if err != nil {
		return ProviderCallbackReceipt{}, false, err
	}
	return existing, false, nil
}

func (r *PostgresRepository) FindProviderCallbackReceipt(ctx context.Context, eventID string) (ProviderCallbackReceipt, bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ProviderCallbackReceipt{}, false, ErrProviderCallbackInvalid
	}
	const columns = `event_id, adapter_id, attempt_id, provider_id, provider_account_id, provider_task_id, payload_hash, status, error_type, created_at, processed_at`
	receipt, err := scanProviderCallbackReceipt(r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM provider_callback_receipts WHERE event_id=$1`, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderCallbackReceipt{}, false, nil
	}
	if err != nil {
		return ProviderCallbackReceipt{}, false, err
	}
	return receipt, true, nil
}

func (r *PostgresRepository) CompleteProviderCallbackReceipt(ctx context.Context, eventID, status, errorType string, processedAt time.Time) error {
	eventID = strings.TrimSpace(eventID)
	status = strings.TrimSpace(status)
	if eventID == "" || !oneOf(status, ProviderCallbackReceiptApplied, ProviderCallbackReceiptRejected) {
		return ErrProviderCallbackInvalid
	}
	if processedAt.IsZero() {
		processedAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `UPDATE provider_callback_receipts
SET status=$1, error_type=$2, processed_at=$3
WHERE event_id=$4 AND status=$5`, status, strings.TrimSpace(errorType), processedAt, eventID, ProviderCallbackReceiptProcessing)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provider_callback_receipts WHERE event_id=$1)`, eventID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
	}
	return nil
}

type providerCallbackReceiptScanner interface {
	Scan(...any) error
}

func scanProviderCallbackReceipt(scanner providerCallbackReceiptScanner) (ProviderCallbackReceipt, error) {
	var receipt ProviderCallbackReceipt
	var processedAt sql.NullTime
	if err := scanner.Scan(&receipt.EventID, &receipt.AdapterID, &receipt.AttemptID, &receipt.ProviderID, &receipt.ProviderAccountID,
		&receipt.ProviderTaskID, &receipt.PayloadHash, &receipt.Status, &receipt.ErrorType, &receipt.CreatedAt, &processedAt); err != nil {
		return ProviderCallbackReceipt{}, err
	}
	if processedAt.Valid {
		value := processedAt.Time
		receipt.ProcessedAt = &value
	}
	return receipt, nil
}
