package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProviderBillingRoutingHealthPolicy(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Hour)
	stale := now.Add(-7 * time.Hour)
	tests := []struct {
		name       string
		source     ProviderBillingSource
		balance    *ProviderBalanceSnapshotRecord
		wantStatus string
		wantHard   bool
		wantSwitch bool
		wantReason string
	}{
		{name: "observe only ignores invalid key evidence", source: ProviderBillingSource{Status: ProviderBillingSourceObserveOnly, SyncIntervalSeconds: 3600, Warnings: []string{"account_key_reported_invalid"}}, wantStatus: ProviderBillingRoutingHealthObserveOnly, wantReason: ProviderBillingReasonSourceObserveOnly},
		{name: "active invalid key is blocked", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh, Warnings: []string{"account_key_reported_invalid"}}, wantStatus: ProviderBillingRoutingHealthBlocked, wantHard: true, wantReason: ProviderBillingReasonKeyInvalid},
		{name: "active auth rejection is blocked", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh, LastErrorCode: "upstream_auth_rejected"}, wantStatus: ProviderBillingRoutingHealthBlocked, wantHard: true, wantReason: ProviderBillingReasonAuthRejected},
		{name: "key quota exhaustion is blocked", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh}, balance: &ProviderBalanceSnapshotRecord{Kind: ProviderBalanceKindKeyQuota, AmountMicros: 0, ObservedAt: fresh}, wantStatus: ProviderBillingRoutingHealthBlocked, wantHard: true, wantReason: ProviderBillingReasonKeyQuotaExhausted},
		{name: "subscription exhaustion is blocked", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh}, balance: &ProviderBalanceSnapshotRecord{Kind: ProviderBalanceKindSubscription, AmountMicros: 0, ObservedAt: fresh}, wantStatus: ProviderBillingRoutingHealthBlocked, wantHard: true, wantReason: ProviderBillingReasonSubscriptionExhausted},
		{name: "unlimited subscription remains healthy", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh}, balance: &ProviderBalanceSnapshotRecord{Kind: ProviderBalanceKindSubscription, AmountMicros: 0, Unlimited: true, ObservedAt: fresh}, wantStatus: ProviderBillingRoutingHealthHealthy, wantSwitch: true},
		{name: "zero wallet is not assumed to be prepaid exhaustion", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh}, balance: &ProviderBalanceSnapshotRecord{Kind: ProviderBalanceKindWallet, AmountMicros: 0, ObservedAt: fresh}, wantStatus: ProviderBillingRoutingHealthHealthy, wantSwitch: true},
		{name: "consecutive sync failures freeze switching", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh, ConsecutiveFailures: 3}, wantStatus: ProviderBillingRoutingHealthDegraded, wantReason: ProviderBillingReasonSyncUnhealthy},
		{name: "stale evidence freezes switching", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &stale}, wantStatus: ProviderBillingRoutingHealthDegraded, wantReason: ProviderBillingReasonEvidenceStale},
		{name: "missing evidence freezes switching", source: ProviderBillingSource{Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600}, wantStatus: ProviderBillingRoutingHealthDegraded, wantReason: ProviderBillingReasonEvidenceMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			health := providerBillingRoutingHealth(test.source, test.balance, now)
			if health.Status != test.wantStatus || health.HardBlocked != test.wantHard || health.EconomicSwitchEligible != test.wantSwitch {
				t.Fatalf("health=%+v", health)
			}
			if test.wantReason != "" && !containsString(health.ReasonCodes, test.wantReason) {
				t.Fatalf("health reasons=%v, want %s", health.ReasonCodes, test.wantReason)
			}
		})
	}
}

func TestGatewayRoutingUsesOnlyActiveHardBillingEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-routing-test")
	svc.now = func() time.Time { return now }
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{Name: "Billing route", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, APIKey: "provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{ProviderID: provider.ID, Name: "Billing account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"model"}, Secret: "account-secret"})
	if err != nil {
		t.Fatal(err)
	}
	mustCreateGatewayModelRoutes(t, svc, "model", []ProviderAccount{account})
	fresh := now.Add(-time.Hour)
	source := ProviderBillingSource{ID: "billing-route-source", ProviderID: provider.ID, ProviderAccountID: account.ID, AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceObserveOnly, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh, Warnings: []string{"account_key_reported_invalid"}, CreatedAt: now, UpdatedAt: now}
	if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
		t.Fatalf("save observe-only source applied=%t err=%v", applied, err)
	}
	if candidates, _, err := svc.GatewayProviderCandidatesForModel(ctx, "model"); err != nil || len(candidates) != 1 {
		t.Fatalf("observe-only source changed routing candidates=%+v err=%v", candidates, err)
	}
	stored, found, err := repo.FindProviderBillingSource(ctx, source.ID)
	if err != nil || !found {
		t.Fatalf("source found=%t err=%v", found, err)
	}
	version := stored.Version
	stored.Status = ProviderBillingSourceActive
	stored.UpdatedAt = now.Add(time.Minute)
	if applied, err := repo.UpsertProviderBillingSource(ctx, stored, &version); err != nil || !applied {
		t.Fatalf("activate source applied=%t err=%v", applied, err)
	}
	if _, found, err := svc.GatewayProviderForModel(ctx, "model"); !errors.Is(err, ErrGatewayRouteUnavailable) || found {
		t.Fatalf("active invalid-key source found=%t err=%v", found, err)
	}
}

func TestGatewayRoutingFiltersExhaustedKeyQuota(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-quota-test")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{Name: "Quota route", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, APIKey: "provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{ProviderID: provider.ID, Name: "Quota account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"model"}, Secret: "account-secret"})
	if err != nil {
		t.Fatal(err)
	}
	mustCreateGatewayModelRoutes(t, svc, "model", []ProviderAccount{account})
	source := ProviderBillingSource{ID: "quota-source", ProviderID: provider.ID, ProviderAccountID: account.ID, AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &now, CreatedAt: now, UpdatedAt: now}
	if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
		t.Fatalf("save source applied=%t err=%v", applied, err)
	}
	repo.mu.Lock()
	repo.providerBalanceSnapshots["quota-zero"] = ProviderBalanceSnapshotRecord{ID: "quota-zero", SourceID: source.ID, ProviderAccountID: account.ID, Kind: ProviderBalanceKindKeyQuota, AmountMicros: 0, Currency: "USD", ObservedAt: now, CreatedAt: now}
	repo.mu.Unlock()
	if candidates, hasRoutes, err := svc.GatewayProviderCandidatesForModel(ctx, "model"); err != nil || !hasRoutes || len(candidates) != 0 {
		t.Fatalf("quota-exhausted candidates=%+v hasRoutes=%t err=%v", candidates, hasRoutes, err)
	}
}

func TestEffectivePricingDoesNotPromoteBillingUnhealthyCandidate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "billing-decision-test")
	svc.now = func() time.Time { return now }
	fresh := now.Add(-time.Hour)
	source := ProviderBillingSource{ID: "candidate-source", ProviderID: "provider-b", ProviderAccountID: "account-b", AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, LastSuccessAt: &fresh, ConsecutiveFailures: 3, CreatedAt: now, UpdatedAt: now}
	if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
		t.Fatalf("save source applied=%t err=%v", applied, err)
	}
	decision := EffectivePricingDecision{ID: "billing-unhealthy-decision", Model: "public-model", Protocol: "openai_chat_completions", CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b", Status: EffectivePricingDecisionActive, CanaryPercent: 100, Confidence: ProcurementCostConfidenceExact, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	candidates := []GatewayProvider{{ID: "provider-a", AccountID: "account-a"}, {ID: "provider-b", AccountID: "account-b"}}
	ordered := svc.OrderGatewayCandidatesByEffectivePricing(ctx, "public-model", "openai_chat_completions", "cohort", candidates)
	if ordered[0].AccountID != "account-a" {
		t.Fatalf("unhealthy candidate was promoted: %+v", ordered)
	}
	decision.ID = "billing-unhealthy-recommended"
	decision.Status = EffectivePricingDecisionRecommended
	if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ActOnEffectivePricingDecision(ctx, "tester", decision.ID, EffectivePricingDecisionActionRequest{Action: "approve_canary"})
	if err == nil || !strings.Contains(err.Error(), ProviderBillingReasonSyncUnhealthy) {
		t.Fatalf("approve unhealthy candidate err=%v", err)
	}
}

func TestEffectivePricingReportIncludesProviderBillingHealth(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	source := ProviderBillingSource{ID: "report-source", ProviderID: "provider-a", ProviderAccountID: "account-a", AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceActive, SyncIntervalSeconds: 3600, CreatedAt: now, UpdatedAt: now}
	if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
		t.Fatalf("save source applied=%t err=%v", applied, err)
	}
	total := 100
	cost := int64(50)
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "billing-health-key"}}, GatewayUsageInput{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", InputTokens: total, TotalInputTokens: &total, ProcurementCostMicros: &cost, ProcurementCostConfidence: ProcurementCostConfidenceExact}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: "model", Protocol: "openai_chat_completions"})
	if err != nil || len(report.Rows) != 1 {
		t.Fatalf("report rows=%+v err=%v", report.Rows, err)
	}
	row := report.Rows[0]
	if row.ProviderBillingRoutingHealth == nil || row.ProviderBillingRoutingHealth.Status != ProviderBillingRoutingHealthDegraded || !containsString(row.ReasonCodes, ProviderBillingReasonEvidenceMissing) || row.Recommendation != "observe" {
		t.Fatalf("report row=%+v", row)
	}
}
