package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var ErrDurableAIJobRuntimeConfig = errors.New("invalid durable ai job runtime configuration")

type DurableAIJobRuntimeConfig struct {
	WorkerID          string
	LeaseDuration     time.Duration
	SchedulerInterval time.Duration
	DeliveryWait      time.Duration
	ReconcileInterval time.Duration
	RebuildInterval   time.Duration
	BatchSize         int
}

type DurableAIJobRuntime struct {
	service *Service
	queue   AIJobDeliveryQueue
	adapter DurableAIJobAdapter
	config  DurableAIJobRuntimeConfig

	runMu      sync.Mutex
	running    bool
	startedAt  *time.Time
	components map[string]AIJobRuntimeComponentStatus
}

type AIJobRuntimeComponentStatus struct {
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	Runs          int64      `json:"runs"`
	Errors        int64      `json:"errors"`
}

type DurableAIJobRuntimeStatus struct {
	Running     bool                        `json:"running"`
	QueueDriver string                      `json:"queue_driver"`
	WorkerID    string                      `json:"worker_id"`
	StartedAt   *time.Time                  `json:"started_at,omitempty"`
	Scheduler   AIJobRuntimeComponentStatus `json:"scheduler"`
	Delivery    AIJobRuntimeComponentStatus `json:"delivery"`
	Reconciler  AIJobRuntimeComponentStatus `json:"reconciler"`
	Rebuilder   AIJobRuntimeComponentStatus `json:"rebuilder"`
}

func NewDurableAIJobRuntime(service *Service, queue AIJobDeliveryQueue, adapter DurableAIJobAdapter, config DurableAIJobRuntimeConfig) (*DurableAIJobRuntime, error) {
	if service == nil || queue == nil || adapter == nil {
		return nil, fmt.Errorf("%w: service, queue, and adapter are required", ErrDurableAIJobRuntimeConfig)
	}
	config = normalizeDurableAIJobRuntimeConfig(config)
	if config.LeaseDuration <= 0 || config.SchedulerInterval <= 0 || config.DeliveryWait < 0 ||
		config.ReconcileInterval <= 0 || config.RebuildInterval <= 0 || config.BatchSize <= 0 {
		return nil, ErrDurableAIJobRuntimeConfig
	}
	return &DurableAIJobRuntime{
		service: service, queue: queue, adapter: adapter, config: config,
		components: map[string]AIJobRuntimeComponentStatus{},
	}, nil
}

func normalizeDurableAIJobRuntimeConfig(config DurableAIJobRuntimeConfig) DurableAIJobRuntimeConfig {
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.WorkerID == "" {
		config.WorkerID = "durable-runtime"
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = 30 * time.Second
	}
	if config.SchedulerInterval == 0 {
		config.SchedulerInterval = 250 * time.Millisecond
	}
	if config.DeliveryWait == 0 {
		config.DeliveryWait = time.Second
	}
	if config.ReconcileInterval == 0 {
		config.ReconcileInterval = time.Second
	}
	if config.RebuildInterval == 0 {
		config.RebuildInterval = 30 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 16
	}
	return config
}

// SupportsDurableAIJob is the admission boundary used before a Job is
// persisted. Authorization and policy checks happen first; this method then
// proves that at least one current route has a matching executable adapter.
func (runtime *DurableAIJobRuntime) SupportsDurableAIJob(ctx context.Context, auth gatewaycore.CanonicalAuthContext, request gatewaycore.CanonicalRequest) (bool, error) {
	evaluation, err := runtime.EvaluateDurableAIJobSupport(ctx, auth, request)
	return evaluation.Supported, err
}

func (runtime *DurableAIJobRuntime) isRunning() bool {
	if runtime == nil {
		return false
	}
	runtime.runMu.Lock()
	defer runtime.runMu.Unlock()
	return runtime.running
}

func (runtime *DurableAIJobRuntime) Status() DurableAIJobRuntimeStatus {
	if runtime == nil {
		return DurableAIJobRuntimeStatus{}
	}
	runtime.runMu.Lock()
	defer runtime.runMu.Unlock()
	return DurableAIJobRuntimeStatus{
		Running: runtime.running, QueueDriver: durableAIJobQueueDriver(runtime.queue), WorkerID: runtime.config.WorkerID,
		StartedAt: cloneTimePointer(runtime.startedAt), Scheduler: runtime.components["scheduler"],
		Delivery: runtime.components["delivery"], Reconciler: runtime.components["reconciler"], Rebuilder: runtime.components["rebuild"],
	}
}

// Run owns the durable scheduler, delivery, reconciliation, and queue rebuild
// loops until ctx is canceled. Only one Run call may be active per runtime.
func (runtime *DurableAIJobRuntime) Run(ctx context.Context, onError func(component string, err error)) error {
	if runtime == nil {
		return ErrDurableAIJobRuntimeConfig
	}
	runtime.runMu.Lock()
	if runtime.running {
		runtime.runMu.Unlock()
		return fmt.Errorf("%w: runtime is already running", ErrDurableAIJobRuntimeConfig)
	}
	runtime.running = true
	startedAt := time.Now().UTC()
	runtime.startedAt = &startedAt
	runtime.components = map[string]AIJobRuntimeComponentStatus{}
	runtime.runMu.Unlock()
	defer func() {
		runtime.runMu.Lock()
		runtime.running = false
		runtime.runMu.Unlock()
	}()

	reportError := func(component string, err error) {
		if err != nil && !errors.Is(err, context.Canceled) && onError != nil {
			onError(component, err)
		}
	}
	if _, err := runtime.service.RebuildDurableAIJobDeliveriesOnce(
		ctx, runtime.config.WorkerID+"-rebuild", runtime.config.LeaseDuration, runtime.config.BatchSize, runtime.queue,
	); err != nil {
		runtime.recordComponentRun("rebuild", err)
		reportError("rebuild", err)
	} else {
		runtime.recordComponentRun("rebuild", nil)
	}

	var workers sync.WaitGroup
	workers.Add(4)
	go func() {
		defer workers.Done()
		runtime.runScheduler(ctx, reportError)
	}()
	go func() {
		defer workers.Done()
		runtime.runDeliveryWorker(ctx, reportError)
	}()
	go func() {
		defer workers.Done()
		runtime.runReconciler(ctx, reportError)
	}()
	go func() {
		defer workers.Done()
		runtime.runRebuilder(ctx, reportError)
	}()
	workers.Wait()
	return nil
}

func (runtime *DurableAIJobRuntime) runScheduler(ctx context.Context, reportError func(string, error)) {
	ticker := time.NewTicker(runtime.config.SchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := runtime.service.RunDurableAIJobSchedulerOnce(
				ctx, runtime.config.WorkerID+"-scheduler", runtime.config.LeaseDuration, runtime.config.BatchSize, runtime.queue,
			)
			runtime.recordComponentRun("scheduler", err)
			reportError("scheduler", err)
		}
	}
}

func (runtime *DurableAIJobRuntime) runDeliveryWorker(ctx context.Context, reportError func(string, error)) {
	for ctx.Err() == nil {
		_, err := runtime.service.RunDurableAIJobDeliveryWorkerOnce(
			ctx, runtime.config.WorkerID+"-delivery", runtime.config.BatchSize, runtime.config.DeliveryWait, runtime.queue, runtime.adapter,
		)
		runtime.recordComponentRun("delivery", err)
		reportError("delivery", err)
		if err != nil {
			backoff := max(runtime.config.SchedulerInterval, 250*time.Millisecond)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (runtime *DurableAIJobRuntime) runReconciler(ctx context.Context, reportError func(string, error)) {
	ticker := time.NewTicker(runtime.config.ReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, durableErr := runtime.service.RunDurableAIJobReconcilerOnce(ctx, runtime.config.BatchSize, runtime.adapter)
			var directErr error
			if directAdapter, ok := runtime.adapter.(DirectAIProviderReconciler); ok {
				_, directErr = runtime.service.RunDirectAIReconcilerOnce(ctx, runtime.config.BatchSize, directAdapter)
			}
			err := errors.Join(durableErr, directErr)
			runtime.recordComponentRun("reconciler", err)
			reportError("reconciler", err)
		}
	}
}

func (runtime *DurableAIJobRuntime) runRebuilder(ctx context.Context, reportError func(string, error)) {
	ticker := time.NewTicker(runtime.config.RebuildInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := runtime.service.RebuildDurableAIJobDeliveriesOnce(
				ctx, runtime.config.WorkerID+"-rebuild", runtime.config.LeaseDuration, runtime.config.BatchSize, runtime.queue,
			)
			runtime.recordComponentRun("rebuild", err)
			reportError("rebuild", err)
		}
	}
}

func (runtime *DurableAIJobRuntime) recordComponentRun(component string, runErr error) {
	now := time.Now().UTC()
	runtime.runMu.Lock()
	defer runtime.runMu.Unlock()
	status := runtime.components[component]
	status.Runs++
	status.LastRunAt = &now
	if runErr == nil {
		status.LastSuccessAt = &now
		status.LastError = ""
	} else if !errors.Is(runErr, context.Canceled) {
		status.Errors++
		status.LastErrorAt = &now
		status.LastError = truncateRuntimeError(runErr.Error())
	}
	runtime.components[component] = status
}

func durableAIJobQueueDriver(queue AIJobDeliveryQueue) string {
	switch queue.(type) {
	case *MemoryAIJobDeliveryQueue:
		return "memory"
	case *RedisAIJobDeliveryQueue:
		return "redis"
	default:
		return "custom"
	}
}

func truncateRuntimeError(message string) string {
	message = strings.TrimSpace(message)
	const maxBytes = 512
	if len(message) <= maxBytes {
		return message
	}
	return message[:maxBytes]
}
