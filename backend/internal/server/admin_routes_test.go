package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func TestAdminDashboardEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                    `json:"code"`
		Data controlplane.Dashboard `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderCount != 1 || resp.Data.APIKeyCount != 0 {
		t.Fatalf("unexpected dashboard: %+v", resp.Data)
	}
}

func TestAdminModelPricingEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	createBody := bytes.NewBufferString(`{"model":"priced-model","currency":"USD","input_price_cents_per_1m_tokens":120,"output_price_cents_per_1m_tokens":480,"status":"active"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/model-pricings", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create pricing status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.ModelPricing `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create pricing: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Model != "priced-model" || createResp.Data.InputPriceCentsPer1MTokens != 120 {
		t.Fatalf("created pricing mismatch: %+v", createResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"model":"priced-model","currency":"USD","input_price_cents_per_1m_tokens":150,"output_price_cents_per_1m_tokens":500,"status":"disabled"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/model-pricings/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update pricing status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.ModelPricing `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update pricing: %v", err)
	}
	if updateResp.Data.Status != controlplane.ModelPricingStatusDisabled || updateResp.Data.OutputPriceCentsPer1MTokens != 500 {
		t.Fatalf("updated pricing mismatch: %+v", updateResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/model-pricings", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list pricing status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ModelPricing `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list pricing: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ID != createResp.Data.ID {
		t.Fatalf("list pricing mismatch: %+v", listResp.Data)
	}
}

func TestAdminGatewayModelAndRouteEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "route provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-chat"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-chat", "account-secret", 10, 3)

	modelCreate := httptest.NewRequest(http.MethodPost, "/api/v1/admin/gateway-models", bytes.NewBufferString(`{"model_id":"public-chat","name":"Public Chat","modality":"chat","default_route_group":"stable","status":"active"}`))
	modelCreate.Header.Set("Content-Type", "application/json")
	modelCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(modelCreateRec, modelCreate)
	if modelCreateRec.Code != http.StatusOK {
		t.Fatalf("create gateway model status = %d body=%s", modelCreateRec.Code, modelCreateRec.Body.String())
	}
	var modelResp struct {
		Data controlplane.GatewayModel `json:"data"`
	}
	if err := json.Unmarshal(modelCreateRec.Body.Bytes(), &modelResp); err != nil {
		t.Fatalf("decode gateway model: %v", err)
	}

	routeBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","priority":10,"weight":100,"status":"active"}`, modelResp.Data.ID, account.ID)
	routeCreate := httptest.NewRequest(http.MethodPost, "/api/v1/admin/model-routes", bytes.NewBufferString(routeBody))
	routeCreate.Header.Set("Content-Type", "application/json")
	routeCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(routeCreateRec, routeCreate)
	if routeCreateRec.Code != http.StatusOK {
		t.Fatalf("create model route status = %d body=%s", routeCreateRec.Code, routeCreateRec.Body.String())
	}
	var routeResp struct {
		Data controlplane.ModelRoute `json:"data"`
	}
	if err := json.Unmarshal(routeCreateRec.Body.Bytes(), &routeResp); err != nil {
		t.Fatalf("decode model route: %v", err)
	}
	if routeResp.Data.UpstreamModel != "upstream-chat" || routeResp.Data.ProviderAccountID != account.ID {
		t.Fatalf("created route mismatch: %+v", routeResp.Data)
	}
	bulkModel, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{ModelID: "public-bulk", Name: "Public Bulk", Modality: "chat", Status: controlplane.GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	bulkBody := fmt.Sprintf(`{"routes":[{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","priority":30,"weight":100,"status":"active"}]}`, bulkModel.ID, account.ID)
	bulkCreate := httptest.NewRequest(http.MethodPost, "/api/v1/admin/model-routes/bulk", bytes.NewBufferString(bulkBody))
	bulkCreate.Header.Set("Content-Type", "application/json")
	bulkCreateRec := httptest.NewRecorder()
	handler.ServeHTTP(bulkCreateRec, bulkCreate)
	if bulkCreateRec.Code != http.StatusOK || !strings.Contains(bulkCreateRec.Body.String(), `"routes"`) {
		t.Fatalf("bulk model route status = %d body=%s", bulkCreateRec.Code, bulkCreateRec.Body.String())
	}

	modelList := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-models", nil)
	modelListRec := httptest.NewRecorder()
	handler.ServeHTTP(modelListRec, modelList)
	if modelListRec.Code != http.StatusOK || !strings.Contains(modelListRec.Body.String(), `"route_count":1`) {
		t.Fatalf("gateway model list status = %d body=%s", modelListRec.Code, modelListRec.Body.String())
	}

	routeUpdateBody := fmt.Sprintf(`{"gateway_model_id":%q,"route_group":"stable","provider_account_id":%q,"upstream_model":"upstream-chat","priority":20,"weight":250,"status":"disabled"}`, modelResp.Data.ID, account.ID)
	routeUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/admin/model-routes/"+routeResp.Data.ID, bytes.NewBufferString(routeUpdateBody))
	routeUpdate.Header.Set("Content-Type", "application/json")
	routeUpdateRec := httptest.NewRecorder()
	handler.ServeHTTP(routeUpdateRec, routeUpdate)
	if routeUpdateRec.Code != http.StatusOK || !strings.Contains(routeUpdateRec.Body.String(), `"weight":250`) {
		t.Fatalf("update model route status = %d body=%s", routeUpdateRec.Code, routeUpdateRec.Body.String())
	}

	modelDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/gateway-models/"+modelResp.Data.ID, nil)
	modelDeleteRec := httptest.NewRecorder()
	handler.ServeHTTP(modelDeleteRec, modelDelete)
	if modelDeleteRec.Code != http.StatusOK {
		t.Fatalf("delete gateway model status = %d body=%s", modelDeleteRec.Code, modelDeleteRec.Body.String())
	}
	routes, err := control.ListModelRoutes(context.Background())
	if err != nil || len(routes) != 1 || routes[0].GatewayModelID != bulkModel.ID {
		t.Fatalf("expected only the deleted model's routes to be cascade deleted: routes=%+v err=%v", routes, err)
	}
}

func TestAdminProviderAccountModelEndpoints(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"existing"},{"id":"new-model"}]}`))
	}))
	defer upstream.Close()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{Name: "Inventory provider", Type: "openai_compatible", BaseURL: upstream.URL + "/v1", Status: controlplane.ProviderStatusActive, Models: []string{"existing"}, APIKey: "provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "existing", "account-secret", 10, 3)

	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-accounts/"+account.ID+"/models", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, list)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), `"model_id":"existing"`) {
		t.Fatalf("model inventory status = %d body=%s", listRec.Code, listRec.Body.String())
	}

	discover := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts/"+account.ID+"/models/discover", nil)
	discoverRec := httptest.NewRecorder()
	handler.ServeHTTP(discoverRec, discover)
	if discoverRec.Code != http.StatusOK || !strings.Contains(discoverRec.Body.String(), `"new-model"`) {
		t.Fatalf("model discovery status = %d body=%s", discoverRec.Code, discoverRec.Body.String())
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts/"+account.ID+"/models/sync", bytes.NewBufferString(`{"enabled_models":["existing","new-model"],"auto_enable_new_models":true}`))
	syncReq.Header.Set("Content-Type", "application/json")
	syncRec := httptest.NewRecorder()
	handler.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK || !strings.Contains(syncRec.Body.String(), `"auto_enable_new_models":true`) {
		t.Fatalf("model sync status = %d body=%s", syncRec.Code, syncRec.Body.String())
	}
}

func TestAdminGovernancePolicyEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	createBody := bytes.NewBufferString(`{"name":"Platform policy","scope_type":"global","model_allowlist":["gpt-4o-mini"],"model_denylist":[],"qps_limit":10,"monthly_token_limit":1000000,"monthly_budget_cents":50000,"overage_action":"block","prompt_logging_mode":"metadata_only","retention_days":30,"tool_call_allowed":true,"image_input_allowed":true,"web_access_allowed":false,"status":"active"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/policies", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create policy status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create policy: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Name != "Platform policy" || createResp.Data.QPSLimit != 10 || createResp.Data.Version != 1 {
		t.Fatalf("created policy mismatch: %+v", createResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"name":"Platform policy updated","scope_type":"global","model_allowlist":[],"model_denylist":["legacy-model"],"qps_limit":0,"monthly_token_limit":0,"monthly_budget_cents":0,"overage_action":"warn","prompt_logging_mode":"disabled","retention_days":0,"tool_call_allowed":false,"image_input_allowed":true,"web_access_allowed":false,"status":"disabled"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update policy status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update policy: %v", err)
	}
	if updateResp.Data.Status != controlplane.GovernancePolicyStatusDisabled || updateResp.Data.OverageAction != controlplane.GovernancePolicyOverageWarn || updateResp.Data.Version != 2 {
		t.Fatalf("updated policy mismatch: %+v", updateResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list policy status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list policy: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ID != createResp.Data.ID {
		t.Fatalf("list policy mismatch: %+v", listResp.Data)
	}
}

func TestAdminRecordEndpointsSupportQueryParameters(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "query key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := control.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", InputTokens: 1, CostCents: 100}); err != nil {
		t.Fatalf("RecordGatewayUsage a: %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), auth, controlplane.GatewayUsageInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", InputTokens: 2, CostCents: 200}); err != nil {
		t.Fatalf("RecordGatewayUsage b: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-a", ResponseSummary: "ok"}); err != nil {
		t.Fatalf("RecordGatewayTrace a: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), auth, controlplane.GatewayTraceInput{Model: "model-b", Status: "error", ProviderID: "provider-b", ErrorType: "policy_error", ResponseSummary: "blocked"}); err != nil {
		t.Fatalf("RecordGatewayTrace b: %v", err)
	}
	other, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name:              "other query key",
		ModelAllowlist:    []string{"model-a"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey other(): %v", err)
	}
	otherAuth, err := control.AuthorizeGatewayModel(context.Background(), other.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel other(): %v", err)
	}
	if err := control.RecordGatewayUsage(context.Background(), otherAuth, controlplane.GatewayUsageInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", InputTokens: 3, CostCents: 300}); err != nil {
		t.Fatalf("RecordGatewayUsage other: %v", err)
	}
	if err := control.RecordGatewayTrace(context.Background(), otherAuth, controlplane.GatewayTraceInput{Model: "model-a", Status: "forwarded", ProviderID: "provider-other", ResponseSummary: "other"}); err != nil {
		t.Fatalf("RecordGatewayTrace other: %v", err)
	}
	if err := control.RecordGatewayCall(context.Background(), auth, "model-a", "forwarded", "Pagination query audit marker"); err != nil {
		t.Fatalf("RecordGatewayCall(): %v", err)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?model=model-b&status=error&limit=1", nil)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	if usageRec.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s", usageRec.Code, usageRec.Body.String())
	}
	var usageResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usageResp); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(usageResp.Data.Recent) != 1 || usageResp.Data.Recent[0].Model != "model-b" || usageResp.Data.Recent[0].Status != "error" {
		t.Fatalf("usage query not applied: %+v", usageResp.Data.Recent)
	}

	usageKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	usageKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(usageKeyRec, usageKeyReq)
	if usageKeyRec.Code != http.StatusOK {
		t.Fatalf("usage key status = %d body=%s", usageKeyRec.Code, usageKeyRec.Body.String())
	}
	var usageKeyResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageKeyRec.Body.Bytes(), &usageKeyResp); err != nil {
		t.Fatalf("decode usage key: %v", err)
	}
	if len(usageKeyResp.Data.Recent) != 2 || usageKeyResp.Data.TotalRequests != 2 {
		t.Fatalf("usage api_key_id filter count mismatch: %+v", usageKeyResp.Data)
	}
	for _, record := range usageKeyResp.Data.Recent {
		if record.APIKeyID != created.Record.ID {
			t.Fatalf("usage api_key_id leaked another key: %+v", usageKeyResp.Data.Recent)
		}
	}

	costReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cost-allocation?dimension=api_key&api_key_id="+url.QueryEscape(created.Record.ID), nil)
	costRec := httptest.NewRecorder()
	handler.ServeHTTP(costRec, costReq)
	if costRec.Code != http.StatusOK {
		t.Fatalf("cost allocation status = %d body=%s", costRec.Code, costRec.Body.String())
	}
	var costResp struct {
		Data controlplane.CostAllocationReport `json:"data"`
	}
	if err := json.Unmarshal(costRec.Body.Bytes(), &costResp); err != nil {
		t.Fatalf("decode cost allocation: %v", err)
	}
	if costResp.Data.Dimension != controlplane.CostAllocationByAPIKey || costResp.Data.TotalRequests != 2 || costResp.Data.TotalCostCents != 300 || len(costResp.Data.Rows) != 1 {
		t.Fatalf("cost allocation mismatch: %+v", costResp.Data)
	}
	if costResp.Data.Rows[0].APIKeyID != created.Record.ID || costResp.Data.Rows[0].APIKeyName != "query key" {
		t.Fatalf("cost allocation row mismatch: %+v", costResp.Data.Rows)
	}

	costBadReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cost-allocation?dimension=project", nil)
	costBadRec := httptest.NewRecorder()
	handler.ServeHTTP(costBadRec, costBadReq)
	if costBadRec.Code != http.StatusBadRequest {
		t.Fatalf("cost allocation invalid dimension status = %d body=%s", costBadRec.Code, costBadRec.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?status=error&q=provider-b", nil)
	traceRec := httptest.NewRecorder()
	handler.ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("trace status = %d body=%s", traceRec.Code, traceRec.Body.String())
	}
	var traceResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceResp); err != nil {
		t.Fatalf("decode traces: %v", err)
	}
	if len(traceResp.Data) != 1 || traceResp.Data[0].ProviderID != "provider-b" || traceResp.Data[0].Status != "error" {
		t.Fatalf("trace query not applied: %+v", traceResp.Data)
	}

	traceKeyReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=10", nil)
	traceKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeyRec, traceKeyReq)
	if traceKeyRec.Code != http.StatusOK {
		t.Fatalf("trace key status = %d body=%s", traceKeyRec.Code, traceKeyRec.Body.String())
	}
	var traceKeyResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceKeyRec.Body.Bytes(), &traceKeyResp); err != nil {
		t.Fatalf("decode trace key: %v", err)
	}
	if len(traceKeyResp.Data) != 2 {
		t.Fatalf("trace api_key_id filter count mismatch: %+v", traceKeyResp.Data)
	}
	for _, trace := range traceKeyResp.Data {
		if trace.APIKeyID != created.Record.ID {
			t.Fatalf("trace api_key_id leaked another key: %+v", traceKeyResp.Data)
		}
	}

	traceSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/summary?limit=1", nil)
	traceSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceSummaryRec, traceSummaryReq)
	if traceSummaryRec.Code != http.StatusOK {
		t.Fatalf("trace summary status = %d body=%s", traceSummaryRec.Code, traceSummaryRec.Body.String())
	}
	var traceSummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceSummaryRec.Body.Bytes(), &traceSummaryResp); err != nil {
		t.Fatalf("decode trace summary: %v", err)
	}
	if traceSummaryResp.Data.Total != 3 || traceSummaryResp.Data.Routed != 3 || traceSummaryResp.Data.Errors != 1 {
		t.Fatalf("trace summary should ignore pagination and include matching records: %+v", traceSummaryResp.Data)
	}

	traceKeySummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/summary?api_key_id="+url.QueryEscape(created.Record.ID)+"&limit=1", nil)
	traceKeySummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(traceKeySummaryRec, traceKeySummaryReq)
	if traceKeySummaryRec.Code != http.StatusOK {
		t.Fatalf("trace key summary status = %d body=%s", traceKeySummaryRec.Code, traceKeySummaryRec.Body.String())
	}
	var traceKeySummaryResp struct {
		Data controlplane.GatewayTraceSummary `json:"data"`
	}
	if err := json.Unmarshal(traceKeySummaryRec.Body.Bytes(), &traceKeySummaryResp); err != nil {
		t.Fatalf("decode trace key summary: %v", err)
	}
	if traceKeySummaryResp.Data.Total != 2 || traceKeySummaryResp.Data.Routed != 2 || traceKeySummaryResp.Data.Errors != 1 {
		t.Fatalf("trace key summary mismatch: %+v", traceKeySummaryResp.Data)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?action=invoke&q=Pagination", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit status = %d body=%s", auditRec.Code, auditRec.Body.String())
	}
	var auditResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditRec.Body.Bytes(), &auditResp); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditResp.Data) != 1 || auditResp.Data[0].Action != "invoke" || !strings.Contains(auditResp.Data[0].Summary, "Pagination") {
		t.Fatalf("audit query not applied: %+v", auditResp.Data)
	}

	auditSummaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs/summary?action=invoke&limit=1", nil)
	auditSummaryRec := httptest.NewRecorder()
	handler.ServeHTTP(auditSummaryRec, auditSummaryReq)
	if auditSummaryRec.Code != http.StatusOK {
		t.Fatalf("audit summary status = %d body=%s", auditSummaryRec.Code, auditSummaryRec.Body.String())
	}
	var auditSummaryResp struct {
		Data controlplane.AuditLogSummary `json:"data"`
	}
	if err := json.Unmarshal(auditSummaryRec.Body.Bytes(), &auditSummaryResp); err != nil {
		t.Fatalf("decode audit summary: %v", err)
	}
	if auditSummaryResp.Data.Total != 1 || auditSummaryResp.Data.Actors != 1 || auditSummaryResp.Data.Resources != 1 || auditSummaryResp.Data.Actions != 1 {
		t.Fatalf("audit summary mismatch: %+v", auditSummaryResp.Data)
	}

	future := url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	usageTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?from="+future, nil)
	usageTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(usageTimeRec, usageTimeReq)
	if usageTimeRec.Code != http.StatusOK {
		t.Fatalf("usage time status = %d body=%s", usageTimeRec.Code, usageTimeRec.Body.String())
	}
	var usageTimeResp struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageTimeRec.Body.Bytes(), &usageTimeResp); err != nil {
		t.Fatalf("decode usage time: %v", err)
	}
	if len(usageTimeResp.Data.Recent) != 0 {
		t.Fatalf("usage time range not applied: %+v", usageTimeResp.Data.Recent)
	}

	traceTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces?from="+future, nil)
	traceTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(traceTimeRec, traceTimeReq)
	if traceTimeRec.Code != http.StatusOK {
		t.Fatalf("trace time status = %d body=%s", traceTimeRec.Code, traceTimeRec.Body.String())
	}
	var traceTimeResp struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	if err := json.Unmarshal(traceTimeRec.Body.Bytes(), &traceTimeResp); err != nil {
		t.Fatalf("decode trace time: %v", err)
	}
	if len(traceTimeResp.Data) != 0 {
		t.Fatalf("trace time range not applied: %+v", traceTimeResp.Data)
	}

	auditTimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?from="+future, nil)
	auditTimeRec := httptest.NewRecorder()
	handler.ServeHTTP(auditTimeRec, auditTimeReq)
	if auditTimeRec.Code != http.StatusOK {
		t.Fatalf("audit time status = %d body=%s", auditTimeRec.Code, auditTimeRec.Body.String())
	}
	var auditTimeResp struct {
		Data []controlplane.AuditLog `json:"data"`
	}
	if err := json.Unmarshal(auditTimeRec.Body.Bytes(), &auditTimeResp); err != nil {
		t.Fatalf("decode audit time: %v", err)
	}
	if len(auditTimeResp.Data) != 0 {
		t.Fatalf("audit time range not applied: %+v", auditTimeResp.Data)
	}
}

func TestCreateAPIKeyEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	body := bytes.NewBufferString(`{"name":"demo","model_allowlist":["gpt-4o-mini"],"qps_limit":2,"monthly_token_limit":1000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int                               `json:"code"`
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Key == "" || resp.Data.Record.Fingerprint == "" {
		t.Fatalf("api key response incomplete: %+v", resp.Data)
	}
}

func TestAPIKeyPolicyExplanationEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	policyBody := bytes.NewBufferString(`{"name":"Platform policy","scope_type":"global","model_allowlist":["gpt-4o-mini"],"qps_limit":5,"monthly_token_limit":1000,"overage_action":"block","prompt_logging_mode":"metadata_only","retention_days":30,"tool_call_allowed":true,"image_input_allowed":true,"web_access_allowed":false,"status":"active"}`)
	policyReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/policies", policyBody)
	policyReq.Header.Set("Content-Type", "application/json")
	policyRec := httptest.NewRecorder()
	handler.ServeHTTP(policyRec, policyReq)
	if policyRec.Code != http.StatusOK {
		t.Fatalf("create policy status = %d body=%s", policyRec.Code, policyRec.Body.String())
	}
	var policyResp struct {
		Data controlplane.GovernancePolicy `json:"data"`
	}
	if err := json.Unmarshal(policyRec.Body.Bytes(), &policyResp); err != nil {
		t.Fatalf("decode policy: %v", err)
	}

	keyBody := bytes.NewBufferString(`{"name":"demo","policy_id":"` + policyResp.Data.ID + `","model_allowlist":["gpt-4o-mini"],"qps_limit":2,"monthly_token_limit":1000}`)
	keyReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys", keyBody)
	keyReq.Header.Set("Content-Type", "application/json")
	keyRec := httptest.NewRecorder()
	handler.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("create key status = %d body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyResp struct {
		Data controlplane.APIKeyCreateResponse `json:"data"`
	}
	if err := json.Unmarshal(keyRec.Body.Bytes(), &keyResp); err != nil {
		t.Fatalf("decode key: %v", err)
	}

	explainReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/api-keys/"+keyResp.Data.Record.ID+"/policy-explanation", nil)
	explainRec := httptest.NewRecorder()
	handler.ServeHTTP(explainRec, explainReq)
	if explainRec.Code != http.StatusOK {
		t.Fatalf("explain status = %d body=%s", explainRec.Code, explainRec.Body.String())
	}
	var explainResp struct {
		Data controlplane.GatewayPolicyExplanation `json:"data"`
	}
	if err := json.Unmarshal(explainRec.Body.Bytes(), &explainResp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if explainResp.Data.SelectedPolicyID != policyResp.Data.ID || explainResp.Data.SelectedPolicyVersion != 1 || explainResp.Data.SelectedSource != controlplane.GatewayPolicySourceAPIKeyExplicit {
		t.Fatalf("explanation mismatch: %+v", explainResp.Data)
	}
	if len(explainResp.Data.Candidates) == 0 || !explainResp.Data.Candidates[0].Selected {
		t.Fatalf("explanation candidates mismatch: %+v", explainResp.Data.Candidates)
	}
}

func TestUpdateProviderEndpointKeepsExistingSecret(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	createBody := bytes.NewBufferString(`{"name":"Vendor A","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini"],"priority":10,"api_key":"sk-test-123456"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	updateBody := bytes.NewBufferString(`{"name":"Vendor A Updated","type":"openai_compatible","base_url":"https://example.com/v1","status":"active","models":["gpt-4o-mini","gpt-4.1-mini"],"priority":20,"api_key":""}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/providers/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updateResp.Data.Status != controlplane.ProviderStatusActive || !updateResp.Data.SecretConfigured {
		t.Fatalf("secret/status not preserved: %+v", updateResp.Data)
	}
}

func TestCheckProviderEndpoint(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/prov_openai_compatible/check", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ProviderID != "prov_openai_compatible" || resp.Data.Status == "" || resp.Data.Message == "" {
		t.Fatalf("incomplete check response: %+v", resp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-health-checks", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].ProviderID != "prov_openai_compatible" {
		t.Fatalf("health list missing check: %+v", listResp.Data)
	}
}

func TestAdminRoutingGroupsAndProviderAccountsEndpoints(t *testing.T) {
	handler := newTestHandler(t, RuntimeConfig{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-account"}]}`))
	}))
	defer upstream.Close()

	groupBody := bytes.NewBufferString(`{"name":"OpenAI default","platform":"openai_compatible","rate_multiplier":1,"status":"active","sort_order":10}`)
	groupReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/routing-groups", groupBody)
	groupReq.Header.Set("Content-Type", "application/json")
	groupRec := httptest.NewRecorder()
	handler.ServeHTTP(groupRec, groupReq)
	if groupRec.Code != http.StatusOK {
		t.Fatalf("group status = %d body=%s", groupRec.Code, groupRec.Body.String())
	}
	var groupResp struct {
		Data controlplane.RoutingGroup `json:"data"`
	}
	if err := json.Unmarshal(groupRec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if groupResp.Data.ID == "" {
		t.Fatalf("group id missing: %+v", groupResp.Data)
	}

	providerPayload := `{"name":"Account Provider","type":"openai_compatible","base_url":"` + upstream.URL + `/v1","status":"active","models":["gpt-4o-mini"],"priority":10,"api_key":"provider-secret"}`
	providerReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers", bytes.NewBufferString(providerPayload))
	providerReq.Header.Set("Content-Type", "application/json")
	providerRec := httptest.NewRecorder()
	handler.ServeHTTP(providerRec, providerReq)
	if providerRec.Code != http.StatusOK {
		t.Fatalf("provider status = %d body=%s", providerRec.Code, providerRec.Body.String())
	}
	var providerResp struct {
		Data controlplane.ProviderConnection `json:"data"`
	}
	if err := json.Unmarshal(providerRec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("decode provider: %v", err)
	}

	accountPayload := `{"provider_id":"` + providerResp.Data.ID + `","name":"Account A","platform":"openai_compatible","auth_type":"api_key","status":"active","schedulable":true,"priority":10,"concurrency":3,"rate_multiplier":1,"models":["gpt-4o-mini"],"group_ids":["` + groupResp.Data.ID + `"],"secret":"account-secret"}`
	accountReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts", bytes.NewBufferString(accountPayload))
	accountReq.Header.Set("Content-Type", "application/json")
	accountRec := httptest.NewRecorder()
	handler.ServeHTTP(accountRec, accountReq)
	if accountRec.Code != http.StatusOK {
		t.Fatalf("account status = %d body=%s", accountRec.Code, accountRec.Body.String())
	}
	var accountResp struct {
		Data controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(accountRec.Body.Bytes(), &accountResp); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if !accountResp.Data.SecretConfigured || accountResp.Data.SecretHint == "" {
		t.Fatalf("account secret metadata missing: %+v", accountResp.Data)
	}
	if accountResp.Data.ProviderID != providerResp.Data.ID {
		t.Fatalf("account provider binding missing: %+v", accountResp.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-accounts", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data []controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].GroupIDs[0] != groupResp.Data.ID {
		t.Fatalf("unexpected account list: %+v", listResp.Data)
	}

	checkReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts/"+accountResp.Data.ID+"/check", nil)
	checkRec := httptest.NewRecorder()
	handler.ServeHTTP(checkRec, checkReq)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("account check status = %d body=%s", checkRec.Code, checkRec.Body.String())
	}
	var checkResp struct {
		Data controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(checkRec.Body.Bytes(), &checkResp); err != nil {
		t.Fatalf("decode account check: %v", err)
	}
	if checkResp.Data.Status != "ok" || checkResp.Data.AccountID != accountResp.Data.ID {
		t.Fatalf("unexpected account check: %+v", checkResp.Data)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-account-health-checks", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("account health list status = %d body=%s", healthRec.Code, healthRec.Body.String())
	}
	var healthResp struct {
		Data []controlplane.ProviderAccountHealthCheck `json:"data"`
	}
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("decode account health list: %v", err)
	}
	if len(healthResp.Data) != 1 || healthResp.Data[0].AccountID != accountResp.Data.ID {
		t.Fatalf("account health list missing check: %+v", healthResp.Data)
	}
}

func TestAdminProviderAccountClearCooldownEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name:    "Cooldown provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  "active",
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	account, err := control.CreateProviderAccount(context.Background(), "tester", controlplane.ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Cooldown account",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         controlplane.AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if err := control.RecordProviderAccountFailure(context.Background(), account.ID, http.StatusInternalServerError, "upstream broke"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-accounts/"+account.ID+"/clear-cooldown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data controlplane.ProviderAccount `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CooldownUntil != nil {
		t.Fatalf("expected cooldown cleared: %+v", resp.Data)
	}

	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	var found bool
	for _, event := range audit {
		if event.ResourceType == "provider_account" && event.Action == "clear_cooldown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clear_cooldown audit event not found: %+v", audit)
	}
}

func TestAdminSystemCheckUpdatesEndpoint(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/check-updates?force=true", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int               `json:"code"`
		Data system.UpdateInfo `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.CurrentVersion != "test" || resp.Data.Warning == "" {
		t.Fatalf("unexpected update info: %+v", resp.Data)
	}
	audit, err := control.ListAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	for _, event := range audit {
		if event.ResourceType == "system" && event.Action == "check_update" {
			return
		}
	}
	t.Fatalf("system update audit event not found: %+v", audit)
}

func TestAdminSystemUpdateWithoutManifestRequiresManualConfiguration(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	req.Header.Set("Idempotency-Key", "test-update")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manifest") {
		t.Fatalf("expected manifest guidance: %s", rec.Body.String())
	}
}

func TestSystemBackupEndpointsExposeEmptyListAndRejectMemoryBackup(t *testing.T) {
	handler, _ := newTestRuntime(t, RuntimeConfig{})

	for _, path := range []string{"/api/v1/admin/system/backups", "/api/v1/console/system/backups", "/api/v1/operator/system/backups"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/backups", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "PostgreSQL") {
		t.Fatalf("POST backup status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/backups/restore", bytes.NewBufferString(`{"backup_id":"missing","confirm":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "confirmation") {
		t.Fatalf("POST restore status = %d body=%s", rec.Code, rec.Body.String())
	}
}
