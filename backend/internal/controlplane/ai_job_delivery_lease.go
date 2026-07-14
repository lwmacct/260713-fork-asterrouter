package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type durableAIJobDeliveryLeaseHeartbeat struct {
	cancel context.CancelFunc
	done   <-chan error
}

func (s *Service) startAIJobDeliveryLeaseHeartbeat(ctx context.Context, queue AIJobDeliveryQueue, delivery AIJobDelivery, job AIJob) (context.Context, durableAIJobDeliveryLeaseHeartbeat, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := s.nowUTC()
	if job.QueueLeaseUntil == nil || !job.QueueLeaseUntil.After(now) || !delivery.LeaseUntil.After(now) {
		return ctx, durableAIJobDeliveryLeaseHeartbeat{}, ErrAIJobStateConflict
	}
	jobLeaseDuration := job.QueueLeaseUntil.Sub(now)
	deliveryLeaseDuration := delivery.LeaseUntil.Sub(now)
	heartbeatInterval := minDuration(jobLeaseDuration, deliveryLeaseDuration) / 3
	if heartbeatInterval < time.Millisecond {
		heartbeatInterval = time.Millisecond
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- s.runAIJobDeliveryLeaseHeartbeat(
			runCtx, cancel, queue, delivery, job, jobLeaseDuration, deliveryLeaseDuration, heartbeatInterval,
		)
	}()
	return runCtx, durableAIJobDeliveryLeaseHeartbeat{cancel: cancel, done: done}, nil
}

func (s *Service) runAIJobDeliveryLeaseHeartbeat(
	ctx context.Context,
	cancel context.CancelFunc,
	queue AIJobDeliveryQueue,
	delivery AIJobDelivery,
	job AIJob,
	jobLeaseDuration time.Duration,
	deliveryLeaseDuration time.Duration,
	heartbeatInterval time.Duration,
) error {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := s.ExtendAIJobQueueLease(ctx, delivery.Envelope, jobLeaseDuration); err != nil {
				if errors.Is(err, ErrAIJobStateConflict) && s.aiJobDeliveryLeaseNoLongerNeeded(ctx, job.ID) {
					return nil
				}
				cancel()
				return fmt.Errorf("extend ai job %s queue lease: %w", job.ID, err)
			}
			if err := queue.Extend(ctx, delivery, s.nowUTC().Add(deliveryLeaseDuration)); err != nil {
				cancel()
				return fmt.Errorf("extend ai job %s delivery lease: %w", job.ID, err)
			}
		}
	}
}

func (s *Service) aiJobDeliveryLeaseNoLongerNeeded(ctx context.Context, jobID string) bool {
	job, found, err := s.repo.FindAIJob(ctx, jobID)
	return err == nil && found && job.Status != AIJobStatusDispatching
}

func (h durableAIJobDeliveryLeaseHeartbeat) Stop() error {
	if h.cancel == nil || h.done == nil {
		return nil
	}
	h.cancel()
	return <-h.done
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
