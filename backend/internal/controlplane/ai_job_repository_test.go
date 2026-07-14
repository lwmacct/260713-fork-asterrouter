package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestAIJobAdmissionCancellationAndOwnershipContract(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		ctx := context.Background()
		auth := aiJobTestAuth("tenant-a", "principal-a")
		request := aiJobTestRequest("job-idem-1", "fingerprint-1")

		job, created, err := svc.BeginDurableAIJob(ctx, auth, request)
		if err != nil || !created || job.Status != AIJobStatusQueued || job.StatusVersion != 1 || job.RequestPayloadCiphertext == "" || job.RequestPayloadCiphertext == string(request.Payload) {
			t.Fatalf("BeginDurableAIJob() job=%+v created=%t err=%v", job, created, err)
		}
		replayed, created, err := svc.BeginDurableAIJob(ctx, auth, request)
		if err != nil || created || replayed.ID != job.ID {
			t.Fatalf("replay job=%+v created=%t err=%v", replayed, created, err)
		}
		conflict := request
		conflict.Fingerprint = "different-fingerprint"
		if _, _, err := svc.BeginDurableAIJob(ctx, auth, conflict); !errors.Is(err, ErrGatewayIdempotencyConflict) {
			t.Fatalf("fingerprint conflict error=%v", err)
		}

		if _, found, err := svc.AIJobForAuth(ctx, aiJobTestAuth("tenant-b", "principal-a"), job.ID); err != nil || found {
			t.Fatalf("cross-tenant lookup found=%t err=%v", found, err)
		}
		if _, found, err := svc.AIJobForAuth(ctx, aiJobTestAuth("tenant-a", "principal-b"), job.ID); err != nil || found {
			t.Fatalf("cross-principal lookup found=%t err=%v", found, err)
		}

		canceled, found, err := svc.CancelAIJobForAuth(ctx, auth, job.ID)
		if err != nil || !found || canceled.Status != AIJobStatusCanceled || canceled.StatusVersion != 2 || canceled.CompletedAt == nil {
			t.Fatalf("CancelAIJobForAuth() job=%+v found=%t err=%v", canceled, found, err)
		}
		replayedCancel, found, err := svc.CancelAIJobForAuth(ctx, auth, job.ID)
		if err != nil || !found || replayedCancel.StatusVersion != 2 {
			t.Fatalf("cancel replay job=%+v found=%t err=%v", replayedCancel, found, err)
		}
		operation, found, err := svc.AIOperation(ctx, job.OperationID)
		if err != nil || !found || operation.Status != AIOperationStatusCanceled {
			t.Fatalf("operation=%+v found=%t err=%v", operation, found, err)
		}
		events, err := svc.AIJobEvents(ctx, job.ID)
		if err != nil || len(events) != 2 || events[0].EventType != AIJobEventQueued || events[1].EventType != AIJobEventCancelled {
			t.Fatalf("events=%+v err=%v", events, err)
		}
		outbox, err := svc.TransactionalOutboxEvents(ctx, job.ID)
		if err != nil || len(outbox) != 2 || outbox[0].EventVersion != 1 || outbox[1].EventVersion != 2 {
			t.Fatalf("outbox=%+v err=%v", outbox, err)
		}
	})
}

func TestAIJobQueueLeaseFenceAndCancellationRaceContract(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 14, 14, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return base }
		auth := aiJobTestAuth("tenant-a", "principal-a")
		job, _, err := svc.BeginDurableAIJob(context.Background(), auth, aiJobTestRequest("job-idem-fence", "fingerprint-fence"))
		if err != nil {
			t.Fatal(err)
		}

		claimed, err := svc.ClaimReadyAIJobs(context.Background(), "worker-a", time.Minute, 1)
		if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID || claimed[0].Status != AIJobStatusDispatching || claimed[0].FenceToken != 1 || claimed[0].StatusVersion != 2 {
			t.Fatalf("claimed=%+v err=%v", claimed, err)
		}
		if claimed[0].RequestPayload == "" || claimed[0].RequestPayloadCiphertext != "" {
			t.Fatalf("claimed payload was not decrypted in memory: %+v", claimed[0])
		}
		if _, err := svc.TransitionAIJob(context.Background(), job.ID, 2, 999, AIJobStatusRunning, ""); !errors.Is(err, ErrAIJobStateConflict) {
			t.Fatalf("wrong fence error=%v", err)
		}
		if active, err := svc.ClaimReadyAIJobs(context.Background(), "worker-b", time.Minute, 1); err != nil || len(active) != 0 {
			t.Fatalf("active lease was reclaimed: jobs=%+v err=%v", active, err)
		}
		base = base.Add(time.Minute)
		reclaimed, err := svc.ClaimReadyAIJobs(context.Background(), "worker-b", time.Minute, 1)
		if err != nil || len(reclaimed) != 1 || reclaimed[0].Status != AIJobStatusDispatching || reclaimed[0].FenceToken != 2 || reclaimed[0].StatusVersion != 3 {
			t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
		}
		if _, err := svc.TransitionAIJob(context.Background(), job.ID, 2, claimed[0].FenceToken, AIJobStatusRunning, ""); !errors.Is(err, ErrAIJobStateConflict) {
			t.Fatalf("expired worker completion error=%v", err)
		}
		running, err := svc.TransitionAIJob(context.Background(), job.ID, 3, reclaimed[0].FenceToken, AIJobStatusRunning, "")
		if err != nil || running.Status != AIJobStatusRunning || running.StatusVersion != 4 || running.QueueLeaseUntil != nil {
			t.Fatalf("running=%+v err=%v", running, err)
		}

		canceling, found, err := svc.CancelAIJobForAuth(context.Background(), auth, job.ID)
		if err != nil || !found || canceling.Status != AIJobStatusCanceling || canceling.StatusVersion != 5 {
			t.Fatalf("canceling=%+v found=%t err=%v", canceling, found, err)
		}
		if _, err := svc.TransitionAIJob(context.Background(), job.ID, 4, running.FenceToken, AIJobStatusSucceeded, ""); !errors.Is(err, ErrAIJobStateConflict) {
			t.Fatalf("stale completion error=%v", err)
		}
		succeeded, err := svc.TransitionAIJob(context.Background(), job.ID, canceling.StatusVersion, canceling.FenceToken, AIJobStatusSucceeded, "")
		if err != nil || succeeded.Status != AIJobStatusSucceeded {
			t.Fatalf("successful cancellation race result=%+v err=%v", succeeded, err)
		}
		operation, found, err := svc.AIOperation(context.Background(), job.OperationID)
		if err != nil || !found || operation.Status != AIOperationStatusSucceeded {
			t.Fatalf("operation=%+v found=%t err=%v", operation, found, err)
		}
		events, err := svc.AIJobEvents(context.Background(), job.ID)
		if err != nil || len(events) != 6 || events[2].EventType != AIJobEventLeaseReassigned {
			t.Fatalf("job events=%+v err=%v", events, err)
		}
	})
}

func TestAIJobDeliveryRebuildAndLeaseExtensionRepositoryContract(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		svc := newAIJobTestService(t, repo)
		base := time.Date(2026, time.July, 15, 1, 0, 0, 0, time.UTC)
		now := base
		svc.now = func() time.Time { return now }
		job, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-rebuild", "principal-rebuild"), aiJobTestRequest("job-idem-rebuild", "fingerprint-rebuild"))
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := svc.ClaimReadyAIJobs(ctx, "scheduler-rebuild", time.Minute, 1)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claimed=%+v err=%v", claimed, err)
		}
		envelope, err := NewAIJobDeliveryEnvelope(claimed[0])
		if err != nil {
			t.Fatal(err)
		}
		active, err := repo.ListAIJobsForDeliveryRebuild(ctx, base.Add(30*time.Second), 10)
		if err != nil || len(active) != 1 || active[0].ID != job.ID {
			t.Fatalf("active rebuild jobs=%+v err=%v", active, err)
		}

		now = base.Add(30 * time.Second)
		extended, err := svc.ExtendAIJobQueueLease(ctx, envelope, 2*time.Minute)
		wantLeaseUntil := now.Add(2 * time.Minute)
		if err != nil || extended.QueueLeaseUntil == nil || !extended.QueueLeaseUntil.Equal(wantLeaseUntil) || extended.StatusVersion != claimed[0].StatusVersion || extended.FenceToken != claimed[0].FenceToken {
			t.Fatalf("extended job=%+v err=%v", extended, err)
		}
		wrongLease := envelope
		wrongLease.QueueLeaseToken = "wrong-job-lease"
		if _, err := svc.ExtendAIJobQueueLease(ctx, wrongLease, 3*time.Minute); !errors.Is(err, ErrAIJobStateConflict) {
			t.Fatalf("wrong lease extension error=%v", err)
		}
		active, err = repo.ListAIJobsForDeliveryRebuild(ctx, base.Add(90*time.Second), 10)
		if err != nil || len(active) != 1 || active[0].QueueLeaseUntil == nil || !active[0].QueueLeaseUntil.Equal(wantLeaseUntil) {
			t.Fatalf("extended rebuild jobs=%+v err=%v", active, err)
		}
		events, err := svc.AIJobEvents(ctx, job.ID)
		if err != nil || len(events) != 2 {
			t.Fatalf("lease extension events=%+v err=%v", events, err)
		}
		outbox, err := svc.TransactionalOutboxEvents(ctx, job.ID)
		if err != nil || len(outbox) != 2 {
			t.Fatalf("lease extension outbox=%+v err=%v", outbox, err)
		}

		now = wantLeaseUntil
		if active, err := repo.ListAIJobsForDeliveryRebuild(ctx, now, 10); err != nil || len(active) != 0 {
			t.Fatalf("expired rebuild jobs=%+v err=%v", active, err)
		}
		reclaimed, err := svc.ClaimReadyAIJobs(ctx, "scheduler-reclaimed", time.Minute, 1)
		if err != nil || len(reclaimed) != 1 || reclaimed[0].StatusVersion <= claimed[0].StatusVersion || reclaimed[0].FenceToken <= claimed[0].FenceToken {
			t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
		}
	})
}

func TestAIJobAdmissionIsAtomicAcrossConcurrentRequests(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		auth := aiJobTestAuth("tenant-concurrent", "principal-concurrent")
		request := aiJobTestRequest("job-idem-concurrent", "fingerprint-concurrent")
		var createdCount atomic.Int32
		ids := make(chan string, 20)
		errorsSeen := make(chan error, 20)
		var wait sync.WaitGroup
		for index := 0; index < 20; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				job, created, err := svc.BeginDurableAIJob(context.Background(), auth, request)
				if err != nil {
					errorsSeen <- err
					return
				}
				if created {
					createdCount.Add(1)
				}
				ids <- job.ID
			}()
		}
		wait.Wait()
		close(ids)
		close(errorsSeen)
		for err := range errorsSeen {
			t.Errorf("BeginDurableAIJob(): %v", err)
		}
		var firstID string
		for id := range ids {
			if firstID == "" {
				firstID = id
			}
			if id != firstID {
				t.Errorf("job id=%s want=%s", id, firstID)
			}
		}
		if createdCount.Load() != 1 || firstID == "" {
			t.Fatalf("created=%d firstID=%q", createdCount.Load(), firstID)
		}
		events, _ := svc.AIJobEvents(context.Background(), firstID)
		outbox, _ := svc.TransactionalOutboxEvents(context.Background(), firstID)
		if len(events) != 1 || len(outbox) != 1 {
			t.Fatalf("events=%d outbox=%d", len(events), len(outbox))
		}
	})
}

func TestAIJobAdmissionRollsBackOnOutboxConflict(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		svc := newAIJobTestService(t, repo)
		ctx := context.Background()
		auth := aiJobTestAuth("tenant-rollback", "principal-rollback")
		first, _, err := svc.BeginDurableAIJob(ctx, auth, aiJobTestRequest("job-idem-existing", "fingerprint-existing"))
		if err != nil {
			t.Fatal(err)
		}
		existingOutbox, err := svc.TransactionalOutboxEvents(ctx, first.ID)
		if err != nil || len(existingOutbox) != 1 {
			t.Fatalf("existing outbox=%+v err=%v", existingOutbox, err)
		}

		now := time.Date(2026, time.July, 14, 16, 0, 0, 0, time.UTC)
		operation := AIOperation{
			ID: "aio_rollback", ProfileScope: auth.ProfileScope, TenantID: auth.TenantID, CredentialID: auth.CredentialID,
			CredentialSource: string(auth.CredentialSource), PrincipalType: auth.PrincipalType, PrincipalID: auth.PrincipalID,
			RequestFingerprint: "fingerprint-rollback", IdempotencyKey: "job-idem-rollback", Protocol: string(gatewaycore.ProtocolAsterJobs),
			Operation: "image_generation", Modality: "image", Lane: string(gatewaycore.LaneDurable), Model: "image-model",
			Status: AIOperationStatusAccepted, CreatedAt: now, UpdatedAt: now,
		}
		job := AIJob{
			ID: "job_rollback", OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
			CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource, PrincipalType: operation.PrincipalType,
			PrincipalID: operation.PrincipalID, RequestFingerprint: operation.RequestFingerprint, IdempotencyKey: operation.IdempotencyKey,
			Protocol: operation.Protocol, Operation: operation.Operation, Modality: operation.Modality, Model: operation.Model,
			ArtifactPolicy: GatewayArtifactPolicyManaged, RequestPayloadCiphertext: "synthetic-ciphertext", Status: AIJobStatusQueued, StatusVersion: 1,
			NextEligibleAt: now, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(AIJobDefaultTTL),
		}
		event, outbox, err := newAIJobTransitionRecords(job, AIJobStatusAccepted, "", now)
		if err != nil {
			t.Fatal(err)
		}
		outbox.ID = existingOutbox[0].ID
		if _, created, err := repo.CreateDurableAIJob(ctx, operation, job, event, outbox); err == nil || created {
			t.Fatalf("conflicting admission created=%t err=%v", created, err)
		}
		if _, found, err := repo.FindAIJob(ctx, job.ID); err != nil || found {
			t.Fatalf("rolled-back job found=%t err=%v", found, err)
		}
		if _, found, err := repo.FindAIOperation(ctx, operation.ID); err != nil || found {
			t.Fatalf("rolled-back operation found=%t err=%v", found, err)
		}
		events, err := repo.ListAIJobEvents(ctx, job.ID)
		if err != nil || len(events) != 0 {
			t.Fatalf("rolled-back events=%+v err=%v", events, err)
		}
	})
}

func TestAIJobClaimIsAtomicAcrossPostgresInstances(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repoA, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoA.Close()
	repoB, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoB.Close()
	svc := newAIJobTestService(t, repoA)
	base := time.Date(2026, time.July, 14, 15, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if _, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-claim", "principal-claim"), aiJobTestRequest("job-idem-claim", "fingerprint-claim")); err != nil {
		t.Fatal(err)
	}

	var claimCount atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			repo := Repository(repoA)
			if worker%2 == 1 {
				repo = repoB
			}
			claimed, err := repo.ClaimQueuedAIJobs(ctx, base, base.Add(time.Minute), fmt.Sprintf("worker-%d", worker), fmt.Sprintf("lease-%d", worker), 1)
			if err != nil {
				errorsSeen <- err
				return
			}
			claimCount.Add(int32(len(claimed)))
		}(index)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("claim: %v", err)
	}
	if claimCount.Load() != 1 {
		t.Fatalf("claimed=%d want=1", claimCount.Load())
	}
}

func TestAIJobEncryptedPayloadSurvivesPostgresRestart(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, "/v1", "job-payload-encryption-key")
	if _, err := svc.CreateGatewayModel(ctx, "test", GatewayModelRequest{
		ModelID: "image-model", Name: "Image model", Modality: "image", Status: GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	created, _, err := svc.BeginDurableAIJob(ctx, aiJobTestAuth("tenant-restart", "principal-restart"), aiJobTestRequest("job-idem-restart", "fingerprint-restart"))
	if err != nil {
		t.Fatal(err)
	}
	stored, found, err := repo.FindAIJob(ctx, created.ID)
	if err != nil || !found || stored.RequestPayloadCiphertext == "" || strings.Contains(stored.RequestPayloadCiphertext, "synthetic") {
		t.Fatalf("stored job=%+v found=%t err=%v", stored, found, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	restartedRepo, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedRepo.Close()
	restarted := NewService(restartedRepo, "/v1", "job-payload-encryption-key")
	claimed, err := restarted.ClaimReadyAIJobs(ctx, "worker-restarted", time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != created.ID || !strings.Contains(claimed[0].RequestPayload, "synthetic") || claimed[0].RequestPayloadCiphertext != "" {
		t.Fatalf("restarted claim=%+v err=%v", claimed, err)
	}
}

func forEachAIJobRepository(t *testing.T, run func(*testing.T, Repository)) {
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
				t.Fatalf("NewPostgresRepository(): %v", err)
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

func newAIJobTestService(t *testing.T, repo Repository) *Service {
	t.Helper()
	svc := NewService(repo, "/v1")
	if _, err := svc.CreateGatewayModel(context.Background(), "test", GatewayModelRequest{
		ModelID: "image-model", Name: "Image model", Modality: "image", Status: GatewayModelStatusActive,
	}); err != nil {
		t.Fatalf("CreateGatewayModel(): %v", err)
	}
	return svc
}

func aiJobTestAuth(tenantID, principalID string) gatewaycore.CanonicalAuthContext {
	return gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "credential-a", ProfileScope: ProfileScopePlatform,
		TenantID: tenantID, PrincipalType: GatewayPrincipalTypeService, PrincipalID: principalID, ArtifactPolicy: GatewayArtifactPolicyManaged,
	}
}

func aiJobTestRequest(idempotencyKey, fingerprint string) gatewaycore.CanonicalRequest {
	return gatewaycore.CanonicalRequest{
		ClientRequestID: "request-job", Fingerprint: fingerprint, IdempotencyKey: idempotencyKey, Protocol: gatewaycore.ProtocolAsterJobs,
		Operation: "image_generation", Modality: "image", Lane: gatewaycore.LaneDurable, Model: "image-model",
		Payload: []byte(`{"model":"image-model","operation":"image_generation","modality":"image","input":{"prompt":"synthetic"}}`),
	}
}
