package controlplane

import (
	"context"
	"errors"
	"testing"
)

func TestAPIKeyQuotaAlertEscalatesAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:              "Quota Key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		MonthlyTokenLimit: 100,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthenticateGatewayKey(ctx, created.Key)
	if err != nil {
		t.Fatalf("AuthenticateGatewayKey(): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-4o-mini", Status: "accepted", InputTokens: 80}); err != nil {
		t.Fatalf("RecordGatewayUsage(warning): %v", err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyQuota, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != AlertSeverityWarning || alerts[0].ResourceID != created.Record.ID {
		t.Fatalf("unexpected warning alert: %+v", alerts)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-4o-mini", Status: "accepted", InputTokens: 20}); err != nil {
		t.Fatalf("RecordGatewayUsage(critical): %v", err)
	}
	alerts, err = svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyQuota, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != AlertSeverityCritical {
		t.Fatalf("quota alert was not escalated in place: %+v", alerts)
	}
}

func TestAPIKeyBudgetAlertEscalatesAndDeduplicatesForEffectivePolicy(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "Budget Key", ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := svc.AuthenticateGatewayKey(ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	auth.Policy = &GovernancePolicy{ID: "policy", MonthlyBudgetCents: 100, OverageAction: GovernancePolicyOverageBlock, Status: GovernancePolicyStatusActive}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", CostCents: 80}); err != nil {
		t.Fatal(err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyBudget, Status: AlertStatusActive})
	if err != nil || len(alerts) != 1 || alerts[0].Severity != AlertSeverityWarning {
		t.Fatalf("budget warning=%+v err=%v", alerts, err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", CostCents: 20}); err != nil {
		t.Fatal(err)
	}
	alerts, err = svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyBudget, Status: AlertStatusActive})
	if err != nil || len(alerts) != 1 || alerts[0].Severity != AlertSeverityCritical {
		t.Fatalf("budget critical=%+v err=%v", alerts, err)
	}
	if err := svc.EnforceGatewayPolicy(ctx, auth); !errors.Is(err, ErrGatewayBudgetExceeded) {
		t.Fatalf("budget enforcement err=%v", err)
	}
}

func TestGatewayErrorRateAlertUsesWorkspaceKeyBoundary(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:           "Observed Key",
		ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthenticateGatewayKey(ctx, created.Key)
	if err != nil {
		t.Fatalf("AuthenticateGatewayKey(): %v", err)
	}
	for i := 0; i < gatewayErrorRateMinRequests; i++ {
		if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "gpt-4o-mini", Status: "upstream_error", ErrorType: "upstream_5xx"}); err != nil {
			t.Fatalf("RecordGatewayUsage(%d): %v", i, err)
		}
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeGatewayErrorRate, Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlertEventsQuery(): %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != AlertSeverityCritical || alerts[0].ResourceType != "api_key" || alerts[0].ResourceID != created.Record.ID {
		t.Fatalf("unexpected gateway error-rate alert: %+v", alerts)
	}
}

func TestAlertAcknowledgeAndResolveLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	if err := svc.upsertAlert(ctx, alertInput{
		Type:         AlertTypeProviderHealth,
		Severity:     AlertSeverityWarning,
		Title:        "Provider latency",
		Summary:      "Latency exceeded threshold",
		ResourceType: "provider",
		ResourceID:   "prov_test",
		DedupeKey:    "provider_health:prov_test",
	}); err != nil {
		t.Fatalf("upsertAlert(): %v", err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Status: AlertStatusActive})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("active alerts: %+v err=%v", alerts, err)
	}
	acknowledged, err := svc.AcknowledgeAlert(ctx, "auditor", alerts[0].ID)
	if err != nil || acknowledged.Status != AlertStatusAcknowledged {
		t.Fatalf("AcknowledgeAlert(): %+v err=%v", acknowledged, err)
	}
	resolved, err := svc.ResolveAlert(ctx, "operator", alerts[0].ID)
	if err != nil || resolved.Status != AlertStatusResolved || resolved.ResolvedBy != "operator" {
		t.Fatalf("ResolveAlert(): %+v err=%v", resolved, err)
	}
}
