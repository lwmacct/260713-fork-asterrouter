package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func assertEmptyList[T any](t *testing.T, name string, load func() ([]T, error)) {
	t.Helper()
	items, err := load()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if items == nil {
		t.Fatalf("%s returned a nil slice", name)
	}
	if len(items) != 0 {
		t.Fatalf("%s returned %d items, want 0", name, len(items))
	}
}

func TestPostgresRepositoryEmptyListContracts(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	defer repo.Close()

	assertEmptyList(t, "ListDepartments", func() ([]Department, error) { return repo.ListDepartments(ctx) })
	assertEmptyList(t, "ListOrganizationGroups", func() ([]OrganizationGroup, error) { return repo.ListOrganizationGroups(ctx) })
	assertEmptyList(t, "ListGovernancePolicies", func() ([]GovernancePolicy, error) { return repo.ListGovernancePolicies(ctx) })
	assertEmptyList(t, "ListWorkspaceUsers", func() ([]WorkspaceUser, error) { return repo.ListWorkspaceUsers(ctx) })
	assertEmptyList(t, "ListRoleBindings", func() ([]RoleBinding, error) { return repo.ListRoleBindings(ctx) })
	assertEmptyList(t, "ListProviders", func() ([]ProviderConnection, error) { return repo.ListProviders(ctx) })
	assertEmptyList(t, "ListLatestProviderHealthChecks", func() ([]ProviderHealthCheck, error) { return repo.ListLatestProviderHealthChecks(ctx) })
	assertEmptyList(t, "ListRoutingGroups", func() ([]RoutingGroup, error) { return repo.ListRoutingGroups(ctx) })
	assertEmptyList(t, "ListProviderAccounts", func() ([]ProviderAccount, error) { return repo.ListProviderAccounts(ctx) })
	assertEmptyList(t, "ListLatestProviderAccountHealthChecks", func() ([]ProviderAccountHealthCheck, error) { return repo.ListLatestProviderAccountHealthChecks(ctx) })
	assertEmptyList(t, "ListGatewayModels", func() ([]GatewayModel, error) { return repo.ListGatewayModels(ctx) })
	assertEmptyList(t, "ListModelRoutes", func() ([]ModelRoute, error) { return repo.ListModelRoutes(ctx) })
	assertEmptyList(t, "ListModelPricings", func() ([]ModelPricing, error) { return repo.ListModelPricings(ctx) })
	assertEmptyList(t, "ListAPIKeys", func() ([]APIKeyRecord, error) { return repo.ListAPIKeys(ctx) })
	assertEmptyList(t, "ListUsageRecords", func() ([]UsageRecord, error) { return repo.ListUsageRecords(ctx, 100) })
	assertEmptyList(t, "ListGatewayTraces", func() ([]GatewayTrace, error) { return repo.ListGatewayTraces(ctx, 100) })
	assertEmptyList(t, "ListAuditLogs", func() ([]AuditLog, error) { return repo.ListAuditLogs(ctx, 100) })
}

func TestListParsersNormalizeJSONNull(t *testing.T) {
	if values := parseStringList("null"); values == nil || len(values) != 0 {
		t.Fatalf("parseStringList(null) = %#v, want []", values)
	}
	if rules := parseTempUnschedulableRules("null"); rules == nil || len(rules) != 0 {
		t.Fatalf("parseTempUnschedulableRules(null) = %#v, want []", rules)
	}
}

func TestPostgresRepositoryPersistsCoreRecordsAcrossRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	provider := ProviderConnection{
		ID: "provider-postgres", Name: "Postgres Provider", Type: "openai_compatible",
		BaseURL: "https://provider.test/v1", Status: ProviderStatusActive, Models: []string{"model-a"},
		SecretConfigured: true, SecretHint: "...test", SecretCiphertext: "ciphertext", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveProvider(ctx, provider); err != nil {
		t.Fatalf("SaveProvider(): %v", err)
	}
	key := APIKeyRecord{
		ID: "key-postgres", Name: "Postgres Key", KeyHash: "hash-postgres", Fingerprint: "fingerprint",
		Prefix: "ast_test", Status: APIKeyStatusActive, KeyType: APIKeyTypeWorkspace,
		TenantID: gatewayDefaultTenantID, PrincipalType: APIKeyTypeService, PrincipalReference: "service-postgres",
		Scopes: []string{GatewayScopeInvoke}, ModelAllowlist: []string{"model-a"}, AllowedModalities: []string{GatewayModalityText},
		AllowedOperations: []string{GatewayOperationChatCompletion}, QPSLimit: 2, RPMLimit: 30, TPMLimit: 4000,
		ConcurrencyLimit: 3, MonthlyTokenLimit: 5000, MonthlyBudgetCents: 600,
		MonthlyImageLimit: 7, MonthlyVideoSecondsLimit: 8, MonthlyAudioSecondsLimit: 9,
		AllowedCIDRs: []string{"192.0.2.0/24"}, LanePolicy: GatewayLanePolicyDirectAndDurable,
		ArtifactPolicy: GatewayArtifactPolicyManaged, RotationFamilyID: "key-family-postgres", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveAPIKey(ctx, key); err != nil {
		t.Fatalf("SaveAPIKey(): %v", err)
	}
	user := WorkspaceUser{
		ID: "user-postgres", Email: "user-postgres@example.test", DisplayName: "Postgres User",
		Status: WorkspaceUserStatusActive, Role: RoleDeveloper, SessionVersion: 7,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.SaveWorkspaceUser(ctx, user); err != nil {
		t.Fatalf("SaveWorkspaceUser(): %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewPostgresRepository(): %v", err)
	}
	defer reopened.Close()
	providers, err := reopened.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || providers[0].ID != provider.ID || providers[0].SecretCiphertext != "ciphertext" {
		t.Fatalf("persisted providers = %#v", providers)
	}
	found, ok, err := reopened.FindAPIKeyByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("FindAPIKeyByHash(): %v", err)
	}
	if !ok || found.ID != key.ID || len(found.ModelAllowlist) != 1 || found.ModelAllowlist[0] != "model-a" ||
		found.TenantID != gatewayDefaultTenantID || found.PrincipalReference != "service-postgres" || found.RPMLimit != 30 ||
		found.TPMLimit != 4000 || found.ConcurrencyLimit != 3 || found.MonthlyBudgetCents != 600 ||
		found.MonthlyImageLimit != 7 || found.MonthlyVideoSecondsLimit != 8 || found.MonthlyAudioSecondsLimit != 9 ||
		len(found.AllowedCIDRs) != 1 || found.LanePolicy != GatewayLanePolicyDirectAndDurable ||
		found.ArtifactPolicy != GatewayArtifactPolicyManaged || found.RotationFamilyID != "key-family-postgres" {
		t.Fatalf("persisted key ok=%t key=%#v", ok, found)
	}
	users, err := reopened.ListWorkspaceUsers(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceUsers(): %v", err)
	}
	if len(users) != 1 || users[0].ID != user.ID || users[0].SessionVersion != 7 {
		t.Fatalf("persisted session version users=%#v", users)
	}
}

func TestUsageMonthlyBoundaryContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			boundary := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
			records := []UsageRecord{
				{ID: "before", APIKeyID: "key-boundary", InputTokens: 40, CostCents: 4, CreatedAt: boundary.Add(-time.Microsecond)},
				{ID: "at", APIKeyID: "key-boundary", InputTokens: 50, CostCents: 5, CreatedAt: boundary},
				{ID: "after", APIKeyID: "key-boundary", OutputTokens: 60, CostCents: 6, CreatedAt: boundary.Add(time.Microsecond)},
			}
			for _, record := range records {
				if err := repo.SaveUsageRecord(ctx, record); err != nil {
					t.Fatalf("SaveUsageRecord(%s): %v", record.ID, err)
				}
			}
			tokens, err := repo.SumUsageTokensByAPIKeySince(ctx, "key-boundary", boundary)
			if err != nil {
				t.Fatal(err)
			}
			cost, err := repo.SumUsageCostCentsByAPIKeySince(ctx, "key-boundary", boundary)
			if err != nil {
				t.Fatal(err)
			}
			if tokens != 110 || cost != 11 {
				t.Fatalf("monthly aggregate tokens=%d cost=%d", tokens, cost)
			}
		})
	}
}

func TestPostgresCustomerRedeemIsAtomicUnderConcurrentRequests(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewPostgresRepository(): %v", err)
	}
	defer repo.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	users := []WorkspaceUser{
		{ID: "redeem-user-a", Email: "redeem-a@example.test", DisplayName: "Redeem A", Status: WorkspaceUserStatusActive, Role: RoleDeveloper, CreatedAt: now, UpdatedAt: now},
		{ID: "redeem-user-b", Email: "redeem-b@example.test", DisplayName: "Redeem B", Status: WorkspaceUserStatusActive, Role: RoleDeveloper, CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if err := repo.SaveWorkspaceUser(ctx, user); err != nil {
			t.Fatalf("SaveWorkspaceUser(%s): %v", user.ID, err)
		}
	}
	const code = "POSTGRES-CONCURRENT-REDEEM"
	if err := repo.SaveCustomerRedemptionCode(ctx, CustomerRedemptionCode{
		ID: "redeem-code", CodeHash: hashCustomerRedemptionCode(code), Title: "Concurrent redemption",
		AmountCents: 500, Status: CustomerRedemptionCodeActive, MaxRedemptions: 1, CreatedAt: now,
	}); err != nil {
		t.Fatalf("SaveCustomerRedemptionCode(): %v", err)
	}

	type result struct {
		userID string
		entry  CustomerBillingEntry
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, len(users))
	var workers sync.WaitGroup
	for index, user := range users {
		workers.Add(1)
		go func(index int, user WorkspaceUser) {
			defer workers.Done()
			<-start
			entry, err := repo.RedeemCustomerCode(ctx, CustomerCodeRedemption{
				UserID: user.ID, CodeHash: hashCustomerRedemptionCode(code), EntryID: fmt.Sprintf("redeem-entry-%d", index), Now: now,
			})
			results <- result{userID: user.ID, entry: entry, err: err}
		}(index, user)
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	for result := range results {
		if result.err == nil {
			successes++
			if result.entry.AmountCents != 500 || result.entry.UserID != result.userID {
				t.Fatalf("successful redemption = %+v", result.entry)
			}
			continue
		}
		if !errors.Is(result.err, ErrCustomerCodeUnavailable) {
			t.Fatalf("concurrent redemption error = %v, want ErrCustomerCodeUnavailable", result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent redemptions = %d, want 1", successes)
	}

	var redeemedCount, redemptionRows, entryRows int
	if err := repo.db.QueryRowContext(ctx, `SELECT redeemed_count FROM customer_redemption_codes WHERE id = 'redeem-code'`).Scan(&redeemedCount); err != nil {
		t.Fatalf("read redemption code: %v", err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT count(*) FROM customer_redemptions WHERE code_id = 'redeem-code'`).Scan(&redemptionRows); err != nil {
		t.Fatalf("count redemptions: %v", err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT count(*) FROM customer_billing_entries WHERE reference = 'redeem-code'`).Scan(&entryRows); err != nil {
		t.Fatalf("count billing entries: %v", err)
	}
	if redeemedCount != 1 || redemptionRows != 1 || entryRows != 1 {
		t.Fatalf("redemption persistence counts redeemed=%d redemptions=%d entries=%d", redeemedCount, redemptionRows, entryRows)
	}
}
