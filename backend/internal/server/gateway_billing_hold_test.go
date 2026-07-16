package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestGatewayBillingHoldLifecycle(t *testing.T) {
	t.Run("authoritative usage settles actual cost", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"billing-success","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":5}}`))
		}))
		defer upstream.Close()
		handler, control, key := setupGatewayBillingHoldRuntime(t, upstream.URL)

		recorder := invokeGatewayBillingHoldRequest(t, handler, key)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		hold := gatewayBillingHoldFromResponse(t, control, recorder)
		if hold.Status != controlplane.BillingHoldStatusSettled || hold.ReservedAmountCents != 10 || hold.SettledAmountCents != 5 {
			t.Fatalf("settled hold=%+v", hold)
		}
	})

	t.Run("missing usage remains disputed", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"billing-missing","choices":[]}`))
		}))
		defer upstream.Close()
		handler, control, key := setupGatewayBillingHoldRuntime(t, upstream.URL)

		recorder := invokeGatewayBillingHoldRequest(t, handler, key)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		hold := gatewayBillingHoldFromResponse(t, control, recorder)
		if hold.Status != controlplane.BillingHoldStatusDisputed || hold.SettledAt != nil {
			t.Fatalf("missing usage hold=%+v", hold)
		}
	})

	t.Run("provider capacity rejection releases reservation", func(t *testing.T) {
		var upstreamCalls atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			upstreamCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer upstream.Close()
		handler, control, key := setupGatewayBillingHoldRuntime(t, upstream.URL)
		candidates, _, err := control.GatewayProviderCandidatesForModel(context.Background(), "billing-model")
		if err != nil || len(candidates) != 1 {
			t.Fatalf("candidates=%+v err=%v", candidates, err)
		}
		permit, _, acquired, err := control.TryAcquireProviderAccountPermitContext(context.Background(), candidates[0], 20, "billing-capacity-test")
		if err != nil || !acquired {
			t.Fatalf("capacity acquired=%t err=%v", acquired, err)
		}
		defer permit.Release()

		recorder := invokeGatewayBillingHoldRequest(t, handler, key)
		if recorder.Code != http.StatusBadGateway || upstreamCalls.Load() != 0 {
			t.Fatalf("status=%d calls=%d body=%s", recorder.Code, upstreamCalls.Load(), recorder.Body.String())
		}
		hold := gatewayBillingHoldFromResponse(t, control, recorder)
		if hold.Status != controlplane.BillingHoldStatusReleased || hold.ReleasedAt == nil {
			t.Fatalf("capacity-rejected hold=%+v", hold)
		}
	})

	t.Run("transport result remains disputed", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		handler, control, key := setupGatewayBillingHoldRuntime(t, upstream.URL)
		upstream.Close()

		recorder := invokeGatewayBillingHoldRequest(t, handler, key)
		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		hold := gatewayBillingHoldFromResponse(t, control, recorder)
		if hold.Status != controlplane.BillingHoldStatusDisputed || hold.SettledAt != nil || hold.ReleasedAt != nil {
			t.Fatalf("transport-unknown hold=%+v", hold)
		}
	})
}

func setupGatewayBillingHoldRuntime(t *testing.T, upstreamURL string) (http.Handler, *controlplane.Service, string) {
	t.Helper()
	handler, control := newTestRuntime(t, RuntimeConfig{})
	provider, err := control.CreateProvider(context.Background(), "tester", controlplane.ProviderRequest{
		Name: "Billing provider", Type: "openai_compatible", BaseURL: upstreamURL + "/v1",
		Status: controlplane.ProviderStatusActive, Models: []string{"billing-upstream"}, APIKey: "billing-provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createGatewayTestAccount(t, control, provider, "billing-upstream", "billing-account-secret", 10, 1)
	createGatewayTestModelAndRoutes(t, control, "billing-model", "default", []gatewayTestRoute{{account: account, upstreamModel: "billing-upstream", priority: 10}})
	if _, err := control.CreateModelPricing(context.Background(), "tester", controlplane.ModelPricingRequest{
		Model: "billing-model", Currency: "USD", OutputPriceCentsPer1MTokens: 1_000_000, Status: controlplane.ModelPricingStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := control.CreateAPIKey(context.Background(), "tester", controlplane.APIKeyCreateRequest{
		Name: "Billing key", ModelAllowlist: []string{"billing-model"}, MonthlyBudgetCents: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, control, created.Key
}

func invokeGatewayBillingHoldRequest(t *testing.T, handler http.Handler, key string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"billing-model","max_tokens":10,"messages":[{"role":"user","content":"billing"}]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func gatewayBillingHoldFromResponse(t *testing.T, control *controlplane.Service, recorder *httptest.ResponseRecorder) controlplane.BillingHold {
	t.Helper()
	operationID := recorder.Header().Get("X-AsterRouter-Operation-ID")
	if operationID == "" {
		t.Fatal("missing operation id response header")
	}
	hold, found, err := control.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found {
		t.Fatalf("billing hold found=%t err=%v", found, err)
	}
	return hold
}
