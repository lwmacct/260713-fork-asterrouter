package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type DurableAIJobSchedulerReport struct {
	Claimed   int
	Published int
	Errors    int
}

type DurableAIJobDeliveryWorkerReport struct {
	Received int
	Acked    int
	Nacked   int
	Skipped  int
	Accepted int
	Requeued int
	Unknown  int
	Errors   int
}

type DurableAIJobDeliveryRebuildReport struct {
	Scanned     int
	Republished int
	Claimed     int
	Published   int
	Errors      int
}

// RunDurableAIJobSchedulerOnce owns only the PostgreSQL claim and broker
// publication boundary. It never places request data in the queue. A failed
// publish leaves the database lease to expire so the rebuild/reclaim path can
// safely retry without guessing whether the broker accepted the message.
func (s *Service) RunDurableAIJobSchedulerOnce(ctx context.Context, workerID string, leaseDuration time.Duration, limit int, queue AIJobDeliveryQueue) (DurableAIJobSchedulerReport, error) {
	if queue == nil {
		return DurableAIJobSchedulerReport{}, ErrAIJobDeliveryQueueRequired
	}
	jobs, err := s.claimReadyAIJobMetadata(ctx, workerID, leaseDuration, limit)
	if err != nil {
		return DurableAIJobSchedulerReport{}, err
	}
	report := DurableAIJobSchedulerReport{Claimed: len(jobs)}
	var runErrs []error
	for _, job := range jobs {
		if publishErr := s.publishAIJobDelivery(ctx, queue, job); publishErr != nil {
			report.Errors++
			runErrs = append(runErrs, fmt.Errorf("publish ai job %s delivery: %w", job.ID, publishErr))
			continue
		}
		report.Published++
	}
	return report, errors.Join(runErrs...)
}

// RebuildDurableAIJobDeliveriesOnce restores queue state from PostgreSQL.
// Active dispatch claims are republished with the same deterministic delivery
// ID; queued and expired dispatching Jobs are then claimed through the normal
// fairness/fence path. Broker state never becomes authoritative.
func (s *Service) RebuildDurableAIJobDeliveriesOnce(ctx context.Context, workerID string, leaseDuration time.Duration, limit int, queue AIJobDeliveryQueue) (DurableAIJobDeliveryRebuildReport, error) {
	if queue == nil {
		return DurableAIJobDeliveryRebuildReport{}, ErrAIJobDeliveryQueueRequired
	}
	if limit <= 0 {
		return DurableAIJobDeliveryRebuildReport{}, nil
	}
	jobs, err := s.repo.ListAIJobsForDeliveryRebuild(ctx, s.nowUTC(), limit)
	if err != nil {
		return DurableAIJobDeliveryRebuildReport{}, err
	}
	report := DurableAIJobDeliveryRebuildReport{Scanned: len(jobs)}
	var runErrs []error
	for _, job := range jobs {
		if publishErr := s.publishAIJobDelivery(ctx, queue, job); publishErr != nil {
			report.Errors++
			runErrs = append(runErrs, fmt.Errorf("rebuild ai job %s delivery: %w", job.ID, publishErr))
			continue
		}
		report.Republished++
		report.Published++
	}
	remaining := limit - len(jobs)
	if remaining <= 0 {
		return report, errors.Join(runErrs...)
	}
	scheduled, scheduleErr := s.RunDurableAIJobSchedulerOnce(ctx, workerID, leaseDuration, remaining, queue)
	report.Claimed = scheduled.Claimed
	report.Published += scheduled.Published
	report.Errors += scheduled.Errors
	return report, errors.Join(errors.Join(runErrs...), scheduleErr)
}

func (s *Service) publishAIJobDelivery(ctx context.Context, queue AIJobDeliveryQueue, job AIJob) error {
	envelope, err := NewAIJobDeliveryEnvelope(job)
	if err != nil {
		return err
	}
	return queue.Publish(ctx, envelope, envelope.DedupeKey(), s.nowUTC())
}

// RunDurableAIJobDeliveryWorkerOnce receives only the small delivery envelope,
// reloads the current Job from PostgreSQL, and then delegates to the existing
// fenced Provider dispatch path. A stale or duplicate envelope is harmlessly
// acknowledged; a still-dispatching Job is negatively acknowledged only when
// the dispatch attempt failed before the Job moved to a durable next state.
func (s *Service) RunDurableAIJobDeliveryWorkerOnce(ctx context.Context, consumer string, maxItems int, wait time.Duration, queue AIJobDeliveryQueue, adapter DurableAIJobAdapter) (DurableAIJobDeliveryWorkerReport, error) {
	if queue == nil {
		return DurableAIJobDeliveryWorkerReport{}, ErrAIJobDeliveryQueueRequired
	}
	if adapter == nil {
		return DurableAIJobDeliveryWorkerReport{}, ErrDurableAIJobAdapterRequired
	}
	deliveries, err := queue.Receive(ctx, consumer, maxItems, wait)
	if err != nil {
		return DurableAIJobDeliveryWorkerReport{}, err
	}
	report := DurableAIJobDeliveryWorkerReport{Received: len(deliveries)}
	var runErrs []error
	for _, delivery := range deliveries {
		result := s.processAIJobDelivery(ctx, queue, delivery, adapter)
		switch result.Action {
		case aiJobDeliveryAcked:
			report.Acked++
		case aiJobDeliveryNacked:
			report.Nacked++
		}
		if result.Skipped {
			report.Skipped++
		}
		switch result.JobStatus {
		case AIJobStatusRunning:
			report.Accepted++
		case AIJobStatusQueued:
			report.Requeued++
		case AIJobStatusUnknown:
			report.Unknown++
		}
		if result.Err != nil {
			report.Errors++
			runErrs = append(runErrs, result.Err)
		}
	}
	return report, errors.Join(runErrs...)
}

type aiJobDeliveryResult string

const (
	aiJobDeliveryAcked  aiJobDeliveryResult = "acked"
	aiJobDeliveryNacked aiJobDeliveryResult = "nacked"
)

type aiJobDeliveryProcessResult struct {
	Action    aiJobDeliveryResult
	JobStatus string
	Skipped   bool
	Err       error
}

func (s *Service) processAIJobDelivery(ctx context.Context, queue AIJobDeliveryQueue, delivery AIJobDelivery, adapter DurableAIJobAdapter) aiJobDeliveryProcessResult {
	envelope := delivery.Envelope
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		if ackErr := queue.Ack(ctx, delivery); ackErr != nil {
			return aiJobDeliveryProcessResult{Skipped: true, Err: errors.Join(err, ackErr)}
		}
		return aiJobDeliveryProcessResult{Action: aiJobDeliveryAcked, Skipped: true}
	}
	job, found, err := s.repo.FindAIJob(ctx, envelope.JobID)
	if err != nil {
		return s.nackAIJobDelivery(ctx, queue, delivery, err)
	}
	if !found || !aiJobDeliveryMatches(job, envelope, s.nowUTC()) {
		if ackErr := queue.Ack(ctx, delivery); ackErr != nil {
			return aiJobDeliveryProcessResult{Skipped: true, Err: ackErr}
		}
		return aiJobDeliveryProcessResult{Action: aiJobDeliveryAcked, Skipped: true}
	}
	payload, decryptErr := decryptSecret(s.secretKey, job.RequestPayloadCiphertext)
	if decryptErr != nil {
		return s.nackAIJobDelivery(ctx, queue, delivery, decryptErr)
	}
	job.RequestPayload = payload
	job.RequestPayloadCiphertext = ""
	dispatchCtx, heartbeat, heartbeatErr := s.startAIJobDeliveryLeaseHeartbeat(ctx, queue, delivery, job)
	if heartbeatErr != nil {
		return s.nackAIJobDelivery(ctx, queue, delivery, heartbeatErr)
	}
	outcome, dispatchErr := s.dispatchClaimedAIJob(dispatchCtx, job, adapter)
	dispatchErr = errors.Join(dispatchErr, heartbeat.Stop())
	latest, latestFound, latestErr := s.repo.FindAIJob(ctx, job.ID)
	if latestErr != nil {
		return s.nackAIJobDelivery(ctx, queue, delivery, errors.Join(dispatchErr, latestErr))
	}
	if !latestFound || !oneOf(latest.Status, AIJobStatusDispatching) {
		if ackErr := queue.Ack(ctx, delivery); ackErr != nil {
			return aiJobDeliveryProcessResult{JobStatus: outcome, Err: errors.Join(dispatchErr, ackErr)}
		}
		return aiJobDeliveryProcessResult{Action: aiJobDeliveryAcked, JobStatus: outcome, Err: dispatchErr}
	}
	if dispatchErr == nil {
		if ackErr := queue.Ack(ctx, delivery); ackErr != nil {
			return aiJobDeliveryProcessResult{JobStatus: outcome, Err: ackErr}
		}
		return aiJobDeliveryProcessResult{Action: aiJobDeliveryAcked, JobStatus: outcome}
	}
	return s.nackAIJobDelivery(ctx, queue, delivery, dispatchErr)
}

func (s *Service) nackAIJobDelivery(ctx context.Context, queue AIJobDeliveryQueue, delivery AIJobDelivery, cause error) aiJobDeliveryProcessResult {
	retryAt := s.nowUTC().Add(AIJobDefaultRetryAfter)
	if err := queue.Nack(ctx, delivery, retryAt, cause.Error()); err != nil {
		return aiJobDeliveryProcessResult{Err: errors.Join(cause, err)}
	}
	return aiJobDeliveryProcessResult{Action: aiJobDeliveryNacked, Err: cause}
}

func aiJobDeliveryMatches(job AIJob, envelope AIJobDeliveryEnvelope, now time.Time) bool {
	return job.OperationID == envelope.OperationID &&
		job.Status == AIJobStatusDispatching &&
		job.StatusVersion == envelope.StatusVersion &&
		job.FenceToken == envelope.FenceToken &&
		strings.TrimSpace(job.QueueLeaseToken) == strings.TrimSpace(envelope.QueueLeaseToken) &&
		job.QueueLeaseUntil != nil && job.QueueLeaseUntil.After(now)
}
