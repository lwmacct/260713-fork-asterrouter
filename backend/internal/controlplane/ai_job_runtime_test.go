package controlplane

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

func TestDurableAIJobRuntimeExecutesAndShutsDown(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC()
	service := NewService(NewMemoryRepository(), "/v1", "runtime-test-secret")
	service.now = func() time.Time { return base }
	setupDurableWorkerRoutes(t, service)
	queue, err := NewMemoryAIJobDeliveryQueue(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &runtimeSelectingAdapter{
		adapterID: "com.asterrouter.test.runtime-adapter",
		durableAIJobAdapterStub: &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{
			result: ProviderDispatchResult{
				Outcome: ProviderDispatchOutcomeAccepted,
				Task:    ProviderTaskReference{ProviderTaskID: "runtime-task", Status: "running"}, ReconcileAfter: base.Add(time.Hour),
			},
		}}},
	}
	runtime, err := NewDurableAIJobRuntime(service, queue, adapter, DurableAIJobRuntimeConfig{
		WorkerID: "runtime-test", LeaseDuration: 100 * time.Millisecond, SchedulerInterval: 5 * time.Millisecond,
		DeliveryWait: 5 * time.Millisecond, ReconcileInterval: 20 * time.Millisecond, RebuildInterval: 25 * time.Millisecond, BatchSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	var errorsMu sync.Mutex
	var runtimeErrors []error
	go func() {
		done <- runtime.Run(runCtx, func(_ string, err error) {
			errorsMu.Lock()
			runtimeErrors = append(runtimeErrors, err)
			errorsMu.Unlock()
		})
	}()
	t.Cleanup(cancel)

	request := gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolAsterJobs, Lane: gatewaycore.LaneDurable,
		Model: "worker-image", Modality: "image", Operation: "image_generation",
	}
	auth := gatewaycore.CanonicalAuthContext{ArtifactPolicy: GatewayArtifactPolicyTemporary}
	waitForCondition(t, time.Second, func() bool {
		supported, supportErr := runtime.SupportsDurableAIJob(ctx, auth, request)
		return supportErr == nil && supported
	})
	status := runtime.Status()
	if !status.Running || status.QueueDriver != "memory" || status.WorkerID != "runtime-test" {
		t.Fatalf("runtime status=%+v", status)
	}
	job := beginDurableWorkerJob(t, service, "runtime-execution")
	waitForCondition(t, time.Second, func() bool {
		current, found, findErr := service.repo.FindAIJob(ctx, job.ID)
		return findErr == nil && found && current.Status == AIJobStatusRunning
	})
	attempts := adapter.Attempts()
	if len(attempts) != 1 || attempts[0].ProviderAdapterID != adapter.adapterID {
		t.Fatalf("attempts=%+v", attempts)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime Run()=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after cancellation")
	}
	if supported, err := runtime.SupportsDurableAIJob(ctx, auth, request); err != nil || supported {
		t.Fatalf("stopped runtime supported=%t err=%v", supported, err)
	}
	if status := runtime.Status(); status.Running {
		t.Fatalf("runtime remained running after shutdown: %+v", status)
	}
	errorsMu.Lock()
	defer errorsMu.Unlock()
	if len(runtimeErrors) != 0 {
		t.Fatalf("runtime errors=%v", runtimeErrors)
	}
}

func TestDurableAIJobRuntimeRejectsIncompleteConfiguration(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1")
	queue, err := NewMemoryAIJobDeliveryQueue(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDurableAIJobRuntime(nil, queue, &durableAIJobAdapterStub{}, DurableAIJobRuntimeConfig{}); !errors.Is(err, ErrDurableAIJobRuntimeConfig) {
		t.Fatalf("nil service error=%v", err)
	}
	if _, err := NewDurableAIJobRuntime(service, queue, nil, DurableAIJobRuntimeConfig{}); !errors.Is(err, ErrDurableAIJobRuntimeConfig) {
		t.Fatalf("nil adapter error=%v", err)
	}
	if _, err := NewDurableAIJobRuntime(service, queue, &durableAIJobAdapterStub{}, DurableAIJobRuntimeConfig{BatchSize: -1}); !errors.Is(err, ErrDurableAIJobRuntimeConfig) {
		t.Fatalf("invalid batch error=%v", err)
	}
}

func TestDurableAIJobRuntimeExplainsAdapterCapabilityExclusions(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryRepository(), "/v1", "runtime-capability-secret")
	setupDurableWorkerRoutes(t, service)
	queue, err := NewMemoryAIJobDeliveryQueue(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &runtimeSelectingAdapter{
		adapterID: "com.asterrouter.test.rejecting-adapter", selectionReason: DurableAIJobCapabilityModalityUnsupported,
		durableAIJobAdapterStub: &durableAIJobAdapterStub{},
	}
	runtime, err := NewDurableAIJobRuntime(service, queue, adapter, DurableAIJobRuntimeConfig{
		SchedulerInterval: time.Second, DeliveryWait: time.Second, ReconcileInterval: time.Second, RebuildInterval: time.Second, BatchSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(runCtx, func(string, error) {}) }()
	waitForCondition(t, time.Second, func() bool { return runtime.Status().Running })

	evaluation, err := runtime.EvaluateDurableAIJobSupport(ctx, gatewaycore.CanonicalAuthContext{ArtifactPolicy: GatewayArtifactPolicyTemporary}, gatewaycore.CanonicalRequest{
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: GatewayOperationImageGeneration,
		Modality: GatewayModalityImage, Lane: gatewaycore.LaneDurable, Model: "worker-image",
	})
	if err != nil || evaluation.Supported || !evaluation.HasRoutes || evaluation.RejectionReason != DurableAIJobCapabilityAllAdaptersExcluded || len(evaluation.Exclusions) != 2 {
		t.Fatalf("evaluation=%+v err=%v", evaluation, err)
	}
	for _, exclusion := range evaluation.Exclusions {
		if exclusion.RouteID == "" || exclusion.ProviderAccountID == "" || exclusion.Reason != DurableAIJobCapabilityModalityUnsupported {
			t.Fatalf("exclusion=%+v", exclusion)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after capability evaluation")
	}
	stopped, err := runtime.EvaluateDurableAIJobSupport(ctx, gatewaycore.CanonicalAuthContext{}, gatewaycore.CanonicalRequest{})
	if err != nil || stopped.RejectionReason != DurableAIJobCapabilityRuntimeUnavailable || len(stopped.Exclusions) != 1 {
		t.Fatalf("stopped evaluation=%+v err=%v", stopped, err)
	}
}

func TestDurableAIJobAdapterEvidenceRejectsUntrustedReasonText(t *testing.T) {
	adapter := &runtimeSelectingAdapter{
		adapterID: "unused", selectionReason: "provider secret\nleak",
		durableAIJobAdapterStub: &durableAIJobAdapterStub{},
	}
	_, supported, reason, err := selectDurableAIJobAdapterWithEvidence(context.Background(), adapter, GatewayProvider{}, AIJob{})
	if err != nil || supported || reason != DurableAIJobCapabilityAdapterUnsupported {
		t.Fatalf("supported=%t reason=%q err=%v", supported, reason, err)
	}
}

func TestDurableAIJobRuntimeBacksOffWhenDeliveryQueueIsUnavailable(t *testing.T) {
	service := NewService(NewMemoryRepository(), "/v1")
	queue := &unavailableAIJobDeliveryQueue{received: make(chan struct{})}
	runtime, err := NewDurableAIJobRuntime(service, queue, &durableAIJobAdapterStub{}, DurableAIJobRuntimeConfig{
		SchedulerInterval: time.Second, DeliveryWait: time.Millisecond, ReconcileInterval: time.Second, RebuildInterval: time.Second, BatchSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, func(string, error) {}) }()
	select {
	case <-queue.received:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("delivery worker did not call the queue")
	}
	time.Sleep(50 * time.Millisecond)
	if calls := queue.receiveCalls.Load(); calls != 1 {
		cancel()
		t.Fatalf("delivery queue receive calls=%d, want 1 during backoff", calls)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop during delivery backoff")
	}
}

type runtimeSelectingAdapter struct {
	*durableAIJobAdapterStub
	adapterID       string
	selectionReason string
}

func (adapter *runtimeSelectingAdapter) SelectDurableAIJobAdapter(context.Context, GatewayProvider, AIJob) (string, bool, error) {
	if adapter.selectionReason != "" {
		return "", false, nil
	}
	return adapter.adapterID, true, nil
}

func (adapter *runtimeSelectingAdapter) ExplainDurableAIJobAdapterSelection(context.Context, GatewayProvider, AIJob) (string, bool, string, error) {
	if adapter.selectionReason != "" {
		return "", false, adapter.selectionReason, nil
	}
	return adapter.adapterID, true, "", nil
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

type unavailableAIJobDeliveryQueue struct {
	receiveCalls atomic.Int32
	received     chan struct{}
	once         sync.Once
}

func (*unavailableAIJobDeliveryQueue) Publish(context.Context, AIJobDeliveryEnvelope, string, time.Time) error {
	return nil
}

func (queue *unavailableAIJobDeliveryQueue) Receive(context.Context, string, int, time.Duration) ([]AIJobDelivery, error) {
	queue.receiveCalls.Add(1)
	queue.once.Do(func() { close(queue.received) })
	return nil, errors.New("delivery queue unavailable")
}

func (*unavailableAIJobDeliveryQueue) Extend(context.Context, AIJobDelivery, time.Time) error {
	return nil
}
func (*unavailableAIJobDeliveryQueue) Ack(context.Context, AIJobDelivery) error { return nil }
func (*unavailableAIJobDeliveryQueue) Nack(context.Context, AIJobDelivery, time.Time, string) error {
	return nil
}
func (*unavailableAIJobDeliveryQueue) DeadLetter(context.Context, AIJobDelivery, string) error {
	return nil
}
