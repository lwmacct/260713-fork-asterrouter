package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestEffectivePricingAdminEndpointsCreatePriceAndReconcileBilling(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "pricing provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "account-secret", 10, 2)
	priceBody := fmt.Sprintf(`{"provider_id":%q,"provider_account_id":%q,"upstream_model":"upstream-model","protocol":"openai_chat_completions","currency":"USD","uncached_input_micros_per_1m_tokens":1000000,"cache_read_micros_per_1m_tokens":100000,"output_micros_per_1m_tokens":2000000,"reference_input_micros_per_1m_tokens":1000000,"reference_output_micros_per_1m_tokens":2000000,"quoted_multiplier":0.2,"recharge_multiplier":1,"source_kind":"manual","confidence":"estimated","status":"active"}`, provider.ID, account.ID)
	create := httptest.NewRequest(http.MethodPost, "/api/v1/admin/procurement-prices", bytes.NewBufferString(priceBody))
	create.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create price status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}

	if err := control.RecordGatewayUsage(context.Background(), controlplane.GatewayAuthContext{APIKey: controlplane.APIKeyRecord{ID: "billing-key"}}, controlplane.GatewayUsageInput{
		Model: "public-model", UpstreamModel: "upstream-model", Protocol: "openai_chat_completions",
		ProviderID: provider.ID, ProviderAccountID: account.ID, Status: "forwarded", InputTokens: 100,
		UpstreamRequestID: "upstream-request-route-test",
	}); err != nil {
		t.Fatal(err)
	}
	billingBody := fmt.Sprintf(`{"provider_id":%q,"provider_account_id":%q,"external_line_id":"line-route-test","external_request_id":"upstream-request-route-test","upstream_model":"upstream-model","currency":"USD","amount_micros":77,"source_kind":"api","confidence":"exact"}`, provider.ID, account.ID)
	billing := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-billing-lines", bytes.NewBufferString(billingBody))
	billing.Header.Set("Content-Type", "application/json")
	billingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(billingRecorder, billing)
	if billingRecorder.Code != http.StatusOK {
		t.Fatalf("billing status=%d body=%s", billingRecorder.Code, billingRecorder.Body.String())
	}
	var billingResponse struct {
		Data controlplane.ProviderBillingLine `json:"data"`
	}
	if err := json.Unmarshal(billingRecorder.Body.Bytes(), &billingResponse); err != nil {
		t.Fatal(err)
	}
	if billingResponse.Data.ReconciliationStatus != controlplane.BillingReconciliationMatched || billingResponse.Data.UsageRecordID == "" {
		t.Fatalf("billing response=%+v", billingResponse.Data)
	}

	report := httptest.NewRequest(http.MethodGet, "/api/v1/admin/effective-pricing/report?model=upstream-model&protocol=openai_chat_completions&window_hours=24", nil)
	reportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reportRecorder, report)
	if reportRecorder.Code != http.StatusOK || !bytes.Contains(reportRecorder.Body.Bytes(), []byte(`"procurement_cost_confidence"`)) && !bytes.Contains(reportRecorder.Body.Bytes(), []byte(`"cost_confidence":"exact"`)) {
		t.Fatalf("report status=%d body=%s", reportRecorder.Code, reportRecorder.Body.String())
	}
}

func TestEffectivePricingPolicyEndpointRejectsUnsafeValues(t *testing.T) {
	handler := newTestHandler(t, config.Config{})
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/effective-pricing/policy", bytes.NewBufferString(`{"mode":"canary","window_hours":24,"min_sample_count":0,"min_metrics_coverage":0.8,"min_billing_consistency":0.95,"min_cost_improvement":0.08,"max_error_rate_regression":0.005,"max_p95_latency_regression":0.2,"canary_percent":5,"supplier_affinity_ttl_seconds":86400,"account_affinity_ttl_seconds":1800}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderCacheProbeEndpointRunsControlledSequenceAndRejectsMissingConfirmation(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		cached := 0
		if call == 2 {
			cached = 240
		}
		w.Header().Set("X-Request-ID", fmt.Sprintf("route-probe-%d", call))
		_, _ = fmt.Fprintf(w, `{"usage":{"prompt_tokens":256,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":%d}}}`, cached)
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "probe provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"probe-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "probe-model", "account-secret", 10, 2)
	if _, err := control.CreateProcurementPrice(context.Background(), "tester", controlplane.ProcurementPriceRequest{
		ProviderID: provider.ID, ProviderAccountID: account.ID, UpstreamModel: "probe-model", Protocol: "openai_chat_completions",
		Currency: "USD", UncachedInputMicrosPer1MTokens: 1_000_000, CacheReadMicrosPer1MTokens: 100_000,
		OutputMicrosPer1MTokens: 2_000_000, ReferenceInputMicrosPer1MTokens: 1_000_000,
		ReferenceOutputMicrosPer1MTokens: 2_000_000, SourceKind: "manual", Confidence: "estimated", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := control.UpdateEffectivePricingPolicy(context.Background(), "tester", controlplane.EffectivePricingPolicyRequest{
		Mode: "observe_only", WindowHours: 24, MinSampleCount: 1, MinMetricsCoverage: 0, MinBillingConsistency: 0,
		MinCostImprovement: 0, MaxErrorRateRegression: 1, MaxP95LatencyRegression: 1, CanaryPercent: 5,
		SupplierAffinityTTLSeconds: 3600, AccountAffinityTTLSeconds: 1800, ProbeEnabled: true,
		ProbeDailyTokenBudget: 100_000, ProbeDailyCostBudgetMicros: 100_000, ProbeCooldownSeconds: 0,
	}); err != nil {
		t.Fatal(err)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-cache-probes", bytes.NewBufferString(fmt.Sprintf(`{"provider_account_id":%q,"upstream_model":"probe-model","protocol":"openai_chat_completions","prefix_tokens":256}`, account.ID)))
	invalid.Header.Set("Content-Type", "application/json")
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("invalid status=%d calls=%d body=%s", invalidRecorder.Code, calls.Load(), invalidRecorder.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-cache-probes", bytes.NewBufferString(fmt.Sprintf(`{"provider_account_id":%q,"upstream_model":"probe-model","protocol":"openai_chat_completions","prefix_tokens":256,"max_cost_micros":100000}`, account.ID)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data controlplane.ProviderCacheProbeRun `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Status != controlplane.CacheProbeStatusSucceeded || response.Data.ReuseCacheReadTokens != 240 || response.Data.ReuseUpstreamRequestID != "route-probe-2" || calls.Load() != 3 {
		t.Fatalf("response=%+v calls=%d", response.Data, calls.Load())
	}
}
