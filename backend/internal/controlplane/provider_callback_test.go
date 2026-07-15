package controlplane

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestProviderCallbackReceiptMemoryIsAtomicAndIdempotent(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	receipt := ProviderCallbackReceipt{
		EventID: "event-1", AdapterID: "adapter-1", AttemptID: "attempt-1", ProviderID: "provider-1",
		ProviderAccountID: "account-1", ProviderTaskID: "task-1", PayloadHash: "hash-1", Status: ProviderCallbackReceiptProcessing,
		CreatedAt: time.Now().UTC(),
	}
	first, created, err := repo.CreateOrGetProviderCallbackReceipt(ctx, receipt)
	if err != nil || !created || first.EventID != receipt.EventID {
		t.Fatalf("first receipt=%+v created=%t err=%v", first, created, err)
	}
	second, created, err := repo.CreateOrGetProviderCallbackReceipt(ctx, receipt)
	if err != nil || created || second.PayloadHash != receipt.PayloadHash {
		t.Fatalf("replayed receipt=%+v created=%t err=%v", second, created, err)
	}
	if err := repo.CompleteProviderCallbackReceipt(ctx, receipt.EventID, ProviderCallbackReceiptApplied, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	completed, found, err := repo.FindProviderCallbackReceipt(ctx, receipt.EventID)
	if err != nil || !found || completed.Status != ProviderCallbackReceiptApplied || completed.ProcessedAt == nil {
		t.Fatalf("completed receipt=%+v found=%t err=%v", completed, found, err)
	}
}

func TestProviderCallbackUsesAttemptStateMachineAndRejectsBindingConflicts(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1", "callback-secret")
	base := time.Date(2026, time.July, 15, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if err := svc.SetArtifactStore(NewMemoryArtifactStore()); err != nil {
		t.Fatal(err)
	}
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "callback-state-machine")
	adapter := &callbackAdapter{durableAIJobAdapterStub: &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{
		result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeAccepted, Task: ProviderTaskReference{
			ProviderTaskID: "callback-task", ProviderRequestID: "callback-request", Status: "running",
		}, ReconcileAfter: base.Add(time.Hour)},
	}}}}
	if report, err := svc.RunDurableAIJobWorkerOnce(ctx, "callback-worker", time.Minute, 1, adapter); err != nil || report.Accepted != 1 {
		t.Fatalf("worker report=%+v err=%v", report, err)
	}
	attempts, err := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	attempt := attempts[0]
	percent := 25
	callback := ProviderCallback{
		EventID: "callback-event-1", AdapterID: adapter.ID(), AttemptID: attempt.ID, ProviderID: attempt.ProviderID,
		ProviderAccountID: attempt.ProviderAccountID, ProviderTaskID: attempt.ProviderTaskID, ProviderRequestID: attempt.ProviderRequestID,
		Status: "running", Progress: &ProviderProgressObservation{Sequence: 1, Percent: &percent, Stage: "rendering"},
	}
	result, err := svc.ProcessProviderCallback(ctx, callback, adapter)
	if err != nil || result.Duplicate || result.Status != "running" {
		t.Fatalf("callback result=%+v err=%v", result, err)
	}
	events, err := svc.AIJobProgressEvents(ctx, job.ID)
	if err != nil || len(events) != 1 || events[0].Percent == nil || *events[0].Percent != percent {
		t.Fatalf("progress events=%+v err=%v", events, err)
	}
	replayed, err := svc.ProcessProviderCallback(ctx, callback, adapter)
	if err != nil || !replayed.Duplicate || replayed.Status != ProviderCallbackReceiptApplied {
		t.Fatalf("replayed callback=%+v err=%v", replayed, err)
	}
	stale := callback
	stale.EventID = "callback-event-stale"
	stale.Status = "queued"
	if result, err := svc.ProcessProviderCallback(ctx, stale, adapter); err != nil || result.Status != "running" {
		t.Fatalf("stale callback result=%+v err=%v", result, err)
	}
	currentAttempt, found, err := repo.FindAIAttempt(ctx, attempt.ID)
	if err != nil || !found || currentAttempt.ProviderTaskStatus != "running" {
		t.Fatalf("stale callback changed attempt=%+v found=%t err=%v", currentAttempt, found, err)
	}
	conflict := callback
	conflict.Status = "completed"
	if _, err := svc.ProcessProviderCallback(ctx, conflict, adapter); !errors.Is(err, ErrProviderCallbackReplayConflict) {
		t.Fatalf("event conflict err=%v", err)
	}
	wrongTask := callback
	wrongTask.EventID = "callback-event-2"
	wrongTask.ProviderTaskID = "other-task"
	if _, err := svc.ProcessProviderCallback(ctx, wrongTask, adapter); !errors.Is(err, ErrProviderCallbackBinding) {
		t.Fatalf("task binding err=%v", err)
	}
	terminal := callback
	terminal.EventID = "callback-event-3"
	terminal.Status = "failed"
	terminal.Progress = nil
	terminal.Billing = ProviderBillingObservation{Status: ProviderBillingStatusNotCharged}
	if result, err := svc.ProcessProviderCallback(ctx, terminal, adapter); err != nil || result.Status != "failed" {
		t.Fatalf("terminal callback result=%+v err=%v", result, err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusFailed)
	assertBillingHoldStatus(t, svc, job.OperationID, BillingHoldStatusSettled)
	completedAttempts, err := repo.ListAIAttemptsByOperationID(ctx, job.OperationID)
	if err != nil || len(completedAttempts) != 1 || completedAttempts[0].Status != AIAttemptStatusFailed {
		t.Fatalf("terminal attempts=%+v err=%v", completedAttempts, err)
	}
}

func TestProviderCallbackConcurrentReplayProcessesOnce(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	callback := ProviderCallbackReceipt{EventID: "event-concurrent", AdapterID: "adapter", AttemptID: "attempt", ProviderID: "provider", ProviderAccountID: "account", ProviderTaskID: "task", PayloadHash: "hash", Status: ProviderCallbackReceiptProcessing}
	var wg sync.WaitGroup
	created := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, wasCreated, err := repo.CreateOrGetProviderCallbackReceipt(ctx, callback)
			if err != nil {
				t.Errorf("create receipt: %v", err)
			}
			created <- wasCreated
		}()
	}
	wg.Wait()
	close(created)
	count := 0
	for wasCreated := range created {
		if wasCreated {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created count=%d, want 1", count)
	}
}

type callbackAdapter struct {
	*durableAIJobAdapterStub
}

const callbackAdapterID = "callback-adapter"

var _ DurableAIJobAdapterSelector = (*callbackAdapter)(nil)

func (a *callbackAdapter) SelectDurableAIJobAdapter(context.Context, GatewayProvider, AIJob) (string, bool, error) {
	return callbackAdapterID, true, nil
}

func (a *callbackAdapter) ID() string { return callbackAdapterID }
