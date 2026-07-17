package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestAIAttemptProviderDispatchContract(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
			svc.now = func() time.Time { return base }
			ctx := context.Background()
			operation, _, err := svc.BeginCanonicalOperation(ctx, operationTestAuth(), operationTestRequest("dispatch-idem", "dispatch-fingerprint"))
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.MarkAIOperationRunning(ctx, operation.ID); err != nil {
				t.Fatal(err)
			}
			provider := GatewayProvider{ID: "provider-dispatch", AccountID: "account-dispatch", AdapterID: "adapter-dispatch", RouteID: "route-dispatch", UpstreamModel: "upstream-dispatch"}
			attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, provider)
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := svc.BeginAIAttempt(ctx, operation.ID, 1, provider)
			if err != nil || replayed.ID != attempt.ID {
				t.Fatalf("BeginAIAttempt replay=%+v err=%v", replayed, err)
			}
			adapterChanged := provider
			adapterChanged.AdapterID = "adapter-must-not-replace-snapshot"
			replayed, err = svc.BeginAIAttempt(ctx, operation.ID, 1, adapterChanged)
			if err != nil || replayed.ProviderAdapterID != provider.AdapterID {
				t.Fatalf("BeginAIAttempt adapter replay=%+v err=%v", replayed, err)
			}
			if _, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{ID: provider.ID, AccountID: "different-account", RouteID: provider.RouteID, UpstreamModel: provider.UpstreamModel}); !errors.Is(err, ErrAIAttemptDispatchConflict) {
				t.Fatalf("conflicting attempt provider error=%v", err)
			}

			prepared, changed, err := svc.PrepareAIAttemptDispatch(ctx, attempt.ID)
			if err != nil || !changed || prepared.DispatchState != AIAttemptDispatchPrepared || prepared.DispatchVersion != 1 {
				t.Fatalf("PrepareAIAttemptDispatch() attempt=%+v changed=%t err=%v", prepared, changed, err)
			}
			if prepared.ProviderAdapterID != provider.AdapterID || prepared.DispatchIntentJSON == "" || !strings.Contains(prepared.DispatchIntentJSON, provider.AdapterID) || strings.Contains(prepared.DispatchIntentJSON, "prompt") || strings.Contains(prepared.DispatchIntentJSON, "secret") {
				t.Fatalf("dispatch intent contains sensitive request data: %s", prepared.DispatchIntentJSON)
			}
			preparedReplay, changed, err := svc.PrepareAIAttemptDispatch(ctx, attempt.ID)
			if err != nil || changed || preparedReplay.ID != attempt.ID {
				t.Fatalf("PrepareAIAttemptDispatch replay=%+v changed=%t err=%v", preparedReplay, changed, err)
			}

			submitted, changed, err := svc.MarkAIAttemptDispatchSubmitted(ctx, attempt.ID, prepared.DispatchVersion, base.Add(time.Minute))
			if err != nil || !changed || submitted.DispatchState != AIAttemptDispatchSubmitted || submitted.DispatchVersion != 2 {
				t.Fatalf("MarkAIAttemptDispatchSubmitted() attempt=%+v changed=%t err=%v", submitted, changed, err)
			}
			unknown, changed, err := svc.MarkAIAttemptDispatchUnknown(ctx, attempt.ID, submitted.DispatchVersion, base.Add(2*time.Minute))
			if err != nil || !changed || unknown.DispatchState != AIAttemptDispatchUnknown || unknown.DispatchVersion != 3 {
				t.Fatalf("MarkAIAttemptDispatchUnknown() attempt=%+v changed=%t err=%v", unknown, changed, err)
			}

			reference := ProviderTaskReference{ProviderTaskID: "provider-task-1", ProviderRequestID: "provider-request-1", Status: "queued"}
			accepted, changed, err := svc.BindAIAttemptProviderTask(ctx, attempt.ID, unknown.DispatchVersion, reference, base.Add(3*time.Minute))
			if err != nil || !changed || accepted.DispatchState != AIAttemptDispatchAccepted || accepted.ProviderTaskID != reference.ProviderTaskID || accepted.DispatchVersion != 4 {
				t.Fatalf("BindAIAttemptProviderTask() attempt=%+v changed=%t err=%v", accepted, changed, err)
			}
			acceptedReplay, changed, err := svc.BindAIAttemptProviderTask(ctx, attempt.ID, accepted.DispatchVersion, reference, base.Add(3*time.Minute))
			if err != nil || changed || acceptedReplay.ProviderTaskID != reference.ProviderTaskID {
				t.Fatalf("BindAIAttemptProviderTask replay=%+v changed=%t err=%v", acceptedReplay, changed, err)
			}
			if _, _, err := svc.BindAIAttemptProviderTask(ctx, attempt.ID, accepted.DispatchVersion, ProviderTaskReference{ProviderTaskID: "different-task"}, base.Add(3*time.Minute)); !errors.Is(err, ErrAIAttemptDispatchConflict) {
				t.Fatalf("conflicting provider task error=%v", err)
			}
			unknownAfterAccepted, changed, err := svc.MarkAIAttemptDispatchUnknown(ctx, attempt.ID, accepted.DispatchVersion, base.Add(3*time.Minute))
			if err != nil || !changed || unknownAfterAccepted.DispatchState != AIAttemptDispatchUnknown {
				t.Fatalf("accepted task unknown=%+v changed=%t err=%v", unknownAfterAccepted, changed, err)
			}
			reconfirmed, changed, err := svc.BindAIAttemptProviderTask(ctx, attempt.ID, unknownAfterAccepted.DispatchVersion, reference, base.Add(3*time.Minute))
			if err != nil || !changed || reconfirmed.DispatchState != AIAttemptDispatchAccepted || reconfirmed.ProviderTaskID != reference.ProviderTaskID {
				t.Fatalf("reconfirmed provider task=%+v changed=%t err=%v", reconfirmed, changed, err)
			}

			svc.now = func() time.Time { return base.Add(2 * time.Minute) }
			if ready, err := svc.AIAttemptsForReconciliation(ctx, 10); err != nil || len(ready) != 0 {
				t.Fatalf("early AIAttemptsForReconciliation() attempts=%+v err=%v", ready, err)
			}
			svc.now = func() time.Time { return base.Add(4 * time.Minute) }
			ready, err := svc.AIAttemptsForReconciliation(ctx, 10)
			if err != nil || len(ready) != 1 || ready[0].ProviderTaskID != reference.ProviderTaskID {
				t.Fatalf("AIAttemptsForReconciliation() attempts=%+v err=%v", ready, err)
			}
			durableReady, err := svc.DurableAIAttemptsForReconciliation(ctx, 10)
			if err != nil || len(durableReady) != 0 {
				t.Fatalf("direct attempt leaked into durable reconciliation: attempts=%+v err=%v", durableReady, err)
			}
			observed, changed, err := svc.RecordAIAttemptReconciliation(ctx, attempt.ID, reconfirmed.DispatchVersion, "running", base.Add(10*time.Minute))
			if err != nil || !changed || observed.ProviderTaskStatus != "running" || observed.DispatchVersion != reconfirmed.DispatchVersion+1 {
				t.Fatalf("RecordAIAttemptReconciliation() attempt=%+v changed=%t err=%v", observed, changed, err)
			}
		})
	}
}

func TestAIOperationFreezesArtifactPolicyAcrossReplay(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
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
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			auth := operationTestAuth()
			auth.ArtifactPolicy = GatewayArtifactPolicyManaged
			request := operationTestRequest("operation-policy-idem", "operation-policy-fingerprint")
			operation, created, err := svc.BeginCanonicalOperation(context.Background(), auth, request)
			if err != nil || !created || operation.ArtifactPolicy != GatewayArtifactPolicyManaged {
				t.Fatalf("BeginCanonicalOperation() operation=%+v created=%t err=%v", operation, created, err)
			}
			auth.ArtifactPolicy = GatewayArtifactPolicyProxyOnly
			replayed, created, err := svc.BeginCanonicalOperation(context.Background(), auth, request)
			if err != nil || created || replayed.ID != operation.ID || replayed.ArtifactPolicy != GatewayArtifactPolicyManaged {
				t.Fatalf("replay operation=%+v created=%t err=%v", replayed, created, err)
			}
			persisted, found, err := repo.FindAIOperation(context.Background(), operation.ID)
			if err != nil || !found || persisted.ArtifactPolicy != GatewayArtifactPolicyManaged {
				t.Fatalf("FindAIOperation() operation=%+v found=%t err=%v", persisted, found, err)
			}
		})
	}
}

func TestAIOperationArtifactPolicySnapshotFailsClosed(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	auth := operationTestAuth()
	auth.ArtifactPolicy = "unknown_policy"
	operation, _, err := svc.BeginCanonicalOperation(context.Background(), auth, operationTestRequest("policy-fail-closed", "policy-fail-closed-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	if operation.ArtifactPolicy != GatewayArtifactPolicyProxyOnly {
		t.Fatalf("artifact policy snapshot=%q, want %q", operation.ArtifactPolicy, GatewayArtifactPolicyProxyOnly)
	}
}

func TestAIOperationFreezesCustomerArtifactSinkAcrossReplay(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	auth := operationTestAuth()
	auth.ArtifactPolicy = GatewayArtifactPolicyCustomerSink
	auth.ArtifactSinkID = "sink-snapshot-v1"
	request := operationTestRequest("sink-policy-idem", "sink-policy-fingerprint")
	operation, created, err := svc.BeginCanonicalOperation(context.Background(), auth, request)
	if err != nil || !created || operation.ArtifactSinkID != "sink-snapshot-v1" {
		t.Fatalf("operation=%+v created=%t err=%v", operation, created, err)
	}
	auth.ArtifactSinkID = "sink-snapshot-v2"
	replayed, created, err := svc.BeginCanonicalOperation(context.Background(), auth, request)
	if err != nil || created || replayed.ID != operation.ID || replayed.ArtifactSinkID != "sink-snapshot-v1" {
		t.Fatalf("replay=%+v created=%t err=%v", replayed, created, err)
	}
}

func TestAIAttemptCreationIsAtomicAcrossConcurrentInstances(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) (Repository, Repository)
	}{
		{name: "memory", open: func(*testing.T) (Repository, Repository) {
			repo := NewMemoryRepository()
			return repo, repo
		}},
		{name: "postgres", open: func(t *testing.T) (Repository, Repository) {
			schema := testutil.NewPostgresSchema(t)
			first, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatal(err)
			}
			second, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				_ = first.Close()
				t.Fatal(err)
			}
			return first, second
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, second := test.open(t)
			t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
			firstService := NewService(first, "/v1")
			secondService := NewService(second, "/v1")
			operation, _, err := firstService.BeginCanonicalOperation(context.Background(), operationTestAuth(), operationTestRequest("attempt-concurrent", "attempt-concurrent-fingerprint"))
			if err != nil {
				t.Fatal(err)
			}
			provider := GatewayProvider{ID: "provider", AccountID: "account", RouteID: "route", UpstreamModel: "model"}
			var ids sync.Map
			errorsSeen := make(chan error, 20)
			var wait sync.WaitGroup
			for index := 0; index < 20; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					service := firstService
					if index%2 == 1 {
						service = secondService
					}
					attempt, beginErr := service.BeginAIAttempt(context.Background(), operation.ID, 1, provider)
					if beginErr != nil {
						errorsSeen <- beginErr
						return
					}
					ids.Store(attempt.ID, struct{}{})
				}(index)
			}
			wait.Wait()
			close(errorsSeen)
			for err := range errorsSeen {
				t.Errorf("BeginAIAttempt(): %v", err)
			}
			count := 0
			ids.Range(func(_, _ any) bool { count++; return true })
			if count != 1 {
				t.Fatalf("distinct attempt ids=%d, want 1", count)
			}
		})
	}
}

func TestAIAttemptDispatchPersistsAcrossPostgresRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, "/v1")
	base := time.Date(2026, time.July, 14, 14, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	operation, _, err := svc.BeginCanonicalOperation(ctx, operationTestAuth(), operationTestRequest("restart-dispatch", "restart-dispatch-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkAIOperationRunning(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{ID: "provider-restart", AccountID: "account-restart", AdapterID: "adapter-restart", RouteID: "route-restart", UpstreamModel: "model-restart"})
	if err != nil {
		t.Fatal(err)
	}
	executor := &providerDispatchExecutorStub{result: ProviderDispatchResult{
		Outcome:        ProviderDispatchOutcomeAccepted,
		Task:           ProviderTaskReference{ProviderTaskID: "task-restart", ProviderRequestID: "request-restart", Status: "running"},
		ReconcileAfter: base.Add(time.Minute),
	}}
	persisted, _, err := svc.ExecuteAIAttemptDispatch(ctx, attempt.ID, []byte(`{"input":"synthetic"}`), executor)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewService(reopened, "/v1")
	restarted.now = func() time.Time { return base.Add(2 * time.Minute) }
	found, ok, err := restarted.AIAttempt(ctx, persisted.ID)
	if err != nil || !ok || found.DispatchState != AIAttemptDispatchAccepted || found.ProviderTaskID != "task-restart" || found.ProviderAdapterID != "adapter-restart" || found.DispatchIntentJSON == "" {
		t.Fatalf("restarted attempt=%+v found=%t err=%v", found, ok, err)
	}
	due, err := restarted.AIAttemptsForReconciliation(ctx, 10)
	if err != nil || len(due) != 1 || due[0].ID != persisted.ID {
		t.Fatalf("restarted reconciliation=%+v err=%v", due, err)
	}
}

func TestOperationUsageLedgerContract(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			ctx := context.Background()
			auth := operationTestAuth()
			request := operationTestRequest("idem-ledger", "fingerprint-ledger")
			publishTestUsagePricingRule(t, svc, `v1: fixed_line("usage", "request", 3)`)

			operation, created, err := svc.BeginCanonicalOperation(ctx, auth, request)
			if err != nil || !created {
				t.Fatalf("BeginCanonicalOperation() operation=%+v created=%t err=%v", operation, created, err)
			}
			if err := svc.MarkAIOperationRunning(ctx, operation.ID); err != nil {
				t.Fatalf("MarkAIOperationRunning(): %v", err)
			}
			attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{ID: "provider-1", AccountID: "account-1", RouteID: "route-1", UpstreamModel: "upstream-model"})
			if err != nil {
				t.Fatalf("BeginAIAttempt(): %v", err)
			}
			input := GatewayUsageInput{
				OperationID: operation.ID, AttemptID: attempt.ID, UsageVersion: 1, UsageSource: "upstream_final",
				RequestFingerprint: request.Fingerprint, Model: request.Model, Status: "forwarded", InputTokens: 7, OutputTokens: 11,
				UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 2, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported}},
			}
			if err := svc.RecordGatewayUsage(ctx, operationTestLegacyAuth(), input); err != nil {
				t.Fatalf("RecordGatewayUsage(first): %v", err)
			}
			if err := svc.RecordGatewayUsage(ctx, operationTestLegacyAuth(), input); err != nil {
				t.Fatalf("RecordGatewayUsage(replay): %v", err)
			}
			usage, err := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
			if err != nil || len(usage) != 1 || usage[0].OperationID != operation.ID || usage[0].AttemptID != attempt.ID || usage[0].UsageVersion != 1 || usage[0].UsageDimensions[UsageDimensionOutputImages].Quantity != 2 {
				t.Fatalf("usage=%+v err=%v", usage, err)
			}
			billing, err := repo.ListBillingLedgerEntries(ctx, operation.ID)
			if err != nil || len(billing) != 1 || billing[0].AmountMicros != 3 || billing[0].UsageRecordID != usage[0].ID {
				t.Fatalf("billing=%+v err=%v", billing, err)
			}
			outbox, err := repo.ListTransactionalOutboxEvents(ctx, "")
			if err != nil || len(outbox) != 1 || outbox[0].EventType != OutboxEventUsageRecorded || outbox[0].Status != OutboxStatusPending {
				t.Fatalf("outbox=%+v err=%v", outbox, err)
			}

			conflict := input
			conflict.RequestFingerprint = "different-fingerprint"
			if err := svc.RecordGatewayUsage(ctx, operationTestLegacyAuth(), conflict); !errors.Is(err, ErrUsageLedgerConflict) {
				t.Fatalf("conflicting usage error=%v", err)
			}
			dimensionConflict := input
			dimensionConflict.UsageDimensions = UsageDimensions{UsageDimensionOutputImages: {Quantity: 3, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported}}
			if err := svc.RecordGatewayUsage(ctx, operationTestLegacyAuth(), dimensionConflict); !errors.Is(err, ErrUsageLedgerConflict) {
				t.Fatalf("conflicting usage dimensions error=%v", err)
			}
		})
	}
}

func TestOperationIdempotencyIsAtomicAcrossConcurrentRequests(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			var createdCount atomic.Int32
			var firstID atomic.Value
			errorsSeen := make(chan error, 20)
			var wait sync.WaitGroup
			for index := 0; index < 20; index++ {
				wait.Add(1)
				go func() {
					defer wait.Done()
					operation, created, err := svc.BeginCanonicalOperation(context.Background(), operationTestAuth(), operationTestRequest("idem-concurrent", "fingerprint-concurrent"))
					if err != nil {
						errorsSeen <- err
						return
					}
					if created {
						createdCount.Add(1)
						firstID.Store(operation.ID)
					}
				}()
			}
			wait.Wait()
			close(errorsSeen)
			for err := range errorsSeen {
				t.Errorf("BeginCanonicalOperation(): %v", err)
			}
			if createdCount.Load() != 1 || firstID.Load() == nil {
				t.Fatalf("created operations=%d first=%v", createdCount.Load(), firstID.Load())
			}
			if _, _, err := svc.BeginCanonicalOperation(context.Background(), operationTestAuth(), operationTestRequest("idem-concurrent", "different-fingerprint")); !errors.Is(err, ErrGatewayIdempotencyConflict) {
				t.Fatalf("fingerprint conflict error=%v", err)
			}
		})
	}
}

func TestOperationIdempotencyScopeIncludesPrincipalAndOperation(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			request := operationTestRequest("shared-idempotency-key", "fingerprint-a")
			firstAuth := operationTestAuth()
			first, created, err := svc.BeginCanonicalOperation(context.Background(), firstAuth, request)
			if err != nil || !created {
				t.Fatalf("first operation=%+v created=%t err=%v", first, created, err)
			}
			principalAuth := firstAuth
			principalAuth.PrincipalID = "different-principal"
			principalRequest := request
			principalRequest.Fingerprint = "fingerprint-b"
			principal, created, err := svc.BeginCanonicalOperation(context.Background(), principalAuth, principalRequest)
			if err != nil || !created || principal.ID == first.ID {
				t.Fatalf("principal operation=%+v created=%t err=%v", principal, created, err)
			}
			operationRequest := request
			operationRequest.Operation = "different_operation"
			operationRequest.Fingerprint = "fingerprint-c"
			operation, created, err := svc.BeginCanonicalOperation(context.Background(), firstAuth, operationRequest)
			if err != nil || !created || operation.ID == first.ID {
				t.Fatalf("operation-scoped result=%+v created=%t err=%v", operation, created, err)
			}
		})
	}
}

func TestUsageLedgerTransactionRollsBackOnOutboxConflict(t *testing.T) {
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
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1")
			ctx := context.Background()
			publishTestUsagePricingRule(t, svc, `v1: fixed_line("free", "request", 0)`)
			operation, _, err := svc.BeginCanonicalOperation(ctx, operationTestAuth(), operationTestRequest("", "rollback-fingerprint"))
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{})
			if err != nil {
				t.Fatal(err)
			}
			base := UsageRecord{ID: "usage-base", OperationID: operation.ID, AttemptID: attempt.ID, UsageVersion: 1, UsageSource: "synthetic", RequestFingerprint: "rollback-fingerprint", APIKeyID: "key-1", APIFingerprint: "fingerprint", Model: "model-a", Status: "forwarded", UsageCostCurrency: "USD", PricingStatus: "unpriced", CreatedAt: svc.nowUTC()}
			settlement, err := svc.buildUsageSettlement(ctx, base)
			if err != nil {
				t.Fatal(err)
			}
			settlement.OutboxEvents[0].ID = "outbox-shared"
			if applied, err := repo.ApplyUsageSettlement(ctx, settlement); err != nil || !applied {
				t.Fatalf("ApplyUsageSettlement(base) applied=%t err=%v", applied, err)
			}
			second := base
			second.ID = "usage-second"
			second.UsageVersion = 2
			secondSettlement, err := svc.buildUsageSettlement(ctx, second)
			if err != nil {
				t.Fatal(err)
			}
			secondSettlement.OutboxEvents[0].ID = "outbox-shared"
			if applied, err := repo.ApplyUsageSettlement(ctx, secondSettlement); err == nil || applied {
				t.Fatalf("ApplyUsageSettlement(conflict) applied=%t err=%v", applied, err)
			}
			usage, _ := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
			billingEntries, _ := repo.ListBillingLedgerEntries(ctx, operation.ID)
			if len(usage) != 1 || len(billingEntries) != 1 {
				t.Fatalf("rollback usage=%d billing=%d", len(usage), len(billingEntries))
			}
		})
	}
}

func operationTestAuth() gatewaycore.CanonicalAuthContext {
	return gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "key-operation", ProfileScope: "platform",
		TenantID: "tenant-operation", PrincipalType: APIKeyTypeService, PrincipalID: "principal-operation",
	}
}

func operationTestLegacyAuth() GatewayAuthContext {
	return GatewayAuthContext{APIKey: APIKeyRecord{ID: "key-operation", Fingerprint: "fingerprint-operation", KeyType: APIKeyTypeService}}
}

func operationTestRequest(idempotencyKey, fingerprint string) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ClientRequestID: "request-operation", Fingerprint: fingerprint, IdempotencyKey: idempotencyKey,
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion, Modality: GatewayModalityText,
		Lane: gatewaycore.LaneDirect, Model: "model-a",
	}
}
