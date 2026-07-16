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

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestEffectivePricingAdminEndpointsCreatePriceAndReconcileBilling(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "pricing provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "account-secret", 10, 2)
	incompletePriceBody := fmt.Sprintf(`{"provider_id":%q,"provider_account_id":%q,"upstream_model":"upstream-model","protocol":"openai_chat_completions","currency":"USD","uncached_input_micros_per_1m_tokens":1000000,"cache_read_micros_per_1m_tokens":100000,"cache_write_5m_micros_per_1m_tokens":1250000,"output_micros_per_1m_tokens":2000000,"request_micros":0,"reference_input_micros_per_1m_tokens":1000000,"reference_output_micros_per_1m_tokens":2000000}`, provider.ID, account.ID)
	incompletePrice := httptest.NewRequest(http.MethodPost, "/api/v1/admin/procurement-prices", bytes.NewBufferString(incompletePriceBody))
	incompletePrice.Header.Set("Content-Type", "application/json")
	incompletePriceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(incompletePriceRecorder, incompletePrice)
	if incompletePriceRecorder.Code != http.StatusBadRequest || !bytes.Contains(incompletePriceRecorder.Body.Bytes(), []byte("cache_write_1h_micros_per_1m_tokens is required")) {
		t.Fatalf("incomplete price status=%d body=%s", incompletePriceRecorder.Code, incompletePriceRecorder.Body.String())
	}

	priceBody := fmt.Sprintf(`{"provider_id":%q,"provider_account_id":%q,"upstream_model":"upstream-model","protocol":"openai_chat_completions","currency":"USD","uncached_input_micros_per_1m_tokens":1000000,"cache_read_micros_per_1m_tokens":100000,"cache_write_5m_micros_per_1m_tokens":1250000,"cache_write_1h_micros_per_1m_tokens":2000000,"output_micros_per_1m_tokens":2000000,"request_micros":0,"reference_input_micros_per_1m_tokens":1000000,"reference_output_micros_per_1m_tokens":2000000,"quoted_multiplier":0.2,"recharge_multiplier":1,"source_kind":"manual","confidence":"estimated","status":"active"}`, provider.ID, account.ID)
	create := httptest.NewRequest(http.MethodPost, "/api/v1/admin/procurement-prices", bytes.NewBufferString(priceBody))
	create.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create price status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}

	capabilityBody := fmt.Sprintf(`{"provider_account_id":%q,"upstream_model":"upstream-model","protocol":"openai_chat_completions","support_status":"claimed","pool_affinity_grade":"unknown","affinity_transport":"header","affinity_field":"X-Session-ID","cache_control_mode":"prompt_cache_key","usage_schema":"openai"}`, account.ID)
	capability := httptest.NewRequest(http.MethodPut, "/api/v1/admin/provider-cache-capabilities", bytes.NewBufferString(capabilityBody))
	capability.Header.Set("Content-Type", "application/json")
	capabilityRecorder := httptest.NewRecorder()
	handler.ServeHTTP(capabilityRecorder, capability)
	if capabilityRecorder.Code != http.StatusOK || !bytes.Contains(capabilityRecorder.Body.Bytes(), []byte(`"affinity_field":"X-Session-ID"`)) {
		t.Fatalf("capability status=%d body=%s", capabilityRecorder.Code, capabilityRecorder.Body.String())
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
	if reportRecorder.Code != http.StatusOK || !bytes.Contains(reportRecorder.Body.Bytes(), []byte(`"cost_available":true`)) || !bytes.Contains(reportRecorder.Body.Bytes(), []byte(`"cache_read_micros_per_1m_tokens":100000`)) || !bytes.Contains(reportRecorder.Body.Bytes(), []byte(`"cost_confidence":"exact"`)) {
		t.Fatalf("report status=%d body=%s", reportRecorder.Code, reportRecorder.Body.String())
	}
}

func TestEffectivePricingPolicyEndpointRejectsUnsafeValues(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/effective-pricing/policy", bytes.NewBufferString(`{"mode":"canary","window_hours":24,"min_sample_count":0,"min_metrics_coverage":0.8,"min_billing_consistency":0.95,"min_cost_improvement":0.08,"max_error_rate_regression":0.005,"max_p95_latency_regression":0.2,"canary_percent":5,"supplier_affinity_ttl_seconds":86400,"account_affinity_ttl_seconds":1800}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderBillingSourceInspectionEndpointDetectsSub2APIWithoutInventingLines(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" || r.Header.Get("Authorization") != "Bearer account-billing-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"mode":"unrestricted","isValid":true,"unit":"USD","balance":9.25,"usage":{"total":{"requests":5,"input_tokens":100,"output_tokens":20,"cache_creation_tokens":30,"cache_read_tokens":40,"cost":1.5,"actual_cost":0.75}}}`))
	}))
	defer upstream.Close()

	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "billing source", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "model", "account-billing-secret", 10, 2)
	body := fmt.Sprintf(`{"provider_account_id":%q,"adapter_id":"auto"}`, account.ID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-billing-sources/inspect", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"adapter_id":"sub2api_compatible"`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"kind":"wallet_balance"`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"amount_micros":9250000`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"discovered_lines":0`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"usage_cost_lines":false`)) {
		t.Fatalf("body=%s", recorder.Body.String())
	}

	configureBody := fmt.Sprintf(`{"provider_account_id":%q,"adapter_id":"sub2api_compatible","status":"observe_only","automatic_sync_enabled":true,"sync_interval_seconds":3600}`, account.ID)
	configure := httptest.NewRequest(http.MethodPut, "/api/v1/admin/provider-billing-sources", bytes.NewBufferString(configureBody))
	configure.Header.Set("Content-Type", "application/json")
	configureRecorder := httptest.NewRecorder()
	handler.ServeHTTP(configureRecorder, configure)
	if configureRecorder.Code != http.StatusOK {
		t.Fatalf("configure status=%d body=%s", configureRecorder.Code, configureRecorder.Body.String())
	}
	var configured struct {
		Data controlplane.ProviderBillingSource `json:"data"`
	}
	if err := json.Unmarshal(configureRecorder.Body.Bytes(), &configured); err != nil {
		t.Fatal(err)
	}
	if configured.Data.ID == "" || configured.Data.Version != 1 || !configured.Data.AutomaticSyncEnabled {
		t.Fatalf("configured source=%+v", configured.Data)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-billing-sources", nil)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK || !bytes.Contains(listRecorder.Body.Bytes(), []byte(configured.Data.ID)) {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}

	syncRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-billing-sources/"+configured.Data.ID+"/sync", nil)
	syncRecorder := httptest.NewRecorder()
	handler.ServeHTTP(syncRecorder, syncRequest)
	if syncRecorder.Code != http.StatusOK || !bytes.Contains(syncRecorder.Body.Bytes(), []byte(`"status":"succeeded"`)) {
		t.Fatalf("sync status=%d body=%s", syncRecorder.Code, syncRecorder.Body.String())
	}

	evidence := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-billing-sources/"+configured.Data.ID+"/evidence?limit=20", nil)
	evidenceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(evidenceRecorder, evidence)
	if evidenceRecorder.Code != http.StatusOK || !bytes.Contains(evidenceRecorder.Body.Bytes(), []byte(`"amount_micros":9250000`)) || !bytes.Contains(evidenceRecorder.Body.Bytes(), []byte(`"scope":"total"`)) {
		t.Fatalf("evidence status=%d body=%s", evidenceRecorder.Code, evidenceRecorder.Body.String())
	}

	staleBody := fmt.Sprintf(`{"provider_account_id":%q,"adapter_id":"sub2api_compatible","status":"disabled","automatic_sync_enabled":false,"sync_interval_seconds":3600,"version":1}`, account.ID)
	stale := httptest.NewRequest(http.MethodPut, "/api/v1/admin/provider-billing-sources", bytes.NewBufferString(staleBody))
	stale.Header.Set("Content-Type", "application/json")
	staleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(staleRecorder, stale)
	if staleRecorder.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", staleRecorder.Code, staleRecorder.Body.String())
	}

	badRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-billing-sources/inspect", bytes.NewBufferString(`{"provider_account_id":"missing"}`))
	badRequest.Header.Set("Content-Type", "application/json")
	badRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d body=%s", badRecorder.Code, badRecorder.Body.String())
	}
}

func TestEffectivePricingDecisionEvaluationsEndpointReturnsEmptyHistory(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/effective-pricing/decisions/decision-missing/evaluations?limit=20", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"data":[]`)) {
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

	handler, control := newTestRuntime(t, RuntimeConfig{})
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
		Currency: "USD", UncachedInputMicrosPer1MTokens: pricingMicrosValue(1_000_000), CacheReadMicrosPer1MTokens: pricingMicrosValue(100_000),
		CacheWrite5mMicrosPer1MTokens: pricingMicrosValue(0), CacheWrite1hMicrosPer1MTokens: pricingMicrosValue(0),
		OutputMicrosPer1MTokens: pricingMicrosValue(2_000_000), RequestMicros: pricingMicrosValue(0), ReferenceInputMicrosPer1MTokens: pricingMicrosValue(1_000_000),
		ReferenceOutputMicrosPer1MTokens: pricingMicrosValue(2_000_000), SourceKind: "manual", Confidence: "estimated", Status: "active",
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

func pricingMicrosValue(value int64) *int64 {
	return &value
}
