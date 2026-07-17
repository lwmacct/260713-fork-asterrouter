package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestBillingHoldAdmissionIsAtomicAndIdempotent(t *testing.T) {
	forEachBillingHoldRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		base := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)
		svc := NewService(repo, "/v1", "billing-hold-secret")
		svc.now = func() time.Time { return base }
		saveBillingHoldTestPricing(t, svc, "model-a")
		auth := billingHoldTestAuth("tenant-budget", "credential-budget", 30)

		var createdCount atomic.Int32
		var budgetRejected atomic.Int32
		successes := make(chan gatewaycore.CanonicalRequest, 20)
		rejections := make(chan gatewaycore.CanonicalRequest, 20)
		errorsSeen := make(chan error, 20)
		var wait sync.WaitGroup
		for index := 0; index < 20; index++ {
			request := billingHoldTestRequest(fmt.Sprintf("budget-%d", index))
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, created, err := svc.BeginCanonicalOperation(ctx, auth, request)
				switch {
				case err == nil && created:
					createdCount.Add(1)
					successes <- request
				case errors.Is(err, ErrBillingHoldBudgetExceeded):
					budgetRejected.Add(1)
					rejections <- request
				default:
					errorsSeen <- err
				}
			}()
		}
		wait.Wait()
		close(successes)
		close(rejections)
		close(errorsSeen)
		for err := range errorsSeen {
			t.Errorf("unexpected admission result: %v", err)
		}
		if createdCount.Load() != 3 || budgetRejected.Load() != 17 {
			t.Fatalf("created=%d rejected=%d, want 3/17", createdCount.Load(), budgetRejected.Load())
		}
		holds, err := repo.ListBillingHolds(ctx)
		if err != nil || len(holds) != 3 {
			t.Fatalf("holds=%+v err=%v", holds, err)
		}
		for _, hold := range holds {
			if hold.Status != BillingHoldStatusReserved || hold.ReservedAmountMicros != 10 {
				t.Fatalf("unexpected hold after concurrent admission: %+v", hold)
			}
		}

		success := <-successes
		replayed, created, err := svc.BeginCanonicalOperation(ctx, auth, success)
		if err != nil || created || replayed.ID == "" {
			t.Fatalf("idempotent replay operation=%+v created=%t err=%v", replayed, created, err)
		}
		holds, _ = repo.ListBillingHolds(ctx)
		if len(holds) != 3 {
			t.Fatalf("idempotent replay created %d holds, want 3", len(holds))
		}

		rejected := <-rejections
		largerBudget := auth
		largerBudget.Limits.MonthlyBudgetMicros = 1_000
		_, created, err = svc.BeginCanonicalOperation(ctx, largerBudget, rejected)
		if err != nil || !created {
			t.Fatalf("retry after budget rejection created=%t err=%v", created, err)
		}
		holds, _ = repo.ListBillingHolds(ctx)
		if len(holds) != 4 {
			t.Fatalf("retry after rejection holds=%d, want 4", len(holds))
		}
	})
}

func TestBillingHoldSettlementUsesActualCostAndPreservesIsolation(t *testing.T) {
	forEachBillingHoldRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		base := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)
		svc := NewService(repo, "/v1", "billing-hold-secret")
		svc.now = func() time.Time { return base }
		saveBillingHoldTestPricing(t, svc, "model-a")
		auth := billingHoldTestAuth("tenant-settle", "credential-settle", 100)
		request := billingHoldTestRequest("settle")
		operation, created, err := svc.BeginCanonicalOperation(ctx, auth, request)
		if err != nil || !created {
			t.Fatalf("BeginCanonicalOperation() operation=%+v created=%t err=%v", operation, created, err)
		}
		if err := svc.CommitBillingHold(ctx, operation.ID, "provider_accepted"); err != nil {
			t.Fatal(err)
		}
		usage := GatewayUsageInput{
			OperationID: operation.ID, UsageVersion: 1, UsageSource: "upstream_final",
			RequestFingerprint: operation.RequestFingerprint, Model: operation.Model, Status: "forwarded", OutputTokens: 25,
		}
		legacyAuth := GatewayAuthContext{APIKey: APIKeyRecord{ID: auth.CredentialID, Fingerprint: "billing-fingerprint"}}
		if err := svc.RecordGatewayUsage(ctx, legacyAuth, usage); err != nil {
			t.Fatal(err)
		}
		hold, found, err := svc.BillingHoldForOperation(ctx, operation.ID)
		if err != nil || !found || hold.Status != BillingHoldStatusSettled || hold.ReservedAmountMicros != 10 || hold.SettledAmountMicros != 25 || hold.Version != 3 {
			t.Fatalf("settled hold=%+v found=%t err=%v", hold, found, err)
		}
		if err := svc.RecordGatewayUsage(ctx, legacyAuth, usage); err != nil {
			t.Fatal(err)
		}
		replayed, _, _ := svc.BillingHoldForOperation(ctx, operation.ID)
		if replayed.Version != hold.Version || replayed.SettledAmountMicros != hold.SettledAmountMicros {
			t.Fatalf("usage replay changed hold: before=%+v after=%+v", hold, replayed)
		}
		if _, changed, err := repo.TransitionBillingHold(ctx, operation.ID, hold.Version, BillingHoldStatusReleased, 0, "invalid", base); err == nil || changed {
			t.Fatalf("terminal hold transition changed=%t err=%v", changed, err)
		}

		tightBudget := auth
		tightBudget.Limits.MonthlyBudgetMicros = 30
		if _, _, err := svc.BeginCanonicalOperation(ctx, tightBudget, billingHoldTestRequest("actual-cost-budget")); !errors.Is(err, ErrBillingHoldBudgetExceeded) {
			t.Fatalf("actual settled cost budget error=%v", err)
		}
		otherTenant := billingHoldTestAuth("tenant-other", auth.CredentialID, 10)
		if _, created, err := svc.BeginCanonicalOperation(ctx, otherTenant, billingHoldTestRequest("other-tenant")); err != nil || !created {
			t.Fatalf("other tenant admission created=%t err=%v", created, err)
		}
		otherCredential := billingHoldTestAuth(auth.TenantID, "credential-other", 10)
		if _, created, err := svc.BeginCanonicalOperation(ctx, otherCredential, billingHoldTestRequest("other-credential")); err != nil || !created {
			t.Fatalf("other credential admission created=%t err=%v", created, err)
		}

		unconfirmedAuth := auth
		unconfirmedAuth.Limits.MonthlyBudgetMicros = 1_000
		unconfirmed, created, err := svc.BeginCanonicalOperation(ctx, unconfirmedAuth, billingHoldTestRequest("unconfirmed"))
		if err != nil || !created {
			t.Fatalf("unconfirmed operation created=%t err=%v", created, err)
		}
		if err := svc.DisputeBillingHold(ctx, unconfirmed.ID, "transport_unknown"); err != nil {
			t.Fatal(err)
		}
		if err := svc.RecordGatewayUsage(ctx, legacyAuth, GatewayUsageInput{
			OperationID: unconfirmed.ID, UsageVersion: 1, UsageSource: "gateway_observation",
			RequestFingerprint: unconfirmed.RequestFingerprint, Model: unconfirmed.Model, Status: "upstream_error",
		}); err != nil {
			t.Fatal(err)
		}
		unconfirmedHold, _, _ := svc.BillingHoldForOperation(ctx, unconfirmed.ID)
		if unconfirmedHold.Status != BillingHoldStatusDisputed || unconfirmedHold.SettledAt != nil {
			t.Fatalf("unconfirmed usage incorrectly settled hold: %+v", unconfirmedHold)
		}
	})
}

func TestDurableBillingHoldAdmissionAndQueuedCancellationAreAtomic(t *testing.T) {
	forEachBillingHoldRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return base }
		saveBillingHoldTestPricing(t, svc, "image-model")
		auth := aiJobTestAuth("tenant-job-hold", "principal-job-hold")
		auth.Limits.MonthlyBudgetMicros = 100
		request := aiJobTestRequest("job-hold", "job-hold-fingerprint")

		job, created, err := svc.BeginDurableAIJob(ctx, auth, request)
		if err != nil || !created {
			t.Fatalf("BeginDurableAIJob() job=%+v created=%t err=%v", job, created, err)
		}
		if _, created, err := svc.BeginDurableAIJob(ctx, auth, request); err != nil || created {
			t.Fatalf("durable replay created=%t err=%v", created, err)
		}
		holds, _ := repo.ListBillingHolds(ctx)
		outbox, _ := repo.ListTransactionalOutboxEvents(ctx, job.ID)
		if len(holds) != 1 || len(outbox) != 1 || holds[0].OperationID != job.OperationID {
			t.Fatalf("durable admission holds=%+v outbox=%+v", holds, outbox)
		}
		if _, found, err := svc.CancelAIJobForAuth(ctx, auth, job.ID); err != nil || !found {
			t.Fatalf("CancelAIJobForAuth() found=%t err=%v", found, err)
		}
		hold, found, err := svc.BillingHoldForOperation(ctx, job.OperationID)
		if err != nil || !found || hold.Status != BillingHoldStatusReleased || hold.ReleasedAt == nil {
			t.Fatalf("canceled job hold=%+v found=%t err=%v", hold, found, err)
		}

		rejectedRequest := aiJobTestRequest("job-budget-rejected", "job-budget-rejected-fingerprint")
		rejectedAuth := auth
		rejectedAuth.Limits.MonthlyBudgetMicros = 1
		if _, _, err := svc.BeginDurableAIJob(ctx, rejectedAuth, rejectedRequest); !errors.Is(err, ErrBillingHoldBudgetExceeded) {
			t.Fatalf("durable budget rejection error=%v", err)
		}
		retryAuth := rejectedAuth
		retryAuth.Limits.MonthlyBudgetMicros = 1_000
		retried, created, err := svc.BeginDurableAIJob(ctx, retryAuth, rejectedRequest)
		if err != nil || !created {
			t.Fatalf("durable retry created=%t err=%v", created, err)
		}
		if retryOutbox, err := repo.ListTransactionalOutboxEvents(ctx, retried.ID); err != nil || len(retryOutbox) != 1 {
			t.Fatalf("durable retry outbox=%+v err=%v", retryOutbox, err)
		}
	})
}

func TestBillingHoldMediaQuotaReservationAndSettlement(t *testing.T) {
	forEachBillingHoldRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		base := time.Date(2026, time.July, 15, 11, 0, 0, 0, time.UTC)
		svc := NewService(repo, "/v1", "media-quota-secret")
		svc.now = func() time.Time { return base }
		auth := billingHoldTestAuth("tenant-media", "credential-media", 0)
		auth.Limits.MonthlyImageLimit = 2

		requests := []gatewaycore.CanonicalRequest{billingHoldImageRequest("concurrent-a", 2), billingHoldImageRequest("concurrent-b", 2)}
		var created atomic.Int32
		var rejected atomic.Int32
		var operationsMu sync.Mutex
		operations := []AIOperation{}
		var wait sync.WaitGroup
		for _, request := range requests {
			request := request
			wait.Add(1)
			go func() {
				defer wait.Done()
				operation, wasCreated, err := svc.BeginCanonicalOperation(ctx, auth, request)
				switch {
				case err == nil && wasCreated:
					created.Add(1)
					operationsMu.Lock()
					operations = append(operations, operation)
					operationsMu.Unlock()
				case errors.Is(err, ErrBillingHoldImageQuotaExceeded):
					rejected.Add(1)
				default:
					t.Errorf("unexpected concurrent quota result: created=%t err=%v", wasCreated, err)
				}
			}()
		}
		wait.Wait()
		if created.Load() != 1 || rejected.Load() != 1 || len(operations) != 1 {
			t.Fatalf("created=%d rejected=%d operations=%d", created.Load(), rejected.Load(), len(operations))
		}

		operation := operations[0]
		if err := svc.DisputeBillingHold(ctx, operation.ID, "provider_unknown"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.BeginCanonicalOperation(ctx, auth, billingHoldImageRequest("disputed-still-counts", 1)); !errors.Is(err, ErrBillingHoldImageQuotaExceeded) {
			t.Fatalf("disputed hold quota error=%v", err)
		}
		if err := svc.ReleaseBillingHold(ctx, operation.ID, "provider_proven_not_created"); err != nil {
			t.Fatal(err)
		}
		releasedReplacement, createdReplacement, err := svc.BeginCanonicalOperation(ctx, auth, billingHoldImageRequest("released-retry", 2))
		if err != nil || !createdReplacement {
			t.Fatalf("released retry operation=%+v created=%t err=%v", releasedReplacement, createdReplacement, err)
		}

		usageAuth := GatewayAuthContext{APIKey: APIKeyRecord{ID: auth.CredentialID, Fingerprint: "media-fingerprint"}}
		usage := GatewayUsageInput{
			OperationID: releasedReplacement.ID, UsageVersion: 1, UsageSource: "provider_final",
			RequestFingerprint: releasedReplacement.RequestFingerprint, Model: releasedReplacement.Model, Status: "forwarded",
			UsageDimensions: UsageDimensions{UsageDimensionOutputImages: {Quantity: 1, Unit: UsageUnitCount, Source: "core_artifact", Confidence: UsageConfidenceObserved}},
		}
		if err := svc.RecordGatewayUsage(ctx, usageAuth, usage); err != nil {
			t.Fatal(err)
		}
		if err := svc.RecordGatewayUsage(ctx, usageAuth, usage); err != nil {
			t.Fatalf("usage replay: %v", err)
		}
		if _, created, err := svc.BeginCanonicalOperation(ctx, auth, billingHoldImageRequest("actual-usage-boundary", 1)); err != nil || !created {
			t.Fatalf("actual usage boundary created=%t err=%v", created, err)
		}
		if _, _, err := svc.BeginCanonicalOperation(ctx, auth, billingHoldImageRequest("actual-usage-over", 1)); !errors.Is(err, ErrBillingHoldImageQuotaExceeded) {
			t.Fatalf("actual usage overage error=%v", err)
		}
	})
}

func TestBillingHoldVideoAndAudioQuotaRequireBoundedDuration(t *testing.T) {
	forEachBillingHoldRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		svc := NewService(repo, "/v1", "media-duration-secret")
		svc.now = func() time.Time { return time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC) }

		videoAuth := billingHoldTestAuth("tenant-video", "credential-video", 0)
		videoAuth.Limits.MonthlyVideoSecondsLimit = 2
		if _, _, err := svc.BeginCanonicalOperation(ctx, videoAuth, billingHoldMediaRequest("video-missing", "video", 0, 0)); !errors.Is(err, ErrBillingHoldUsageEstimate) {
			t.Fatalf("missing video duration error=%v", err)
		}
		if _, created, err := svc.BeginCanonicalOperation(ctx, videoAuth, billingHoldMediaRequest("video-boundary", "video", 2000, 0)); err != nil || !created {
			t.Fatalf("video boundary created=%t err=%v", created, err)
		}
		if _, _, err := svc.BeginCanonicalOperation(ctx, videoAuth, billingHoldMediaRequest("video-over", "video", 1, 0)); !errors.Is(err, ErrBillingHoldVideoQuotaExceeded) {
			t.Fatalf("video overage error=%v", err)
		}

		audioAuth := billingHoldTestAuth("tenant-audio", "credential-audio", 0)
		audioAuth.Limits.MonthlyAudioSecondsLimit = 1
		if _, _, err := svc.BeginCanonicalOperation(ctx, audioAuth, billingHoldMediaRequest("audio-missing", "audio", 0, 0)); !errors.Is(err, ErrBillingHoldUsageEstimate) {
			t.Fatalf("missing audio duration error=%v", err)
		}
		if _, created, err := svc.BeginCanonicalOperation(ctx, audioAuth, billingHoldMediaRequest("audio-boundary", "audio", 0, 1000)); err != nil || !created {
			t.Fatalf("audio boundary created=%t err=%v", created, err)
		}
		if _, _, err := svc.BeginCanonicalOperation(ctx, audioAuth, billingHoldMediaRequest("audio-over", "audio", 0, 1)); !errors.Is(err, ErrBillingHoldAudioQuotaExceeded) {
			t.Fatalf("audio overage error=%v", err)
		}

		transcriptionAuth := billingHoldTestAuth("tenant-transcription", "credential-transcription", 0)
		transcriptionAuth.Limits.MonthlyAudioSecondsLimit = 1
		transcription := billingHoldMediaRequest("transcription-boundary", "audio", 0, 0)
		transcription.Protocol = gatewaycore.ProtocolOpenAIAudioTranscriptions
		transcription.Operation = GatewayOperationAudioTranscription
		transcription.Lane = gatewaycore.LaneDirect
		transcription.InputAudioDurationMS = 1000
		if _, created, err := svc.BeginCanonicalOperation(ctx, transcriptionAuth, transcription); err != nil || !created {
			t.Fatalf("transcription boundary created=%t err=%v", created, err)
		}
		transcription.ClientRequestID = "request-transcription-over"
		transcription.Fingerprint = "fingerprint-transcription-over"
		transcription.IdempotencyKey = "idempotency-transcription-over"
		transcription.InputAudioDurationMS = 1
		if _, _, err := svc.BeginCanonicalOperation(ctx, transcriptionAuth, transcription); !errors.Is(err, ErrBillingHoldAudioQuotaExceeded) {
			t.Fatalf("transcription overage error=%v", err)
		}
	})
}

func forEachBillingHoldRepository(t *testing.T, run func(*testing.T, Repository)) {
	t.Helper()
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
			run(t, repo)
		})
	}
}

func saveBillingHoldTestPricing(t *testing.T, service *Service, model string) {
	t.Helper()
	expression := `v1: unit_line("output", output_tokens, "token", 1)`
	if model == "image-model" {
		expression = `v1: unit_line("image", output_images, "image", 10)`
	}
	publishTestUsagePricingRule(t, service, expression)
}

func TestBillingHoldUsesRequestMaxCostMicros(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	t.Cleanup(func() { _ = repo.Close() })
	service := NewService(repo, "/v1", "request-max-cost-secret")
	auth := billingHoldTestAuth("tenant-request-max", "credential-request-max", 100)
	request := billingHoldTestRequest("request-max")
	request.Payload = []byte(`{"max_cost_micros":75}`)

	operation, created, err := service.BeginCanonicalOperation(ctx, auth, request)
	if err != nil || !created {
		t.Fatalf("BeginCanonicalOperation() operation=%+v created=%t err=%v", operation, created, err)
	}
	hold, found, err := service.BillingHoldForOperation(ctx, operation.ID)
	if err != nil || !found || hold.ReservedAmountMicros != 75 || hold.EstimateSource != "request_max_cost" {
		t.Fatalf("request max-cost hold=%+v found=%t err=%v", hold, found, err)
	}
}

func TestBillingHoldSettlesCumulativeIncrementalUsage(t *testing.T) {
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
			ctx := context.Background()
			repo := test.open(t)
			defer repo.Close()
			svc := NewService(repo, "/v1", "incremental-hold-secret")
			saveBillingHoldTestPricing(t, svc, "model-a")
			identity := testutil.UniqueID("incremental-hold")
			auth := billingHoldTestAuth("tenant-"+identity, "credential-"+identity, 0)
			request := billingHoldTestRequest(identity)
			operation, created, err := svc.BeginCanonicalOperation(ctx, auth, request)
			if err != nil || !created {
				t.Fatalf("operation=%+v created=%t err=%v", operation, created, err)
			}
			attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{ID: "provider", AccountID: "account", UpstreamModel: operation.Model})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.CommitBillingHold(ctx, operation.ID, "provider_connected"); err != nil {
				t.Fatal(err)
			}
			legacyAuth := GatewayAuthContext{APIKey: APIKeyRecord{ID: auth.CredentialID, Fingerprint: "fingerprint-" + identity, KeyType: APIKeyTypeService}}
			record := func(version, amount int, source string) {
				t.Helper()
				err := svc.RecordGatewayUsage(ctx, legacyAuth, GatewayUsageInput{
					OperationID: operation.ID, AttemptID: attempt.ID, UsageVersion: version, UsageSource: source,
					RequestFingerprint: operation.RequestFingerprint, Model: operation.Model, UpstreamModel: attempt.UpstreamModel,
					Protocol: operation.Protocol, ProviderID: attempt.ProviderID, ProviderAccountID: attempt.ProviderAccountID,
					Status: "forwarded", OutputTokens: amount, SkipProcurementCostEstimate: true,
				})
				if err != nil {
					t.Fatalf("RecordGatewayUsage(version=%d): %v", version, err)
				}
			}
			record(1, 3, "provider_incremental")
			record(2, 4, "provider_incremental")
			hold, found, err := svc.BillingHoldForOperation(ctx, operation.ID)
			if err != nil || !found || hold.Status != BillingHoldStatusCommitted || hold.SettledAmountMicros != 0 {
				t.Fatalf("incremental hold=%+v found=%t err=%v", hold, found, err)
			}
			record(3, 0, "gateway_final")
			hold, found, err = svc.BillingHoldForOperation(ctx, operation.ID)
			if err != nil || !found || hold.Status != BillingHoldStatusSettled || hold.SettledAmountMicros != 7 {
				t.Fatalf("settled hold=%+v found=%t err=%v", hold, found, err)
			}
			entries, err := svc.BillingLedgerEntries(ctx, operation.ID)
			if err != nil || len(entries) != 3 {
				t.Fatalf("billing entries=%+v err=%v", entries, err)
			}
		})
	}
}

func billingHoldTestAuth(tenantID, credentialID string, budget int64) gatewaycore.CanonicalAuthContext {
	return gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: credentialID, ProfileScope: ProfileScopePlatform,
		TenantID: tenantID, PrincipalType: APIKeyTypeService, PrincipalID: "principal-" + tenantID,
		Limits: gatewaycore.CanonicalLimits{MonthlyBudgetMicros: budget},
	}
}

func billingHoldTestRequest(identity string) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ClientRequestID: "request-" + identity, Fingerprint: "fingerprint-" + identity, IdempotencyKey: "idempotency-" + identity,
		Protocol: gatewaycore.ProtocolOpenAIChat, Operation: GatewayOperationChatCompletion, Modality: GatewayModalityText,
		Lane: gatewaycore.LaneDirect, Model: "model-a", Payload: []byte(`{"max_tokens":10}`),
	}
}

func billingHoldImageRequest(identity string, count int) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ClientRequestID: "request-" + identity, Fingerprint: "fingerprint-" + identity, IdempotencyKey: "idempotency-" + identity,
		Protocol: gatewaycore.ProtocolOpenAIImages, Operation: GatewayOperationImageGeneration, Modality: GatewayModalityImage,
		Lane: gatewaycore.LaneDirect, Model: "image-model", OutputCount: count, Payload: []byte(`{"prompt":"synthetic"}`),
	}
}

func billingHoldMediaRequest(identity, modality string, videoMS, audioMS int64) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ClientRequestID: "request-" + identity, Fingerprint: "fingerprint-" + identity, IdempotencyKey: "idempotency-" + identity,
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: modality + "_generation", Modality: modality,
		Lane: gatewaycore.LaneDurable, Model: modality + "-model", VideoDurationMS: videoMS, AudioDurationMS: audioMS,
		Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	}
}
