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

const providerBillingSourceSelectColumns = `id, provider_id, provider_account_id, adapter_id, status,
automatic_sync_enabled, sync_interval_seconds, cursor, usage_cost_lines, aggregate_usage, balance_supported,
incremental_sync, price_feed, detection_status, contract_version, evidence_hash, warnings, next_sync_at,
last_sync_started_at, last_sync_completed_at, last_success_at, consecutive_failures, last_error_code,
lease_token, lease_expires_at, version, created_by, updated_by, created_at, updated_at`

func normalizeProviderBillingSource(source *ProviderBillingSource) error {
	source.ID = strings.TrimSpace(source.ID)
	source.ProviderID = strings.TrimSpace(source.ProviderID)
	source.ProviderAccountID = strings.TrimSpace(source.ProviderAccountID)
	source.AdapterID = strings.TrimSpace(source.AdapterID)
	source.Status = strings.TrimSpace(source.Status)
	source.Cursor = strings.TrimSpace(source.Cursor)
	source.DetectionStatus = strings.TrimSpace(source.DetectionStatus)
	source.ContractVersion = strings.TrimSpace(source.ContractVersion)
	source.EvidenceHash = strings.TrimSpace(source.EvidenceHash)
	source.LastErrorCode = strings.TrimSpace(source.LastErrorCode)
	source.CreatedBy = strings.TrimSpace(source.CreatedBy)
	source.UpdatedBy = strings.TrimSpace(source.UpdatedBy)
	if source.Status == "" {
		source.Status = ProviderBillingSourceObserveOnly
	}
	if source.SyncIntervalSeconds == 0 {
		source.SyncIntervalSeconds = 3600
	}
	if source.ID == "" || source.ProviderID == "" || source.ProviderAccountID == "" || source.AdapterID == "" ||
		!oneOf(source.Status, ProviderBillingSourceObserveOnly, ProviderBillingSourceActive, ProviderBillingSourceDisabled) ||
		source.SyncIntervalSeconds < 60 || source.SyncIntervalSeconds > 86400 || source.ConsecutiveFailures < 0 {
		return errors.New("invalid provider billing source")
	}
	if source.Version <= 0 {
		source.Version = 1
	}
	if source.CreatedAt.IsZero() {
		source.CreatedAt = time.Now().UTC()
	}
	if source.UpdatedAt.IsZero() {
		source.UpdatedAt = source.CreatedAt
	}
	if source.Warnings == nil {
		source.Warnings = []string{}
	}
	return nil
}

func cloneProviderBillingSource(source ProviderBillingSource) ProviderBillingSource {
	source.Warnings = append([]string(nil), source.Warnings...)
	source.NextSyncAt = cloneTimePointer(source.NextSyncAt)
	source.LastSyncStartedAt = cloneTimePointer(source.LastSyncStartedAt)
	source.LastSyncCompletedAt = cloneTimePointer(source.LastSyncCompletedAt)
	source.LastSuccessAt = cloneTimePointer(source.LastSuccessAt)
	source.LeaseExpiresAt = cloneTimePointer(source.LeaseExpiresAt)
	if source.RoutingHealth != nil {
		health := *source.RoutingHealth
		health.ReasonCodes = append([]string(nil), health.ReasonCodes...)
		health.EvidenceObservedAt = cloneTimePointer(health.EvidenceObservedAt)
		source.RoutingHealth = &health
	}
	return source
}

func cloneProviderBillingSyncRun(run ProviderBillingSyncRun) ProviderBillingSyncRun {
	run.Warnings = append([]string(nil), run.Warnings...)
	run.FinishedAt = cloneTimePointer(run.FinishedAt)
	return run
}

func providerBillingSourceMatches(source ProviderBillingSource, idOrAccountID string) bool {
	idOrAccountID = strings.TrimSpace(idOrAccountID)
	return source.ID == idOrAccountID || source.ProviderAccountID == idOrAccountID
}

func (r *MemoryRepository) ListProviderBillingSources(_ context.Context) ([]ProviderBillingSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderBillingSource, 0, len(r.providerBillingSources))
	for _, source := range r.providerBillingSources {
		out = append(out, cloneProviderBillingSource(source))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *MemoryRepository) FindProviderBillingSource(_ context.Context, idOrAccountID string) (ProviderBillingSource, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, source := range r.providerBillingSources {
		if providerBillingSourceMatches(source, idOrAccountID) {
			return cloneProviderBillingSource(source), true, nil
		}
	}
	return ProviderBillingSource{}, false, nil
}

func (r *MemoryRepository) UpsertProviderBillingSource(_ context.Context, source ProviderBillingSource, expectedVersion *int64) (bool, error) {
	if err := normalizeProviderBillingSource(&source); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.providerBillingSources[source.ID]
	if expectedVersion == nil {
		if exists {
			return false, nil
		}
		for _, candidate := range r.providerBillingSources {
			if candidate.ProviderAccountID == source.ProviderAccountID {
				return false, nil
			}
		}
		source.Version = 1
		r.providerBillingSources[source.ID] = cloneProviderBillingSource(source)
		return true, nil
	}
	if !exists || current.Version != *expectedVersion {
		return false, nil
	}
	for id, candidate := range r.providerBillingSources {
		if id != source.ID && candidate.ProviderAccountID == source.ProviderAccountID {
			return false, nil
		}
	}
	source.Version = current.Version + 1
	source.CreatedAt = current.CreatedAt
	source.UpdatedAt = source.UpdatedAt.UTC()
	source.LeaseToken = current.LeaseToken
	source.LeaseExpiresAt = cloneTimePointer(current.LeaseExpiresAt)
	r.providerBillingSources[source.ID] = cloneProviderBillingSource(source)
	return true, nil
}

func (r *MemoryRepository) ClaimProviderBillingSources(_ context.Context, request ProviderBillingSourceClaimRequest) ([]ProviderBillingSourceClaim, error) {
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if request.LeaseDuration <= 0 {
		request.LeaseDuration = 5 * time.Minute
	}
	if request.Limit <= 0 {
		request.Limit = 1
	}
	if !oneOf(request.Trigger, ProviderBillingSyncTriggerManual, ProviderBillingSyncTriggerScheduled) {
		return nil, errors.New("invalid provider billing sync trigger")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.providerBillingSources))
	for id := range r.providerBillingSources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	claimed := make([]ProviderBillingSourceClaim, 0, request.Limit)
	for _, id := range ids {
		source := r.providerBillingSources[id]
		if request.SourceID != "" && source.ID != strings.TrimSpace(request.SourceID) {
			continue
		}
		if source.Status == ProviderBillingSourceDisabled || (request.Trigger == ProviderBillingSyncTriggerScheduled && !source.AutomaticSyncEnabled) {
			continue
		}
		if request.Trigger == ProviderBillingSyncTriggerScheduled && source.NextSyncAt != nil && source.NextSyncAt.After(now) {
			continue
		}
		if source.LeaseExpiresAt != nil && source.LeaseExpiresAt.After(now) {
			continue
		}
		if source.LeaseToken != "" {
			for runID, existing := range r.providerBillingSyncRuns {
				if existing.SourceID == source.ID && existing.Status == ProviderBillingSyncRunning {
					existing.Status = ProviderBillingSyncLeaseExpired
					existing.ErrorCode = ProviderBillingSyncLeaseExpired
					existing.FinishedAt = timePointer(now)
					r.providerBillingSyncRuns[runID] = existing
				}
			}
		}
		lease := "billing_lease_" + randomID(16)
		run := ProviderBillingSyncRun{ID: "billing_run_" + randomID(12), SourceID: source.ID, ProviderID: source.ProviderID, ProviderAccountID: source.ProviderAccountID, Trigger: request.Trigger, TriggeredBy: strings.TrimSpace(request.TriggeredBy), AdapterID: source.AdapterID, Status: ProviderBillingSyncRunning, Capabilities: source.Capabilities, DetectionStatus: source.DetectionStatus, ContractVersion: source.ContractVersion, StartedAt: now, CreatedAt: now}
		source.LeaseToken = lease
		source.LeaseExpiresAt = timePointer(now.Add(request.LeaseDuration))
		source.LastSyncStartedAt = timePointer(now)
		source.UpdatedAt = now
		source.Version++
		r.providerBillingSources[id] = cloneProviderBillingSource(source)
		r.providerBillingSyncRuns[run.ID] = cloneProviderBillingSyncRun(run)
		claimed = append(claimed, ProviderBillingSourceClaim{Source: cloneProviderBillingSource(source), Run: cloneProviderBillingSyncRun(run)})
		if len(claimed) >= request.Limit {
			break
		}
	}
	return claimed, nil
}

func (r *MemoryRepository) CommitProviderBillingSync(_ context.Context, commit ProviderBillingSyncCommit) (bool, error) {
	now := commit.CompletedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	source, found := r.providerBillingSources[commit.SourceID]
	if !found || source.LeaseToken == "" || source.LeaseToken != strings.TrimSpace(commit.LeaseToken) {
		return false, nil
	}
	if source.LeaseExpiresAt == nil || !source.LeaseExpiresAt.After(now) {
		return false, nil
	}
	if commit.Run.SourceID == "" {
		commit.Run.SourceID = source.ID
	}
	if commit.Run.ID == "" || commit.Run.SourceID != source.ID || !oneOf(commit.Run.Status, ProviderBillingSyncSucceeded, ProviderBillingSyncFailed) {
		return false, errors.New("invalid provider billing sync commit")
	}
	existing, exists := r.providerBillingSyncRuns[commit.Run.ID]
	if !exists || existing.SourceID != source.ID || existing.Status != ProviderBillingSyncRunning {
		return false, nil
	}
	balance, aggregates, err := prepareProviderBillingSyncSnapshots(commit, source, now)
	if err != nil {
		return false, err
	}
	if balance != nil {
		if _, duplicate := r.providerBalanceSnapshots[balance.ID]; duplicate {
			return false, errors.New("duplicate provider balance snapshot")
		}
	}
	for _, aggregate := range aggregates {
		if _, duplicate := r.providerUsageAggregateSnapshots[aggregate.ID]; duplicate {
			return false, errors.New("duplicate provider usage aggregate snapshot")
		}
	}

	completedRun := commit.Run
	completedRun.SourceID = existing.SourceID
	completedRun.ProviderID = existing.ProviderID
	completedRun.ProviderAccountID = existing.ProviderAccountID
	completedRun.Trigger = existing.Trigger
	completedRun.TriggeredBy = existing.TriggeredBy
	completedRun.AdapterID = existing.AdapterID
	completedRun.StartedAt = existing.StartedAt
	completedRun.CreatedAt = existing.CreatedAt
	completedRun.FinishedAt = timePointer(now)
	r.providerBillingSyncRuns[completedRun.ID] = cloneProviderBillingSyncRun(completedRun)
	if balance != nil {
		r.providerBalanceSnapshots[balance.ID] = *balance
	}
	for _, aggregate := range aggregates {
		r.providerUsageAggregateSnapshots[aggregate.ID] = aggregate
	}
	source.Cursor = strings.TrimSpace(commit.Cursor)
	source.NextSyncAt = cloneTimePointer(commit.NextSyncAt)
	source.LastSyncCompletedAt = timePointer(now)
	source.LeaseToken = ""
	source.LeaseExpiresAt = nil
	source.UpdatedAt = now
	source.Version++
	if completedRun.Status == ProviderBillingSyncSucceeded {
		source.LastSuccessAt = timePointer(now)
		source.ConsecutiveFailures = 0
		source.LastErrorCode = ""
	} else {
		source.ConsecutiveFailures++
		source.LastErrorCode = strings.TrimSpace(completedRun.ErrorCode)
	}
	source.Capabilities = completedRun.Capabilities
	source.DetectionStatus = completedRun.DetectionStatus
	source.ContractVersion = completedRun.ContractVersion
	source.EvidenceHash = completedRun.EvidenceHash
	source.Warnings = append([]string(nil), completedRun.Warnings...)
	r.providerBillingSources[source.ID] = cloneProviderBillingSource(source)
	return true, nil
}

func prepareProviderBillingSyncSnapshots(commit ProviderBillingSyncCommit, source ProviderBillingSource, now time.Time) (*ProviderBalanceSnapshotRecord, []ProviderUsageAggregateSnapshot, error) {
	if commit.Run.DiscoveredLines < 0 || commit.Run.ImportedLines < 0 || commit.Run.SkippedLines < 0 {
		return nil, nil, errors.New("invalid provider billing sync counters")
	}
	if commit.Run.Status == ProviderBillingSyncFailed && (commit.Balance != nil || len(commit.Aggregates) > 0) {
		return nil, nil, errors.New("failed provider billing sync cannot persist snapshots")
	}
	var balance *ProviderBalanceSnapshotRecord
	if commit.Balance != nil {
		value := *commit.Balance
		value.ID = strings.TrimSpace(value.ID)
		value.Kind = strings.TrimSpace(value.Kind)
		value.Currency = strings.ToUpper(strings.TrimSpace(value.Currency))
		if value.ID == "" || !oneOf(value.Kind, ProviderBalanceKindWallet, ProviderBalanceKindKeyQuota, ProviderBalanceKindSubscription) || !validProviderBillingCurrency(value.Currency) {
			return nil, nil, errors.New("invalid provider balance snapshot")
		}
		value.SourceID = source.ID
		value.SyncRunID = commit.Run.ID
		value.ProviderAccountID = source.ProviderAccountID
		value.ObservedAt = nonZeroProviderBillingTime(value.ObservedAt, now)
		value.CreatedAt = nonZeroProviderBillingTime(value.CreatedAt, now)
		balance = &value
	}
	aggregates := make([]ProviderUsageAggregateSnapshot, 0, len(commit.Aggregates))
	seenIDs := make(map[string]struct{}, len(commit.Aggregates))
	seenScopes := make(map[string]struct{}, len(commit.Aggregates))
	for _, value := range commit.Aggregates {
		value.ID = strings.TrimSpace(value.ID)
		value.Scope = strings.TrimSpace(value.Scope)
		value.Model = strings.TrimSpace(value.Model)
		value.Currency = strings.ToUpper(strings.TrimSpace(value.Currency))
		key := value.Scope + "\x00" + value.Model
		_, duplicateID := seenIDs[value.ID]
		_, duplicateScope := seenScopes[key]
		if value.ID == "" || value.Scope == "" || duplicateID || duplicateScope || !validProviderBillingCurrency(value.Currency) ||
			value.RequestCount < 0 || value.InputTokens < 0 || value.OutputTokens < 0 || value.CacheCreationTokens < 0 || value.CacheReadTokens < 0 ||
			(value.ListCostMicros != nil && *value.ListCostMicros < 0) || (value.ActualCostMicros != nil && *value.ActualCostMicros < 0) {
			return nil, nil, errors.New("invalid provider usage aggregate snapshot")
		}
		seenIDs[value.ID] = struct{}{}
		seenScopes[key] = struct{}{}
		value.SourceID = source.ID
		value.SyncRunID = commit.Run.ID
		value.ProviderAccountID = source.ProviderAccountID
		value.ObservedAt = nonZeroProviderBillingTime(value.ObservedAt, now)
		value.CreatedAt = nonZeroProviderBillingTime(value.CreatedAt, now)
		aggregates = append(aggregates, cloneProviderUsageAggregateSnapshot(value))
	}
	return balance, aggregates, nil
}

func validProviderBillingCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func nonZeroProviderBillingTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value.UTC()
}

func cloneProviderUsageAggregateSnapshot(snapshot ProviderUsageAggregateSnapshot) ProviderUsageAggregateSnapshot {
	if snapshot.ListCostMicros != nil {
		value := *snapshot.ListCostMicros
		snapshot.ListCostMicros = &value
	}
	if snapshot.ActualCostMicros != nil {
		value := *snapshot.ActualCostMicros
		snapshot.ActualCostMicros = &value
	}
	return snapshot
}

func (r *MemoryRepository) ListProviderBillingSyncRuns(_ context.Context, sourceID string, limit int) ([]ProviderBillingSyncRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderBillingSyncRun, 0)
	for _, run := range r.providerBillingSyncRuns {
		if strings.TrimSpace(sourceID) == "" || run.SourceID == strings.TrimSpace(sourceID) {
			out = append(out, cloneProviderBillingSyncRun(run))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) ListProviderBalanceSnapshots(_ context.Context, sourceID string, limit int) ([]ProviderBalanceSnapshotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderBalanceSnapshotRecord, 0)
	for _, snapshot := range r.providerBalanceSnapshots {
		if strings.TrimSpace(sourceID) == "" || snapshot.SourceID == strings.TrimSpace(sourceID) {
			out = append(out, snapshot)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObservedAt.After(out[j].ObservedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) ListLatestProviderBalanceSnapshots(_ context.Context) ([]ProviderBalanceSnapshotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	latestBySource := make(map[string]ProviderBalanceSnapshotRecord)
	for _, snapshot := range r.providerBalanceSnapshots {
		current, found := latestBySource[snapshot.SourceID]
		if !found || snapshot.ObservedAt.After(current.ObservedAt) || snapshot.ObservedAt.Equal(current.ObservedAt) && snapshot.ID > current.ID {
			latestBySource[snapshot.SourceID] = snapshot
		}
	}
	out := make([]ProviderBalanceSnapshotRecord, 0, len(latestBySource))
	for _, snapshot := range latestBySource {
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceID < out[j].SourceID })
	return out, nil
}

func (r *MemoryRepository) ListProviderUsageAggregateSnapshots(_ context.Context, sourceID string, limit int) ([]ProviderUsageAggregateSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderUsageAggregateSnapshot, 0)
	for _, snapshot := range r.providerUsageAggregateSnapshots {
		if strings.TrimSpace(sourceID) == "" || snapshot.SourceID == strings.TrimSpace(sourceID) {
			out = append(out, cloneProviderUsageAggregateSnapshot(snapshot))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObservedAt.After(out[j].ObservedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type providerBillingSourceScanner interface{ Scan(...any) error }

func scanProviderBillingSource(scanner providerBillingSourceScanner) (ProviderBillingSource, error) {
	var source ProviderBillingSource
	var warningsJSON string
	if err := scanner.Scan(&source.ID, &source.ProviderID, &source.ProviderAccountID, &source.AdapterID, &source.Status, &source.AutomaticSyncEnabled,
		&source.SyncIntervalSeconds, &source.Cursor, &source.Capabilities.UsageCostLines, &source.Capabilities.AggregateUsage, &source.Capabilities.Balance,
		&source.Capabilities.IncrementalSync, &source.Capabilities.PriceFeed, &source.DetectionStatus, &source.ContractVersion, &source.EvidenceHash,
		&warningsJSON, &source.NextSyncAt, &source.LastSyncStartedAt, &source.LastSyncCompletedAt, &source.LastSuccessAt, &source.ConsecutiveFailures,
		&source.LastErrorCode, &source.LeaseToken, &source.LeaseExpiresAt, &source.Version, &source.CreatedBy, &source.UpdatedBy, &source.CreatedAt, &source.UpdatedAt); err != nil {
		return ProviderBillingSource{}, err
	}
	if err := json.Unmarshal([]byte(warningsJSON), &source.Warnings); err != nil {
		return ProviderBillingSource{}, err
	}
	return source, nil
}

func providerBillingSourceWarningsJSON(warnings []string) (string, error) {
	if warnings == nil {
		warnings = []string{}
	}
	payload, err := json.Marshal(warnings)
	return string(payload), err
}

func (r *PostgresRepository) ListProviderBillingSources(ctx context.Context) ([]ProviderBillingSource, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+providerBillingSourceSelectColumns+` FROM provider_billing_sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBillingSource{}
	for rows.Next() {
		source, scanErr := scanProviderBillingSource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindProviderBillingSource(ctx context.Context, idOrAccountID string) (ProviderBillingSource, bool, error) {
	source, err := scanProviderBillingSource(r.db.QueryRowContext(ctx, `SELECT `+providerBillingSourceSelectColumns+` FROM provider_billing_sources WHERE id=$1 OR provider_account_id=$1`, strings.TrimSpace(idOrAccountID)))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderBillingSource{}, false, nil
	}
	return source, err == nil, err
}

func (r *PostgresRepository) UpsertProviderBillingSource(ctx context.Context, source ProviderBillingSource, expectedVersion *int64) (bool, error) {
	if err := normalizeProviderBillingSource(&source); err != nil {
		return false, err
	}
	warnings, err := providerBillingSourceWarningsJSON(source.Warnings)
	if err != nil {
		return false, err
	}
	if expectedVersion == nil {
		result, err := r.db.ExecContext(ctx, `INSERT INTO provider_billing_sources(id,provider_id,provider_account_id,adapter_id,status,automatic_sync_enabled,sync_interval_seconds,cursor,usage_cost_lines,aggregate_usage,balance_supported,incremental_sync,price_feed,detection_status,contract_version,evidence_hash,warnings,next_sync_at,last_sync_started_at,last_sync_completed_at,last_success_at,consecutive_failures,last_error_code,lease_token,lease_expires_at,version,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30) ON CONFLICT(id) DO NOTHING`, source.ID, source.ProviderID, source.ProviderAccountID, source.AdapterID, source.Status, source.AutomaticSyncEnabled, source.SyncIntervalSeconds, source.Cursor, source.Capabilities.UsageCostLines, source.Capabilities.AggregateUsage, source.Capabilities.Balance, source.Capabilities.IncrementalSync, source.Capabilities.PriceFeed, source.DetectionStatus, source.ContractVersion, source.EvidenceHash, warnings, source.NextSyncAt, source.LastSyncStartedAt, source.LastSyncCompletedAt, source.LastSuccessAt, source.ConsecutiveFailures, source.LastErrorCode, source.LeaseToken, source.LeaseExpiresAt, source.Version, source.CreatedBy, source.UpdatedBy, source.CreatedAt, source.UpdatedAt)
		if err != nil {
			return false, err
		}
		count, err := result.RowsAffected()
		return count == 1, err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE provider_billing_sources SET provider_id=$1,provider_account_id=$2,adapter_id=$3,status=$4,automatic_sync_enabled=$5,sync_interval_seconds=$6,cursor=$7,usage_cost_lines=$8,aggregate_usage=$9,balance_supported=$10,incremental_sync=$11,price_feed=$12,detection_status=$13,contract_version=$14,evidence_hash=$15,warnings=$16,next_sync_at=$17,last_sync_started_at=$18,last_sync_completed_at=$19,last_success_at=$20,consecutive_failures=$21,last_error_code=$22,updated_by=$23,updated_at=$24,version=version+1 WHERE id=$25 AND version=$26`, source.ProviderID, source.ProviderAccountID, source.AdapterID, source.Status, source.AutomaticSyncEnabled, source.SyncIntervalSeconds, source.Cursor, source.Capabilities.UsageCostLines, source.Capabilities.AggregateUsage, source.Capabilities.Balance, source.Capabilities.IncrementalSync, source.Capabilities.PriceFeed, source.DetectionStatus, source.ContractVersion, source.EvidenceHash, warnings, source.NextSyncAt, source.LastSyncStartedAt, source.LastSyncCompletedAt, source.LastSuccessAt, source.ConsecutiveFailures, source.LastErrorCode, source.UpdatedBy, source.UpdatedAt, source.ID, *expectedVersion)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *PostgresRepository) ClaimProviderBillingSources(ctx context.Context, request ProviderBillingSourceClaimRequest) ([]ProviderBillingSourceClaim, error) {
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if request.LeaseDuration <= 0 {
		request.LeaseDuration = 5 * time.Minute
	}
	if request.Limit <= 0 {
		request.Limit = 1
	}
	if !oneOf(request.Trigger, ProviderBillingSyncTriggerManual, ProviderBillingSyncTriggerScheduled) {
		return nil, errors.New("invalid provider billing sync trigger")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	query := `SELECT ` + providerBillingSourceSelectColumns + ` FROM provider_billing_sources WHERE status <> $1 AND (lease_token='' OR lease_expires_at IS NULL OR lease_expires_at <= $2)`
	args := []any{ProviderBillingSourceDisabled, now}
	if request.SourceID != "" {
		query += ` AND id=$3`
		args = append(args, strings.TrimSpace(request.SourceID))
		if request.Trigger == ProviderBillingSyncTriggerScheduled {
			query += ` AND automatic_sync_enabled=TRUE AND (next_sync_at IS NULL OR next_sync_at <= $2)`
		}
	} else if request.Trigger == ProviderBillingSyncTriggerScheduled {
		query += ` AND automatic_sync_enabled=TRUE AND (next_sync_at IS NULL OR next_sync_at <= $2)`
	}
	query += ` ORDER BY COALESCE(next_sync_at, created_at), id FOR UPDATE SKIP LOCKED LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, request.Limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	sources := []ProviderBillingSource{}
	for rows.Next() {
		source, scanErr := scanProviderBillingSource(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	claimed := make([]ProviderBillingSourceClaim, 0, len(sources))
	for _, source := range sources {
		if source.LeaseToken != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE provider_billing_sync_runs SET status=$1,error_code=$1,finished_at=$2 WHERE source_id=$3 AND status=$4`, ProviderBillingSyncLeaseExpired, now, source.ID, ProviderBillingSyncRunning); err != nil {
				return nil, err
			}
		}
		lease := "billing_lease_" + randomID(16)
		run := ProviderBillingSyncRun{ID: "billing_run_" + randomID(12), SourceID: source.ID, ProviderID: source.ProviderID, ProviderAccountID: source.ProviderAccountID, Trigger: request.Trigger, TriggeredBy: strings.TrimSpace(request.TriggeredBy), AdapterID: source.AdapterID, Status: ProviderBillingSyncRunning, Capabilities: source.Capabilities, DetectionStatus: source.DetectionStatus, ContractVersion: source.ContractVersion, StartedAt: now, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `UPDATE provider_billing_sources SET lease_token=$1,lease_expires_at=$2,last_sync_started_at=$3,updated_at=$3,version=version+1 WHERE id=$4`, lease, now.Add(request.LeaseDuration), now, source.ID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_billing_sync_runs(id,source_id,provider_id,provider_account_id,trigger,triggered_by,adapter_id,status,usage_cost_lines,aggregate_usage,balance_supported,incremental_sync,price_feed,detection_status,contract_version,started_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$16)`, run.ID, run.SourceID, run.ProviderID, run.ProviderAccountID, run.Trigger, run.TriggeredBy, run.AdapterID, run.Status, run.Capabilities.UsageCostLines, run.Capabilities.AggregateUsage, run.Capabilities.Balance, run.Capabilities.IncrementalSync, run.Capabilities.PriceFeed, run.DetectionStatus, run.ContractVersion, run.StartedAt); err != nil {
			return nil, err
		}
		source.LeaseToken = lease
		source.LeaseExpiresAt = timePointer(now.Add(request.LeaseDuration))
		source.LastSyncStartedAt = timePointer(now)
		source.Version++
		claimed = append(claimed, ProviderBillingSourceClaim{Source: source, Run: run})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *PostgresRepository) CommitProviderBillingSync(ctx context.Context, commit ProviderBillingSyncCommit) (bool, error) {
	now := commit.CompletedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	source, err := scanProviderBillingSource(tx.QueryRowContext(ctx, `SELECT `+providerBillingSourceSelectColumns+` FROM provider_billing_sources WHERE id=$1 FOR UPDATE`, commit.SourceID))
	if err != nil {
		return false, err
	}
	if source.LeaseToken == "" || source.LeaseToken != strings.TrimSpace(commit.LeaseToken) {
		return false, nil
	}
	if source.LeaseExpiresAt == nil || !source.LeaseExpiresAt.After(now) {
		return false, nil
	}
	if !oneOf(commit.Run.Status, ProviderBillingSyncSucceeded, ProviderBillingSyncFailed) {
		return false, errors.New("invalid provider billing sync status")
	}
	balance, aggregates, err := prepareProviderBillingSyncSnapshots(commit, source, now)
	if err != nil {
		return false, err
	}
	run := commit.Run
	run.SourceID = source.ID
	run.ProviderID = source.ProviderID
	run.ProviderAccountID = source.ProviderAccountID
	run.FinishedAt = timePointer(now)
	if run.CreatedAt.IsZero() {
		run.CreatedAt = source.UpdatedAt
	}
	warnings, err := providerBillingSourceWarningsJSON(run.Warnings)
	if err != nil {
		return false, err
	}
	runResult, err := tx.ExecContext(ctx, `UPDATE provider_billing_sync_runs SET status=$1,usage_cost_lines=$2,aggregate_usage=$3,balance_supported=$4,incremental_sync=$5,price_feed=$6,detection_status=$7,contract_version=$8,discovered_lines=$9,imported_lines=$10,skipped_lines=$11,evidence_hash=$12,warnings=$13,error_code=$14,finished_at=$15 WHERE id=$16 AND source_id=$17 AND status=$18`, run.Status, run.Capabilities.UsageCostLines, run.Capabilities.AggregateUsage, run.Capabilities.Balance, run.Capabilities.IncrementalSync, run.Capabilities.PriceFeed, run.DetectionStatus, run.ContractVersion, run.DiscoveredLines, run.ImportedLines, run.SkippedLines, run.EvidenceHash, warnings, run.ErrorCode, run.FinishedAt, run.ID, source.ID, ProviderBillingSyncRunning)
	if err != nil {
		return false, err
	}
	runCount, err := runResult.RowsAffected()
	if err != nil {
		return false, err
	}
	if runCount != 1 {
		return false, nil
	}
	if balance != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO provider_balance_snapshots(id,source_id,sync_run_id,provider_account_id,kind,amount_micros,unlimited,currency,evidence_hash,observed_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, balance.ID, balance.SourceID, balance.SyncRunID, balance.ProviderAccountID, balance.Kind, balance.AmountMicros, balance.Unlimited, balance.Currency, balance.EvidenceHash, balance.ObservedAt, balance.CreatedAt); err != nil {
			return false, err
		}
	}
	for _, aggregate := range aggregates {
		if _, err = tx.ExecContext(ctx, `INSERT INTO provider_usage_aggregate_snapshots(id,source_id,sync_run_id,provider_account_id,scope,model,request_count,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,list_cost_micros,actual_cost_micros,currency,evidence_hash,observed_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, aggregate.ID, aggregate.SourceID, aggregate.SyncRunID, aggregate.ProviderAccountID, aggregate.Scope, aggregate.Model, aggregate.RequestCount, aggregate.InputTokens, aggregate.OutputTokens, aggregate.CacheCreationTokens, aggregate.CacheReadTokens, aggregate.ListCostMicros, aggregate.ActualCostMicros, aggregate.Currency, aggregate.EvidenceHash, aggregate.ObservedAt, aggregate.CreatedAt); err != nil {
			return false, err
		}
	}
	sourceResult, err := tx.ExecContext(ctx, `UPDATE provider_billing_sources SET cursor=$1,next_sync_at=$2,last_sync_completed_at=$3,last_success_at=CASE WHEN $4=$5 THEN $3 ELSE last_success_at END,consecutive_failures=CASE WHEN $4=$5 THEN 0 ELSE consecutive_failures+1 END,last_error_code=CASE WHEN $4=$5 THEN '' ELSE $6 END,usage_cost_lines=$7,aggregate_usage=$8,balance_supported=$9,incremental_sync=$10,price_feed=$11,detection_status=$12,contract_version=$13,evidence_hash=$14,warnings=$15,lease_token='',lease_expires_at=NULL,updated_at=$3,version=version+1 WHERE id=$16 AND lease_token=$17`, commit.Cursor, commit.NextSyncAt, now, run.Status, ProviderBillingSyncSucceeded, run.ErrorCode, run.Capabilities.UsageCostLines, run.Capabilities.AggregateUsage, run.Capabilities.Balance, run.Capabilities.IncrementalSync, run.Capabilities.PriceFeed, run.DetectionStatus, run.ContractVersion, run.EvidenceHash, warnings, source.ID, commit.LeaseToken)
	if err != nil {
		return false, err
	}
	sourceCount, err := sourceResult.RowsAffected()
	if err != nil {
		return false, err
	}
	if sourceCount != 1 {
		return false, nil
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PostgresRepository) ListProviderBillingSyncRuns(ctx context.Context, sourceID string, limit int) ([]ProviderBillingSyncRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,source_id,provider_id,provider_account_id,trigger,triggered_by,adapter_id,status,usage_cost_lines,aggregate_usage,balance_supported,incremental_sync,price_feed,detection_status,contract_version,discovered_lines,imported_lines,skipped_lines,evidence_hash,warnings,error_code,started_at,finished_at,created_at FROM provider_billing_sync_runs WHERE ($1='' OR source_id=$1) ORDER BY started_at DESC LIMIT $2`, strings.TrimSpace(sourceID), limitOrDefault(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBillingSyncRun{}
	for rows.Next() {
		run, scanErr := scanProviderBillingSyncRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func scanProviderBillingSyncRun(scanner providerBillingSourceScanner) (ProviderBillingSyncRun, error) {
	var run ProviderBillingSyncRun
	var warnings string
	if err := scanner.Scan(&run.ID, &run.SourceID, &run.ProviderID, &run.ProviderAccountID, &run.Trigger, &run.TriggeredBy, &run.AdapterID, &run.Status, &run.Capabilities.UsageCostLines, &run.Capabilities.AggregateUsage, &run.Capabilities.Balance, &run.Capabilities.IncrementalSync, &run.Capabilities.PriceFeed, &run.DetectionStatus, &run.ContractVersion, &run.DiscoveredLines, &run.ImportedLines, &run.SkippedLines, &run.EvidenceHash, &warnings, &run.ErrorCode, &run.StartedAt, &run.FinishedAt, &run.CreatedAt); err != nil {
		return run, err
	}
	if err := json.Unmarshal([]byte(warnings), &run.Warnings); err != nil {
		return run, err
	}
	return run, nil
}

func (r *PostgresRepository) ListProviderBalanceSnapshots(ctx context.Context, sourceID string, limit int) ([]ProviderBalanceSnapshotRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,source_id,sync_run_id,provider_account_id,kind,amount_micros,unlimited,currency,evidence_hash,observed_at,created_at FROM provider_balance_snapshots WHERE ($1='' OR source_id=$1) ORDER BY observed_at DESC LIMIT $2`, strings.TrimSpace(sourceID), limitOrDefault(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBalanceSnapshotRecord{}
	for rows.Next() {
		var v ProviderBalanceSnapshotRecord
		if err := rows.Scan(&v.ID, &v.SourceID, &v.SyncRunID, &v.ProviderAccountID, &v.Kind, &v.AmountMicros, &v.Unlimited, &v.Currency, &v.EvidenceHash, &v.ObservedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListLatestProviderBalanceSnapshots(ctx context.Context) ([]ProviderBalanceSnapshotRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT ON (source_id) id,source_id,sync_run_id,provider_account_id,kind,amount_micros,unlimited,currency,evidence_hash,observed_at,created_at FROM provider_balance_snapshots ORDER BY source_id,observed_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBalanceSnapshotRecord{}
	for rows.Next() {
		var v ProviderBalanceSnapshotRecord
		if err := rows.Scan(&v.ID, &v.SourceID, &v.SyncRunID, &v.ProviderAccountID, &v.Kind, &v.AmountMicros, &v.Unlimited, &v.Currency, &v.EvidenceHash, &v.ObservedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListProviderUsageAggregateSnapshots(ctx context.Context, sourceID string, limit int) ([]ProviderUsageAggregateSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,source_id,sync_run_id,provider_account_id,scope,model,request_count,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,list_cost_micros,actual_cost_micros,currency,evidence_hash,observed_at,created_at FROM provider_usage_aggregate_snapshots WHERE ($1='' OR source_id=$1) ORDER BY observed_at DESC LIMIT $2`, strings.TrimSpace(sourceID), limitOrDefault(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderUsageAggregateSnapshot{}
	for rows.Next() {
		var v ProviderUsageAggregateSnapshot
		if err := rows.Scan(&v.ID, &v.SourceID, &v.SyncRunID, &v.ProviderAccountID, &v.Scope, &v.Model, &v.RequestCount, &v.InputTokens, &v.OutputTokens, &v.CacheCreationTokens, &v.CacheReadTokens, &v.ListCostMicros, &v.ActualCostMicros, &v.Currency, &v.EvidenceHash, &v.ObservedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
