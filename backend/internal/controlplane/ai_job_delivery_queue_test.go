package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryAIJobDeliveryQueueContract(t *testing.T) {
	base := time.Date(2026, time.July, 14, 20, 0, 0, 0, time.UTC)
	now := base
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	envelope := testAIJobDeliveryEnvelope("job-one", 2, 1, "job-lease-one")
	if err := queue.Publish(context.Background(), envelope, envelope.DedupeKey(), base); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(context.Background(), envelope, envelope.DedupeKey(), base); err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	conflict := testAIJobDeliveryEnvelope("job-conflict", 2, 1, "job-lease-conflict")
	if err := queue.Publish(context.Background(), conflict, envelope.DedupeKey(), base); !errors.Is(err, ErrAIJobDeliveryDedupeConflict) {
		t.Fatalf("dedupe conflict error=%v", err)
	}

	deliveries, err := queue.Receive(context.Background(), "worker-a", 10, 0)
	if err != nil || len(deliveries) != 1 || deliveries[0].Attempt != 1 {
		t.Fatalf("first receive=%+v err=%v", deliveries, err)
	}
	wrongLease := deliveries[0]
	wrongLease.LeaseToken = "wrong"
	if err := queue.Ack(context.Background(), wrongLease); !errors.Is(err, ErrAIJobDeliveryLeaseConflict) {
		t.Fatalf("wrong lease ack error=%v", err)
	}
	if err := queue.Extend(context.Background(), deliveries[0], base.Add(2*time.Minute)); err != nil {
		t.Fatalf("extend delivery: %v", err)
	}
	now = base.Add(90 * time.Second)
	if received, err := queue.Receive(context.Background(), "worker-b", 1, 0); err != nil || len(received) != 0 {
		t.Fatalf("extended lease redelivered=%+v err=%v", received, err)
	}
	now = base.Add(2 * time.Minute)
	redelivered, err := queue.Receive(context.Background(), "worker-b", 1, 0)
	if err != nil || len(redelivered) != 1 || redelivered[0].Attempt != 2 || redelivered[0].LeaseToken == deliveries[0].LeaseToken {
		t.Fatalf("expired lease redelivery=%+v err=%v", redelivered, err)
	}
	if err := queue.Ack(context.Background(), deliveries[0]); !errors.Is(err, ErrAIJobDeliveryLeaseConflict) {
		t.Fatalf("old lease ack error=%v", err)
	}
	if err := queue.Ack(context.Background(), redelivered[0]); err != nil {
		t.Fatalf("redelivery ack: %v", err)
	}
	if received, err := queue.Receive(context.Background(), "worker-c", 1, 0); err != nil || len(received) != 0 {
		t.Fatalf("acked delivery received=%+v err=%v", received, err)
	}
}

func TestMemoryAIJobDeliveryQueueDeadLettersWithLease(t *testing.T) {
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testAIJobDeliveryEnvelope("job-dead-letter", 2, 1, "job-lease-dead-letter")
	if err := queue.Publish(context.Background(), envelope, envelope.DedupeKey(), time.Now()); err != nil {
		t.Fatal(err)
	}
	deliveries, err := queue.Receive(context.Background(), "worker-a", 1, 0)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive=%+v err=%v", deliveries, err)
	}
	wrong := deliveries[0]
	wrong.LeaseToken = "wrong"
	if err := queue.DeadLetter(context.Background(), wrong, "retry budget exhausted"); !errors.Is(err, ErrAIJobDeliveryLeaseConflict) {
		t.Fatalf("wrong lease dead-letter error=%v", err)
	}
	if err := queue.DeadLetter(context.Background(), deliveries[0], "retry budget exhausted"); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}
	if redelivered, err := queue.Receive(context.Background(), "worker-b", 1, 0); err != nil || len(redelivered) != 0 {
		t.Fatalf("dead-letter redelivered=%+v err=%v", redelivered, err)
	}
}

func TestMemoryAIJobDeliveryQueueNackDelay(t *testing.T) {
	base := time.Date(2026, time.July, 14, 21, 0, 0, 0, time.UTC)
	now := base
	queue, err := NewMemoryAIJobDeliveryQueue(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	queue.now = func() time.Time { return now }
	envelope := testAIJobDeliveryEnvelope("job-delay", 2, 1, "job-lease-delay")
	if err := queue.Publish(context.Background(), envelope, "delay", base); err != nil {
		t.Fatal(err)
	}
	deliveries, err := queue.Receive(context.Background(), "worker-a", 1, 0)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("receive=%+v err=%v", deliveries, err)
	}
	if err := queue.Nack(context.Background(), deliveries[0], base.Add(10*time.Second), "temporary failure"); err != nil {
		t.Fatal(err)
	}
	if received, err := queue.Receive(context.Background(), "worker-b", 1, 0); err != nil || len(received) != 0 {
		t.Fatalf("early retry=%+v err=%v", received, err)
	}
	now = base.Add(10 * time.Second)
	retried, err := queue.Receive(context.Background(), "worker-b", 1, 0)
	if err != nil || len(retried) != 1 || retried[0].Attempt != 2 {
		t.Fatalf("delayed retry=%+v err=%v", retried, err)
	}
}

func TestMemoryAIJobDeliveryQueueLongPollReclaimsExpiredLease(t *testing.T) {
	queue, err := NewMemoryAIJobDeliveryQueue(25 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testAIJobDeliveryEnvelope("job-long-poll", 2, 1, "job-lease-long-poll")
	if err := queue.Publish(context.Background(), envelope, "long-poll", time.Now()); err != nil {
		t.Fatal(err)
	}
	first, err := queue.Receive(context.Background(), "worker-a", 1, 0)
	if err != nil || len(first) != 1 {
		t.Fatalf("first receive=%+v err=%v", first, err)
	}
	started := time.Now()
	retried, err := queue.Receive(context.Background(), "worker-b", 1, time.Second)
	if err != nil || len(retried) != 1 || retried[0].Attempt != 2 {
		t.Fatalf("lease-expiry receive=%+v err=%v", retried, err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("lease expiry was not used as the long-poll wake time: %v", elapsed)
	}
}

func TestAIJobDeliveryEnvelopeContainsOnlyDispatchMetadata(t *testing.T) {
	envelope := testAIJobDeliveryEnvelope("job-metadata", 4, 3, "job-lease-metadata")
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"prompt", "secret", "credential", "artifact", "media", "payload"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("delivery envelope contains forbidden field %q: %s", forbidden, serialized)
		}
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 {
		t.Fatalf("delivery envelope fields=%v", fields)
	}
}

func testAIJobDeliveryEnvelope(jobID string, statusVersion int, fenceToken int64, queueLeaseToken string) AIJobDeliveryEnvelope {
	job := AIJob{ID: jobID, OperationID: "operation-" + jobID, StatusVersion: statusVersion, FenceToken: fenceToken, QueueLeaseToken: queueLeaseToken}
	envelope, err := NewAIJobDeliveryEnvelope(job)
	if err != nil {
		panic(err)
	}
	return envelope
}
