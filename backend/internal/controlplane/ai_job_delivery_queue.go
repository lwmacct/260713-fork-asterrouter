package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrAIJobDeliveryQueueRequired        = errors.New("ai job delivery queue is required")
	ErrAIJobDeliveryEnvelopeInvalid      = errors.New("invalid ai job delivery envelope")
	ErrAIJobDeliveryDedupeConflict       = errors.New("ai job delivery dedupe key conflicts with an existing envelope")
	ErrAIJobDeliveryNotFound             = errors.New("ai job delivery was not found")
	ErrAIJobDeliveryLeaseConflict        = errors.New("ai job delivery lease does not match")
	ErrAIJobDeliveryLeaseExpired         = errors.New("ai job delivery lease has expired")
	ErrAIJobDeliveryLeaseDurationInvalid = errors.New("ai job delivery lease duration must be positive")
)

const aiJobDeliveryEnvelopeSchemaVersion = 1

// AIJobDeliveryEnvelope is the only payload sent through the worker queue.
// Request bodies, provider secrets and artifact bytes remain in their
// respective stores and are loaded only after the worker validates this
// versioned claim against PostgreSQL.
type AIJobDeliveryEnvelope struct {
	SchemaVersion   int    `json:"schema_version"`
	DeliveryID      string `json:"delivery_id"`
	JobID           string `json:"job_id"`
	OperationID     string `json:"operation_id"`
	StatusVersion   int    `json:"status_version"`
	FenceToken      int64  `json:"fence_token"`
	QueueLeaseToken string `json:"queue_lease_token"`
}

func NewAIJobDeliveryEnvelope(job AIJob) (AIJobDeliveryEnvelope, error) {
	envelope := AIJobDeliveryEnvelope{
		SchemaVersion:   aiJobDeliveryEnvelopeSchemaVersion,
		DeliveryID:      durableAIJobDeliveryID(job),
		JobID:           strings.TrimSpace(job.ID),
		OperationID:     strings.TrimSpace(job.OperationID),
		StatusVersion:   job.StatusVersion,
		FenceToken:      job.FenceToken,
		QueueLeaseToken: strings.TrimSpace(job.QueueLeaseToken),
	}
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return AIJobDeliveryEnvelope{}, err
	}
	return envelope, nil
}

func (e AIJobDeliveryEnvelope) DedupeKey() string {
	return strings.TrimSpace(e.DeliveryID)
}

func validateAIJobDeliveryEnvelope(envelope AIJobDeliveryEnvelope) error {
	if envelope.SchemaVersion != aiJobDeliveryEnvelopeSchemaVersion ||
		strings.TrimSpace(envelope.DeliveryID) == "" ||
		strings.TrimSpace(envelope.JobID) == "" ||
		strings.TrimSpace(envelope.OperationID) == "" ||
		envelope.StatusVersion <= 0 ||
		envelope.FenceToken <= 0 ||
		strings.TrimSpace(envelope.QueueLeaseToken) == "" {
		return ErrAIJobDeliveryEnvelopeInvalid
	}
	expectedID := durableAIJobDeliveryIDParts(envelope.JobID, envelope.OperationID, envelope.StatusVersion, envelope.FenceToken)
	if envelope.DeliveryID != expectedID {
		return ErrAIJobDeliveryEnvelopeInvalid
	}
	return nil
}

func durableAIJobDeliveryID(job AIJob) string {
	return durableAIJobDeliveryIDParts(job.ID, job.OperationID, job.StatusVersion, job.FenceToken)
}

func durableAIJobDeliveryIDParts(jobID, operationID string, statusVersion int, fenceToken int64) string {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(operationID) == "" || statusVersion <= 0 || fenceToken <= 0 {
		return ""
	}
	return "job_delivery_" + prefix(hashAPIKey(fmt.Sprintf("%s\x00%s\x00%d\x00%d", jobID, operationID, statusVersion, fenceToken)), 32)
}

// AIJobDelivery is a broker delivery lease. QueueLeaseToken belongs to the
// PostgreSQL Job claim; LeaseToken belongs to this delivery attempt and must
// be presented for every mutation of the queue entry.
type AIJobDelivery struct {
	ID         string
	Envelope   AIJobDeliveryEnvelope
	Consumer   string
	Attempt    int
	LeaseUntil time.Time
	LeaseToken string
}

// AIJobDeliveryQueue is intentionally smaller than a broker-specific API.
// Implementations provide at-least-once delivery with explicit visibility
// leases; Core remains responsible for idempotency and fencing.
type AIJobDeliveryQueue interface {
	Publish(ctx context.Context, envelope AIJobDeliveryEnvelope, dedupeKey string, availableAt time.Time) error
	Receive(ctx context.Context, consumer string, maxItems int, wait time.Duration) ([]AIJobDelivery, error)
	Extend(ctx context.Context, delivery AIJobDelivery, leaseUntil time.Time) error
	Ack(ctx context.Context, delivery AIJobDelivery) error
	Nack(ctx context.Context, delivery AIJobDelivery, retryAt time.Time, reason string) error
	DeadLetter(ctx context.Context, delivery AIJobDelivery, reason string) error
}

type memoryAIJobDeliveryStatus string

const (
	memoryAIJobDeliveryPending  memoryAIJobDeliveryStatus = "pending"
	memoryAIJobDeliveryInflight memoryAIJobDeliveryStatus = "inflight"
	memoryAIJobDeliveryAcked    memoryAIJobDeliveryStatus = "acked"
	memoryAIJobDeliveryDead     memoryAIJobDeliveryStatus = "dead"
)

type memoryAIJobDeliveryEntry struct {
	envelope    AIJobDeliveryEnvelope
	dedupeKey   string
	status      memoryAIJobDeliveryStatus
	availableAt time.Time
	leaseUntil  time.Time
	leaseToken  string
	consumer    string
	attempt     int
	sequence    uint64
	lastError   string
}

// MemoryAIJobDeliveryQueue is the single-process baseline used by tests and
// the smallest development profile. It deliberately preserves the same
// lease, duplicate and at-least-once semantics as the production adapters.
type MemoryAIJobDeliveryQueue struct {
	mu            sync.Mutex
	entries       map[string]*memoryAIJobDeliveryEntry
	byDedupe      map[string]string
	notify        chan struct{}
	now           func() time.Time
	deliveryLease time.Duration
	nextSequence  uint64
}

func NewMemoryAIJobDeliveryQueue(deliveryLease time.Duration) (*MemoryAIJobDeliveryQueue, error) {
	if deliveryLease <= 0 {
		return nil, ErrAIJobDeliveryLeaseDurationInvalid
	}
	return &MemoryAIJobDeliveryQueue{
		entries:       map[string]*memoryAIJobDeliveryEntry{},
		byDedupe:      map[string]string{},
		notify:        make(chan struct{}),
		now:           time.Now,
		deliveryLease: deliveryLease,
	}, nil
}

func (q *MemoryAIJobDeliveryQueue) Publish(ctx context.Context, envelope AIJobDeliveryEnvelope, dedupeKey string, availableAt time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return err
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = envelope.DedupeKey()
	}
	if dedupeKey == "" {
		return ErrAIJobDeliveryEnvelopeInvalid
	}
	now := q.nowUTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if existingID, exists := q.byDedupe[dedupeKey]; exists {
		existing := q.entries[existingID]
		if existing != nil && existing.envelope != envelope {
			return ErrAIJobDeliveryDedupeConflict
		}
		return nil
	}
	if existing := q.entries[envelope.DeliveryID]; existing != nil {
		if existing.envelope != envelope || existing.dedupeKey != dedupeKey {
			return ErrAIJobDeliveryDedupeConflict
		}
		return nil
	}
	q.nextSequence++
	q.entries[envelope.DeliveryID] = &memoryAIJobDeliveryEntry{
		envelope: envelope, dedupeKey: dedupeKey, status: memoryAIJobDeliveryPending,
		availableAt: availableAt, sequence: q.nextSequence,
	}
	q.byDedupe[dedupeKey] = envelope.DeliveryID
	q.signalLocked()
	return nil
}

func (q *MemoryAIJobDeliveryQueue) Receive(ctx context.Context, consumer string, maxItems int, wait time.Duration) ([]AIJobDelivery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	consumer = strings.TrimSpace(consumer)
	if consumer == "" || maxItems <= 0 {
		return []AIJobDelivery{}, nil
	}
	if wait < 0 {
		wait = 0
	}
	deadline := time.Time{}
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		now := q.nowUTC()
		q.mu.Lock()
		q.reclaimExpiredLocked(now)
		ready := q.readyEntriesLocked(now, maxItems)
		if len(ready) > 0 {
			out := make([]AIJobDelivery, 0, len(ready))
			for _, entry := range ready {
				entry.status = memoryAIJobDeliveryInflight
				entry.attempt++
				entry.consumer = consumer
				entry.leaseToken = "delivery_lease_" + randomID(16)
				entry.leaseUntil = now.Add(q.deliveryLease)
				out = append(out, AIJobDelivery{
					ID: entry.envelope.DeliveryID, Envelope: entry.envelope, Consumer: entry.consumer,
					Attempt: entry.attempt, LeaseUntil: entry.leaseUntil, LeaseToken: entry.leaseToken,
				})
			}
			q.mu.Unlock()
			return out, nil
		}
		if deadline.IsZero() {
			q.mu.Unlock()
			return []AIJobDelivery{}, nil
		}
		waitFor := time.Duration(0)
		waitFor = time.Until(deadline)
		if waitFor <= 0 {
			q.mu.Unlock()
			return []AIJobDelivery{}, nil
		}
		notify := q.notify
		nextWake := q.nextWakeLocked(now)
		if !nextWake.IsZero() {
			untilNext := nextWake.Sub(now)
			if untilNext < waitFor {
				waitFor = untilNext
			}
		}
		q.mu.Unlock()
		if waitFor <= 0 {
			continue
		}
		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-notify:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

func (q *MemoryAIJobDeliveryQueue) Extend(ctx context.Context, delivery AIJobDelivery, leaseUntil time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if !leaseUntil.After(q.nowUTC()) {
		return ErrAIJobDeliveryLeaseExpired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, err := q.validateLeaseLocked(delivery)
	if err != nil {
		return err
	}
	entry.leaseUntil = leaseUntil
	return nil
}

func (q *MemoryAIJobDeliveryQueue) Ack(ctx context.Context, delivery AIJobDelivery) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, err := q.validateLeaseLocked(delivery)
	if err != nil {
		return err
	}
	entry.status = memoryAIJobDeliveryAcked
	entry.leaseUntil = time.Time{}
	entry.leaseToken = ""
	entry.consumer = ""
	q.signalLocked()
	return nil
}

func (q *MemoryAIJobDeliveryQueue) Nack(ctx context.Context, delivery AIJobDelivery, retryAt time.Time, reason string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now := q.nowUTC()
	if retryAt.IsZero() || retryAt.Before(now) {
		retryAt = now
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, err := q.validateLeaseLocked(delivery)
	if err != nil {
		return err
	}
	entry.status = memoryAIJobDeliveryPending
	entry.availableAt = retryAt
	entry.lastError = strings.TrimSpace(reason)
	entry.leaseUntil = time.Time{}
	entry.leaseToken = ""
	entry.consumer = ""
	q.signalLocked()
	return nil
}

func (q *MemoryAIJobDeliveryQueue) DeadLetter(ctx context.Context, delivery AIJobDelivery, reason string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, err := q.validateLeaseLocked(delivery)
	if err != nil {
		return err
	}
	entry.status = memoryAIJobDeliveryDead
	entry.lastError = strings.TrimSpace(reason)
	entry.leaseUntil = time.Time{}
	entry.leaseToken = ""
	entry.consumer = ""
	q.signalLocked()
	return nil
}

func (q *MemoryAIJobDeliveryQueue) validateLeaseLocked(delivery AIJobDelivery) (*memoryAIJobDeliveryEntry, error) {
	entry := q.entries[strings.TrimSpace(delivery.ID)]
	if entry == nil {
		return nil, ErrAIJobDeliveryNotFound
	}
	if entry.status != memoryAIJobDeliveryInflight || entry.leaseToken == "" || entry.leaseToken != delivery.LeaseToken {
		return nil, ErrAIJobDeliveryLeaseConflict
	}
	if !entry.leaseUntil.After(q.nowUTC()) {
		return nil, ErrAIJobDeliveryLeaseExpired
	}
	return entry, nil
}

func (q *MemoryAIJobDeliveryQueue) reclaimExpiredLocked(now time.Time) {
	changed := false
	for _, entry := range q.entries {
		if entry.status != memoryAIJobDeliveryInflight || entry.leaseUntil.After(now) {
			continue
		}
		entry.status = memoryAIJobDeliveryPending
		entry.availableAt = now
		entry.leaseUntil = time.Time{}
		entry.leaseToken = ""
		entry.consumer = ""
		changed = true
	}
	if changed {
		q.signalLocked()
	}
}

func (q *MemoryAIJobDeliveryQueue) readyEntriesLocked(now time.Time, limit int) []*memoryAIJobDeliveryEntry {
	ready := make([]*memoryAIJobDeliveryEntry, 0, len(q.entries))
	for _, entry := range q.entries {
		if entry.status == memoryAIJobDeliveryPending && !entry.availableAt.After(now) {
			ready = append(ready, entry)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		if !ready[i].availableAt.Equal(ready[j].availableAt) {
			return ready[i].availableAt.Before(ready[j].availableAt)
		}
		return ready[i].sequence < ready[j].sequence
	})
	if len(ready) > limit {
		ready = ready[:limit]
	}
	return ready
}

func (q *MemoryAIJobDeliveryQueue) nextWakeLocked(now time.Time) time.Time {
	var next time.Time
	for _, entry := range q.entries {
		candidate := time.Time{}
		switch entry.status {
		case memoryAIJobDeliveryPending:
			candidate = entry.availableAt
		case memoryAIJobDeliveryInflight:
			candidate = entry.leaseUntil
		}
		if !candidate.After(now) {
			continue
		}
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	return next
}

func (q *MemoryAIJobDeliveryQueue) signalLocked() {
	close(q.notify)
	q.notify = make(chan struct{})
}

func (q *MemoryAIJobDeliveryQueue) nowUTC() time.Time {
	if q != nil && q.now != nil {
		return q.now().UTC()
	}
	return time.Now().UTC()
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
