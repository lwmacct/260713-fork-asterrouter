package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestGatewayOpenAIContractForJSONAndStreaming(t *testing.T) {
	for _, test := range []struct {
		name        string
		mode        testutil.OpenAIMode
		stream      bool
		wantAccept  string
		wantContent string
		wantCached  int
	}{
		{name: "json", mode: testutil.OpenAINormal, wantAccept: "application/json", wantContent: `"id":"completion-1"`, wantCached: 3},
		{name: "stream", mode: testutil.OpenAIStream, stream: true, wantAccept: "text/event-stream", wantContent: "data: [DONE]", wantCached: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := testutil.NewFakeOpenAI(t)
			upstream.SetMode(test.mode)
			handler, control, key := gatewayContractRuntime(t, upstream)
			body := `{"model":"public-model","messages":[{"role":"user","content":"synthetic"}]`
			if test.stream {
				body += `,"stream":true`
			}
			body += `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+key)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), test.wantContent) {
				t.Fatalf("response status=%d body=%s", rec.Code, rec.Body.String())
			}
			operationID := rec.Header().Get("X-AsterRouter-Operation-ID")
			if operationID == "" {
				t.Fatal("response did not expose the AsterRouter operation id")
			}
			requests := upstream.Requests()
			if len(requests) != 1 || requests[0].Model != "upstream-model" || requests[0].Accept != test.wantAccept || requests[0].Authorization != "Bearer upstream-secret" {
				t.Fatalf("captured upstream requests = %#v", requests)
			}
			usage, err := control.UsageReport(context.Background(), 10)
			if err != nil {
				t.Fatalf("UsageReport(): %v", err)
			}
			if len(usage.Recent) != 1 || usage.Recent[0].Status != "forwarded" {
				t.Fatalf("usage records = %#v", usage.Recent)
			}
			if usage.Recent[0].OperationID != operationID || usage.Recent[0].AttemptID == "" || usage.Recent[0].UsageVersion != 1 || usage.Recent[0].RequestFingerprint == "" {
				t.Fatalf("usage ledger identity = %#v", usage.Recent[0])
			}
			if usage.Recent[0].InputTokens != 7 || usage.Recent[0].OutputTokens != 11 || usage.Recent[0].CacheReadTokens == nil || *usage.Recent[0].CacheReadTokens != test.wantCached {
				t.Fatalf("normalized usage tokens = %#v", usage.Recent[0])
			}
			if usage.Recent[0].Protocol != "openai_chat_completions" || usage.Recent[0].TTFTMS == nil || !usage.Recent[0].CacheFieldsPresent || usage.Recent[0].UsageNormalizationStatus != usageNormalizationOpenAI || usage.Recent[0].UpstreamRequestID != "req-fake-openai-1" {
				t.Fatalf("normalized usage evidence = %#v", usage.Recent[0])
			}
			operation, found, err := control.AIOperation(context.Background(), operationID)
			if err != nil || !found || operation.Status != controlplane.AIOperationStatusSucceeded {
				t.Fatalf("operation=%+v found=%t err=%v", operation, found, err)
			}
			billing, err := control.BillingLedgerEntries(context.Background(), operationID)
			if err != nil || len(billing) != 1 || billing[0].UsageRecordID != usage.Recent[0].ID {
				t.Fatalf("billing=%+v err=%v", billing, err)
			}
			outbox, err := control.TransactionalOutboxEvents(context.Background(), "")
			if err != nil || len(outbox) != 1 || outbox[0].EventType != controlplane.OutboxEventUsage {
				t.Fatalf("outbox=%+v err=%v", outbox, err)
			}
			traces, err := control.ListGatewayTraces(context.Background(), 10)
			if err != nil || len(traces) != 1 || traces[0].OperationID != operationID || traces[0].AttemptID != usage.Recent[0].AttemptID || traces[0].RequestFingerprint != usage.Recent[0].RequestFingerprint {
				t.Fatalf("traces=%+v err=%v", traces, err)
			}
		})
	}
}

func TestGatewayIdempotencyPreventsDuplicateDirectExecution(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, control, key := gatewayContractRuntime(t, upstream)
	body := `{"model":"public-model","messages":[{"role":"user","content":"synthetic"}]}`
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Idempotency-Key", "idem-direct-1")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	first := request(body)
	if first.Code != http.StatusOK || first.Header().Get("X-AsterRouter-Operation-ID") == "" {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replay := request(body)
	if replay.Code != http.StatusConflict || !strings.Contains(replay.Body.String(), "idempotency_replay_unavailable") {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	conflict := request(`{"model":"public-model","messages":[{"role":"user","content":"different"}]}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	if requests := upstream.Requests(); len(requests) != 1 {
		t.Fatalf("upstream requests=%#v", requests)
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 1 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}

func TestGatewayOpenAIContractPassesThroughFinalUpstreamError(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	upstream.SetHTTPError(http.StatusTooManyRequests)
	handler, control, key := gatewayContractRuntime(t, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-model","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "synthetic failure") {
		t.Fatalf("response status=%d body=%s", rec.Code, rec.Body.String())
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 || traces[0].Status != "upstream_error" || traces[0].HTTPStatus != http.StatusTooManyRequests || traces[0].ErrorType != "upstream_status" {
		t.Fatalf("gateway traces = %#v", traces)
	}
}

func TestGatewayOpenAIContractRejectsInvalidPayloadBeforeUpstream(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, _, key := gatewayContractRuntime(t, upstream)
	for _, body := range []string{`{"messages":[]}`, `{"model":`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request_error") {
			t.Fatalf("body=%q status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
	if requests := upstream.Requests(); len(requests) != 0 {
		t.Fatalf("invalid payload reached upstream: %#v", requests)
	}
}

func TestGatewayProtocolEdgeRejectsQueryAndConflictingCredentials(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, _, key := gatewayContractRuntime(t, upstream)
	tests := []struct {
		name    string
		target  string
		headers http.Header
	}{
		{name: "query credential", target: "/v1/chat/completions?api_key=" + key},
		{name: "conflicting transports", target: "/v1/chat/completions", headers: http.Header{"Authorization": []string{"Bearer " + key}, "X-Api-Key": []string{key}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.target, bytes.NewBufferString(`{"model":"public-model","messages":[]}`))
			for key, values := range test.headers {
				req.Header[key] = append([]string(nil), values...)
			}
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid_api_key") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), key) {
				t.Fatalf("gateway response exposed credential: %s", rec.Body.String())
			}
		})
	}
	if requests := upstream.Requests(); len(requests) != 0 {
		t.Fatalf("rejected credentials reached upstream: %#v", requests)
	}
}

func TestGatewayCanonicalPlannerRejectsChatOnImageOnlyModel(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "image provider", Type: "openai_compatible", BaseURL: upstream.BaseURL(), Status: controlplane.ProviderStatusActive,
		Models: []string{"upstream-image"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-image", "upstream-secret", 10, 2)
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID: "public-image", Name: "Image", Modality: "image", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "tester", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: controlplane.DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "upstream-image", Priority: 1, Weight: 100, Status: controlplane.ModelRouteStatusActive,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "image key", ModelAllowlist: []string{"public-image"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-image","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "route_unavailable") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if requests := upstream.Requests(); len(requests) != 0 {
		t.Fatalf("capability mismatch reached upstream: %#v", requests)
	}
}

func TestGatewayTraceIncludesPlannerExclusionEvidence(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "excluded provider", Type: "openai_compatible", BaseURL: upstream.BaseURL(), Status: controlplane.ProviderStatusActive,
		Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "upstream-secret", 10, 2)
	model, err := control.CreateGatewayModel(context.Background(), "tester", controlplane.GatewayModelRequest{
		ModelID: "excluded-model", Name: "Excluded", Modality: "chat", Status: controlplane.GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := control.CreateModelRoute(context.Background(), "tester", controlplane.ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: controlplane.DefaultModelRouteGroup, ProviderAccountID: account.ID,
		UpstreamModel: "upstream-model", Priority: 1, Weight: 100, Status: controlplane.ModelRouteStatusDisabled,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{Name: "excluded key", ModelAllowlist: []string{"excluded-model"}})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"excluded-model","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+created.Key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	traces, err := control.ListGatewayTraces(context.Background(), 10)
	if err != nil || len(traces) != 1 || !strings.Contains(traces[0].RouteAttempts, `"outcome":"excluded"`) || !strings.Contains(traces[0].RouteAttempts, `"detail":"route_disabled"`) {
		t.Fatalf("traces=%+v err=%v", traces, err)
	}
	if requests := upstream.Requests(); len(requests) != 0 {
		t.Fatalf("excluded route reached upstream: %#v", requests)
	}
}

func TestGatewayCanonicalPolicySeparatesModelsAndInvokeScopes(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, _, key := gatewayContractRuntimeWithKeyRequest(t, upstream, controlplane.APIKeyCreateRequest{
		Scopes: []string{controlplane.GatewayScopeInvoke},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "policy_not_allowed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayCanonicalPolicyRejectsDisallowedNetworkBeforeUpstream(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, _, key := gatewayContractRuntimeWithKeyRequest(t, upstream, controlplane.APIKeyCreateRequest{
		AllowedCIDRs: []string{"192.0.2.0/24"},
	})
	body := `{"model":"public-model","messages":[]}`

	denied := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	denied.RemoteAddr = "203.0.113.10:43100"
	denied.Header.Set("Authorization", "Bearer "+key)
	denied.Header.Set("X-Forwarded-For", "192.0.2.10")
	deniedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden || !strings.Contains(deniedRecorder.Body.String(), "policy_not_allowed") {
		t.Fatalf("denied status=%d body=%s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
	if requests := upstream.Requests(); len(requests) != 0 {
		t.Fatalf("network-rejected request reached upstream: %#v", requests)
	}

	allowed := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	allowed.RemoteAddr = "192.0.2.10:43100"
	allowed.Header.Set("Authorization", "Bearer "+key)
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("allowed status=%d body=%s", allowedRecorder.Code, allowedRecorder.Body.String())
	}
	if requests := upstream.Requests(); len(requests) != 1 {
		t.Fatalf("allowed upstream requests = %#v", requests)
	}
}

func TestGatewayCredentialRPMLimitRejectsBeforeUpstream(t *testing.T) {
	upstream := testutil.NewFakeOpenAI(t)
	handler, control, key := gatewayContractRuntimeWithKeyRequest(t, upstream, controlplane.APIKeyCreateRequest{RPMLimit: 1})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-model","messages":[]}`))
		req.Header.Set("Authorization", "Bearer "+key)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	if first := request(); first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	if second := request(); second.Code != http.StatusTooManyRequests || !strings.Contains(second.Body.String(), "capacity_limit_exceeded") {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	if requests := upstream.Requests(); len(requests) != 1 {
		t.Fatalf("upstream requests=%#v", requests)
	}
	usage, err := control.UsageReport(context.Background(), 10)
	if err != nil || len(usage.Recent) != 2 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}

func gatewayContractRuntime(t *testing.T, upstream *testutil.FakeOpenAI) (http.Handler, *controlplane.Service, string) {
	return gatewayContractRuntimeWithKeyRequest(t, upstream, controlplane.APIKeyCreateRequest{})
}

func gatewayContractRuntimeWithKeyRequest(t *testing.T, upstream *testutil.FakeOpenAI, keyRequest controlplane.APIKeyCreateRequest) (http.Handler, *controlplane.Service, string) {
	t.Helper()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "contract provider", Type: "openai_compatible", BaseURL: upstream.BaseURL(),
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "upstream-secret", 10, 2)
	createGatewayTestModelAndRoutes(t, control, "public-model", "default", []gatewayTestRoute{{account: account, upstreamModel: "upstream-model", priority: 10}})
	keyRequest.Name = "contract key"
	keyRequest.ModelAllowlist = []string{"public-model"}
	key, err := control.CreateAPIKey(context.Background(), "tester", keyRequest)
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	return handler, control, key.Key
}
