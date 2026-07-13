package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunChannelMonitorCycleChecksOnlyActiveChannels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"enterprise-chat"}]}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	repo := NewMemoryRepository()
	service := NewService(repo, "/v1", "test-secret")
	active, err := service.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Active", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, APIKey: "secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(active): %v", err)
	}
	_, err = service.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Disabled", Type: "openai_compatible", BaseURL: "http://127.0.0.1:1/v1",
		Status: ProviderStatusDisabled, APIKey: "secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(disabled): %v", err)
	}

	var reported []error
	service.runChannelMonitorCycle(ctx, func(_ string, err error) { reported = append(reported, err) })
	if len(reported) != 0 {
		t.Fatalf("runChannelMonitorCycle() reported errors: %v", reported)
	}
	checks, err := service.ListProviderHealthChecks(ctx)
	if err != nil {
		t.Fatalf("ListProviderHealthChecks(): %v", err)
	}
	if len(checks) != 1 || checks[0].ProviderID != active.ID || checks[0].Status != "ok" {
		t.Fatalf("unexpected checks: %#v", checks)
	}
}
