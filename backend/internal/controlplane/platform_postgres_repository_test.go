package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryPersistsPlatformDomainAndEvidenceSnapshots(t *testing.T) {
	ctx := context.Background()
	schema := testutil.NewPostgresSchema(t)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	now := time.Date(2026, time.July, 14, 3, 30, 0, 0, time.UTC)
	tenant := PlatformTenant{ID: "ptn-postgres", Name: "Postgres Platform", Slug: "postgres-platform", EntitlementReference: "external-plan-1", Status: PlatformTenantStatusActive, CreatedAt: now, UpdatedAt: now}
	principal := GatewayPrincipal{ID: "gpr-postgres", TenantID: tenant.ID, Name: "Postgres Service", PrincipalType: GatewayPrincipalTypeService, ExternalSubjectReference: "svc-1", Status: GatewayPrincipalStatusActive, CreatedAt: now, UpdatedAt: now}
	integration := ExternalAuthIntegration{ID: "eai-postgres", TenantID: tenant.ID, GatewayPrincipalID: principal.ID, Name: "Postgres Integration", Protocol: ExternalAuthIntegrationProtocolJWT, KeyID: "postgres-v1", Issuer: "https://identity.example", JWKSURL: "https://identity.example/.well-known/jwks.json", SubjectClaim: "subject_ref", ModelsClaim: "models", QPSLimitClaim: "qps", MonthlyTokenClaim: "monthly_tokens", Audience: "https://gateway.example/v1", ModelAllowlist: []string{"model"}, QPSLimit: 2, MonthlyTokenLimit: 100, MaxTTLSeconds: 300, Status: ExternalAuthIntegrationStatusActive, CreatedAt: now, UpdatedAt: now}
	key := APIKeyRecord{ID: "key-platform-postgres", Name: "Platform Key", KeyHash: "platform-hash", Fingerprint: "platform-fingerprint", Prefix: "ar_platform", Status: APIKeyStatusActive, KeyType: APIKeyTypeService, ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID, ModelAllowlist: []string{"model"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.SavePlatformTenant(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveGatewayPrincipal(ctx, principal); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveExternalAuthIntegration(ctx, integration); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAPIKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	sink := PlatformUsageSink{ID: "pus-postgres", TenantID: tenant.ID, ExternalAuthIntegrationID: integration.ID, Name: "Postgres delivery", EndpointURLCiphertext: "endpoint-ciphertext", EndpointURLHint: "https://b.../events", SigningSecretCiphertext: "secret-ciphertext", SigningSecretHint: "aus_...test", Status: PlatformUsageSinkStatusActive, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now}
	if err := repo.SavePlatformUsageSink(ctx, sink); err != nil {
		t.Fatal(err)
	}
	deliveryUsage := UsageRecord{ID: "usage-delivery-postgres", APIKeyID: key.ID, APIFingerprint: key.Fingerprint, ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, PlatformTenantName: tenant.Name, GatewayPrincipalID: principal.ID, GatewayPrincipalName: principal.Name, ExternalAuthIntegrationID: integration.ID, ExternalSubjectReference: "opaque-delivery-subject", Model: "model", Status: "forwarded", InputTokens: 3, OutputTokens: 2, UsageCostMicros: testMicros(1), UsageCostCurrency: "USD", PricingStatus: "priced", CreatedAt: now}
	deliveryEvent := PlatformUsageDeliveryEvent{ID: "pud-postgres", SinkID: sink.ID, UsageRecordID: deliveryUsage.ID, EventID: "usage_evt_postgres", PayloadJSON: `{"event_id":"usage_evt_postgres"}`, Status: PlatformUsageDeliveryStatusPending, MaxAttempts: sink.MaxAttempts, NextAttemptAt: now, TargetHint: sink.EndpointURLHint, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveUsageRecordAndEnqueuePlatformUsage(ctx, deliveryUsage, []PlatformUsageDeliveryEvent{deliveryEvent}); err != nil {
		t.Fatal(err)
	}
	ttftMS := int64(125)
	totalInputTokens, uncachedInputTokens, cacheReadTokens := 100, 40, 60
	procurementCostMicros := int64(321)
	usage := UsageRecord{ID: "usage-platform-postgres", APIKeyID: key.ID, APIFingerprint: key.Fingerprint, ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, PlatformTenantName: tenant.Name, GatewayPrincipalID: principal.ID, GatewayPrincipalName: principal.Name, ExternalAuthIntegrationID: integration.ID, ExternalSubjectReference: "opaque-subject", Model: "model", UpstreamModel: "upstream-model", Protocol: "openai_chat_completions", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", TTFTMS: &ttftMS, InputTokens: 100, OutputTokens: 20, TotalInputTokens: &totalInputTokens, UncachedInputTokens: &uncachedInputTokens, CacheReadTokens: &cacheReadTokens, CacheFieldsPresent: true, UsageNormalizationStatus: "normalized_openai", UpstreamRequestID: "upstream-request-a", ProcurementCostMicros: &procurementCostMicros, ProcurementCostCurrency: "USD", ProcurementCostSource: "procurement_price", ProcurementCostConfidence: ProcurementCostConfidenceEstimated, ProcurementPriceID: "price-a", CreatedAt: now}
	affinity := RoutingAffinityBinding{ScopeKey: "affinity-postgres", Kind: AffinityBindingAccount, ProviderID: "provider-a", ProviderAccountID: "account-a", RouteID: "route-a", Model: "model", Protocol: "openai_chat_completions", PolicyVersion: 3, CreatedAt: now, LastReusedAt: now, ExpiresAt: now.Add(time.Hour)}
	trace := GatewayTrace{ID: "trace-platform-postgres", APIKeyID: key.ID, APIFingerprint: key.Fingerprint, ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, PlatformTenantName: tenant.Name, GatewayPrincipalID: principal.ID, GatewayPrincipalName: principal.Name, ExternalAuthIntegrationID: integration.ID, ExternalSubjectReference: "opaque-subject", Model: "model", Status: "forwarded", CreatedAt: now}
	audit := AuditLog{ID: "audit-platform-postgres", Actor: "operator", Action: "invoke", ResourceType: "gateway_call", ResourceID: "call", Summary: "platform call", ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, PlatformTenantName: tenant.Name, GatewayPrincipalID: principal.ID, GatewayPrincipalName: principal.Name, ExternalAuthIntegrationID: integration.ID, ExternalSubjectReference: "opaque-subject", CreatedAt: now}
	alert := AlertEvent{ID: "alert-platform-postgres", Type: AlertTypeAPIKeyQuota, Severity: AlertSeverityWarning, Status: AlertStatusActive, Title: "platform alert", Summary: "platform alert", ResourceType: "api_key", ResourceID: key.ID, ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, PlatformTenantName: tenant.Name, GatewayPrincipalID: principal.ID, GatewayPrincipalName: principal.Name, ExternalAuthIntegrationID: integration.ID, ExternalSubjectReference: "opaque-subject", DedupeKey: "platform-postgres", FirstSeenAt: now, LastSeenAt: now}
	if err := repo.SaveUsageRecord(ctx, usage); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRoutingAffinityBinding(ctx, affinity); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveGatewayTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAuditLog(ctx, audit); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAlertEvent(ctx, alert); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	tenants, err := reopened.ListPlatformTenants(ctx)
	if err != nil || len(tenants) != 1 || tenants[0].Slug != tenant.Slug || tenants[0].EntitlementReference != tenant.EntitlementReference {
		t.Fatalf("platform tenants=%+v err=%v", tenants, err)
	}
	principals, err := reopened.ListGatewayPrincipals(ctx)
	if err != nil || len(principals) != 1 || principals[0].TenantID != tenant.ID || principals[0].ExternalSubjectReference != principal.ExternalSubjectReference {
		t.Fatalf("gateway principals=%+v err=%v", principals, err)
	}
	integrations, err := reopened.ListExternalAuthIntegrations(ctx)
	if err != nil || len(integrations) != 1 || integrations[0].ID != integration.ID || integrations[0].GatewayPrincipalID != principal.ID || integrations[0].Protocol != ExternalAuthIntegrationProtocolJWT || integrations[0].Issuer != integration.Issuer || integrations[0].JWKSURL != integration.JWKSURL || integrations[0].SubjectClaim != integration.SubjectClaim || integrations[0].ModelsClaim != integration.ModelsClaim || integrations[0].QPSLimitClaim != integration.QPSLimitClaim || integrations[0].MonthlyTokenClaim != integration.MonthlyTokenClaim {
		t.Fatalf("external auth integrations=%+v err=%v", integrations, err)
	}
	sinks, err := reopened.ListPlatformUsageSinks(ctx)
	if err != nil || len(sinks) != 1 || sinks[0].ID != sink.ID || sinks[0].EndpointURLCiphertext != sink.EndpointURLCiphertext || sinks[0].SigningSecretCiphertext != sink.SigningSecretCiphertext {
		t.Fatalf("platform usage sinks=%+v err=%v", sinks, err)
	}
	deliveries, err := reopened.QueryPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: sink.ID, DeliveryID: deliveryEvent.ID, Limit: 1})
	if err != nil || len(deliveries) != 1 || deliveries[0].EventID != deliveryEvent.EventID || deliveries[0].PayloadJSON != deliveryEvent.PayloadJSON || deliveries[0].Status != PlatformUsageDeliveryStatusPending {
		t.Fatalf("platform usage deliveries=%+v err=%v", deliveries, err)
	}
	foundKey, found, err := reopened.FindAPIKeyByHash(ctx, key.KeyHash)
	if err != nil || !found || foundKey.ProfileScope != ProfileScopePlatform || foundKey.PlatformTenantID != tenant.ID || foundKey.GatewayPrincipalID != principal.ID {
		t.Fatalf("platform key=%+v found=%t err=%v", foundKey, found, err)
	}
	usageRecords, err := reopened.QueryUsageRecords(ctx, UsageQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID, ExternalAuthIntegrationID: integration.ID})
	if err != nil || len(usageRecords) != 2 {
		t.Fatalf("platform usage=%+v err=%v", usageRecords, err)
	}
	var persistedUsage UsageRecord
	for _, record := range usageRecords {
		if record.ID == usage.ID {
			persistedUsage = record
			break
		}
	}
	if persistedUsage.ID == "" || persistedUsage.PlatformTenantName != tenant.Name || persistedUsage.GatewayPrincipalName != principal.Name || persistedUsage.ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("persisted platform usage=%+v all=%+v", persistedUsage, usageRecords)
	}
	if persistedUsage.TTFTMS == nil || *persistedUsage.TTFTMS != ttftMS || persistedUsage.CacheReadTokens == nil || *persistedUsage.CacheReadTokens != cacheReadTokens || persistedUsage.ProcurementCostMicros == nil || *persistedUsage.ProcurementCostMicros != procurementCostMicros || persistedUsage.Protocol != usage.Protocol || persistedUsage.UpstreamRequestID != usage.UpstreamRequestID {
		t.Fatalf("persisted effective pricing usage=%+v", persistedUsage)
	}
	persistedAffinity, found, err := reopened.FindRoutingAffinityBinding(ctx, affinity.ScopeKey, now)
	if err != nil || !found || persistedAffinity.ProviderAccountID != affinity.ProviderAccountID || persistedAffinity.PolicyVersion != affinity.PolicyVersion {
		t.Fatalf("persisted affinity=%+v found=%t err=%v", persistedAffinity, found, err)
	}
	traces, err := reopened.QueryGatewayTraces(ctx, GatewayTraceQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID, ExternalAuthIntegrationID: integration.ID})
	if err != nil || len(traces) != 1 || traces[0].PlatformTenantName != tenant.Name || traces[0].GatewayPrincipalName != principal.Name || traces[0].ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("platform traces=%+v err=%v", traces, err)
	}
	auditLogs, err := reopened.QueryAuditLogs(ctx, AuditLogQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID, ExternalAuthIntegrationID: integration.ID})
	if err != nil || len(auditLogs) != 1 || auditLogs[0].PlatformTenantName != tenant.Name || auditLogs[0].GatewayPrincipalName != principal.Name || auditLogs[0].ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("platform audit=%+v err=%v", auditLogs, err)
	}
	alerts, err := reopened.QueryAlertEvents(ctx, AlertQuery{ProfileScope: ProfileScopePlatform, PlatformTenantID: tenant.ID, GatewayPrincipalID: principal.ID, ExternalAuthIntegrationID: integration.ID})
	if err != nil || len(alerts) != 1 || alerts[0].PlatformTenantName != tenant.Name || alerts[0].GatewayPrincipalName != principal.Name || alerts[0].ExternalSubjectReference != "opaque-subject" {
		t.Fatalf("platform alerts=%+v err=%v", alerts, err)
	}
}
