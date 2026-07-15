package controlplane

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestProviderDispatchCoordinatorPersistsBeforeSideEffectAndRecoversUnknown(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	base := time.Date(2026, time.July, 14, 13, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	operation, _, err := svc.BeginCanonicalOperation(ctx, operationTestAuth(), operationTestRequest("provider-dispatch", "provider-dispatch-fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkAIOperationRunning(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"prompt":"synthetic-sensitive-payload"}`)
	acceptedAttempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{ID: "provider-1", AccountID: "account-1", RouteID: "route-1", UpstreamModel: "model-1"})
	if err != nil {
		t.Fatal(err)
	}
	acceptedExecutor := &providerDispatchExecutorStub{result: ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "task-accepted", ProviderRequestID: "request-accepted", Status: "queued"},
	}}
	accepted, _, err := svc.ExecuteAIAttemptDispatch(ctx, acceptedAttempt.ID, payload, acceptedExecutor)
	if err != nil || accepted.DispatchState != AIAttemptDispatchAccepted || accepted.ProviderTaskID != "task-accepted" {
		t.Fatalf("ExecuteAIAttemptDispatch accepted=%+v err=%v", accepted, err)
	}
	if acceptedExecutor.calls != 1 || acceptedExecutor.command.Intent.AttemptID != acceptedAttempt.ID || !bytes.Equal(acceptedExecutor.command.Payload, payload) {
		t.Fatalf("dispatch command=%+v calls=%d", acceptedExecutor.command, acceptedExecutor.calls)
	}
	if bytes.Contains([]byte(accepted.DispatchIntentJSON), payload) || bytes.Contains([]byte(accepted.DispatchIntentJSON), []byte("synthetic-sensitive-payload")) {
		t.Fatalf("dispatch intent leaked payload: %s", accepted.DispatchIntentJSON)
	}
	if replayed, _, replayErr := svc.ExecuteAIAttemptDispatch(ctx, acceptedAttempt.ID, payload, acceptedExecutor); !errors.Is(replayErr, ErrAIAttemptRequiresReconciliation) || replayed.ProviderTaskID != accepted.ProviderTaskID || acceptedExecutor.calls != 1 {
		t.Fatalf("replayed dispatch=%+v calls=%d err=%v", replayed, acceptedExecutor.calls, replayErr)
	}

	unknownAttempt, err := svc.BeginAIAttempt(ctx, operation.ID, 2, GatewayProvider{ID: "provider-2", AccountID: "account-2", RouteID: "route-2", UpstreamModel: "model-2"})
	if err != nil {
		t.Fatal(err)
	}
	responseLost := errors.New("provider response lost after request submission")
	unknownExecutor := &providerDispatchExecutorStub{err: responseLost}
	unknown, _, err := svc.ExecuteAIAttemptDispatch(ctx, unknownAttempt.ID, payload, unknownExecutor)
	if !errors.Is(err, responseLost) || !errors.Is(err, ErrAIAttemptRequiresReconciliation) || unknown.DispatchState != AIAttemptDispatchUnknown {
		t.Fatalf("unknown dispatch=%+v err=%v", unknown, err)
	}
	reconciler := &providerTaskReconcilerStub{result: ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "task-recovered", ProviderRequestID: "request-recovered", Status: "running"},
	}}
	recovered, _, err := svc.ReconcileAIAttemptDispatch(ctx, unknown.ID, reconciler)
	if err != nil || recovered.DispatchState != AIAttemptDispatchAccepted || recovered.ProviderTaskID != "task-recovered" || reconciler.calls != 1 {
		t.Fatalf("reconciled dispatch=%+v calls=%d err=%v", recovered, reconciler.calls, err)
	}

	rejectedAttempt, err := svc.BeginAIAttempt(ctx, operation.ID, 3, GatewayProvider{ID: "provider-3", AccountID: "account-3", RouteID: "route-3", UpstreamModel: "model-3"})
	if err != nil {
		t.Fatal(err)
	}
	rejectedExecutor := &providerDispatchExecutorStub{result: ProviderDispatchResult{Outcome: ProviderDispatchOutcomeProvenNotCreated}}
	rejected, _, err := svc.ExecuteAIAttemptDispatch(ctx, rejectedAttempt.ID, payload, rejectedExecutor)
	if err != nil || rejected.DispatchState != AIAttemptDispatchProvenNotCreated || rejected.Status != AIAttemptStatusSkipped || rejected.ProviderTaskID != "" {
		t.Fatalf("proven not created dispatch=%+v err=%v", rejected, err)
	}
}

func TestProviderTaskStatusStalePreventsLifecycleRegression(t *testing.T) {
	tests := []struct {
		current string
		next    string
		stale   bool
	}{
		{current: "queued", next: "running", stale: false},
		{current: "running", next: "queued", stale: true},
		{current: "processing", next: "running", stale: true},
		{current: "unknown", next: "running", stale: false},
		{current: "succeeded", next: "running", stale: true},
		{current: "completed", next: "succeeded", stale: false},
		{current: "failed", next: "error", stale: false},
		{current: "canceled", next: "failed", stale: true},
	}
	for _, test := range tests {
		t.Run(test.current+"_to_"+test.next, func(t *testing.T) {
			if got := providerTaskStatusStale(test.current, test.next); got != test.stale {
				t.Fatalf("providerTaskStatusStale(%q,%q)=%t, want %t", test.current, test.next, got, test.stale)
			}
		})
	}
}

type providerDispatchExecutorStub struct {
	calls   int
	command ProviderDispatchCommand
	result  ProviderDispatchResult
	err     error
}

func (s *providerDispatchExecutorStub) DispatchProviderTask(_ context.Context, command ProviderDispatchCommand) (ProviderDispatchResult, error) {
	s.calls++
	s.command = ProviderDispatchCommand{Intent: command.Intent, Payload: append([]byte(nil), command.Payload...)}
	return s.result, s.err
}

type providerTaskReconcilerStub struct {
	calls  int
	result ProviderDispatchResult
	err    error
}

func (s *providerTaskReconcilerStub) ReconcileProviderTask(_ context.Context, _ ProviderDispatchIntent, _ ProviderTaskReference) (ProviderDispatchResult, error) {
	s.calls++
	return s.result, s.err
}
