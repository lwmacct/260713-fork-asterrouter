package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunProviderCacheProbeObservesReuseAndCapturesEvidence(t *testing.T) {
	var calls atomic.Int32
	var bodiesMu sync.Mutex
	bodies := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(calls.Add(1))
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer probe-secret" {
			t.Errorf("unexpected request path=%s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		encoded, _ := json.Marshal(payload)
		bodiesMu.Lock()
		bodies = append(bodies, string(encoded))
		bodiesMu.Unlock()
		cached := 0
		if call == 2 {
			cached = 1900
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", fmt.Sprintf("probe-request-%d", call))
		_, _ = fmt.Fprintf(w, `{"usage":{"prompt_tokens":2048,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":%d}}}`, cached)
	}))
	defer upstream.Close()

	svc, repo, accountID := newCacheProbeTestService(t, upstream.URL+"/v1", 0, 100_000, 100_000)
	run, err := svc.RunProviderCacheProbe(context.Background(), "tester", CacheProbeRequest{
		ProviderAccountID: accountID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions",
		PrefixTokens: 2048, MaxCostMicros: 100_000,
	})
	if err != nil {
		t.Fatalf("RunProviderCacheProbe(): %v", err)
	}
	if run.Status != CacheProbeStatusSucceeded || run.ReuseCacheReadTokens != 1900 || run.WarmCacheReadTokens != 0 || run.ControlCacheReadTokens != 0 || !run.CacheFieldsPresent {
		t.Fatalf("probe run = %+v", run)
	}
	if run.WarmUpstreamRequestID != "probe-request-1" || run.ReuseUpstreamRequestID != "probe-request-2" || run.ControlUpstreamRequestID != "probe-request-3" {
		t.Fatalf("upstream request evidence = %+v", run)
	}
	if calls.Load() != 3 || len(bodies) != 3 {
		t.Fatalf("calls=%d bodies=%d", calls.Load(), len(bodies))
	}
	for _, body := range bodies {
		if !strings.Contains(body, "ASTERROUTER SYNTHETIC") || strings.Contains(strings.ToLower(body), "customer prompt") {
			t.Fatalf("probe body is not synthetic-only: %s", body)
		}
	}
	capabilities, err := repo.ListProviderCacheCapabilities(context.Background())
	if err != nil || len(capabilities) != 1 {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	if capabilities[0].SupportStatus != CacheSupportObserved || capabilities[0].PoolAffinityGrade != PoolAffinityProbable || capabilities[0].ProbeSampleCount != 1 || capabilities[0].AffinityConsistencyRate != 1 {
		t.Fatalf("capability = %+v", capabilities[0])
	}
}

func TestRunProviderCacheProbeRequiresThreeConsecutiveMissesBeforeFragmenting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1024,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}}`))
	}))
	defer upstream.Close()

	svc, repo, accountID := newCacheProbeTestService(t, upstream.URL+"/v1", 0, 100_000, 100_000)
	now := time.Now().UTC().Add(time.Minute)
	svc.now = func() time.Time { return now }
	for attempt := 1; attempt <= 3; attempt++ {
		run, err := svc.RunProviderCacheProbe(context.Background(), "tester", CacheProbeRequest{
			ProviderAccountID: accountID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions",
			PrefixTokens: 1024, MaxCostMicros: 100_000,
		})
		if err != nil || run.Status != CacheProbeStatusFailed || run.FailureReason != "cache_reuse_not_observed" {
			t.Fatalf("attempt=%d run=%+v err=%v", attempt, run, err)
		}
		capabilities, capabilityErr := repo.ListProviderCacheCapabilities(context.Background())
		if capabilityErr != nil || len(capabilities) != 1 {
			t.Fatalf("attempt=%d capabilities=%+v err=%v", attempt, capabilities, capabilityErr)
		}
		if attempt < 3 && capabilities[0].PoolAffinityGrade == PoolAffinityFragmented {
			t.Fatalf("single miss fragmented capability: %+v", capabilities[0])
		}
		if attempt == 3 && (capabilities[0].PoolAffinityGrade != PoolAffinityFragmented || capabilities[0].SupportStatus != CacheSupportDegraded) {
			t.Fatalf("three misses did not degrade capability: %+v", capabilities[0])
		}
		now = now.Add(time.Second)
	}
}

func TestRunProviderCacheProbeSkipsWithoutSpendingWhenDisabledOrBudgetExhausted(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":256,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}}`))
	}))
	defer upstream.Close()

	disabled, _, disabledAccountID := newCacheProbeTestService(t, upstream.URL+"/v1", 0, 768, 100_000)
	policy, err := disabled.EffectivePricingPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	policy.ProbeEnabled = false
	if err := disabled.repo.SaveEffectivePricingPolicy(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	run, err := disabled.RunProviderCacheProbe(context.Background(), "tester", CacheProbeRequest{
		ProviderAccountID: disabledAccountID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions", PrefixTokens: 256, MaxCostMicros: 100_000,
	})
	if err != nil || run.Status != CacheProbeStatusSkipped || run.FailureReason != "probe_disabled" || calls.Load() != 0 {
		t.Fatalf("disabled run=%+v calls=%d err=%v", run, calls.Load(), err)
	}

	limited, _, limitedAccountID := newCacheProbeTestService(t, upstream.URL+"/v1", 0, 767, 100_000)
	run, err = limited.RunProviderCacheProbe(context.Background(), "tester", CacheProbeRequest{
		ProviderAccountID: limitedAccountID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions", PrefixTokens: 256, MaxCostMicros: 100_000,
	})
	if err != nil || run.Status != CacheProbeStatusSkipped || run.FailureReason != "probe_daily_token_budget_exceeded" || calls.Load() != 0 {
		t.Fatalf("budget run=%+v calls=%d err=%v", run, calls.Load(), err)
	}
}

func TestReserveProviderCacheProbeRunIsAtomicInMemory(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	limits := CacheProbeReservationLimits{
		DayStart: now.Add(-12 * time.Hour), Now: now, Cooldown: time.Hour, StaleAfter: time.Minute,
		DailyTokenBudget: 10_000, DailyCostBudgetMicros: 10_000,
	}
	var reserved atomic.Int32
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ok, _, err := repo.ReserveProviderCacheProbeRun(context.Background(), ProviderCacheProbeRun{
				ID: fmt.Sprintf("probe-%d", id), ProviderAccountID: "account", UpstreamModel: "model", Protocol: "openai_chat_completions",
				PrefixTokens: 256, EstimatedCostMicros: 100, Status: CacheProbeStatusRunning, StartedAt: now,
			}, limits)
			if err != nil {
				t.Errorf("ReserveProviderCacheProbeRun(): %v", err)
			}
			if ok {
				reserved.Add(1)
			}
		}(index)
	}
	wg.Wait()
	if reserved.Load() != 1 {
		t.Fatalf("reserved=%d, want 1", reserved.Load())
	}
}

func newCacheProbeTestService(t *testing.T, baseURL string, cooldownSeconds int, dailyTokenBudget, dailyCostBudget int64) (*Service, *MemoryRepository, string) {
	t.Helper()
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "probe-test-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "probe provider", Type: "openai_compatible", BaseURL: baseURL, Status: ProviderStatusActive,
		Models: []string{"probe-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateProviderAccount(ctx, "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "probe account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Concurrency: 2, Models: []string{"probe-model"}, Secret: "probe-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateProcurementPrice(ctx, "tester", ProcurementPriceRequest{
		ProviderID: provider.ID, ProviderAccountID: account.ID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions",
		Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000,
		OutputMicrosPer1MTokens: 2_000_000, ReferenceInputMicrosPer1MTokens: 1_000_000,
		ReferenceOutputMicrosPer1MTokens: 2_000_000, SourceKind: "manual", Confidence: ProcurementCostConfidenceEstimated,
		Status: ProcurementPriceStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateEffectivePricingPolicy(ctx, "tester", EffectivePricingPolicyRequest{
		Mode: EffectivePricingModeObserveOnly, WindowHours: 24, MinSampleCount: 1, MinMetricsCoverage: 0,
		MinBillingConsistency: 0, MinCostImprovement: 0, MaxErrorRateRegression: 1, MaxP95LatencyRegression: 1,
		CanaryPercent: 5, SupplierAffinityTTLSeconds: 3600, AccountAffinityTTLSeconds: 1800,
		ProbeEnabled: true, ProbeDailyTokenBudget: dailyTokenBudget, ProbeDailyCostBudgetMicros: dailyCostBudget,
		ProbeCooldownSeconds: cooldownSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, account.ID
}
