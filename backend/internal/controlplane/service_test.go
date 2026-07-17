package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnsureSeedDataCreatesProductBaselineResources(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	dashboard, err := svc.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard(): %v", err)
	}
	if dashboard.ProviderCount != 1 || dashboard.APIKeyCount != 0 {
		t.Fatalf("unexpected dashboard counts: %+v", dashboard)
	}
	if len(dashboard.Models) != 0 {
		t.Fatalf("models = %+v", dashboard.Models)
	}
	providers, err := svc.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || len(providers[0].Models) != 0 {
		t.Fatalf("seed provider must not declare a static model catalog: %+v", providers)
	}
}

func TestCreateAPIKeyReturnsSecretOnceAndStoresHash(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}

	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name:              "CI key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          5,
		MonthlyTokenLimit: 100000,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	if created.Key == "" || created.Record.KeyHash == "" {
		t.Fatalf("secret/hash not generated: %+v", created)
	}
	if created.Record.KeyHash == created.Key {
		t.Fatal("api key stored without hashing")
	}

	keys, err := svc.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys(): %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("key count = %d", len(keys))
	}
	if keys[0].KeyHash == "" || keys[0].Fingerprint == "" || keys[0].Prefix == "" {
		t.Fatalf("stored key metadata incomplete: %+v", keys[0])
	}
}

func TestGovernancePolicyLifecycleValidatesWorkspaceKeyScope(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:           "Scoped key",
		ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	policy, err := svc.CreateGovernancePolicy(ctx, "tester", GovernancePolicyRequest{
		Name:                "Workspace key policy",
		ScopeType:           GovernancePolicyScopeAPIKey,
		ScopeID:             created.Record.ID,
		ModelAllowlist:      []string{"gpt-4o-mini", "gpt-4o-mini", ""},
		QPSLimit:            10,
		MonthlyTokenLimit:   1000000,
		MonthlyBudgetMicros: 50000,
		OverageAction:       GovernancePolicyOverageBlock,
		PromptLoggingMode:   GovernancePolicyPromptLoggingMetadataOnly,
		RetentionDays:       30,
		Status:              GovernancePolicyStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGovernancePolicy(): %v", err)
	}
	if policy.ScopeID != created.Record.ID || len(policy.ModelAllowlist) != 1 || policy.Version != 1 {
		t.Fatalf("policy mismatch: %+v", policy)
	}
	updated, err := svc.UpdateGovernancePolicy(ctx, "tester", policy.ID, GovernancePolicyRequest{
		Name:              "Global policy",
		ScopeType:         GovernancePolicyScopeGlobal,
		OverageAction:     GovernancePolicyOverageWarn,
		PromptLoggingMode: GovernancePolicyPromptLoggingDisabled,
		Status:            GovernancePolicyStatusDisabled,
	})
	if err != nil {
		t.Fatalf("UpdateGovernancePolicy(): %v", err)
	}
	if updated.ScopeType != GovernancePolicyScopeGlobal || updated.ScopeID != "" || updated.Version != 2 {
		t.Fatalf("updated policy mismatch: %+v", updated)
	}
	if _, err := svc.CreateGovernancePolicy(ctx, "tester", GovernancePolicyRequest{
		Name:      "Missing key",
		ScopeType: GovernancePolicyScopeAPIKey,
		ScopeID:   "key_missing",
	}); err == nil {
		t.Fatal("CreateGovernancePolicy() accepted missing workspace key scope")
	}
}

func TestEnforceGatewayPolicyRejectsQPSLimit(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name:              "QPS key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          1,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); err != nil {
		t.Fatalf("first EnforceGatewayPolicy(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayRateLimited) {
		t.Fatalf("second EnforceGatewayPolicy() err = %v", err)
	}
}

func TestGatewayPolicyReferenceOverridesAPIKeyLimitsAndModels(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	policy, err := svc.CreateGovernancePolicy(context.Background(), "tester", GovernancePolicyRequest{
		Name:              "Strict gateway policy",
		ScopeType:         GovernancePolicyScopeGlobal,
		ModelAllowlist:    []string{"policy-model"},
		ModelDenylist:     []string{"blocked-model"},
		QPSLimit:          1,
		MonthlyTokenLimit: 3,
		OverageAction:     GovernancePolicyOverageBlock,
		Status:            GovernancePolicyStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGovernancePolicy(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name:              "Policy key",
		PolicyID:          policy.ID,
		ModelAllowlist:    []string{"legacy-model", "policy-model", "blocked-model"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	if _, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "legacy-model"); !errors.Is(err, ErrGatewayForbidden) {
		t.Fatalf("legacy model should be rejected by policy allowlist, err=%v", err)
	}
	if _, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "blocked-model"); !errors.Is(err, ErrGatewayForbidden) {
		t.Fatalf("blocked model should be rejected by policy denylist, err=%v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "policy-model")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if auth.Policy == nil || auth.Policy.ID != policy.ID {
		t.Fatalf("effective policy not attached: %+v", auth.Policy)
	}
	if auth.PolicySource != GatewayPolicySourceAPIKeyExplicit {
		t.Fatalf("policy source = %q", auth.PolicySource)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); err != nil {
		t.Fatalf("first EnforceGatewayPolicy(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayRateLimited) {
		t.Fatalf("second EnforceGatewayPolicy() err = %v", err)
	}
	if err := svc.RecordGatewayUsage(context.Background(), auth, GatewayUsageInput{
		Model:        "policy-model",
		Status:       "forwarded",
		InputTokens:  2,
		OutputTokens: 1,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := svc.RecordGatewayTrace(context.Background(), auth, GatewayTraceInput{
		Model:           "policy-model",
		Status:          "forwarded",
		RouteSource:     "provider_connection",
		ResponseSummary: "ok",
	}); err != nil {
		t.Fatalf("RecordGatewayTrace(): %v", err)
	}
	traces, err := svc.ListGatewayTraces(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListGatewayTraces(): %v", err)
	}
	if len(traces) != 1 || traces[0].PolicyID != policy.ID || traces[0].PolicyName != policy.Name || traces[0].PolicySource != GatewayPolicySourceAPIKeyExplicit {
		t.Fatalf("trace policy evidence mismatch: %+v", traces)
	}
	if traces[0].PolicyVersion != policy.Version {
		t.Fatalf("trace policy version mismatch: trace=%d policy=%d", traces[0].PolicyVersion, policy.Version)
	}
	var snapshot gatewayTracePolicySnapshot
	if err := json.Unmarshal([]byte(traces[0].PolicySnapshot), &snapshot); err != nil {
		t.Fatalf("policy snapshot json: %v snapshot=%q", err, traces[0].PolicySnapshot)
	}
	if snapshot.Version != policy.Version || snapshot.QPSLimit != 1 || snapshot.MonthlyTokenLimit != 3 || snapshot.OverageAction != GovernancePolicyOverageBlock || snapshot.ModelAllowlistCount != 1 || snapshot.ModelDenylistCount != 1 {
		t.Fatalf("policy snapshot mismatch: %+v", snapshot)
	}
	explanation, err := svc.ExplainGatewayPolicyForAPIKey(context.Background(), created.Record.ID)
	if err != nil {
		t.Fatalf("ExplainGatewayPolicyForAPIKey(): %v", err)
	}
	if explanation.SelectedPolicyID != policy.ID || explanation.SelectedPolicyVersion != policy.Version || explanation.SelectedSource != GatewayPolicySourceAPIKeyExplicit {
		t.Fatalf("policy explanation mismatch: %+v", explanation)
	}
	if len(explanation.Candidates) == 0 || !explanation.Candidates[0].Selected {
		t.Fatalf("policy explanation candidates mismatch: %+v", explanation.Candidates)
	}
	svc.rateWindows = map[string][]time.Time{}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayQuotaExceeded) {
		t.Fatalf("policy monthly token quota err = %v", err)
	}
}

func TestEnforceGatewayPolicyRejectsMonthlyTokenQuota(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name:              "Quota key",
		ModelAllowlist:    []string{"gpt-4o-mini"},
		QPSLimit:          0,
		MonthlyTokenLimit: 10,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	if err := svc.RecordGatewayUsage(context.Background(), auth, GatewayUsageInput{
		Model:        "gpt-4o-mini",
		Status:       "forwarded",
		InputTokens:  4,
		OutputTokens: 6,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := svc.EnforceGatewayPolicy(context.Background(), auth); !errors.Is(err, ErrGatewayQuotaExceeded) {
		t.Fatalf("EnforceGatewayPolicy() err = %v", err)
	}
}

func TestEnforceGatewayPolicyDefersWorkspaceKeyBudgetToAdmissionHold(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	policy, err := svc.CreateGovernancePolicy(ctx, "tester", GovernancePolicyRequest{
		Name:                "Budget guard",
		ScopeType:           GovernancePolicyScopeGlobal,
		MonthlyBudgetMicros: 250,
		OverageAction:       GovernancePolicyOverageBlock,
		Status:              GovernancePolicyStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGovernancePolicy(): %v", err)
	}
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name:           "Budget key",
		PolicyID:       policy.ID,
		ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	recordTestPricedGatewayUsage(t, svc, auth, GatewayUsageInput{Model: "gpt-4o-mini", Status: "forwarded"}, 250)
	if err := svc.EnforceGatewayPolicy(ctx, auth); err != nil {
		t.Fatalf("EnforceGatewayPolicy() must defer budget enforcement to billing hold: %v", err)
	}
	if err := svc.EnforceGatewayOngoingPolicy(ctx, auth); !errors.Is(err, ErrGatewayBudgetExceeded) {
		t.Fatalf("EnforceGatewayOngoingPolicy() err = %v", err)
	}
}

func TestUsageReportQueryAggregatesBeyondCurrentPage(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if err := svc.EnsureSeedData(context.Background()); err != nil {
		t.Fatalf("EnsureSeedData(): %v", err)
	}
	created, err := svc.CreateAPIKey(context.Background(), "tester", APIKeyCreateRequest{
		Name:              "Usage aggregate key",
		ModelAllowlist:    []string{"model-a", "model-b"},
		QPSLimit:          0,
		MonthlyTokenLimit: 0,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := svc.AuthorizeGatewayModel(context.Background(), created.Key, "model-a")
	if err != nil {
		t.Fatalf("AuthorizeGatewayModel(): %v", err)
	}
	inputs := []GatewayUsageInput{
		{Model: "model-a", ProviderID: "provider-a", ProviderAccountID: "account-a", Status: "forwarded", InputTokens: 1, OutputTokens: 1, LatencyMS: 10, UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitCount, Source: "core", Confidence: UsageConfidenceObserved}}},
		{Model: "model-b", ProviderID: "provider-b", ProviderAccountID: "account-b", Status: "error", ErrorType: "policy_error", InputTokens: 2, OutputTokens: 2, LatencyMS: 20, UsageDimensions: UsageDimensions{UsageDimensionOutputVideoMilliseconds: {Quantity: 1500, Unit: UsageUnitMillisecond, Source: "provider", Confidence: UsageConfidenceReported}}},
		{Model: "model-b", ProviderID: "provider-b", ProviderAccountID: "account-c", Status: "forwarded", InputTokens: 3, OutputTokens: 3, LatencyMS: 30, UsageDimensions: UsageDimensions{UsageDimensionOutputAudioMilliseconds: {Quantity: 2500, Unit: UsageUnitMillisecond, Source: "provider", Confidence: UsageConfidenceReported}}},
	}
	for index, input := range inputs {
		recordTestPricedGatewayUsage(t, svc, auth, input, int64((index+1)*100))
	}

	report, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 1})
	if err != nil {
		t.Fatalf("UsageReportQuery(): %v", err)
	}
	if len(report.Recent) != 1 {
		t.Fatalf("recent should remain paginated, got %d", len(report.Recent))
	}
	if report.TotalRequests != 3 || report.ErrorRequests != 1 || report.TotalTokens != 12 || report.TotalOutputImages != 1 || report.TotalVideoDuration != 1500 || report.TotalAudioDuration != 2500 || report.TotalUsageCostMicros != 600 || report.AvgLatencyMS != 20 {
		t.Fatalf("aggregate does not include all records: %+v", report)
	}
	byModel := map[string]UsageModelSummary{}
	for _, item := range report.ByModel {
		byModel[item.Model] = item
	}
	if byModel["model-b"].Requests != 2 || byModel["model-b"].Errors != 1 || byModel["model-b"].Tokens != 10 || byModel["model-b"].VideoMilliseconds != 1500 || byModel["model-b"].AudioMilliseconds != 2500 {
		t.Fatalf("model aggregate does not include all matching records: %+v", report.ByModel)
	}

	filtered, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 1, Model: "model-b"})
	if err != nil {
		t.Fatalf("filtered UsageReportQuery(): %v", err)
	}
	if len(filtered.Recent) != 1 || filtered.TotalRequests != 2 || len(filtered.ByModel) != 1 || filtered.ByModel[0].Model != "model-b" {
		t.Fatalf("filtered aggregate mismatch: %+v", filtered)
	}

	filteredByProvider, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 10, ProviderID: "provider-b"})
	if err != nil {
		t.Fatalf("provider filtered UsageReportQuery(): %v", err)
	}
	if len(filteredByProvider.Recent) != 2 || filteredByProvider.TotalRequests != 2 || filteredByProvider.TotalTokens != 10 {
		t.Fatalf("provider filtered aggregate mismatch: %+v", filteredByProvider)
	}

	filteredByAccount, err := svc.UsageReportQuery(context.Background(), UsageQuery{Limit: 10, AccountID: "account-b"})
	if err != nil {
		t.Fatalf("account filtered UsageReportQuery(): %v", err)
	}
	if len(filteredByAccount.Recent) != 1 || filteredByAccount.TotalRequests != 1 || filteredByAccount.Recent[0].ProviderAccountID != "account-b" {
		t.Fatalf("account filtered aggregate mismatch: %+v", filteredByAccount)
	}
}

func TestPricingRuleEstimatesGatewayUsageCost(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	publishTestUsagePricingRule(t, svc, `v1: token_line("input", uncached_input_tokens, 3000000) + token_line("output", output_tokens, 6000000)`)
	amount, found, err := svc.EstimateModelUsageCostMicros(context.Background(), "priced-model", 1_000_000, 500_000)
	if err != nil || !found || amount != 6_000_000 {
		t.Fatalf("EstimateModelUsageCostMicros() amount=%d found=%t err=%v", amount, found, err)
	}
	if _, err := svc.CreatePricingRule(context.Background(), "tester", PricingRuleCreateRequest{Name: "Duplicate", Purpose: PricingPurposeUsageCost, ScopeType: PricingScopeGlobal, Model: "*", Currency: "USD", Expression: `v1: fixed_line("free", "request", 0)`}); err == nil {
		t.Fatal("expected duplicate pricing slot error")
	}
}

func TestCostAllocationReportAggregatesByWorkspaceKeyAndModel(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	platformKey, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "Platform key", ModelAllowlist: []string{"model-a"}})
	if err != nil {
		t.Fatalf("CreateAPIKey(platform): %v", err)
	}
	billingKey, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "Billing key", ModelAllowlist: []string{"model-a", "model-b"}})
	if err != nil {
		t.Fatalf("CreateAPIKey(billing): %v", err)
	}
	platformAuth, _ := svc.AuthorizeGatewayModel(ctx, platformKey.Key, "model-a")
	billingAuth, _ := svc.AuthorizeGatewayModel(ctx, billingKey.Key, "model-a")
	inputs := []struct {
		auth  GatewayAuthContext
		usage GatewayUsageInput
		cost  int64
	}{
		{platformAuth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 10, OutputTokens: 10, LatencyMS: 10}, 100},
		{billingAuth, GatewayUsageInput{Model: "model-a", Status: "forwarded", InputTokens: 20, OutputTokens: 5, LatencyMS: 20}, 250},
		{billingAuth, GatewayUsageInput{Model: "model-b", Status: "error", ErrorType: "policy_error", InputTokens: 7, OutputTokens: 3, LatencyMS: 40}, 150},
	}
	for _, input := range inputs {
		recordTestPricedGatewayUsage(t, svc, input.auth, input.usage, input.cost)
	}
	keyReport, err := svc.CostAllocationReportQuery(ctx, CostAllocationByAPIKey, UsageQuery{})
	if err != nil {
		t.Fatalf("CostAllocationReportQuery(api_key): %v", err)
	}
	if keyReport.TotalRequests != 3 || len(keyReport.Rows) != 2 || keyReport.Rows[0].APIKeyName != "Billing key" || keyReport.Rows[0].TotalUsageCostMicros != 400 {
		t.Fatalf("workspace key allocation mismatch: %+v", keyReport)
	}
	modelReport, err := svc.CostAllocationReportQuery(ctx, CostAllocationByModel, UsageQuery{})
	if err != nil {
		t.Fatalf("CostAllocationReportQuery(model): %v", err)
	}
	if len(modelReport.Rows) != 2 || modelReport.Rows[0].Model != "model-a" || modelReport.Rows[0].TotalUsageCostMicros != 350 {
		t.Fatalf("model allocation mismatch: %+v", modelReport)
	}
	if _, err := svc.CostAllocationReportQuery(ctx, "project", UsageQuery{}); !errors.Is(err, ErrInvalidCostAllocationDimension) {
		t.Fatalf("invalid dimension err = %v", err)
	}
}

func TestCostAllocationReportAggregatesByUserAndDepartment(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	department, err := svc.CreateDepartment(ctx, "tester", DepartmentRequest{Name: "Engineering", Code: "eng", Status: DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.ProvisionOIDCUser(ctx, "https://id.example.test", "subject", "engineer@example.test", "Engineer", "eng")
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "Engineer key", KeyType: APIKeyTypeUser, OwnerUserID: user.ID, ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := svc.AuthorizeGatewayModel(ctx, created.Key, "model")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", InputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	userReport, err := svc.CostAllocationReportQuery(ctx, CostAllocationByUser, UsageQuery{})
	if err != nil || len(userReport.Rows) != 1 || userReport.Rows[0].ResourceID != user.ID || userReport.Rows[0].ResourceName != "Engineer" {
		t.Fatalf("user report=%+v err=%v", userReport, err)
	}
	departmentReport, err := svc.CostAllocationReportQuery(ctx, CostAllocationByDepartment, UsageQuery{})
	if err != nil || len(departmentReport.Rows) != 1 || departmentReport.Rows[0].ResourceID != department.ID || departmentReport.Rows[0].ResourceName != department.Name {
		t.Fatalf("department report=%+v err=%v", departmentReport, err)
	}
}

func TestCreateProviderRequiresAbsoluteURL(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	_, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "bad",
		Type:    "openai_compatible",
		BaseURL: "/relative",
		Models:  []string{"gpt-4o-mini"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateProviderEncryptsSecret(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "OpenAI-compatible test",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	if !provider.SecretConfigured || provider.SecretCiphertext == "" {
		t.Fatalf("secret metadata not configured: %+v", provider)
	}
	if provider.SecretCiphertext == "upstream-secret" {
		t.Fatal("provider secret stored in plaintext")
	}

}

func TestCheckProviderProbesModelsAndPersistsHealth(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "test-secret-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-real"},{"id":"gpt-fast"}]}`))
	}))
	defer upstream.Close()

	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Probe provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-model"},
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	check, err := svc.CheckProvider(context.Background(), "tester", provider.ID)
	if err != nil {
		t.Fatalf("CheckProvider(): %v", err)
	}
	if check.Status != "ok" || len(check.Models) != 2 {
		t.Fatalf("unexpected health check: %+v", check)
	}

	health, err := svc.ListProviderHealthChecks(context.Background())
	if err != nil {
		t.Fatalf("ListProviderHealthChecks(): %v", err)
	}
	if len(health) != 1 || health[0].ProviderID != provider.ID || health[0].Status != "ok" {
		t.Fatalf("health not persisted: %+v", health)
	}

	providers, err := svc.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || !sameStringList(providers[0].Models, []string{"gpt-fast", "gpt-real"}) {
		t.Fatalf("provider model snapshot was not replaced: %+v", providers)
	}
}

func TestProviderModelSnapshotCanOnlyBeWrittenByDiscovery(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"discovered-current"}]}`))
	}))
	defer upstream.Close()

	svc := NewService(NewMemoryRepository(), "/v1", "provider-snapshot-secret")
	provider, err := svc.CreateProvider(ctx, "tester", ProviderRequest{
		Name: "Read-only snapshot", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, Models: []string{"forged-on-create"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Models) != 0 {
		t.Fatalf("create accepted caller-owned model snapshot: %+v", provider.Models)
	}
	if _, err := svc.CheckProvider(ctx, "tester", provider.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateProvider(ctx, "tester", provider.ID, ProviderRequest{
		Name: "Read-only snapshot updated", Type: "openai_compatible", BaseURL: upstream.URL + "/v1",
		Status: ProviderStatusActive, Models: []string{"forged-on-update"}, Priority: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameStringList(updated.Models, []string{"discovered-current"}) {
		t.Fatalf("update replaced discovered model snapshot: %+v", updated.Models)
	}
}

func TestCheckProviderUsesTypedModelDiscoveryAdapters(t *testing.T) {
	tests := []struct {
		name           string
		providerType   string
		basePath       string
		expectedHeader string
		expectedValue  string
		response       string
		expectedModels []string
	}{
		{
			name:           "anthropic",
			providerType:   "anthropic",
			basePath:       "/v1",
			expectedHeader: "x-api-key",
			expectedValue:  "provider-secret",
			response:       `{"data":[{"id":"claude-current"}],"has_more":false}`,
			expectedModels: []string{"claude-current"},
		},
		{
			name:           "gemini",
			providerType:   "gemini",
			basePath:       "/v1beta",
			expectedHeader: "x-goog-api-key",
			expectedValue:  "provider-secret",
			response:       `{"models":[{"name":"models/gemini-current"}]}`,
			expectedModels: []string{"gemini-current"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.basePath+"/models" {
					t.Fatalf("path = %s", r.URL.Path)
				}
				if got := r.Header.Get(tc.expectedHeader); got != tc.expectedValue {
					t.Fatalf("%s = %q", tc.expectedHeader, got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer upstream.Close()

			svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
			provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
				Name:    tc.name + " provider",
				Type:    tc.providerType,
				BaseURL: upstream.URL + tc.basePath,
				Status:  ProviderStatusActive,
				Models:  []string{"stale-model"},
				APIKey:  "provider-secret",
			})
			if err != nil {
				t.Fatalf("CreateProvider(): %v", err)
			}

			check, err := svc.CheckProvider(context.Background(), "tester", provider.ID)
			if err != nil {
				t.Fatalf("CheckProvider(): %v", err)
			}
			if check.Status != "ok" || !sameStringList(check.Models, tc.expectedModels) {
				t.Fatalf("unexpected health check: %+v", check)
			}
			providers, err := svc.ListProviders(context.Background())
			if err != nil {
				t.Fatalf("ListProviders(): %v", err)
			}
			if len(providers) != 1 || !sameStringList(providers[0].Models, tc.expectedModels) {
				t.Fatalf("provider model snapshot was not replaced: %+v", providers)
			}
		})
	}
}

func TestProviderAccountLifecyclePreservesEncryptedSecretAndUpdatesGroupCounts(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Account provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	group, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:           "OpenAI default",
		Platform:       "openai_compatible",
		RateMultiplier: 1,
		Status:         RoutingGroupStatusActive,
		SortOrder:      10,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(): %v", err)
	}

	schedulable := true
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account A",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    4,
		RateMultiplier: 1.2,
		Models:         []string{"gpt-4o-mini"},
		GroupIDs:       []string{group.ID},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	if !account.SecretConfigured || account.SecretCiphertext == "" {
		t.Fatalf("secret metadata not configured: %+v", account)
	}
	if account.SecretCiphertext == "account-secret" {
		t.Fatal("provider account secret stored in plaintext")
	}

	groups, err := svc.ListRoutingGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRoutingGroups(): %v", err)
	}
	if len(groups) != 1 || groups[0].AccountCount != 1 || groups[0].ActiveAccounts != 1 {
		t.Fatalf("unexpected group counts: %+v", groups)
	}

	schedulable = false
	updated, err := svc.UpdateProviderAccount(context.Background(), "tester", account.ID, ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account A updated",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       30,
		Concurrency:    2,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		GroupIDs:       []string{group.ID},
	})
	if err != nil {
		t.Fatalf("UpdateProviderAccount(): %v", err)
	}
	if updated.SecretCiphertext != account.SecretCiphertext || !updated.SecretConfigured {
		t.Fatalf("secret not preserved: before=%+v after=%+v", account, updated)
	}

	groups, err = svc.ListRoutingGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRoutingGroups(): %v", err)
	}
	if groups[0].AccountCount != 1 || groups[0].ActiveAccounts != 0 {
		t.Fatalf("schedulable count not updated: %+v", groups[0])
	}
}

func TestRoutingGroupTypeSpecificConfiguration(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")

	subscription, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:                "Team subscription",
		Platform:            "openai_compatible",
		GroupType:           RoutingGroupTypeSubscription,
		RateMultiplier:      1,
		MonthlyBudgetMicros: 5000,
		Status:              RoutingGroupStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(subscription): %v", err)
	}
	if subscription.GroupType != RoutingGroupTypeSubscription || subscription.MonthlyBudgetMicros != 5000 {
		t.Fatalf("subscription fields not preserved: %+v", subscription)
	}

	exclusive, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:           "Dedicated customer",
		Platform:       "anthropic",
		GroupType:      RoutingGroupTypeExclusive,
		RateMultiplier: 1.2,
		Status:         RoutingGroupStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(exclusive): %v", err)
	}
	if !exclusive.IsExclusive {
		t.Fatalf("exclusive group must force is_exclusive: %+v", exclusive)
	}

	image, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:                "Image pool",
		Platform:            "gemini",
		GroupType:           RoutingGroupTypeImageGeneration,
		RateMultiplier:      1,
		ImageRateMultiplier: 1.5,
		ImagePrice1KCents:   4,
		Status:              RoutingGroupStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(image): %v", err)
	}
	if !image.ImageEnabled || image.VideoEnabled || image.ImageRateMultiplier != 1.5 {
		t.Fatalf("image group not normalized correctly: %+v", image)
	}

	video, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:                "Video pool",
		Platform:            "grok",
		GroupType:           RoutingGroupTypeVideoGeneration,
		RateMultiplier:      1,
		VideoRateMultiplier: 1.8,
		VideoPrice720PCents: 12,
		Status:              RoutingGroupStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(video): %v", err)
	}
	if !video.VideoEnabled || video.ImageEnabled || video.VideoRateMultiplier != 1.8 {
		t.Fatalf("video group not normalized correctly: %+v", video)
	}
}

func TestRoutingGroupTypeSpecificValidation(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")

	if _, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:           "Bad subscription",
		Platform:       "openai_compatible",
		GroupType:      RoutingGroupTypeSubscription,
		RateMultiplier: 1,
		Status:         RoutingGroupStatusActive,
	}); err == nil {
		t.Fatal("subscription group without a budget should fail")
	}

	if _, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:              "Bad image",
		Platform:          "gemini",
		GroupType:         RoutingGroupTypeImageGeneration,
		RateMultiplier:    1,
		ImagePrice1KCents: -1,
		Status:            RoutingGroupStatusActive,
	}); err == nil {
		t.Fatal("negative image price should fail")
	}

	if _, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:                "Bad peak",
		Platform:            "openai_compatible",
		GroupType:           RoutingGroupTypeSubscription,
		RateMultiplier:      1,
		MonthlyBudgetMicros: 5000,
		PeakRateEnabled:     true,
		Status:              RoutingGroupStatusActive,
	}); err == nil {
		t.Fatal("peak rate without start/end should fail")
	}
}

func TestRoutingGroupClearsFieldsThatDoNotBelongToType(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")

	group, err := svc.CreateRoutingGroup(context.Background(), "tester", RoutingGroupRequest{
		Name:                "Standard",
		Platform:            "openai_compatible",
		GroupType:           RoutingGroupTypeStandard,
		RateMultiplier:      1,
		MonthlyBudgetMicros: 5000,
		ImageEnabled:        true,
		ImagePrice1KCents:   10,
		VideoEnabled:        true,
		VideoPrice720PCents: 20,
		PeakRateEnabled:     true,
		PeakStart:           "09:00",
		PeakEnd:             "18:00",
		PeakRateMultiplier:  2,
		Status:              RoutingGroupStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateRoutingGroup(): %v", err)
	}
	if group.MonthlyBudgetMicros != 0 || group.ImageEnabled || group.VideoEnabled || group.PeakRateEnabled {
		t.Fatalf("standard group retained type-specific fields: %+v", group)
	}
}

func TestCreateProviderAccountRejectsLegacyAuthTypes(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Account provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	for _, legacyAuthType := range []string{"oauth", "session", "cookie", "service_account", "custom"} {
		_, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
			ProviderID: provider.ID,
			Name:       "Legacy auth account",
			Platform:   "openai_compatible",
			AuthType:   legacyAuthType,
			Status:     AccountStatusActive,
			Models:     []string{"gpt-4o-mini"},
			Secret:     "account-secret",
		})
		if err == nil {
			t.Fatalf("CreateProviderAccount() with auth_type %q: expected error, got none", legacyAuthType)
		}
	}
}

func TestGatewayProviderForModelPrefersSchedulableProviderAccount(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Route provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	schedulable := true
	_, err = svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account slow",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       30,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "slow-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount slow: %v", err)
	}
	fast, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account fast",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       10,
		Concurrency:    3,
		RateMultiplier: 1,
		Models:         []string{"gpt-4o-mini"},
		Secret:         "fast-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount fast: %v", err)
	}
	mustCreateGatewayModelRoutes(t, svc, "gpt-4o-mini", []ProviderAccount{fast})

	selected, ok, err := svc.GatewayProviderForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderForModel(): %v", err)
	}
	if !ok || selected.ID != provider.ID || selected.AccountID != fast.ID || selected.APIKey != "fast-secret" {
		t.Fatalf("unexpected selected route: %+v ok=%v", selected, ok)
	}

	accounts, err := svc.ListProviderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccounts(): %v", err)
	}
	for _, account := range accounts {
		if account.ID == fast.ID && account.LastUsedAt == nil {
			t.Fatalf("selected account last_used_at not updated: %+v", account)
		}
	}
}

func TestCheckProviderAccountProbesModelsAndPersistsHealth(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-account"},{"id":"gpt-fast"}]}`))
	}))
	defer upstream.Close()

	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Account probe provider",
		Type:    "openai_compatible",
		BaseURL: upstream.URL + "/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"manual-provider-model"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	schedulable := true
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID:     provider.ID,
		Name:           "Account probe",
		Platform:       "openai_compatible",
		AuthType:       "api_key",
		Status:         AccountStatusActive,
		Schedulable:    &schedulable,
		Priority:       20,
		Concurrency:    4,
		RateMultiplier: 1,
		Models:         []string{"manual-account-model"},
		Secret:         "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}

	check, err := svc.CheckProviderAccount(context.Background(), "tester", account.ID)
	if err != nil {
		t.Fatalf("CheckProviderAccount(): %v", err)
	}
	if check.Status != "ok" || check.ProviderID != provider.ID || len(check.Models) != 2 {
		t.Fatalf("unexpected account check: %+v", check)
	}

	health, err := svc.ListProviderAccountHealthChecks(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccountHealthChecks(): %v", err)
	}
	if len(health) != 1 || health[0].AccountID != account.ID || health[0].Status != "ok" {
		t.Fatalf("account health not persisted: %+v", health)
	}

	accounts, err := svc.ListProviderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListProviderAccounts(): %v", err)
	}
	if len(accounts) != 1 || !contains(accounts[0].Models, "manual-account-model") || contains(accounts[0].Models, "gpt-account") {
		t.Fatalf("account discovery changed the explicit model selection: %+v", accounts)
	}
	inventory, err := svc.GetProviderAccountModelInventory(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("GetProviderAccountModelInventory(): %v", err)
	}
	if model := findProviderAccountModel(inventory.Models, "gpt-account"); model == nil || model.Source != ProviderAccountModelSourceDiscovered || model.Availability != ProviderAccountModelAvailabilityAvailable || model.Enabled {
		t.Fatalf("discovered model inventory mismatch: %+v", inventory.Models)
	}
}

func TestProviderAccountEffectiveLoadFactor(t *testing.T) {
	loadFactor := 20
	cases := []struct {
		name    string
		account ProviderAccount
		want    int
	}{
		{name: "explicit load factor wins", account: ProviderAccount{LoadFactor: &loadFactor, Concurrency: 3}, want: 20},
		{name: "falls back to concurrency", account: ProviderAccount{Concurrency: 5}, want: 5},
		{name: "floors at one when concurrency is zero", account: ProviderAccount{Concurrency: 0}, want: 1},
	}
	for _, tc := range cases {
		if got := tc.account.EffectiveLoadFactor(); got != tc.want {
			t.Fatalf("%s: EffectiveLoadFactor() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTryAcquireProviderAccountSlot(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")

	unlimitedRelease, ok := svc.TryAcquireProviderAccountSlot("acct_unlimited", 0)
	if !ok {
		t.Fatal("expected unlimited slot to be acquired")
	}
	unlimitedRelease()

	release1, ok := svc.TryAcquireProviderAccountSlot("acct_limited", 1)
	if !ok {
		t.Fatal("expected first slot to be acquired")
	}
	if _, ok := svc.TryAcquireProviderAccountSlot("acct_limited", 1); ok {
		t.Fatal("expected second slot to be rejected while first is held")
	}
	release1()
	releaseAfterFirstRelease, ok := svc.TryAcquireProviderAccountSlot("acct_limited", 1)
	if !ok {
		t.Fatal("expected slot to be acquirable again after release")
	}
	releaseAfterFirstRelease()

	// Releasing twice must not corrupt the counter for other callers.
	release2, ok := svc.TryAcquireProviderAccountSlot("acct_limited", 1)
	if !ok {
		t.Fatal("expected slot to be acquired for double-release check")
	}
	release2()
	release2()
	if _, ok := svc.TryAcquireProviderAccountSlot("acct_limited", 1); !ok {
		t.Fatal("expected slot to remain acquirable after double release")
	}
}

func TestRankedProviderAccountCandidatesOrdersByPriorityThenLoadThenRate(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Ranking provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true

	// Same priority, but "busy" has an occupied concurrency slot so its load
	// ratio (1/1) is higher than "idle" (0/1) — idle must rank first.
	idle, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "idle", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Schedulable: &schedulable, Priority: 10, Concurrency: 1, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "idle-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount idle: %v", err)
	}
	busy, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "busy", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Schedulable: &schedulable, Priority: 10, Concurrency: 1, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "busy-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount busy: %v", err)
	}
	release, ok := svc.TryAcquireProviderAccountSlot(busy.ID, busy.Concurrency)
	if !ok {
		t.Fatal("expected to occupy busy account's only slot")
	}
	defer release()

	candidates, hasPool, err := svc.rankedProviderAccountCandidates(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("rankedProviderAccountCandidates(): %v", err)
	}
	if !hasPool {
		t.Fatal("expected provider account pool to be detected")
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2: %+v", len(candidates), candidates)
	}
	if candidates[0].account.ID != idle.ID || candidates[1].account.ID != busy.ID {
		t.Fatalf("unexpected candidate order: %+v", candidates)
	}
}

func TestGatewayProviderCandidatesForModelSkipsCooldownAccounts(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Cooldown provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	schedulable := true
	primary, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "primary", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Schedulable: &schedulable, Priority: 10, Concurrency: 3, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "primary-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount primary: %v", err)
	}
	backup, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "backup", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Schedulable: &schedulable, Priority: 20, Concurrency: 3, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "backup-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount backup: %v", err)
	}
	mustCreateGatewayModelRoutes(t, svc, "gpt-4o-mini", []ProviderAccount{primary, backup})

	candidates, hasPool, err := svc.GatewayProviderCandidatesForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderCandidatesForModel(): %v", err)
	}
	if !hasPool || len(candidates) != 2 || candidates[0].AccountID != primary.ID || candidates[1].AccountID != backup.ID {
		t.Fatalf("unexpected candidates before cooldown: %+v hasPool=%v", candidates, hasPool)
	}

	if err := svc.RecordProviderAccountFailure(context.Background(), primary.ID, http.StatusInternalServerError, "upstream error"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}

	candidates, hasPool, err = svc.GatewayProviderCandidatesForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderCandidatesForModel() after cooldown: %v", err)
	}
	if !hasPool || len(candidates) != 1 || candidates[0].AccountID != backup.ID {
		t.Fatalf("expected only backup candidate after primary cooldown: %+v hasPool=%v", candidates, hasPool)
	}

	updated, err := svc.providerAccountByID(context.Background(), primary.ID)
	if err != nil {
		t.Fatalf("providerAccountByID(): %v", err)
	}
	if updated.CooldownUntil == nil || !updated.CooldownUntil.After(time.Now().UTC()) {
		t.Fatalf("expected primary account to have a future cooldown: %+v", updated)
	}
}

func TestCreateProviderAccountValidatesTempUnschedulableRules(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Rule provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}

	cases := []struct {
		name  string
		rules []ProviderAccountTempUnschedulableRule
	}{
		{name: "bad status code", rules: []ProviderAccountTempUnschedulableRule{{StatusCode: 0, Keywords: []string{"revoked"}, DurationMinutes: 10}}},
		{name: "zero duration", rules: []ProviderAccountTempUnschedulableRule{{StatusCode: 401, Keywords: []string{"revoked"}, DurationMinutes: 0}}},
		{name: "no keywords", rules: []ProviderAccountTempUnschedulableRule{{StatusCode: 401, Keywords: nil, DurationMinutes: 10}}},
	}
	for _, tc := range cases {
		_, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
			ProviderID: provider.ID, Name: "Rule account", Platform: "openai_compatible", AuthType: "api_key",
			Status: AccountStatusActive, Priority: 10, Concurrency: 3, RateMultiplier: 1,
			Models: []string{"gpt-4o-mini"}, Secret: "rule-secret", TempUnschedulableRules: tc.rules,
		})
		if err == nil {
			t.Fatalf("%s: expected validation error", tc.name)
		}
	}
}

func TestRecordProviderAccountFailureAppliesMatchingRuleDuration(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Rule match provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Rule account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Priority: 10, Concurrency: 3, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "rule-secret",
		TempUnschedulableRules: []ProviderAccountTempUnschedulableRule{
			{StatusCode: 402, Keywords: []string{"insufficient balance"}, DurationMinutes: 120},
			{StatusCode: 401, Keywords: []string{"key revoked", "unauthorized"}, DurationMinutes: 60},
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	mustCreateGatewayModelRoutes(t, svc, "gpt-4o-mini", []ProviderAccount{account})

	// A failure that matches the second rule (case-insensitive keyword
	// match) should cool the account down by that rule's duration and
	// record why, not fall back to the default short cooldown.
	beforeMatch := time.Now().UTC()
	if err := svc.RecordProviderAccountFailure(context.Background(), account.ID, 401, "Error: API Key Revoked for this account"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}
	matched, err := svc.providerAccountByID(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("providerAccountByID(): %v", err)
	}
	if matched.CooldownUntil == nil || matched.CooldownUntil.Before(beforeMatch.Add(59*time.Minute)) {
		t.Fatalf("expected ~60 minute cooldown from matched rule: %+v", matched)
	}
	if matched.TempUnschedulableReason == "" {
		t.Fatalf("expected a temp unschedulable reason to be recorded: %+v", matched)
	}

	if _, err := svc.ClearProviderAccountCooldown(context.Background(), "tester", account.ID); err != nil {
		t.Fatalf("ClearProviderAccountCooldown(): %v", err)
	}

	// A failure that matches no configured rule falls back to the default
	// short cooldown and clears any stale reason.
	if err := svc.RecordProviderAccountFailure(context.Background(), account.ID, 500, "internal server error"); err != nil {
		t.Fatalf("RecordProviderAccountFailure() unmatched: %v", err)
	}
	unmatched, err := svc.providerAccountByID(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("providerAccountByID(): %v", err)
	}
	if unmatched.CooldownUntil == nil || unmatched.CooldownUntil.After(time.Now().UTC().Add(providerAccountFailureCooldown+time.Second)) {
		t.Fatalf("expected default cooldown duration for unmatched failure: %+v", unmatched)
	}
	if unmatched.TempUnschedulableReason != "" {
		t.Fatalf("expected reason to be cleared for unmatched failure: %+v", unmatched)
	}
}

func TestClearProviderAccountCooldownMakesAccountImmediatelyEligible(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name:    "Clear cooldown provider",
		Type:    "openai_compatible",
		BaseURL: "https://provider.example/v1",
		Status:  ProviderStatusActive,
		Models:  []string{"gpt-4o-mini"},
		APIKey:  "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Solo account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Priority: 10, Concurrency: 3, RateMultiplier: 1,
		Models: []string{"gpt-4o-mini"}, Secret: "solo-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	mustCreateGatewayModelRoutes(t, svc, "gpt-4o-mini", []ProviderAccount{account})
	if err := svc.RecordProviderAccountFailure(context.Background(), account.ID, http.StatusInternalServerError, "boom"); err != nil {
		t.Fatalf("RecordProviderAccountFailure(): %v", err)
	}
	if candidates, _, err := svc.GatewayProviderCandidatesForModel(context.Background(), "gpt-4o-mini"); err != nil || len(candidates) != 0 {
		t.Fatalf("expected no candidates while cooling down: candidates=%+v err=%v", candidates, err)
	}

	cleared, err := svc.ClearProviderAccountCooldown(context.Background(), "tester", account.ID)
	if err != nil {
		t.Fatalf("ClearProviderAccountCooldown(): %v", err)
	}
	if cleared.CooldownUntil != nil || cleared.TempUnschedulableReason != "" {
		t.Fatalf("expected cooldown and reason cleared: %+v", cleared)
	}
	candidates, _, err := svc.GatewayProviderCandidatesForModel(context.Background(), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("GatewayProviderCandidatesForModel(): %v", err)
	}
	if len(candidates) != 1 || candidates[0].AccountID != account.ID {
		t.Fatalf("expected account to be schedulable again after clearing cooldown: %+v", candidates)
	}
}

func mustCreateGatewayModelRoutes(t *testing.T, svc *Service, modelID string, accounts []ProviderAccount) GatewayModel {
	t.Helper()
	model, err := svc.CreateGatewayModel(context.Background(), "tester", GatewayModelRequest{
		ModelID: modelID,
		Name:    modelID,
		Status:  GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(%s): %v", modelID, err)
	}
	for index, account := range accounts {
		if _, err := svc.CreateModelRoute(context.Background(), "tester", ModelRouteRequest{
			GatewayModelID:    model.ID,
			RouteGroup:        DefaultModelRouteGroup,
			ProviderAccountID: account.ID,
			UpstreamModel:     modelID,
			Priority:          (index + 1) * 10,
			Weight:            100,
			Status:            ModelRouteStatusActive,
		}); err != nil {
			t.Fatalf("CreateModelRoute(%s, %s): %v", modelID, account.ID, err)
		}
	}
	return model
}

func TestGatewayModelRouteMapsExternalModelAndRouteGroup(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "test-secret-key")
	provider, err := svc.CreateProvider(context.Background(), "tester", ProviderRequest{
		Name: "Mapped provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, Models: []string{"upstream-chat-v2"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	account, err := svc.CreateProviderAccount(context.Background(), "tester", ProviderAccountRequest{
		ProviderID: provider.ID, Name: "Mapped account", Platform: "openai_compatible", AuthType: "api_key",
		Status: AccountStatusActive, Priority: 10, Concurrency: 3, Models: []string{"upstream-chat-v2"}, Secret: "account-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderAccount(): %v", err)
	}
	model, err := svc.CreateGatewayModel(context.Background(), "tester", GatewayModelRequest{
		ModelID: "public-chat", Name: "Public Chat", DefaultRouteGroup: "stable", Status: GatewayModelStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	if _, err := svc.CreateModelRoute(context.Background(), "tester", ModelRouteRequest{
		GatewayModelID: model.ID, RouteGroup: "stable", ProviderAccountID: account.ID,
		UpstreamModel: "upstream-chat-v2", Priority: 10, Weight: 100, Status: ModelRouteStatusActive,
	}); err != nil {
		t.Fatalf("CreateModelRoute(): %v", err)
	}

	for _, requested := range []string{"public-chat", "public-chat:stable"} {
		candidates, hasRoutes, err := svc.GatewayProviderCandidatesForModel(context.Background(), requested)
		if err != nil {
			t.Fatalf("GatewayProviderCandidatesForModel(%s): %v", requested, err)
		}
		if !hasRoutes || len(candidates) != 1 || candidates[0].UpstreamModel != "upstream-chat-v2" || candidates[0].RouteGroup != "stable" {
			t.Fatalf("unexpected mapped candidates for %s: %+v hasRoutes=%v", requested, candidates, hasRoutes)
		}
	}
}
