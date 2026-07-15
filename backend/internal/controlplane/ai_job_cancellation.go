package controlplane

import (
	"context"
	"errors"
)

var ErrDurableAIJobCancellationUnsupported = errors.New("durable ai job provider cancellation is unsupported")

type DurableAIJobAdapterCanceler interface {
	CancelProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error)
}

type DurableAIJobAdapterCancellationSelector interface {
	SupportsDurableAIJobCancellation(context.Context, GatewayProvider, AIJob, AIAttempt) (bool, error)
}

func selectDurableAIJobCancellation(ctx context.Context, adapter DurableAIJobAdapter, provider GatewayProvider, job AIJob, attempt AIAttempt) (bool, error) {
	selector, ok := adapter.(DurableAIJobAdapterCancellationSelector)
	if !ok {
		// Legacy adapters have no negative capability declaration. Preserve the
		// old behavior and let them receive the cancel call when implemented.
		return true, nil
	}
	return selector.SupportsDurableAIJobCancellation(ctx, provider, job, attempt)
}

type durableAIJobCancelExecutor struct {
	adapter  DurableAIJobAdapterCanceler
	provider GatewayProvider
	job      AIJob
	attempt  AIAttempt
}

func (e durableAIJobCancelExecutor) ReconcileProviderTask(ctx context.Context, intent ProviderDispatchIntent, reference ProviderTaskReference) (ProviderDispatchResult, error) {
	return e.adapter.CancelProviderTask(ctx, e.provider, e.job, e.attempt, intent, reference)
}
