package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEffectivePricingReportRanksRealCostInsteadOfQuotedMultiplier(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	seedEffectivePricingProvider(t, repo, now, "provider-b", "account-b", "Channel B")
	for _, price := range []ProcurementPrice{
		{ID: "price-a", ProviderID: "provider-a", ProviderAccountID: "account-a", UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000, ReferenceInputMicrosPer1MTokens: 1_000_000, QuotedMultiplier: 0.2, Confidence: ProcurementCostConfidenceEstimated, Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "price-b", ProviderID: "provider-b", ProviderAccountID: "account-b", UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000, ReferenceInputMicrosPer1MTokens: 1_000_000, QuotedMultiplier: 0.5, Confidence: ProcurementCostConfidenceEstimated, Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.SaveProcurementPrice(ctx, price); err != nil {
			t.Fatal(err)
		}
	}
	uncachedA, totalA := 100, 100
	uncachedB, cachedB, totalB := 10, 90, 100
	inputs := []GatewayUsageInput{
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", InputTokens: 100, TotalInputTokens: &totalA, UncachedInputTokens: &uncachedA, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai"},
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-b", ProviderAccountID: "account-b", Status: "forwarded", InputTokens: 100, TotalInputTokens: &totalB, UncachedInputTokens: &uncachedB, CacheReadTokens: &cachedB, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai"},
	}
	for index, input := range inputs {
		if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-" + string(rune('a'+index))}}, input); err != nil {
			t.Fatal(err)
		}
	}
	report, err := svc.EffectivePricingReport(ctx, EffectivePricingReportQuery{Model: "model", Protocol: "openai_chat_completions", WindowHours: 24})
	if err != nil {
		t.Fatalf("EffectivePricingReport(): %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("report rows = %+v", report.Rows)
	}
	if report.Rows[0].ProviderAccountID != "account-b" || report.Rows[0].QuotedMultiplier <= report.Rows[1].QuotedMultiplier || report.Rows[0].EffectiveCostMicrosPer1M >= report.Rows[1].EffectiveCostMicrosPer1M {
		t.Fatalf("report did not rank real cost over quoted multiplier: %+v", report.Rows)
	}
}

func TestImportProviderBillingLineReconcilesUsageByUpstreamRequestID(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-a"}}, GatewayUsageInput{
		Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a",
		Status: "forwarded", InputTokens: 100, UpstreamRequestID: "upstream-request-1",
	}); err != nil {
		t.Fatal(err)
	}
	line, err := svc.ImportProviderBillingLine(ctx, "tester", ProviderBillingLineRequest{
		ProviderID: "provider-a", ProviderAccountID: "account-a", ExternalLineID: "external-line-1",
		ExternalRequestID: "upstream-request-1", UpstreamModel: "model", Currency: "USD", AmountMicros: 77,
		SourceKind: "api", Confidence: ProcurementCostConfidenceExact,
	})
	if err != nil {
		t.Fatalf("ImportProviderBillingLine(): %v", err)
	}
	if line.ReconciliationStatus != BillingReconciliationMatched || line.UsageRecordID == "" {
		t.Fatalf("billing line = %+v", line)
	}
	records, err := repo.QueryUsageRecords(ctx, UsageQuery{ID: line.UsageRecordID, Limit: 1})
	if err != nil || len(records) != 1 {
		t.Fatalf("usage records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.ProcurementCostMicros == nil || *record.ProcurementCostMicros != 77 || record.ProcurementCostSource != "billing" || record.ProviderBillingLineID != line.ID {
		t.Fatalf("reconciled usage = %+v", record)
	}
}

func TestEffectivePricingDecisionCanaryOrdersCandidateAndRollbackStopsIt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "decision-test-secret")
	svc.now = func() time.Time { return now }
	decision := EffectivePricingDecision{
		ID: "decision-a", Model: "public-model", Protocol: "openai_chat_completions",
		CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
		Status: EffectivePricingDecisionRecommended, CanaryPercent: 100,
		Confidence: ProcurementCostConfidenceExact, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveEffectivePricingDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	decision, err := svc.ActOnEffectivePricingDecision(ctx, "tester", decision.ID, EffectivePricingDecisionActionRequest{Action: "approve_canary", CanaryPercent: 100})
	if err != nil || decision.Status != EffectivePricingDecisionCanary {
		t.Fatalf("approve canary decision=%+v err=%v", decision, err)
	}
	candidates := []GatewayProvider{{ID: "provider-a", AccountID: "account-a"}, {ID: "provider-b", AccountID: "account-b"}}
	ordered := svc.OrderGatewayCandidatesByEffectivePricing(ctx, "public-model", "openai_chat_completions", "fingerprint-a", candidates)
	if ordered[0].AccountID != "account-b" || !strings.Contains(ordered[0].SelectionReason, decision.ID) {
		t.Fatalf("canary candidate order=%+v", ordered)
	}
	decision, err = svc.ActOnEffectivePricingDecision(ctx, "tester", decision.ID, EffectivePricingDecisionActionRequest{Action: "rollback"})
	if err != nil || decision.Status != EffectivePricingDecisionRolledBack {
		t.Fatalf("rollback decision=%+v err=%v", decision, err)
	}
	ordered = svc.OrderGatewayCandidatesByEffectivePricing(ctx, "public-model", "openai_chat_completions", "fingerprint-a", candidates)
	if ordered[0].AccountID != "account-a" {
		t.Fatalf("rolled back decision still changed order=%+v", ordered)
	}
}

func TestEffectivePricingDecisionUsesCacheQualityAsTiebreaker(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	seedEffectivePricingProvider(t, repo, now, "provider-a", "account-a", "Channel A")
	seedEffectivePricingProvider(t, repo, now, "provider-b", "account-b", "Channel B")
	for _, accountID := range []string{"account-a", "account-b"} {
		providerID := "provider-a"
		if accountID == "account-b" {
			providerID = "provider-b"
		}
		if err := repo.SaveProcurementPrice(ctx, ProcurementPrice{
			ID: "price-" + accountID, ProviderID: providerID, ProviderAccountID: accountID,
			UpstreamModel: "model", Protocol: "openai_chat_completions", Currency: "USD",
			UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000,
			ReferenceInputMicrosPer1MTokens: 1_000_000, Confidence: ProcurementCostConfidenceExact,
			Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	currentTotal, currentUncached, currentCached := 100, 90, 10
	candidateTotal, candidateUncached, candidateCached := 100, 20, 80
	currentCost, candidateCost := int64(50), int64(50)
	for _, input := range []GatewayUsageInput{
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", InputTokens: 100, TotalInputTokens: &currentTotal, UncachedInputTokens: &currentUncached, CacheReadTokens: &currentCached, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai", ProcurementCostMicros: &currentCost, ProcurementCostConfidence: ProcurementCostConfidenceExact},
		{Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions", ProviderID: "provider-b", ProviderAccountID: "account-b", Status: "forwarded", InputTokens: 100, TotalInputTokens: &candidateTotal, UncachedInputTokens: &candidateUncached, CacheReadTokens: &candidateCached, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai", ProcurementCostMicros: &candidateCost, ProcurementCostConfidence: ProcurementCostConfidenceExact},
	} {
		if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "cache-tiebreak-key"}}, input); err != nil {
			t.Fatal(err)
		}
	}
	for _, capability := range []ProviderCacheCapability{
		{ID: "cachecap-a", ProviderAccountID: "account-a", UpstreamModel: "model", Protocol: "openai_chat_completions", SupportStatus: CacheSupportObserved, PoolAffinityGrade: PoolAffinityProbable, BillingConsistencyRate: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "cachecap-b", ProviderAccountID: "account-b", UpstreamModel: "model", Protocol: "openai_chat_completions", SupportStatus: CacheSupportObserved, PoolAffinityGrade: PoolAffinityProbable, BillingConsistencyRate: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.SaveProviderCacheCapability(ctx, capability); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.UpdateEffectivePricingPolicy(ctx, "tester", EffectivePricingPolicyRequest{
		Mode: EffectivePricingModeRecommend, WindowHours: 24, MinSampleCount: 1, MinMetricsCoverage: 0.8,
		MinBillingConsistency: 0.95, MinCostImprovement: 0.08, MaxErrorRateRegression: 0.01,
		MaxP95LatencyRegression: 0.2, CanaryPercent: 5, SupplierAffinityTTLSeconds: 3600,
		AccountAffinityTTLSeconds: 1800, ProbeDailyTokenBudget: 1000, ProbeDailyCostBudgetMicros: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := svc.EvaluateEffectivePricingDecision(ctx, "tester", EffectivePricingDecisionEvaluationRequest{
		Model: "public", UpstreamModel: "model", Protocol: "openai_chat_completions",
		CurrentProviderAccountID: "account-a", CandidateProviderAccountID: "account-b",
	})
	if err != nil {
		t.Fatalf("EvaluateEffectivePricingDecision(): %v", err)
	}
	if decision.Status != EffectivePricingDecisionRecommended || !contains(decision.ReasonCodes, "cache_quality_tiebreaker") || decision.CostImprovement != 0 {
		t.Fatalf("cache tiebreak decision = %+v", decision)
	}
}

func seedEffectivePricingProvider(t *testing.T, repo *MemoryRepository, now time.Time, providerID, accountID, name string) {
	t.Helper()
	if err := repo.SaveProvider(context.Background(), ProviderConnection{ID: providerID, Name: name, Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProviderAccount(context.Background(), ProviderAccount{ID: accountID, ProviderID: providerID, Name: name + " Account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, Models: []string{"model"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}
