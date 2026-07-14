package controlplane

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestDurableAIJobSchedulerAndDeliveryWorkerRejectStaleEnvelope(t *testing.T) {
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
			base := time.Date(2026, time.July, 14, 22, 0, 0, 0, time.UTC)
			now := base
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			svc := NewService(repo, "/v1", "delivery-worker-secret")
			svc.now = func() time.Time { return now }
			setupDurableWorkerRoutes(t, svc)
			job := beginDurableWorkerJob(t, svc, "delivery-stale")
			queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			queue.now = func() time.Time { return now }

			first, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-a", time.Minute, 1, queue)
			if err != nil || first.Claimed != 1 || first.Published != 1 || first.Errors != 0 {
				t.Fatalf("first scheduler report=%+v err=%v", first, err)
			}
			now = base.Add(2 * time.Minute)
			second, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-b", time.Minute, 1, queue)
			if err != nil || second.Claimed != 1 || second.Published != 1 || second.Errors != 0 {
				t.Fatalf("second scheduler report=%+v err=%v", second, err)
			}
			adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
				Outcome: ProviderDispatchOutcomeAccepted,
				Task:    ProviderTaskReference{ProviderTaskID: "task-delivery-stale", Status: "running"},
			}}}}
			report, err := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 2, 0, queue, adapter)
			if err != nil || report.Received != 2 || report.Acked != 2 || report.Skipped != 1 || report.Accepted != 1 || report.Errors != 0 {
				t.Fatalf("delivery worker report=%+v err=%v", report, err)
			}
			if adapter.DispatchCalls() != 1 {
				t.Fatalf("provider dispatch calls=%d, want 1", adapter.DispatchCalls())
			}
			assertAIJobStatus(t, svc, job.ID, AIJobStatusRunning)
			if pending, err := queue.Receive(ctx, "delivery-worker", 10, 0); err != nil || len(pending) != 0 {
				t.Fatalf("pending deliveries=%+v err=%v", pending, err)
			}
		})
	}
}

func TestDurableAIJobDeliveryWorkerNacksTransientRepositoryFailure(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 14, 23, 0, 0, 0, time.UTC)
	now := base
	repo := &failOnceFindAIJobRepository{Repository: NewMemoryRepository()}
	svc := NewService(repo, "/v1", "delivery-worker-secret")
	svc.now = func() time.Time { return now }
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "delivery-nack")
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	if report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler", time.Minute, 1, queue); err != nil || report.Published != 1 {
		t.Fatalf("scheduler report=%+v err=%v", report, err)
	}
	repo.failNextFind = true
	transient := errors.New("temporary database read failure")
	repo.findErr = transient
	adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "task-delivery-nack", Status: "running"},
	}}}}
	report, err := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 1, 0, queue, adapter)
	if !errors.Is(err, transient) || report.Received != 1 || report.Nacked != 1 || report.Errors != 1 || adapter.DispatchCalls() != 0 {
		t.Fatalf("failed delivery report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	now = base.Add(AIJobDefaultRetryAfter + time.Second)
	report, err = svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 1, 0, queue, adapter)
	if err != nil || report.Received != 1 || report.Acked != 1 || report.Accepted != 1 || report.Errors != 0 || adapter.DispatchCalls() != 1 {
		t.Fatalf("retried delivery report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusRunning)
}

func TestDurableAIJobSchedulerRecoversAfterAmbiguousPublishFailure(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	now := base
	svc := NewService(NewMemoryRepository(), "/v1", "delivery-worker-secret")
	svc.now = func() time.Time { return now }
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "delivery-publish-failure")
	memoryQueue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	memoryQueue.now = func() time.Time { return now }
	publishErr := errors.New("queue publish result unknown")
	queue := &failPublishOnceAIJobDeliveryQueue{AIJobDeliveryQueue: memoryQueue, err: publishErr, failNext: true}

	report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-a", time.Minute, 1, queue)
	if !errors.Is(err, publishErr) || report.Claimed != 1 || report.Published != 0 || report.Errors != 1 {
		t.Fatalf("failed scheduler report=%+v err=%v", report, err)
	}
	claimed, found, err := svc.repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || claimed.Status != AIJobStatusDispatching || claimed.FenceToken != 1 {
		t.Fatalf("claimed job=%+v found=%t err=%v", claimed, found, err)
	}

	now = base.Add(2 * time.Minute)
	report, err = svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-b", time.Minute, 1, queue)
	if err != nil || report.Claimed != 1 || report.Published != 1 || report.Errors != 0 {
		t.Fatalf("recovered scheduler report=%+v err=%v", report, err)
	}
	reclaimed, found, err := svc.repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || reclaimed.StatusVersion <= claimed.StatusVersion || reclaimed.FenceToken <= claimed.FenceToken {
		t.Fatalf("reclaimed job=%+v previous=%+v found=%t err=%v", reclaimed, claimed, found, err)
	}
	deliveries, err := queue.Receive(ctx, "delivery-worker", 10, 0)
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("recovered deliveries=%+v err=%v", deliveries, err)
	}
	if deliveries[0].Envelope.StatusVersion != claimed.StatusVersion || deliveries[0].Envelope.FenceToken != claimed.FenceToken ||
		deliveries[1].Envelope.StatusVersion != reclaimed.StatusVersion || deliveries[1].Envelope.FenceToken != reclaimed.FenceToken {
		t.Fatalf("ambiguous and recovered deliveries=%+v", deliveries)
	}
}

func TestDurableAIJobDeliveryRebuilderRestoresActiveAndQueuedJobs(t *testing.T) {
	forEachAIJobRepository(t, func(t *testing.T, repo Repository) {
		ctx := context.Background()
		base := time.Date(2026, time.July, 15, 2, 0, 0, 0, time.UTC)
		now := base
		svc := NewService(repo, "/v1", "delivery-rebuild-secret")
		svc.now = func() time.Time { return now }
		setupDurableWorkerRoutes(t, svc)
		activeJob := beginDurableWorkerJob(t, svc, "delivery-rebuild-active")
		lostQueue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		lostQueue.now = func() time.Time { return now }
		if report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-before-loss", time.Minute, 1, lostQueue); err != nil || report.Published != 1 {
			t.Fatalf("initial scheduler report=%+v err=%v", report, err)
		}
		queuedJob := beginDurableWorkerJob(t, svc, "delivery-rebuild-queued")

		rebuiltQueue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		rebuiltQueue.now = func() time.Time { return now }
		report, err := svc.RebuildDurableAIJobDeliveriesOnce(ctx, "delivery-rebuilder", time.Minute, 10, rebuiltQueue)
		if err != nil || report.Scanned != 1 || report.Republished != 1 || report.Claimed != 1 || report.Published != 2 || report.Errors != 0 {
			t.Fatalf("rebuild report=%+v err=%v", report, err)
		}
		adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{
			{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-rebuilt-active", Status: "running"}}},
			{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-rebuilt-queued", Status: "running"}}},
		}}
		workerReport, err := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 10, 0, rebuiltQueue, adapter)
		if err != nil || workerReport.Received != 2 || workerReport.Acked != 2 || workerReport.Accepted != 2 || workerReport.Errors != 0 || adapter.DispatchCalls() != 2 {
			t.Fatalf("rebuilt worker report=%+v calls=%d err=%v", workerReport, adapter.DispatchCalls(), err)
		}
		assertAIJobStatus(t, svc, activeJob.ID, AIJobStatusRunning)
		assertAIJobStatus(t, svc, queuedJob.ID, AIJobStatusRunning)
	})
}

func TestDurableAIJobDeliveryWorkerRejectsExpiredDatabaseLease(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.July, 15, 3, 0, 0, 0, time.UTC)
	now := base
	svc := NewService(NewMemoryRepository(), "/v1", "delivery-expired-secret")
	svc.now = func() time.Time { return now }
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "delivery-expired")
	queue, err := NewMemoryAIJobDeliveryQueue(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	if report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler", time.Minute, 1, queue); err != nil || report.Published != 1 {
		t.Fatalf("scheduler report=%+v err=%v", report, err)
	}
	now = base.Add(2 * time.Minute)
	adapter := &durableAIJobAdapterStub{}
	report, err := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 1, 0, queue, adapter)
	if err != nil || report.Received != 1 || report.Acked != 1 || report.Skipped != 1 || report.Errors != 0 || adapter.DispatchCalls() != 0 {
		t.Fatalf("expired delivery report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusDispatching)
	if schedulerReport, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-reclaim", time.Minute, 1, queue); err != nil || schedulerReport.Claimed != 1 || schedulerReport.Published != 1 {
		t.Fatalf("reclaim scheduler report=%+v err=%v", schedulerReport, err)
	}
}

func TestDurableAIJobDeliveryWorkerRenewsDatabaseAndDeliveryLeases(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "delivery-heartbeat-secret")
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "delivery-heartbeat")
	leaseDuration := 120 * time.Millisecond
	queue, err := NewMemoryAIJobDeliveryQueue(leaseDuration)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler", leaseDuration, 1, queue); err != nil || report.Published != 1 {
		t.Fatalf("scheduler report=%+v err=%v", report, err)
	}
	claimed, found, err := svc.repo.FindAIJob(ctx, job.ID)
	if err != nil || !found || claimed.QueueLeaseUntil == nil {
		t.Fatalf("claimed job=%+v found=%t err=%v", claimed, found, err)
	}
	originalLeaseUntil := *claimed.QueueLeaseUntil
	adapter := &blockingDurableAIJobAdapter{
		started: make(chan struct{}), release: make(chan struct{}),
		result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{ProviderTaskID: "task-heartbeat", Status: "running"}},
	}
	type workerResult struct {
		report DurableAIJobDeliveryWorkerReport
		err    error
	}
	result := make(chan workerResult, 1)
	go func() {
		report, runErr := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "delivery-worker", 1, 0, queue, adapter)
		result <- workerResult{report: report, err: runErr}
	}()
	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider dispatch did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, currentFound, findErr := svc.repo.FindAIJob(ctx, job.ID)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if currentFound && current.QueueLeaseUntil != nil && current.QueueLeaseUntil.After(originalLeaseUntil) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("database lease was not extended: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if wait := time.Until(originalLeaseUntil.Add(20 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	if duplicate, err := queue.Receive(ctx, "duplicate-worker", 1, 0); err != nil || len(duplicate) != 0 {
		t.Fatalf("delivery lease was not extended: deliveries=%+v err=%v", duplicate, err)
	}
	close(adapter.release)
	select {
	case completed := <-result:
		if completed.err != nil || completed.report.Received != 1 || completed.report.Acked != 1 || completed.report.Accepted != 1 || completed.report.Errors != 0 {
			t.Fatalf("heartbeat worker report=%+v err=%v", completed.report, completed.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery worker did not finish")
	}
	if adapter.dispatchCalls.Load() != 1 {
		t.Fatalf("provider dispatch calls=%d", adapter.dispatchCalls.Load())
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusRunning)
}

type failOnceFindAIJobRepository struct {
	Repository
	failNextFind bool
	findErr      error
}

type failPublishOnceAIJobDeliveryQueue struct {
	AIJobDeliveryQueue
	err      error
	failNext bool
}

func (q *failPublishOnceAIJobDeliveryQueue) Publish(ctx context.Context, envelope AIJobDeliveryEnvelope, dedupeKey string, availableAt time.Time) error {
	if q.failNext {
		q.failNext = false
		if err := q.AIJobDeliveryQueue.Publish(ctx, envelope, dedupeKey, availableAt); err != nil {
			return err
		}
		return q.err
	}
	return q.AIJobDeliveryQueue.Publish(ctx, envelope, dedupeKey, availableAt)
}

func (r *failOnceFindAIJobRepository) FindAIJob(ctx context.Context, id string) (AIJob, bool, error) {
	if r.failNextFind {
		r.failNextFind = false
		return AIJob{}, false, r.findErr
	}
	return r.Repository.FindAIJob(ctx, id)
}

type blockingDurableAIJobAdapter struct {
	started       chan struct{}
	release       chan struct{}
	result        ProviderDispatchResult
	dispatchCalls atomic.Int32
}

func (a *blockingDurableAIJobAdapter) DispatchProviderTask(ctx context.Context, _ GatewayProvider, _ AIJob, _ AIAttempt, _ ProviderDispatchCommand) (ProviderDispatchResult, error) {
	if a.dispatchCalls.Add(1) == 1 {
		close(a.started)
	}
	select {
	case <-ctx.Done():
		return ProviderDispatchResult{}, ctx.Err()
	case <-a.release:
		return a.result, nil
	}
}

func (*blockingDurableAIJobAdapter) ReconcileProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error) {
	return ProviderDispatchResult{}, errors.New("unexpected reconciliation")
}
