package controlplane

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestDurableAIJobWorkerAndReconcilerContract(t *testing.T) {
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
			ctx := context.Background()
			base := time.Date(2026, time.July, 14, 15, 0, 0, 0, time.UTC)
			svc := NewService(repo, "/v1", "durable-worker-secret")
			svc.now = func() time.Time { return base }
			setupDurableWorkerRoutes(t, svc)
			responseLost := errors.New("provider response lost")
			adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-accepted", Status: "running"}, ReconcileAfter: base.Add(time.Hour)}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-fallback", Status: "running"}, ReconcileAfter: base.Add(time.Hour)}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeUnknown}, err: responseLost},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
			}, reconcileResult: ProviderDispatchResult{
				Outcome:        ProviderDispatchOutcomeAccepted,
				Task:           ProviderTaskReference{ProviderTaskID: "task-recovered", ProviderRequestID: "request-recovered", Status: "succeeded"},
				ReconcileAfter: base.Add(time.Hour),
			}}

			acceptedJob := beginDurableWorkerJob(t, svc, "worker-accepted")
			report, err := svc.RunDurableAIJobWorkerOnce(ctx, "worker-a", time.Minute, 1, adapter)
			if err != nil || report.Claimed != 1 || report.Accepted != 1 || report.Errors != 0 {
				t.Fatalf("accepted worker report=%+v err=%v", report, err)
			}
			assertAIJobStatus(t, svc, acceptedJob.ID, AIJobStatusRunning)

			fallbackJob := beginDurableWorkerJob(t, svc, "worker-fallback")
			report, err = svc.RunDurableAIJobWorkerOnce(ctx, "worker-b", time.Minute, 1, adapter)
			if err != nil || report.Accepted != 1 || adapter.DispatchCalls() != 3 {
				t.Fatalf("fallback worker report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
			}
			assertAIJobStatus(t, svc, fallbackJob.ID, AIJobStatusRunning)
			attempts := adapter.Attempts()
			if attempts[1].Status != AIAttemptStatusRunning || attempts[2].Status != AIAttemptStatusRunning {
				t.Fatalf("adapter attempts before reload=%+v", attempts)
			}
			firstFallback, found, err := svc.AIAttempt(ctx, attempts[1].ID)
			if err != nil || !found || firstFallback.Status != AIAttemptStatusSkipped || firstFallback.DispatchState != AIAttemptDispatchProvenNotCreated {
				t.Fatalf("first fallback attempt=%+v found=%t err=%v", firstFallback, found, err)
			}

			unknownJob := beginDurableWorkerJob(t, svc, "worker-unknown")
			report, err = svc.RunDurableAIJobWorkerOnce(ctx, "worker-c", time.Minute, 1, adapter)
			if !errors.Is(err, responseLost) || !errors.Is(err, ErrAIAttemptRequiresReconciliation) || report.Unknown != 1 || report.Errors != 1 {
				t.Fatalf("unknown worker report=%+v err=%v", report, err)
			}
			assertAIJobStatus(t, svc, unknownJob.ID, AIJobStatusUnknown)
			if adapter.DispatchCalls() != 4 {
				t.Fatalf("dispatch calls=%d, want 4", adapter.DispatchCalls())
			}

			exhaustedJob := beginDurableWorkerJob(t, svc, "worker-exhausted")
			report, err = svc.RunDurableAIJobWorkerOnce(ctx, "worker-d", time.Minute, 1, adapter)
			if err != nil || report.Requeued != 1 || report.Errors != 0 || adapter.DispatchCalls() != 6 {
				t.Fatalf("exhausted worker report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
			}
			exhausted, found, err := svc.repo.FindAIJob(ctx, exhaustedJob.ID)
			if err != nil || !found || exhausted.Status != AIJobStatusQueued || !exhausted.NextEligibleAt.After(base) {
				t.Fatalf("exhausted job=%+v found=%t err=%v", exhausted, found, err)
			}
			svc.now = func() time.Time { return base.Add(time.Second) }
			if claimed, err := svc.ClaimReadyAIJobs(ctx, "early-retry", time.Minute, 1); err != nil || len(claimed) != 0 {
				t.Fatalf("early retry claimed=%+v err=%v", claimed, err)
			}

			svc.now = func() time.Time { return base.Add(2 * time.Minute) }
			reconcileReport, err := svc.RunDurableAIJobReconcilerOnce(ctx, 10, adapter)
			if err != nil || reconcileReport.Reconciled != 1 || reconcileReport.Completed != 1 || reconcileReport.Errors != 0 || adapter.ReconcileCalls() != 1 {
				t.Fatalf("reconcile report=%+v calls=%d err=%v", reconcileReport, adapter.ReconcileCalls(), err)
			}
			assertAIJobStatus(t, svc, unknownJob.ID, AIJobStatusSucceeded)
			unknownAttempt := adapter.Attempts()[3]
			completedAttempt, found, err := svc.AIAttempt(ctx, unknownAttempt.ID)
			if err != nil || !found || completedAttempt.Status != AIAttemptStatusSucceeded || completedAttempt.ProviderTaskID != "task-recovered" {
				t.Fatalf("completed attempt=%+v found=%t err=%v", completedAttempt, found, err)
			}
			if adapter.DispatchCalls() != 6 {
				t.Fatalf("reconciler invoked provider create; calls=%d", adapter.DispatchCalls())
			}
		})
	}
}

func TestDurableAIJobWorkerRequiresAdapter(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, err := svc.RunDurableAIJobWorkerOnce(context.Background(), "worker", time.Minute, 1, nil); !errors.Is(err, ErrDurableAIJobAdapterRequired) {
		t.Fatalf("worker nil adapter error=%v", err)
	}
	if _, err := svc.RunDurableAIJobReconcilerOnce(context.Background(), 1, nil); !errors.Is(err, ErrDurableAIJobAdapterRequired) {
		t.Fatalf("reconciler nil adapter error=%v", err)
	}
}

func setupDurableWorkerRoutes(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	provider, err := svc.CreateProvider(ctx, "test", ProviderRequest{
		Name: "Durable provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1",
		Status: ProviderStatusActive, Models: []string{"worker-upstream-a", "worker-upstream-b"}, APIKey: "provider-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts := make([]ProviderAccount, 0, 2)
	for index, upstream := range []string{"worker-upstream-a", "worker-upstream-b"} {
		account, createErr := svc.CreateProviderAccount(ctx, "test", ProviderAccountRequest{
			ProviderID: provider.ID, Name: "Durable account " + upstream, Platform: "openai_compatible", AuthType: "api_key",
			Status: AccountStatusActive, Models: []string{upstream}, Secret: "account-secret-" + upstream, Concurrency: 2, Priority: 100 - index,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		accounts = append(accounts, account)
	}
	model, err := svc.CreateGatewayModel(ctx, "test", GatewayModelRequest{ModelID: "worker-image", Name: "Worker image", Modality: "image", Status: GatewayModelStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	for index, account := range accounts {
		if _, err := svc.CreateModelRoute(ctx, "test", ModelRouteRequest{
			GatewayModelID: model.ID, RouteGroup: DefaultModelRouteGroup, ProviderAccountID: account.ID,
			UpstreamModel: []string{"worker-upstream-a", "worker-upstream-b"}[index], Priority: 100 - index, Weight: 100, Status: ModelRouteStatusActive,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func beginDurableWorkerJob(t *testing.T, svc *Service, idempotencyKey string) AIJob {
	t.Helper()
	request := gatewaycore.CanonicalRequest{
		ID: "request-" + idempotencyKey, ClientRequestID: "client-" + idempotencyKey,
		Fingerprint: "fingerprint-" + idempotencyKey, IdempotencyKey: idempotencyKey,
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: "image_generation", Modality: "image",
		Lane: gatewaycore.LaneDurable, Model: "worker-image", Payload: []byte(`{"model":"worker-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	}
	auth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "worker-key", ProfileScope: ProfileScopePlatform,
		TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal", ArtifactPolicy: GatewayArtifactPolicyTemporary,
	}
	job, created, err := svc.BeginDurableAIJob(context.Background(), auth, request)
	if err != nil || !created {
		t.Fatalf("BeginDurableAIJob() job=%+v created=%t err=%v", job, created, err)
	}
	return job
}

func assertAIJobStatus(t *testing.T, svc *Service, jobID, status string) {
	t.Helper()
	job, found, err := svc.repo.FindAIJob(context.Background(), jobID)
	if err != nil || !found || job.Status != status {
		t.Fatalf("job=%+v found=%t err=%v, want status %s", job, found, err, status)
	}
}

type durableDispatchStep struct {
	result ProviderDispatchResult
	err    error
}

type durableAIJobAdapterStub struct {
	mu              sync.Mutex
	dispatchSteps   []durableDispatchStep
	dispatchCalls   int
	reconcileCalls  int
	attempts        []AIAttempt
	reconcileResult ProviderDispatchResult
	reconcileErr    error
}

func (s *durableAIJobAdapterStub) DispatchProviderTask(_ context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, command ProviderDispatchCommand) (ProviderDispatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if provider.APIKey == "" || job.RequestPayload == "" || len(command.Payload) == 0 || command.Intent.AttemptID != attempt.ID {
		return ProviderDispatchResult{}, errors.New("incomplete durable dispatch command")
	}
	index := s.dispatchCalls
	s.dispatchCalls++
	s.attempts = append(s.attempts, attempt)
	if index >= len(s.dispatchSteps) {
		return ProviderDispatchResult{}, errors.New("unexpected durable dispatch call")
	}
	return s.dispatchSteps[index].result, s.dispatchSteps[index].err
}

func (s *durableAIJobAdapterStub) ReconcileProviderTask(_ context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, intent ProviderDispatchIntent, _ ProviderTaskReference) (ProviderDispatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if provider.APIKey == "" || job.ID == "" || intent.AttemptID != attempt.ID {
		return ProviderDispatchResult{}, errors.New("incomplete durable reconcile command")
	}
	s.reconcileCalls++
	return s.reconcileResult, s.reconcileErr
}

func (s *durableAIJobAdapterStub) DispatchCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dispatchCalls
}

func (s *durableAIJobAdapterStub) ReconcileCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reconcileCalls
}

func (s *durableAIJobAdapterStub) Attempts() []AIAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AIAttempt(nil), s.attempts...)
}
