package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultProviderBillingSyncTick  = time.Minute
	defaultProviderBillingSyncLease = 2 * time.Minute
	defaultProviderBillingSyncBatch = 20
)

var (
	ErrProviderBillingSourceConflict = errors.New("provider billing source changed concurrently")
	ErrProviderBillingSourceBusy     = errors.New("provider billing source is already being synchronized")
	ErrProviderBillingSourceDisabled = errors.New("provider billing source is disabled")
	ErrProviderBillingSourceNotFound = errors.New("provider billing source not found")
)

func (s *Service) ListProviderBillingSources(ctx context.Context) ([]ProviderBillingSource, error) {
	sources, err := s.repo.ListProviderBillingSources(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichProviderBillingSources(ctx, sources)
}

func (s *Service) UpsertProviderBillingSource(ctx context.Context, actor string, request ProviderBillingSourceRequest) (ProviderBillingSource, error) {
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	request.AdapterID = strings.TrimSpace(request.AdapterID)
	request.Status = strings.TrimSpace(request.Status)
	if request.ProviderAccountID == "" {
		return ProviderBillingSource{}, errors.New("provider_account_id is required")
	}
	if request.AdapterID == "" {
		request.AdapterID = ProviderBillingAdapterSub2APICompatible
	}
	if request.AdapterID != ProviderBillingAdapterSub2APICompatible {
		return ProviderBillingSource{}, errors.New("unsupported provider billing adapter")
	}
	if request.Status == "" {
		request.Status = ProviderBillingSourceObserveOnly
	}
	if !oneOf(request.Status, ProviderBillingSourceObserveOnly, ProviderBillingSourceActive, ProviderBillingSourceDisabled) {
		return ProviderBillingSource{}, errors.New("invalid provider billing source status")
	}
	if request.SyncIntervalSeconds == 0 {
		request.SyncIntervalSeconds = 3600
	}
	if request.SyncIntervalSeconds < 60 || request.SyncIntervalSeconds > 86400 {
		return ProviderBillingSource{}, errors.New("provider billing sync interval must be between 60 and 86400 seconds")
	}
	account, err := s.providerAccountByID(ctx, request.ProviderAccountID)
	if err != nil {
		return ProviderBillingSource{}, err
	}
	if account.AuthType != "api_key" || !account.SecretConfigured || account.SecretCiphertext == "" {
		return ProviderBillingSource{}, errors.New("provider account must have an API key secret configured")
	}
	now := s.nowUTC()
	source, found, err := s.repo.FindProviderBillingSource(ctx, request.ProviderAccountID)
	if err != nil {
		return ProviderBillingSource{}, err
	}
	action := "create"
	if found {
		if request.Version == nil || *request.Version != source.Version {
			return ProviderBillingSource{}, ErrProviderBillingSourceConflict
		}
		action = "update"
	} else {
		if request.Version != nil {
			return ProviderBillingSource{}, ErrProviderBillingSourceConflict
		}
		source = ProviderBillingSource{
			ID:                "pbs_" + randomID(12),
			ProviderID:        account.ProviderID,
			ProviderAccountID: account.ID,
			CreatedBy:         actor,
			CreatedAt:         now,
		}
	}
	source.ProviderID = account.ProviderID
	source.ProviderAccountID = account.ID
	source.AdapterID = request.AdapterID
	source.Status = request.Status
	source.AutomaticSyncEnabled = request.AutomaticSyncEnabled
	if source.Status == ProviderBillingSourceDisabled {
		source.AutomaticSyncEnabled = false
	}
	source.SyncIntervalSeconds = request.SyncIntervalSeconds
	source.UpdatedBy = actor
	source.UpdatedAt = now
	if source.AutomaticSyncEnabled && source.NextSyncAt == nil {
		source.NextSyncAt = timePointer(now)
	}
	applied, err := s.repo.UpsertProviderBillingSource(ctx, source, request.Version)
	if err != nil {
		return ProviderBillingSource{}, err
	}
	if !applied {
		return ProviderBillingSource{}, ErrProviderBillingSourceConflict
	}
	stored, ok, err := s.repo.FindProviderBillingSource(ctx, source.ID)
	if err != nil {
		return ProviderBillingSource{}, err
	}
	if !ok {
		return ProviderBillingSource{}, errors.New("provider billing source was not persisted")
	}
	stored, err = s.enrichProviderBillingSource(ctx, stored)
	if err != nil {
		return ProviderBillingSource{}, err
	}
	if err := s.audit(ctx, actor, action, "provider_billing_source", stored.ID, fmt.Sprintf("Provider billing source %s for account %s", action, account.ID)); err != nil {
		return ProviderBillingSource{}, err
	}
	return stored, nil
}

func (s *Service) ProviderBillingSourceEvidence(ctx context.Context, sourceID string, limit int) (ProviderBillingSourceEvidence, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ProviderBillingSourceEvidence{}, errors.New("provider billing source id is required")
	}
	source, found, err := s.repo.FindProviderBillingSource(ctx, sourceID)
	if err != nil {
		return ProviderBillingSourceEvidence{}, err
	}
	if !found {
		return ProviderBillingSourceEvidence{}, ErrProviderBillingSourceNotFound
	}
	runs, err := s.repo.ListProviderBillingSyncRuns(ctx, source.ID, limit)
	if err != nil {
		return ProviderBillingSourceEvidence{}, err
	}
	balances, err := s.repo.ListProviderBalanceSnapshots(ctx, source.ID, limit)
	if err != nil {
		return ProviderBillingSourceEvidence{}, err
	}
	aggregates, err := s.repo.ListProviderUsageAggregateSnapshots(ctx, source.ID, limit)
	if err != nil {
		return ProviderBillingSourceEvidence{}, err
	}
	source, err = s.enrichProviderBillingSource(ctx, source)
	if err != nil {
		return ProviderBillingSourceEvidence{}, err
	}
	return ProviderBillingSourceEvidence{Source: source, Runs: runs, Balances: balances, Aggregates: aggregates}, nil
}

func (s *Service) SyncProviderBillingSource(ctx context.Context, actor, sourceID string) (ProviderBillingSyncResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ProviderBillingSyncResult{}, errors.New("provider billing source id is required")
	}
	source, found, err := s.repo.FindProviderBillingSource(ctx, sourceID)
	if err != nil {
		return ProviderBillingSyncResult{}, err
	}
	if !found {
		return ProviderBillingSyncResult{}, ErrProviderBillingSourceNotFound
	}
	if source.Status == ProviderBillingSourceDisabled {
		return ProviderBillingSyncResult{}, ErrProviderBillingSourceDisabled
	}
	claims, err := s.repo.ClaimProviderBillingSources(ctx, ProviderBillingSourceClaimRequest{
		SourceID: source.ID, Trigger: ProviderBillingSyncTriggerManual, TriggeredBy: actor,
		Now: s.nowUTC(), LeaseDuration: defaultProviderBillingSyncLease, Limit: 1,
	})
	if err != nil {
		return ProviderBillingSyncResult{}, err
	}
	if len(claims) == 0 {
		return ProviderBillingSyncResult{}, ErrProviderBillingSourceBusy
	}
	return s.syncClaimedProviderBillingSource(ctx, actor, claims[0])
}

func (s *Service) SyncDueProviderBillingSources(ctx context.Context, workerID string, limit int) (ProviderBillingSyncBatchReport, error) {
	if limit <= 0 {
		limit = defaultProviderBillingSyncBatch
	}
	if limit > 100 {
		limit = 100
	}
	actor := "system:provider-billing-sync"
	if strings.TrimSpace(workerID) != "" {
		actor += ":" + strings.TrimSpace(workerID)
	}
	claims, err := s.repo.ClaimProviderBillingSources(ctx, ProviderBillingSourceClaimRequest{
		Trigger: ProviderBillingSyncTriggerScheduled, TriggeredBy: actor,
		Now: s.nowUTC(), LeaseDuration: defaultProviderBillingSyncLease, Limit: limit,
	})
	if err != nil {
		return ProviderBillingSyncBatchReport{}, err
	}
	report := ProviderBillingSyncBatchReport{Claimed: len(claims), Results: make([]ProviderBillingSyncResult, 0, len(claims))}
	var joined error
	for _, claim := range claims {
		result, syncErr := s.syncClaimedProviderBillingSource(ctx, actor, claim)
		if syncErr != nil {
			joined = errors.Join(joined, fmt.Errorf("source %s: %w", claim.Source.ID, syncErr))
			continue
		}
		report.Results = append(report.Results, result)
		if result.Run.Status == ProviderBillingSyncSucceeded {
			report.Succeeded++
		} else {
			report.Failed++
		}
	}
	return report, joined
}

func (s *Service) syncClaimedProviderBillingSource(ctx context.Context, actor string, claim ProviderBillingSourceClaim) (ProviderBillingSyncResult, error) {
	inspection, inspectErr := s.inspectProviderBillingSource(ctx, ProviderBillingSourceInspectionRequest{
		ProviderAccountID: claim.Source.ProviderAccountID,
		AdapterID:         claim.Source.AdapterID,
	})
	now := s.nowUTC()
	run := claim.Run
	run.FinishedAt = timePointer(now)
	commit := ProviderBillingSyncCommit{
		SourceID: claim.Source.ID, LeaseToken: claim.Source.LeaseToken, Run: run,
		Cursor: claim.Source.Cursor, NextSyncAt: nextProviderBillingSyncAt(claim.Source, now, inspectErr != nil), CompletedAt: now,
	}
	result := ProviderBillingSyncResult{}
	action := "sync"
	if inspectErr != nil {
		run.Status = ProviderBillingSyncFailed
		run.ErrorCode = providerBillingSyncErrorCode(inspectErr)
		commit.Run = run
		action = "sync_failed"
	} else {
		run.Status = ProviderBillingSyncSucceeded
		run.AdapterID = inspection.AdapterID
		run.Capabilities = inspection.Capabilities
		run.DetectionStatus = inspection.DetectionStatus
		run.ContractVersion = inspection.ContractVersion
		run.DiscoveredLines = inspection.DiscoveredLines
		run.EvidenceHash = inspection.EvidenceHash
		run.Warnings = append([]string(nil), inspection.Warnings...)
		commit.Run = run
		if inspection.Balance != nil {
			commit.Balance = &ProviderBalanceSnapshotRecord{
				ID: "pbs_balance_" + randomID(12), SourceID: claim.Source.ID, SyncRunID: run.ID,
				ProviderAccountID: claim.Source.ProviderAccountID, Kind: inspection.Balance.Kind,
				AmountMicros: inspection.Balance.AmountMicros, Unlimited: inspection.Balance.Unlimited,
				Currency: inspection.Balance.Currency, EvidenceHash: inspection.EvidenceHash,
				ObservedAt: inspection.Balance.ObservedAt, CreatedAt: now,
			}
		}
		commit.Aggregates = make([]ProviderUsageAggregateSnapshot, 0, len(inspection.UsageAggregates))
		for _, aggregate := range inspection.UsageAggregates {
			commit.Aggregates = append(commit.Aggregates, ProviderUsageAggregateSnapshot{
				ID: "pbs_usage_" + randomID(12), SourceID: claim.Source.ID, SyncRunID: run.ID,
				ProviderAccountID: claim.Source.ProviderAccountID, Scope: aggregate.Scope, Model: aggregate.Model,
				RequestCount: aggregate.RequestCount, InputTokens: aggregate.InputTokens, OutputTokens: aggregate.OutputTokens,
				CacheCreationTokens: aggregate.CacheCreationTokens, CacheReadTokens: aggregate.CacheReadTokens,
				ListCostMicros: aggregate.ListCostMicros, ActualCostMicros: aggregate.ActualCostMicros,
				Currency: inspection.Currency, EvidenceHash: inspection.EvidenceHash, ObservedAt: inspection.ObservedAt, CreatedAt: now,
			})
		}
	}
	applied, err := s.repo.CommitProviderBillingSync(ctx, commit)
	if err != nil {
		return ProviderBillingSyncResult{}, err
	}
	if !applied {
		return ProviderBillingSyncResult{}, ErrProviderBillingSourceConflict
	}
	stored, found, err := s.repo.FindProviderBillingSource(ctx, claim.Source.ID)
	if err != nil {
		return ProviderBillingSyncResult{}, err
	}
	if !found {
		return ProviderBillingSyncResult{}, errors.New("provider billing source not found after sync")
	}
	stored, err = s.enrichProviderBillingSource(ctx, stored)
	if err != nil {
		return ProviderBillingSyncResult{}, err
	}
	result = ProviderBillingSyncResult{Source: stored, Run: run, Balance: commit.Balance, Aggregates: commit.Aggregates}
	if err := s.audit(ctx, actor, action, "provider_billing_source", stored.ID, fmt.Sprintf("Provider billing sync %s with code %s", run.Status, run.ErrorCode)); err != nil {
		return ProviderBillingSyncResult{}, err
	}
	return result, nil
}

func nextProviderBillingSyncAt(source ProviderBillingSource, now time.Time, failed bool) *time.Time {
	if !source.AutomaticSyncEnabled || source.Status == ProviderBillingSourceDisabled {
		return nil
	}
	interval := time.Duration(source.SyncIntervalSeconds) * time.Second
	if interval < time.Minute {
		interval = time.Hour
	}
	if failed {
		for attempt := 0; attempt < source.ConsecutiveFailures && interval < 24*time.Hour; attempt++ {
			interval *= 2
		}
		if interval > 24*time.Hour {
			interval = 24 * time.Hour
		}
	}
	next := now.Add(interval)
	return &next
}

func providerBillingSyncErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "upstream_timeout"
	case errors.Is(err, context.Canceled):
		return "sync_canceled"
	case errors.Is(err, ErrProviderBillingAdapterMismatch):
		return "adapter_mismatch"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "rejected the account api key"):
		return "upstream_auth_rejected"
	case strings.Contains(message, "secret"):
		return "source_secret_unavailable"
	case strings.Contains(message, "invalid currency"), strings.Contains(message, "response"):
		return "upstream_response_invalid"
	case strings.Contains(message, "url"), strings.Contains(message, "api key secret configured"):
		return "source_configuration_invalid"
	default:
		return "upstream_unavailable"
	}
}

func (s *Service) RunProviderBillingSyncScheduler(ctx context.Context, tick time.Duration, onError func(error)) {
	if tick <= 0 {
		tick = defaultProviderBillingSyncTick
	}
	workerID := randomID(8)
	run := func() {
		if _, err := s.SyncDueProviderBillingSources(ctx, workerID, defaultProviderBillingSyncBatch); err != nil && onError != nil {
			onError(err)
		}
	}
	run()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
