package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestRecordGatewayUsageEstimatesProcurementCostFromCacheAwarePrice(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	price := ProcurementPrice{
		ID: "price-cache-aware", ProviderID: "provider-a", ProviderAccountID: "account-a",
		UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", Currency: "USD",
		UncachedInputMicrosPer1MTokens: 1_000_000,
		CacheReadMicrosPer1MTokens:     100_000,
		CacheWrite5mMicrosPer1MTokens:  1_250_000,
		CacheWrite1hMicrosPer1MTokens:  2_000_000,
		OutputMicrosPer1MTokens:        2_000_000,
		RequestMicros:                  1_000,
		Confidence:                     ProcurementCostConfidenceEstimated, Status: ProcurementPriceStatusActive,
		EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	if err := repo.SaveProcurementPrice(ctx, price); err != nil {
		t.Fatalf("SaveProcurementPrice(): %v", err)
	}
	cacheRead := 60
	cacheWrite5m := 10
	cacheWrite1h := 5
	uncached := 40
	total := 115
	ttft := int64(125)
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-a", Fingerprint: "fingerprint-a"}}, GatewayUsageInput{
		Model: "public-model", UpstreamModel: price.UpstreamModel, Protocol: price.Protocol,
		ProviderID: price.ProviderID, ProviderAccountID: price.ProviderAccountID, Status: "forwarded",
		LatencyMS: 500, TTFTMS: &ttft, InputTokens: total, OutputTokens: 20,
		TotalInputTokens: &total, UncachedInputTokens: &uncached, CacheReadTokens: &cacheRead,
		CacheWrite5mTokens: &cacheWrite5m, CacheWrite1hTokens: &cacheWrite1h, CacheFieldsPresent: true,
		UsageNormalizationStatus: "normalized_openai", UpstreamRequestID: "request-a",
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	records, err := repo.ListUsageRecords(ctx, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("ListUsageRecords() records=%+v err=%v", records, err)
	}
	record := records[0]
	if record.ProcurementCostMicros == nil || *record.ProcurementCostMicros != 1_109 {
		t.Fatalf("procurement cost = %+v, want 1109 micros", record.ProcurementCostMicros)
	}
	if record.ProcurementCostCurrency != "USD" || record.ProcurementCostSource != "procurement_price" || record.ProcurementCostConfidence != ProcurementCostConfidenceEstimated || record.ProcurementPriceID != price.ID {
		t.Fatalf("procurement evidence = %+v", record)
	}
	if record.TTFTMS == nil || *record.TTFTMS != ttft || record.CacheReadTokens == nil || *record.CacheReadTokens != cacheRead || record.UpstreamRequestID != "request-a" {
		t.Fatalf("usage evidence = %+v", record)
	}
}

func TestRecordGatewayUsageTreatsMissingCacheFieldsAsUncached(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return now }
	price := ProcurementPrice{
		ID: "price-no-cache-fields", ProviderID: "provider-a", ProviderAccountID: "account-a",
		UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", Currency: "USD",
		UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 1,
		Status: ProcurementPriceStatusActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveProcurementPrice(ctx, price); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-a"}}, GatewayUsageInput{
		Model: "public-model", UpstreamModel: price.UpstreamModel, Protocol: price.Protocol,
		ProviderID: price.ProviderID, ProviderAccountID: price.ProviderAccountID, InputTokens: 100, Status: "forwarded",
	}); err != nil {
		t.Fatal(err)
	}
	records, _ := repo.ListUsageRecords(ctx, 10)
	if len(records) != 1 || records[0].ProcurementCostMicros == nil || *records[0].ProcurementCostMicros != 100 {
		t.Fatalf("usage records = %+v", records)
	}
}
