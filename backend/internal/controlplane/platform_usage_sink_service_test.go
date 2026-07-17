package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlatformUsageSinkEnqueuesAndDeliversSignedUsageWithoutSensitiveContent(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "usage-sink-test-secret")
	now := time.Date(2026, time.July, 14, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	identity := createExternalAuthIdentity(t, ctx, svc)
	integration, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Usage product", KeyID: "usage-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 10, MonthlyTokenLimit: 1000, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	var receivedBody string
	var eventID, timestamp, signature string
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		eventID, timestamp, signature = r.Header.Get("X-Aster-Event-ID"), r.Header.Get("X-Aster-Timestamp"), r.Header.Get("X-Aster-Signature")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer endpoint.Close()
	svc.platformUsageHTTPClient = endpoint.Client()
	created, err := svc.CreatePlatformUsageSink(ctx, "operator", PlatformUsageSinkRequest{TenantID: identity.tenant.ID, ExternalAuthIntegrationID: integration.Record.ID, Name: "Billing callback", EndpointURL: endpoint.URL, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if created.SigningSecret == "" || created.Record.EndpointURLCiphertext != "" || created.Record.SigningSecretCiphertext != "" {
		t.Fatalf("CreatePlatformUsageSink() response=%+v", created)
	}
	auth := GatewayAuthContext{APIKey: APIKeyRecord{ID: "synthetic-subject", Fingerprint: "subject-fp", ProfileScope: ProfileScopePlatform, PlatformTenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID}, PlatformTenant: &identity.tenant, GatewayPrincipal: &identity.principal, ExternalAuthIntegration: &integration.Record, ExternalSubjectReference: "opaque-subject"}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 12, OutputTokens: 8, UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 2, Unit: UsageUnitCount, Source: "core_artifact", Confidence: UsageConfidenceObserved}}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeliverDuePlatformUsage(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if eventID == "" || timestamp == "" || !strings.HasPrefix(signature, "sha256=") {
		t.Fatalf("delivery headers event_id=%q timestamp=%q signature=%q", eventID, timestamp, signature)
	}
	mac := hmac.New(sha256.New, []byte(created.SigningSecret))
	_, _ = mac.Write([]byte(timestamp + "." + receivedBody))
	if signature != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("delivery signature=%q", signature)
	}
	if strings.Contains(receivedBody, integration.Secret) || strings.Contains(receivedBody, "prompt") || strings.Contains(receivedBody, "provider") {
		t.Fatalf("delivery leaked sensitive content: %s", receivedBody)
	}
	var received platformUsageEventPayload
	if err := json.Unmarshal([]byte(receivedBody), &received); err != nil || received.UsageDimensions[UsageDimensionOutputImages].Quantity != 2 {
		t.Fatalf("received usage dimensions=%+v err=%v body=%s", received.UsageDimensions, err, receivedBody)
	}
	events, err := svc.ListPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: created.Record.ID})
	if err != nil || len(events) != 1 || events[0].Status != PlatformUsageDeliveryStatusDelivered || events[0].DeliveredAt == nil {
		t.Fatalf("delivered events=%+v err=%v", events, err)
	}
}

func TestPlatformUsageSinkPreservesProviderTerminalFailureDimensions(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "usage-sink-terminal-secret")
	svc.now = func() time.Time { return time.Date(2026, time.July, 14, 14, 0, 0, 0, time.UTC) }
	identity := createExternalAuthIdentity(t, ctx, svc)
	integration, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Terminal usage product",
		KeyID: "terminal-usage-v1", Audience: "https://gateway.example/v1", ModelAllowlist: []string{"model-a"},
		QPSLimit: 10, MonthlyTokenLimit: 1000, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := svc.CreatePlatformUsageSink(ctx, "operator", PlatformUsageSinkRequest{
		TenantID: identity.tenant.ID, ExternalAuthIntegrationID: integration.Record.ID,
		Name: "Terminal billing callback", EndpointURL: "https://billing.example/terminal", MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := GatewayAuthContext{
		APIKey:         APIKeyRecord{ID: "terminal-subject", Fingerprint: "terminal-fp", ProfileScope: ProfileScopePlatform, PlatformTenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID},
		PlatformTenant: &identity.tenant, GatewayPrincipal: &identity.principal,
		ExternalAuthIntegration: &integration.Record, ExternalSubjectReference: "opaque-terminal-subject",
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{
		Model: "model-a", UsageSource: "provider_final", Status: "upstream_error", ErrorType: "provider_failed",
		UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 2, Unit: UsageUnitCount, Source: "adapter", Confidence: UsageConfidenceReported}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := svc.ListPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: sink.Record.ID})
	if err != nil || len(events) != 1 {
		t.Fatalf("terminal usage events=%+v err=%v", events, err)
	}
	var payload platformUsageEventPayload
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &payload); err != nil || payload.Status != "upstream_error" || payload.UsageDimensions[UsageDimensionOutputImages].Quantity != 2 {
		t.Fatalf("terminal usage payload=%+v err=%v raw=%s", payload, err, events[0].PayloadJSON)
	}
}

func TestPlatformUsageSinkLifecycleAndDurableEvents(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "usage-sink-test-secret")
	now := time.Date(2026, time.July, 14, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	identity := createExternalAuthIdentity(t, ctx, svc)
	integration, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Usage product", KeyID: "usage-v1", Audience: "https://gateway.example/v1",
		ModelAllowlist: []string{"model-a"}, QPSLimit: 10, MonthlyTokenLimit: 1000, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := svc.CreatePlatformUsageSink(ctx, "operator", PlatformUsageSinkRequest{
		TenantID: identity.tenant.ID, ExternalAuthIntegrationID: integration.Record.ID, Name: "Billing callback", EndpointURL: "https://billing.example/usage", MaxAttempts: 2,
	})
	if err != nil || sink.SigningSecret == "" || sink.Record.EndpointURLCiphertext != "" || sink.Record.SigningSecretCiphertext != "" || sink.Record.EndpointURLHint == "" {
		t.Fatalf("CreatePlatformUsageSink() response=%+v err=%v", sink, err)
	}
	listed, err := svc.ListPlatformUsageSinks(ctx)
	if err != nil || len(listed) != 1 || listed[0].SigningSecretCiphertext != "" || listed[0].EndpointURLCiphertext != "" {
		t.Fatalf("ListPlatformUsageSinks() sinks=%+v err=%v", listed, err)
	}

	auth := GatewayAuthContext{
		APIKey:         APIKeyRecord{ID: "synthetic-subject", Fingerprint: "subject-fp", ProfileScope: ProfileScopePlatform, PlatformTenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, QPSLimit: 1, MonthlyTokenLimit: 1},
		PlatformTenant: &identity.tenant, GatewayPrincipal: &identity.principal, ExternalAuthIntegration: &integration.Record, ExternalSubjectReference: "opaque-subject",
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 12, OutputTokens: 8, UsageDimensions: UsageDimensions{UsageDimensionOutputAudioMilliseconds: {Quantity: 1500, Unit: UsageUnitMillisecond, Source: "provider", Confidence: UsageConfidenceReported}}}); err != nil {
		t.Fatal(err)
	}
	events, err := svc.ListPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: sink.Record.ID})
	if err != nil || len(events) != 1 || events[0].Status != PlatformUsageDeliveryStatusPending || events[0].AttemptCount != 0 || !strings.HasPrefix(events[0].EventID, "usage_evt_") {
		t.Fatalf("usage events=%+v err=%v", events, err)
	}
	var payload platformUsageEventPayload
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &payload); err != nil || payload.ExternalSubjectRef != "opaque-subject" || payload.TenantID != identity.tenant.ID || payload.IntegrationID != integration.Record.ID || payload.InputTokens != 12 || payload.OutputTokens != 8 || payload.UsageDimensions[UsageDimensionOutputAudioMilliseconds].Quantity != 1500 || strings.Contains(events[0].PayloadJSON, integration.Secret) {
		t.Fatalf("usage payload=%s parsed=%+v err=%v", events[0].PayloadJSON, payload, err)
	}

	claimed, err := svc.repo.ClaimDuePlatformUsageDeliveryEvents(ctx, now, now.Add(time.Second), "lease-1", 10)
	if err != nil || len(claimed) != 1 || claimed[0].AttemptCount != 1 || claimed[0].Status != PlatformUsageDeliveryStatusDelivering {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	if err := svc.repo.ReschedulePlatformUsageDeliveryEvent(ctx, claimed[0].ID, "lease-1", now, http.StatusBadGateway, "upstream unavailable", false, now); err != nil {
		t.Fatal(err)
	}
	claimed, err = svc.repo.ClaimDuePlatformUsageDeliveryEvents(ctx, now, now.Add(time.Second), "lease-2", 10)
	if err != nil || len(claimed) != 1 || claimed[0].AttemptCount != 2 {
		t.Fatalf("second claim=%+v err=%v", claimed, err)
	}
	if err := svc.repo.ReschedulePlatformUsageDeliveryEvent(ctx, claimed[0].ID, "lease-2", now, http.StatusBadGateway, "still unavailable", true, now); err != nil {
		t.Fatal(err)
	}
	events, err = svc.ListPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: sink.Record.ID, Status: PlatformUsageDeliveryStatusDeadLetter})
	if err != nil || len(events) != 1 {
		t.Fatalf("dead letter events=%+v err=%v", events, err)
	}
	if err := svc.RequeuePlatformUsageDeliveryEvent(ctx, "operator", "wrong-sink", events[0].ID); err == nil {
		t.Fatal("RequeuePlatformUsageDeliveryEvent() accepted a mismatched sink")
	}
	if err := svc.RequeuePlatformUsageDeliveryEvent(ctx, "operator", sink.Record.ID, events[0].ID); err != nil {
		t.Fatal(err)
	}
}
