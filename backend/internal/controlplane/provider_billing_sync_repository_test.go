package controlplane

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

type providerBillingRepositoryFactory struct {
	name string
	open func(*testing.T) Repository
}

func providerBillingRepositoryFactories() []providerBillingRepositoryFactory {
	return []providerBillingRepositoryFactory{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatal(err)
			}
			return repo
		}},
	}
}

func TestProviderBillingSourceRepositoryClaimCommitContract(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 15, 16, 0, 0, 0, time.UTC)
			if err := repo.SaveProvider(ctx, ProviderConnection{ID: "billing-provider", Name: "Billing provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SaveProviderAccount(ctx, ProviderAccount{ID: "billing-account", ProviderID: "billing-provider", Name: "Billing account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, SecretCiphertext: "ciphertext", SecretConfigured: true, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			source := ProviderBillingSource{ID: "billing-source", ProviderID: "billing-provider", ProviderAccountID: "billing-account", AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceObserveOnly, AutomaticSyncEnabled: true, SyncIntervalSeconds: 3600, CreatedAt: now, UpdatedAt: now}
			if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
				t.Fatalf("create source applied=%t err=%v", applied, err)
			}
			claimed, err := repo.ClaimProviderBillingSources(ctx, ProviderBillingSourceClaimRequest{Trigger: ProviderBillingSyncTriggerScheduled, TriggeredBy: "scheduler", Now: now, LeaseDuration: time.Minute, Limit: 1})
			if err != nil || len(claimed) != 1 || claimed[0].Source.LeaseToken == "" || claimed[0].Run.Status != ProviderBillingSyncRunning {
				t.Fatalf("claimed=%+v err=%v", claimed, err)
			}
			actualCost := int64(321)
			completed := now.Add(time.Second)
			applied, err := repo.CommitProviderBillingSync(ctx, ProviderBillingSyncCommit{
				SourceID: claimed[0].Source.ID, LeaseToken: claimed[0].Source.LeaseToken,
				Run:        ProviderBillingSyncRun{ID: claimed[0].Run.ID, Status: ProviderBillingSyncSucceeded, Capabilities: ProviderBillingSourceCapabilities{AggregateUsage: true, Balance: true}, DetectionStatus: ProviderBillingDetectionSchemaMatch, ContractVersion: "test_v1", EvidenceHash: "hash", Warnings: []string{"synthetic"}, StartedAt: now},
				Balance:    &ProviderBalanceSnapshotRecord{ID: "balance-1", Kind: ProviderBalanceKindWallet, AmountMicros: 1000000, Currency: "USD", EvidenceHash: "hash", ObservedAt: now, CreatedAt: now},
				Aggregates: []ProviderUsageAggregateSnapshot{{ID: "aggregate-1", Scope: "today", Model: "model-a", ActualCostMicros: &actualCost, Currency: "USD", EvidenceHash: "hash", ObservedAt: now, CreatedAt: now}},
				Cursor:     "cursor-1", NextSyncAt: timePointer(completed.Add(time.Hour)), CompletedAt: completed,
			})
			if err != nil || !applied {
				t.Fatalf("commit applied=%t err=%v", applied, err)
			}
			found, ok, err := repo.FindProviderBillingSource(ctx, "billing-account")
			if err != nil || !ok || found.LeaseToken != "" || found.Cursor != "cursor-1" || found.LastSuccessAt == nil || found.ConsecutiveFailures != 0 {
				t.Fatalf("source=%+v found=%t err=%v", found, ok, err)
			}
			runs, err := repo.ListProviderBillingSyncRuns(ctx, source.ID, 10)
			if err != nil || len(runs) != 1 || runs[0].Status != ProviderBillingSyncSucceeded || runs[0].FinishedAt == nil {
				t.Fatalf("runs=%+v err=%v", runs, err)
			}
			balances, err := repo.ListProviderBalanceSnapshots(ctx, source.ID, 10)
			if err != nil || len(balances) != 1 || balances[0].AmountMicros != 1000000 {
				t.Fatalf("balances=%+v err=%v", balances, err)
			}
			aggregates, err := repo.ListProviderUsageAggregateSnapshots(ctx, source.ID, 10)
			if err != nil || len(aggregates) != 1 || aggregates[0].ActualCostMicros == nil || *aggregates[0].ActualCostMicros != actualCost {
				t.Fatalf("aggregates=%+v err=%v", aggregates, err)
			}
			if again, err := repo.ClaimProviderBillingSources(ctx, ProviderBillingSourceClaimRequest{Trigger: ProviderBillingSyncTriggerScheduled, Now: now, Limit: 1}); err != nil || len(again) != 0 {
				t.Fatalf("claimed source before due again=%+v err=%v", again, err)
			}
		})
	}
}

func TestProviderBillingSourceRepositoryCASContract(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 15, 17, 0, 0, 0, time.UTC)
			source := seedProviderBillingSourceRepository(t, repo, now, "cas")
			version := source.Version
			source.AutomaticSyncEnabled = true
			source.UpdatedAt = now.Add(time.Minute)
			if applied, err := repo.UpsertProviderBillingSource(ctx, source, &version); err != nil || !applied {
				t.Fatalf("CAS update applied=%t err=%v", applied, err)
			}
			source.Status = ProviderBillingSourceDisabled
			if applied, err := repo.UpsertProviderBillingSource(ctx, source, &version); err != nil || applied {
				t.Fatalf("stale CAS update applied=%t err=%v", applied, err)
			}
			stored, ok, err := repo.FindProviderBillingSource(ctx, source.ID)
			if err != nil || !ok || stored.Version != version+1 || !stored.AutomaticSyncEnabled || stored.Status == ProviderBillingSourceDisabled {
				t.Fatalf("stored source=%+v found=%t err=%v", stored, ok, err)
			}
		})
	}
}

func TestProviderBillingSourceRepositoryListsLatestBalancePerSource(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 15, 17, 30, 0, 0, time.UTC)
			first := seedProviderBillingSourceRepository(t, repo, now, "latest-a")
			second := seedProviderBillingSourceRepository(t, repo, now, "latest-b")
			commitProviderBillingBalance(t, repo, first, now, "latest-a-old", 100)
			commitProviderBillingBalance(t, repo, first, now.Add(time.Minute), "latest-a-new", 200)
			commitProviderBillingBalance(t, repo, second, now, "latest-b-only", 300)

			balances, err := repo.ListLatestProviderBalanceSnapshots(ctx)
			if err != nil || len(balances) != 2 {
				t.Fatalf("latest balances=%+v err=%v", balances, err)
			}
			amountBySource := map[string]int64{}
			for _, balance := range balances {
				amountBySource[balance.SourceID] = balance.AmountMicros
			}
			if amountBySource[first.ID] != 200 || amountBySource[second.ID] != 300 {
				t.Fatalf("latest amounts=%v", amountBySource)
			}
		})
	}
}

func TestProviderBillingSourceRepositoryConcurrentClaimContract(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			now := time.Date(2026, time.July, 15, 18, 0, 0, 0, time.UTC)
			seedProviderBillingSourceRepository(t, repo, now, "concurrent")
			start := make(chan struct{})
			results := make(chan int, 2)
			errorsFound := make(chan error, 2)
			var wait sync.WaitGroup
			for index := 0; index < 2; index++ {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					claims, err := repo.ClaimProviderBillingSources(context.Background(), ProviderBillingSourceClaimRequest{Trigger: ProviderBillingSyncTriggerManual, Now: now, LeaseDuration: time.Minute, Limit: 1})
					if err != nil {
						errorsFound <- err
						return
					}
					results <- len(claims)
				}()
			}
			close(start)
			wait.Wait()
			close(results)
			close(errorsFound)
			for err := range errorsFound {
				t.Fatalf("concurrent claim: %v", err)
			}
			total := 0
			for count := range results {
				total += count
			}
			if total != 1 {
				t.Fatalf("claimed source count = %d, want 1", total)
			}
		})
	}
}

func TestProviderBillingSourceRepositoryLeaseRecoveryContract(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 15, 19, 0, 0, 0, time.UTC)
			source := seedProviderBillingSourceRepository(t, repo, now, "lease")
			first := claimProviderBillingSource(t, repo, source.ID, now, time.Minute)
			if applied, err := repo.CommitProviderBillingSync(ctx, ProviderBillingSyncCommit{SourceID: source.ID, LeaseToken: first.Source.LeaseToken, Run: ProviderBillingSyncRun{ID: first.Run.ID, Status: ProviderBillingSyncSucceeded}, NextSyncAt: timePointer(now.Add(time.Hour)), CompletedAt: now.Add(2 * time.Minute)}); err != nil || applied {
				t.Fatalf("expired completion applied=%t err=%v", applied, err)
			}
			second := claimProviderBillingSource(t, repo, source.ID, now.Add(2*time.Minute), time.Minute)
			if second.Source.LeaseToken == first.Source.LeaseToken {
				t.Fatal("recovered claim reused expired lease token")
			}
			if applied, err := repo.CommitProviderBillingSync(ctx, ProviderBillingSyncCommit{SourceID: source.ID, LeaseToken: first.Source.LeaseToken, Run: ProviderBillingSyncRun{ID: first.Run.ID, Status: ProviderBillingSyncSucceeded}, NextSyncAt: timePointer(now.Add(time.Hour)), CompletedAt: now.Add(2*time.Minute + time.Second)}); err != nil || applied {
				t.Fatalf("stale completion applied=%t err=%v", applied, err)
			}
			runs, err := repo.ListProviderBillingSyncRuns(ctx, source.ID, 10)
			if err != nil || len(runs) != 2 {
				t.Fatalf("runs=%+v err=%v", runs, err)
			}
			statuses := map[string]int{}
			for _, run := range runs {
				statuses[run.Status]++
			}
			if statuses[ProviderBillingSyncLeaseExpired] != 1 || statuses[ProviderBillingSyncRunning] != 1 {
				t.Fatalf("run statuses=%v", statuses)
			}
		})
	}
}

func TestProviderBillingSourceRepositoryFailedCommitIsAtomic(t *testing.T) {
	for _, test := range providerBillingRepositoryFactories() {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			ctx := context.Background()
			now := time.Date(2026, time.July, 15, 20, 0, 0, 0, time.UTC)
			source := seedProviderBillingSourceRepository(t, repo, now, "failure")
			claim := claimProviderBillingSource(t, repo, source.ID, now, time.Minute)
			invalid := ProviderBillingSyncCommit{
				SourceID: source.ID, LeaseToken: claim.Source.LeaseToken,
				Run:        ProviderBillingSyncRun{ID: claim.Run.ID, Status: ProviderBillingSyncFailed, ErrorCode: "upstream_unavailable"},
				Aggregates: []ProviderUsageAggregateSnapshot{{ID: "must-not-persist", Scope: "today", Currency: "USD"}},
				NextSyncAt: timePointer(now.Add(time.Hour)), CompletedAt: now.Add(time.Second),
			}
			if applied, err := repo.CommitProviderBillingSync(ctx, invalid); err == nil || applied {
				t.Fatalf("invalid failed commit applied=%t err=%v", applied, err)
			}
			runs, _ := repo.ListProviderBillingSyncRuns(ctx, source.ID, 10)
			aggregates, _ := repo.ListProviderUsageAggregateSnapshots(ctx, source.ID, 10)
			stored, _, _ := repo.FindProviderBillingSource(ctx, source.ID)
			if len(runs) != 1 || runs[0].Status != ProviderBillingSyncRunning || len(aggregates) != 0 || stored.LeaseToken == "" || stored.ConsecutiveFailures != 0 {
				t.Fatalf("state changed after rejected commit: runs=%+v aggregates=%+v source=%+v", runs, aggregates, stored)
			}
			invalid.Aggregates = nil
			if applied, err := repo.CommitProviderBillingSync(ctx, invalid); err != nil || !applied {
				t.Fatalf("valid failed commit applied=%t err=%v", applied, err)
			}
			stored, _, _ = repo.FindProviderBillingSource(ctx, source.ID)
			if stored.LeaseToken != "" || stored.ConsecutiveFailures != 1 || stored.LastErrorCode != "upstream_unavailable" {
				t.Fatalf("failed source state=%+v", stored)
			}
		})
	}
}

func TestProviderBillingSourcePostgresRestartPersistence(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 16, 1, 0, 0, 0, time.UTC)
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	source := seedProviderBillingSourceRepository(t, repo, now, "restart")
	claim := claimProviderBillingSource(t, repo, source.ID, now, time.Minute)
	if applied, err := repo.CommitProviderBillingSync(ctx, ProviderBillingSyncCommit{
		SourceID: source.ID, LeaseToken: claim.Source.LeaseToken,
		Run:        ProviderBillingSyncRun{ID: claim.Run.ID, Status: ProviderBillingSyncSucceeded, DetectionStatus: ProviderBillingDetectionSchemaMatch, ContractVersion: "restart_v1", EvidenceHash: "restart-hash"},
		Balance:    &ProviderBalanceSnapshotRecord{ID: "restart-balance", Kind: ProviderBalanceKindWallet, AmountMicros: 2_000_000, Currency: "USD", ObservedAt: now, CreatedAt: now},
		Aggregates: []ProviderUsageAggregateSnapshot{{ID: "restart-aggregate", Scope: "total", Currency: "USD", RequestCount: 2, ObservedAt: now, CreatedAt: now}},
		NextSyncAt: timePointer(now.Add(time.Hour)), CompletedAt: now.Add(time.Second),
	}); err != nil || !applied {
		t.Fatalf("commit applied=%t err=%v", applied, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, found, err := reopened.FindProviderBillingSource(ctx, source.ID)
	if err != nil || !found || stored.LastSuccessAt == nil || stored.EvidenceHash != "restart-hash" {
		t.Fatalf("restarted source=%+v found=%t err=%v", stored, found, err)
	}
	runs, _ := reopened.ListProviderBillingSyncRuns(ctx, source.ID, 10)
	balances, _ := reopened.ListProviderBalanceSnapshots(ctx, source.ID, 10)
	aggregates, _ := reopened.ListProviderUsageAggregateSnapshots(ctx, source.ID, 10)
	if len(runs) != 1 || runs[0].Status != ProviderBillingSyncSucceeded || len(balances) != 1 || balances[0].AmountMicros != 2_000_000 || len(aggregates) != 1 || aggregates[0].RequestCount != 2 {
		t.Fatalf("restarted evidence runs=%+v balances=%+v aggregates=%+v", runs, balances, aggregates)
	}
}

func seedProviderBillingSourceRepository(t *testing.T, repo Repository, now time.Time, suffix string) ProviderBillingSource {
	t.Helper()
	ctx := context.Background()
	providerID := "billing-provider-" + suffix
	accountID := "billing-account-" + suffix
	if err := repo.SaveProvider(ctx, ProviderConnection{ID: providerID, Name: "Billing provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProviderAccount(ctx, ProviderAccount{ID: accountID, ProviderID: providerID, Name: "Billing account", Platform: "openai_compatible", AuthType: "api_key", Status: AccountStatusActive, SecretCiphertext: "ciphertext", SecretConfigured: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	source := ProviderBillingSource{ID: "billing-source-" + suffix, ProviderID: providerID, ProviderAccountID: accountID, AdapterID: ProviderBillingAdapterSub2APICompatible, Status: ProviderBillingSourceObserveOnly, AutomaticSyncEnabled: true, SyncIntervalSeconds: 3600, Version: 1, CreatedAt: now, UpdatedAt: now}
	if applied, err := repo.UpsertProviderBillingSource(ctx, source, nil); err != nil || !applied {
		t.Fatalf("create source applied=%t err=%v", applied, err)
	}
	return source
}

func claimProviderBillingSource(t *testing.T, repo Repository, sourceID string, now time.Time, lease time.Duration) ProviderBillingSourceClaim {
	t.Helper()
	claims, err := repo.ClaimProviderBillingSources(context.Background(), ProviderBillingSourceClaimRequest{SourceID: sourceID, Trigger: ProviderBillingSyncTriggerManual, TriggeredBy: "tester", Now: now, LeaseDuration: lease, Limit: 1})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim source=%s claims=%+v err=%v", sourceID, claims, err)
	}
	return claims[0]
}

func commitProviderBillingBalance(t *testing.T, repo Repository, source ProviderBillingSource, now time.Time, id string, amount int64) {
	t.Helper()
	claim := claimProviderBillingSource(t, repo, source.ID, now, time.Minute)
	applied, err := repo.CommitProviderBillingSync(context.Background(), ProviderBillingSyncCommit{
		SourceID: source.ID, LeaseToken: claim.Source.LeaseToken,
		Run:        ProviderBillingSyncRun{ID: claim.Run.ID, Status: ProviderBillingSyncSucceeded},
		Balance:    &ProviderBalanceSnapshotRecord{ID: id, Kind: ProviderBalanceKindWallet, AmountMicros: amount, Currency: "USD", ObservedAt: now, CreatedAt: now},
		NextSyncAt: timePointer(now.Add(time.Hour)), CompletedAt: now.Add(time.Second),
	})
	if err != nil || !applied {
		t.Fatalf("commit balance %s applied=%t err=%v", id, applied, err)
	}
}
