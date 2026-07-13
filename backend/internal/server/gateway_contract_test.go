package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
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
	}{
		{name: "json", mode: testutil.OpenAINormal, wantAccept: "application/json", wantContent: `"id":"completion-1"`},
		{name: "stream", mode: testutil.OpenAIStream, stream: true, wantAccept: "text/event-stream", wantContent: "data: [DONE]"},
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
			if !test.stream && (usage.Recent[0].InputTokens != 7 || usage.Recent[0].OutputTokens != 11) {
				t.Fatalf("JSON usage tokens = %#v", usage.Recent[0])
			}
		})
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

func gatewayContractRuntime(t *testing.T, upstream *testutil.FakeOpenAI) (http.Handler, *controlplane.Service, string) {
	t.Helper()
	handler, control := newTestRuntime(t, config.Config{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "contract provider", Type: "openai_compatible", BaseURL: upstream.BaseURL(),
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account := createGatewayTestAccount(t, control, provider, "upstream-model", "upstream-secret", 10, 2)
	createGatewayTestModelAndRoutes(t, control, "public-model", "default", []gatewayTestRoute{{account: account, upstreamModel: "upstream-model", priority: 10}})
	key, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "contract key", ModelAllowlist: []string{"public-model"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	return handler, control, key.Key
}
