package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
			dispatchPercent := 10
			reconcilePercent := 100
			svc := NewService(repo, "/v1", "durable-worker-secret")
			svc.now = func() time.Time { return base }
			if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
				t.Fatal(err)
			}
			setupDurableWorkerRoutes(t, svc)
			responseLost := errors.New("provider response lost")
			adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-accepted", Status: "running"}, Progress: &ProviderProgressObservation{Sequence: 1, Percent: &dispatchPercent, Stage: "generating"}, ReconcileAfter: base.Add(time.Hour)}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-fallback", Status: "running"}, ReconcileAfter: base.Add(time.Hour)}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeUnknown}, err: responseLost},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
				{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}},
			}, reconcileResult: ProviderDispatchResult{
				Outcome:         ProviderDispatchOutcomeAccepted,
				Task:            ProviderTaskReference{ProviderTaskID: "task-recovered", ProviderRequestID: "request-recovered", Status: "succeeded"},
				Progress:        &ProviderProgressObservation{Sequence: 1, Percent: &reconcilePercent, Stage: "completed"},
				Outputs:         []ProviderOutputDescriptor{{OutputID: "final-image", Role: ArtifactRoleFinal, MediaType: "image/png", ExpectedSizeBytes: -1}},
				UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 99, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported}},
				ReconcileAfter:  base.Add(time.Hour),
			}}

			acceptedJob := beginDurableWorkerJob(t, svc, "worker-accepted")
			report, err := svc.RunDurableAIJobWorkerOnce(ctx, "worker-a", time.Minute, 1, adapter)
			if err != nil || report.Claimed != 1 || report.Accepted != 1 || report.Errors != 0 {
				t.Fatalf("accepted worker report=%+v err=%v", report, err)
			}
			assertAIJobStatus(t, svc, acceptedJob.ID, AIJobStatusRunning)
			assertBillingHoldStatus(t, svc, acceptedJob.OperationID, BillingHoldStatusCommitted)
			progressEvents, progressErr := svc.AIJobProgressEvents(ctx, acceptedJob.ID)
			if progressErr != nil || len(progressEvents) != 1 || progressEvents[0].Percent == nil || *progressEvents[0].Percent != 10 {
				t.Fatalf("accepted progress=%+v err=%v", progressEvents, progressErr)
			}

			fallbackJob := beginDurableWorkerJob(t, svc, "worker-fallback")
			report, err = svc.RunDurableAIJobWorkerOnce(ctx, "worker-b", time.Minute, 1, adapter)
			if err != nil || report.Accepted != 1 || adapter.DispatchCalls() != 3 {
				t.Fatalf("fallback worker report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
			}
			assertAIJobStatus(t, svc, fallbackJob.ID, AIJobStatusRunning)
			assertBillingHoldStatus(t, svc, fallbackJob.OperationID, BillingHoldStatusCommitted)
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
			assertBillingHoldStatus(t, svc, unknownJob.OperationID, BillingHoldStatusDisputed)
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
			assertBillingHoldStatus(t, svc, exhaustedJob.OperationID, BillingHoldStatusReserved)
			svc.now = func() time.Time { return base.Add(time.Second) }
			if claimed, err := svc.ClaimReadyAIJobs(ctx, "early-retry", time.Minute, 1); err != nil || len(claimed) != 0 {
				t.Fatalf("early retry claimed=%+v err=%v", claimed, err)
			}

			svc.now = func() time.Time { return base.Add(2 * time.Minute) }
			accounts, err := svc.repo.ListProviderAccounts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, account := range accounts {
				account.Status = AccountStatusDisabled
				account.Schedulable = false
				if err := svc.repo.SaveProviderAccount(ctx, account); err != nil {
					t.Fatal(err)
				}
			}
			reconcileReport, err := svc.RunDurableAIJobReconcilerOnce(ctx, 10, adapter)
			if err != nil || reconcileReport.Reconciled != 1 || reconcileReport.Completed != 1 || reconcileReport.Errors != 0 || adapter.ReconcileCalls() != 1 {
				t.Fatalf("reconcile report=%+v calls=%d err=%v", reconcileReport, adapter.ReconcileCalls(), err)
			}
			assertAIJobStatus(t, svc, unknownJob.ID, AIJobStatusSucceeded)
			assertBillingHoldStatus(t, svc, unknownJob.OperationID, BillingHoldStatusSettled)
			progressEvents, progressErr = svc.AIJobProgressEvents(ctx, unknownJob.ID)
			if progressErr != nil || len(progressEvents) != 1 || progressEvents[0].Percent == nil || *progressEvents[0].Percent != 100 || progressEvents[0].Stage != "completed" {
				t.Fatalf("reconciled progress=%+v err=%v", progressEvents, progressErr)
			}
			unknownAttempt := adapter.Attempts()[3]
			completedAttempt, found, err := svc.AIAttempt(ctx, unknownAttempt.ID)
			if err != nil || !found || completedAttempt.Status != AIAttemptStatusSucceeded || completedAttempt.ProviderTaskID != "task-recovered" {
				t.Fatalf("completed attempt=%+v found=%t err=%v", completedAttempt, found, err)
			}
			if adapter.DispatchCalls() != 6 {
				t.Fatalf("reconciler invoked provider create; calls=%d", adapter.DispatchCalls())
			}
			usage, usageErr := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
			if usageErr != nil || len(usage) != 1 || usage[0].OperationID != unknownJob.OperationID || usage[0].AttemptID != unknownAttempt.ID || usage[0].UsageDimensions[UsageDimensionOutputImages].Quantity != 1 {
				t.Fatalf("durable usage=%+v err=%v", usage, usageErr)
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

func TestDurableAIJobWorkerRejectsInvalidProviderProgress(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "invalid-progress-secret")
	base := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "invalid-provider-progress")
	percent := 101
	adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
		Outcome:        ProviderDispatchOutcomeAccepted,
		Task:           ProviderTaskReference{ProviderTaskID: "invalid-progress-task", Status: "running"},
		Progress:       &ProviderProgressObservation{Sequence: 1, Percent: &percent, Stage: "rendering"},
		ReconcileAfter: base.Add(time.Hour),
	}}}}
	report, err := svc.RunDurableAIJobWorkerOnce(ctx, "invalid-progress-worker", time.Minute, 1, adapter)
	if !errors.Is(err, ErrAIJobProgressInvalid) || report.Accepted != 1 || report.Errors != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusRunning)
	events, listErr := svc.AIJobProgressEvents(ctx, job.ID)
	if listErr != nil || len(events) != 0 {
		t.Fatalf("events=%+v err=%v", events, listErr)
	}
	attempts, listErr := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if listErr != nil || len(attempts) != 1 || attempts[0].ReconcileAfter == nil || !attempts[0].ReconcileAfter.Equal(base.Add(time.Hour)) {
		t.Fatalf("attempts=%+v err=%v", attempts, listErr)
	}
}

func TestDurableAIJobReconcilerUsesProviderCancellationWhenJobIsCanceling(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "cancellation-secret")
	base := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "provider-cancellation")
	adapter := &durableCancellationAdapter{
		durableAIJobAdapterStub: &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
			Outcome:        ProviderDispatchOutcomeAccepted,
			Task:           ProviderTaskReference{ProviderTaskID: "cancel-task", ProviderRequestID: "cancel-request", Status: "running"},
			ReconcileAfter: base.Add(-time.Minute),
		}}}},
		cancelResult: ProviderDispatchResult{
			Outcome: ProviderDispatchOutcomeAccepted,
			Task:    ProviderTaskReference{ProviderTaskID: "cancel-task", ProviderRequestID: "cancel-request", Status: "canceled"},
			Billing: ProviderBillingObservation{Status: ProviderBillingStatusNotCharged},
		},
	}
	if report, err := svc.RunDurableAIJobWorkerOnce(ctx, "cancel-worker", time.Minute, 1, adapter); err != nil || report.Accepted != 1 {
		t.Fatalf("worker report=%+v err=%v", report, err)
	}
	auth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "worker-key", ProfileScope: ProfileScopePlatform,
		TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal", ArtifactPolicy: GatewayArtifactPolicyTemporary,
	}
	canceling, found, err := svc.CancelAIJobForAuth(ctx, auth, job.ID)
	if err != nil || !found || canceling.Status != AIJobStatusCanceling {
		t.Fatalf("canceling=%+v found=%t err=%v", canceling, found, err)
	}
	svc.now = func() time.Time { return base.Add(2 * time.Minute) }
	if report, err := svc.RunDurableAIJobReconcilerOnce(ctx, 1, adapter); err != nil || report.Completed != 1 || report.Errors != 0 {
		t.Fatalf("reconcile report=%+v err=%v", report, err)
	}
	current, found, err := repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || current.Status != AIJobStatusCanceled {
		t.Fatalf("current job=%+v found=%t err=%v", current, found, err)
	}
	if adapter.cancelCalls != 1 || adapter.ReconcileCalls() != 0 {
		t.Fatalf("cancel calls=%d reconcile calls=%d", adapter.cancelCalls, adapter.ReconcileCalls())
	}
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusSettled)
}

func TestDurableAIJobProviderBillingResolutionAfterFailure(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "durable-billing-secret")
	base := time.Date(2026, time.July, 15, 14, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	publishTestUsagePricingRule(t, svc, `v1: unit_line("output_images", output_images, "image", 42)`)
	job := beginDurableWorkerJob(t, svc, "provider-failure-billing-resolution")
	cost := int64(42)
	adapter := &durableAIJobAdapterStub{
		dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
			Outcome: ProviderDispatchOutcomeAccepted,
			Task:    ProviderTaskReference{ProviderTaskID: "failed-task", ProviderRequestID: "failed-request", Status: "failed"},
		}}},
		reconcileResult: ProviderDispatchResult{
			Outcome: ProviderDispatchOutcomeAccepted,
			Task:    ProviderTaskReference{ProviderTaskID: "failed-task", ProviderRequestID: "failed-request", Status: "failed"},
			UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {
				Quantity: 1, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported,
			}},
			Billing: ProviderBillingObservation{
				Status: ProviderBillingStatusFinal, ProcurementCostMicros: &cost, Currency: "USD",
				Source: "provider_invoice", Confidence: ProcurementCostConfidenceExact,
			},
		},
	}
	if report, err := svc.RunDurableAIJobWorkerOnce(ctx, "billing-worker", time.Minute, 1, adapter); err != nil || report.Claimed != 1 || report.Errors != 0 {
		t.Fatalf("initial worker report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusFailed)
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusDisputed)
	attempts, err := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != AIAttemptStatusRunning {
		t.Fatalf("unresolved attempt=%+v err=%v", attempts, err)
	}
	usage, err := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
	if err != nil || len(usage) != 0 {
		t.Fatalf("unresolved usage=%+v err=%v", usage, err)
	}

	svc.now = func() time.Time { return base.Add(2 * time.Minute) }
	if report, err := svc.RunDurableAIJobReconcilerOnce(ctx, 1, adapter); err != nil || report.Completed != 1 || report.Errors != 0 {
		t.Fatalf("resolved reconcile report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusFailed)
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusSettled)
	attempts, err = repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != AIAttemptStatusFailed {
		t.Fatalf("resolved attempt=%+v err=%v", attempts, err)
	}
	usage, err = repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
	if err != nil || len(usage) != 1 || usage[0].UsageDimensions[UsageDimensionOutputImages].Quantity != 1 || usage[0].ProcurementCostMicros == nil || *usage[0].ProcurementCostMicros != cost {
		t.Fatalf("resolved usage=%+v err=%v", usage, err)
	}
	entries, err := repo.ListBillingLedgerEntries(ctx, job.OperationID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("billing ledger entries=%+v err=%v", entries, err)
	}

	currentJob, found, err := repo.FindAIJob(ctx, job.ID)
	if err != nil || !found {
		t.Fatalf("find terminal job err=%v", err)
	}
	currentAttempt := attempts[0]
	if _, err := svc.applyAcceptedProviderProgress(ctx, GatewayProvider{ID: currentAttempt.ProviderID}, currentJob, currentAttempt, adapter.reconcileResult, adapter); err != nil {
		t.Fatalf("replayed terminal progress err=%v", err)
	}
	usage, err = repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
	if err != nil || len(usage) != 1 {
		t.Fatalf("replayed usage=%+v err=%v", usage, err)
	}
}

func TestDurableAIJobCanceledNotChargedSettlesZeroUsage(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "durable-canceled-secret")
	base := time.Date(2026, time.July, 15, 15, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	limits := gatewaycore.CanonicalLimits{MonthlyImageLimit: 1}
	job := beginDurableWorkerJobWithLimits(t, svc, "provider-canceled-not-charged", limits)
	adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "canceled-task", Status: "canceled"},
		Billing: ProviderBillingObservation{Status: ProviderBillingStatusNotCharged},
	}}}}
	claimed, err := svc.ClaimReadyAIJobs(ctx, "canceled-worker", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("canceled claim=%+v err=%v", claimed, err)
	}
	cancelAuth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "worker-key", ProfileScope: ProfileScopePlatform,
		TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal",
	}
	canceling, found, err := svc.CancelAIJobForAuth(ctx, cancelAuth, job.ID)
	if err != nil || !found || canceling.Status != AIJobStatusCanceling {
		t.Fatalf("canceling job=%+v found=%t err=%v", canceling, found, err)
	}
	if outcome, err := svc.dispatchClaimedAIJob(ctx, claimed[0], adapter); err != nil || outcome != AIJobStatusCanceled {
		t.Fatalf("canceled dispatch outcome=%s err=%v", outcome, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusCanceled)
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusSettled)
	attempts, err := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != AIAttemptStatusCanceled {
		t.Fatalf("canceled attempts=%+v err=%v", attempts, err)
	}
	usage, err := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
	if err != nil || len(usage) != 1 || len(usage[0].UsageDimensions) != 0 || usage[0].ProcurementCostMicros == nil || *usage[0].ProcurementCostMicros != 0 {
		t.Fatalf("canceled usage=%+v err=%v", usage, err)
	}
	second, created, err := svc.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "worker-key", ProfileScope: ProfileScopePlatform,
		TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal", Limits: limits,
		ArtifactPolicy: GatewayArtifactPolicyTemporary,
	}, gatewaycore.CanonicalRequest{
		ID: "request-after-cancel", ClientRequestID: "client-after-cancel", Fingerprint: "fingerprint-after-cancel",
		IdempotencyKey: "after-cancel", Protocol: gatewaycore.ProtocolAsterJobs, Operation: GatewayOperationImageGeneration,
		Modality: GatewayModalityImage, Lane: gatewaycore.LaneDurable, Model: "worker-image",
		Payload: []byte(`{"model":"worker-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	})
	if err != nil || !created || second.ID == "" {
		t.Fatalf("quota after not-charged cancellation job=%+v created=%t err=%v", second, created, err)
	}
}

func TestFinalizeAIOperationTerminalBillingDoesNotResolveRecordError(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	cost := int64(0)
	resolved, err := svc.finalizeAIOperationTerminalBilling(context.Background(), AIOperation{
		ID: "operation-invalid-credential", CredentialSource: "unsupported_source",
	}, AIAttempt{ID: "attempt-invalid-credential"}, AIJobStatusFailed, ProviderDispatchResult{
		Billing: ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: &cost, Currency: "USD", Source: "provider", Confidence: ProcurementCostConfidenceExact},
	}, 1)
	if resolved || err == nil {
		t.Fatalf("record error resolved=%t err=%v", resolved, err)
	}
}

func TestDurableAIJobFinalUsageEnqueuesPlatformDimensions(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "durable-platform-usage-secret")
	base := time.Date(2026, time.July, 15, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	identity := createExternalAuthIdentity(t, ctx, svc)
	integration, err := svc.CreateExternalAuthIntegration(ctx, "operator", ExternalAuthIntegrationRequest{
		TenantID: identity.tenant.ID, GatewayPrincipalID: identity.principal.ID, Name: "Durable media product",
		KeyID: "durable-media-v1", Audience: "https://gateway.example/v1", ModelAllowlist: []string{"worker-image"},
		QPSLimit: 10, MonthlyTokenLimit: 1000, MaxTTLSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := svc.CreatePlatformUsageSink(ctx, "operator", PlatformUsageSinkRequest{
		TenantID: identity.tenant.ID, ExternalAuthIntegrationID: integration.Record.ID,
		Name: "Durable usage callback", EndpointURL: "https://billing.example/usage", MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject := "opaque-durable-subject"
	subjectHash := hashAPIKey(integration.Record.ID + "\x00" + subject)
	auth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceHMACContext,
		CredentialID:     "eai_subject_" + prefix(subjectHash, 32), CredentialFingerprint: prefix(subjectHash, 12),
		IntegrationID: integration.Record.ID, ProfileScope: ProfileScopePlatform,
		TenantID: identity.tenant.ID, PrincipalType: identity.principal.PrincipalType, PrincipalID: identity.principal.ID,
		ExternalSubjectReference: subject, ArtifactPolicy: GatewayArtifactPolicyTemporary,
	}
	request := gatewaycore.CanonicalRequest{
		ID: "durable-platform-request", ClientRequestID: "durable-platform-client", Fingerprint: "durable-platform-fingerprint",
		IdempotencyKey: "durable-platform-idempotency", Protocol: gatewaycore.ProtocolAsterJobs,
		Operation: GatewayOperationImageGeneration, Modality: GatewayModalityImage, Lane: gatewaycore.LaneDurable,
		Model: "worker-image", OutputCount: 1,
		Payload: []byte(`{"model":"worker-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	}
	job, created, err := svc.BeginDurableAIJob(ctx, auth, request)
	if err != nil || !created {
		t.Fatalf("BeginDurableAIJob() job=%+v created=%t err=%v", job, created, err)
	}
	procurementCost := int64(31)
	adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
		Outcome:         ProviderDispatchOutcomeAccepted,
		Task:            ProviderTaskReference{ProviderTaskID: "platform-task", ProviderRequestID: "platform-request", Status: "succeeded"},
		Outputs:         []ProviderOutputDescriptor{{OutputID: "platform-image", Role: ArtifactRoleFinal, MediaType: "image/png", ExpectedSizeBytes: -1}},
		UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 7, Unit: UsageUnitCount, Source: "provider", Confidence: UsageConfidenceReported}},
		Billing:         ProviderBillingObservation{Status: ProviderBillingStatusFinal, ProcurementCostMicros: &procurementCost, Currency: "USD", Source: "provider_invoice", Confidence: ProcurementCostConfidenceExact},
	}}}}
	if report, err := svc.RunDurableAIJobWorkerOnce(ctx, "platform-worker", time.Minute, 1, adapter); err != nil || report.Claimed != 1 {
		t.Fatalf("worker report=%+v err=%v", report, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusSucceeded)
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusSettled)
	events, err := svc.ListPlatformUsageDeliveryEvents(ctx, PlatformUsageDeliveryQuery{SinkID: sink.Record.ID})
	if err != nil || len(events) != 1 {
		t.Fatalf("platform usage events=%+v err=%v", events, err)
	}
	var payload platformUsageEventPayload
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &payload); err != nil || payload.ExternalSubjectRef != subject || payload.UsageDimensions[UsageDimensionOutputImages].Quantity != 1 {
		t.Fatalf("platform usage payload=%+v err=%v raw=%s", payload, err, events[0].PayloadJSON)
	}
	usage, err := repo.QueryUsageRecords(ctx, UsageQuery{Limit: 10})
	if err != nil || len(usage) != 1 || usage[0].ProcurementCostMicros == nil || *usage[0].ProcurementCostMicros != procurementCost {
		t.Fatalf("successful provider procurement cost usage=%+v err=%v", usage, err)
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
			Status: AccountStatusActive, Models: []string{upstream}, Secret: "account-secret-" + upstream, Concurrency: 10, Priority: 100 - index,
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
	return beginDurableWorkerJobWithLimits(t, svc, idempotencyKey, gatewaycore.CanonicalLimits{})
}

func beginDurableWorkerJobWithLimits(t *testing.T, svc *Service, idempotencyKey string, limits gatewaycore.CanonicalLimits) AIJob {
	t.Helper()
	request := gatewaycore.CanonicalRequest{
		ID: "request-" + idempotencyKey, ClientRequestID: "client-" + idempotencyKey,
		Fingerprint: "fingerprint-" + idempotencyKey, IdempotencyKey: idempotencyKey,
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: "image_generation", Modality: "image",
		Lane: gatewaycore.LaneDurable, Model: "worker-image", Payload: []byte(`{"model":"worker-image","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	}
	auth := gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "worker-key", ProfileScope: ProfileScopePlatform,
		TenantID: "worker-tenant", PrincipalType: APIKeyTypeService, PrincipalID: "worker-principal", Limits: limits, ArtifactPolicy: GatewayArtifactPolicyTemporary,
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

func assertBillingHoldStatus(t *testing.T, svc *Service, operationID, status string) {
	t.Helper()
	hold, found, err := svc.BillingHoldForOperation(context.Background(), operationID)
	if err != nil || !found || hold.Status != status {
		t.Fatalf("billing hold=%+v found=%t err=%v, want status %s", hold, found, err, status)
	}
}

type durableDispatchStep struct {
	result ProviderDispatchResult
	err    error
}

type durableCancellationAdapter struct {
	*durableAIJobAdapterStub
	cancelResult ProviderDispatchResult
	cancelCalls  int
}

func (adapter *durableCancellationAdapter) SupportsDurableAIJobCancellation(context.Context, GatewayProvider, AIJob, AIAttempt) (bool, error) {
	return true, nil
}

func (adapter *durableCancellationAdapter) CancelProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error) {
	adapter.cancelCalls++
	return adapter.cancelResult, nil
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

func (s *durableAIJobAdapterStub) OpenProviderOutput(_ context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, output ProviderOutputDescriptor) (io.ReadCloser, error) {
	if provider.APIKey == "" || job.ID == "" || attempt.ID == "" || output.OutputID == "" {
		return nil, errors.New("incomplete provider output command")
	}
	return io.NopCloser(bytes.NewBufferString("provider-output-" + output.OutputID)), nil
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
