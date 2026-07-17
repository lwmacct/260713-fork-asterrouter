package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestMonthlyPolicyBoundaryResetsUsageAndStartsNewAlertPeriod(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	now := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	svc.now = func() time.Time { return now }
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:              "Period Boundary Key",
		ModelAllowlist:    []string{"model"},
		MonthlyTokenLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := svc.AuthenticateGatewayKey(ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", InputTokens: 100}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnforceGatewayPolicy(ctx, auth); !errors.Is(err, ErrGatewayQuotaExceeded) {
		t.Fatalf("January quota enforcement err=%v", err)
	}

	now = time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	if err := svc.EnforceGatewayPolicy(ctx, auth); err != nil {
		t.Fatalf("new monthly period should reset quota enforcement: %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", InputTokens: 80}); err != nil {
		t.Fatal(err)
	}
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyQuota})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 {
		t.Fatalf("monthly alerts=%+v", alerts)
	}
	seen := map[string]string{}
	for _, alert := range alerts {
		seen[alert.Metadata["quota_month"]] = alert.Severity
	}
	if seen["2026-01"] != AlertSeverityCritical || seen["2026-02"] != AlertSeverityWarning {
		t.Fatalf("monthly alert periods=%+v", seen)
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
	auth.Policy = &GovernancePolicy{ID: "policy", MonthlyBudgetMicros: 100, OverageAction: GovernancePolicyOverageBlock, Status: GovernancePolicyStatusActive}
	recordTestPricedGatewayUsage(t, svc, auth, GatewayUsageInput{Model: "model", Status: "forwarded"}, 80)
	alerts, err := svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyBudget, Status: AlertStatusActive})
	if err != nil || len(alerts) != 1 || alerts[0].Severity != AlertSeverityWarning {
		t.Fatalf("budget warning=%+v err=%v", alerts, err)
	}
	recordTestPricedGatewayUsage(t, svc, auth, GatewayUsageInput{Model: "model", Status: "forwarded"}, 20)
	alerts, err = svc.ListAlertEventsQuery(ctx, AlertQuery{Type: AlertTypeAPIKeyBudget, Status: AlertStatusActive})
	if err != nil || len(alerts) != 1 || alerts[0].Severity != AlertSeverityCritical {
		t.Fatalf("budget critical=%+v err=%v", alerts, err)
	}
	if err := svc.EnforceGatewayOngoingPolicy(ctx, auth); !errors.Is(err, ErrGatewayBudgetExceeded) {
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
