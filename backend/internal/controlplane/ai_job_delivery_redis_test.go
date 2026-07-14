package controlplane

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisAIJobDeliveryQueueConfiguration(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	if _, err := NewRedisAIJobDeliveryQueue(nil, RedisAIJobDeliveryQueueConfig{}); !errors.Is(err, ErrRedisAIJobDeliveryConfig) {
		t.Fatalf("nil client error=%v", err)
	}
	if _, err := NewRedisAIJobDeliveryQueue(client, RedisAIJobDeliveryQueueConfig{Namespace: "invalid namespace"}); !errors.Is(err, ErrRedisAIJobDeliveryConfig) {
		t.Fatalf("invalid namespace error=%v", err)
	}
	if _, err := NewRedisAIJobDeliveryQueue(client, RedisAIJobDeliveryQueueConfig{DeliveryLease: -time.Second}); !errors.Is(err, ErrRedisAIJobDeliveryConfig) {
		t.Fatalf("invalid lease error=%v", err)
	}
}

func TestRedisAIJobDeliveryQueueContract(t *testing.T) {
	ctx := context.Background()
	leaseDuration := 80 * time.Millisecond
	queue, client := newRedisAIJobDeliveryQueueTest(t, leaseDuration)
	envelope := testAIJobDeliveryEnvelope("redis-contract", 2, 1, "redis-job-lease")
	if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	conflict := testAIJobDeliveryEnvelope("redis-conflict", 2, 1, "redis-job-lease-conflict")
	if err := queue.Publish(ctx, conflict, envelope.DedupeKey(), time.Now()); !errors.Is(err, ErrAIJobDeliveryDedupeConflict) {
		t.Fatalf("dedupe conflict error=%v", err)
	}
	deliveries, err := queue.Receive(ctx, "worker-a", 1, 0)
	if err != nil || len(deliveries) != 1 || deliveries[0].Attempt != 1 || deliveries[0].Envelope != envelope {
		t.Fatalf("first receive=%+v err=%v", deliveries, err)
	}
	wrong := deliveries[0]
	wrong.LeaseToken = "wrong"
	if err := queue.Ack(ctx, wrong); !errors.Is(err, ErrAIJobDeliveryLeaseConflict) {
		t.Fatalf("wrong lease ack error=%v", err)
	}
	extendedUntil := time.Now().Add(250 * time.Millisecond)
	if err := queue.Extend(ctx, deliveries[0], extendedUntil); err != nil {
		t.Fatalf("extend: %v", err)
	}
	time.Sleep(leaseDuration + 30*time.Millisecond)
	if early, err := queue.Receive(ctx, "worker-b", 1, 0); err != nil || len(early) != 0 {
		t.Fatalf("extended delivery reclaimed early=%+v err=%v", early, err)
	}
	if wait := time.Until(extendedUntil.Add(20 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	reclaimed, err := queue.Receive(ctx, "worker-b", 1, 0)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Attempt != 2 || reclaimed[0].LeaseToken == deliveries[0].LeaseToken {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
	if err := queue.Ack(ctx, deliveries[0]); !errors.Is(err, ErrAIJobDeliveryLeaseConflict) {
		t.Fatalf("old lease ack error=%v", err)
	}
	if err := queue.Ack(ctx, reclaimed[0]); err != nil {
		t.Fatalf("reclaimed ack: %v", err)
	}
	if length, err := client.XLen(ctx, queue.streamKey).Result(); err != nil || length != 0 {
		t.Fatalf("stream length=%d err=%v", length, err)
	}
	if tokens, err := client.HLen(ctx, queue.leaseTokenKey).Result(); err != nil || tokens != 0 {
		t.Fatalf("lease tokens=%d err=%v", tokens, err)
	}
	if deadlines, err := client.ZCard(ctx, queue.leaseUntilKey).Result(); err != nil || deadlines != 0 {
		t.Fatalf("lease deadlines=%d err=%v", deadlines, err)
	}
	if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err != nil {
		t.Fatalf("acked dedupe replay: %v", err)
	}
	if replayed, err := queue.Receive(ctx, "worker-c", 1, 0); err != nil || len(replayed) != 0 {
		t.Fatalf("acked envelope replayed=%+v err=%v", replayed, err)
	}

	delayedEnvelope := testAIJobDeliveryEnvelope("redis-delayed", 2, 1, "redis-delayed-lease")
	if err := queue.Publish(ctx, delayedEnvelope, delayedEnvelope.DedupeKey(), time.Now()); err != nil {
		t.Fatal(err)
	}
	delayed, err := queue.Receive(ctx, "worker-a", 1, 0)
	if err != nil || len(delayed) != 1 {
		t.Fatalf("delayed initial receive=%+v err=%v", delayed, err)
	}
	retryAt := time.Now().Add(120 * time.Millisecond)
	if err := queue.Nack(ctx, delayed[0], retryAt, "temporary provider failure"); err != nil {
		t.Fatalf("nack: %v", err)
	}
	if early, err := queue.Receive(ctx, "worker-b", 1, 30*time.Millisecond); err != nil || len(early) != 0 {
		t.Fatalf("delayed delivery received early=%+v err=%v", early, err)
	}
	retried, err := queue.Receive(ctx, "worker-b", 1, time.Second)
	if err != nil || len(retried) != 1 || retried[0].Attempt != 2 || retried[0].Envelope != delayedEnvelope {
		t.Fatalf("delayed retry=%+v err=%v", retried, err)
	}
	if err := queue.DeadLetter(ctx, retried[0], "retry budget exhausted"); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}
	if remaining, err := queue.Receive(ctx, "worker-c", 1, 0); err != nil || len(remaining) != 0 {
		t.Fatalf("dead-letter redelivered=%+v err=%v", remaining, err)
	}
	dead, err := client.XRange(ctx, queue.deadLetterKey, "-", "+").Result()
	if err != nil || len(dead) != 1 || redisAIJobDeliveryString(dead[0].Values["reason"]) != "retry budget exhausted" {
		t.Fatalf("dead letters=%+v err=%v", dead, err)
	}
}

func TestRedisAIJobDeliveryQueueRecoversOrphanedPendingEntry(t *testing.T) {
	ctx := context.Background()
	leaseDuration := 50 * time.Millisecond
	queue, client := newRedisAIJobDeliveryQueueTest(t, leaseDuration)
	envelope := testAIJobDeliveryEnvelope("redis-orphan", 2, 1, "redis-orphan-lease")
	if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := queue.ensureConsumerGroup(ctx); err != nil {
		t.Fatal(err)
	}
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: queue.consumerGroup, Consumer: "crashed-worker", Streams: []string{queue.streamKey, ">"}, Count: 1, Block: -1,
	}).Result()
	if err != nil || len(flattenRedisAIJobMessages(streams)) != 1 {
		t.Fatalf("crashed worker read=%+v err=%v", streams, err)
	}
	if leases, err := client.ZCard(ctx, queue.leaseUntilKey).Result(); err != nil || leases != 0 {
		t.Fatalf("orphan unexpectedly has a lease=%d err=%v", leases, err)
	}
	recovered, err := queue.Receive(ctx, "recovery-worker", 1, time.Second)
	if err != nil || len(recovered) != 1 || recovered[0].Attempt != 2 || recovered[0].Envelope != envelope {
		t.Fatalf("orphan recovery=%+v err=%v", recovered, err)
	}
	if err := queue.Ack(ctx, recovered[0]); err != nil {
		t.Fatal(err)
	}
}

func TestRedisAIJobDeliveryQueuePublishRollsBackDedupeOnStreamError(t *testing.T) {
	ctx := context.Background()
	queue, client := newRedisAIJobDeliveryQueueTest(t, time.Second)
	envelope := testAIJobDeliveryEnvelope("redis-publish-error", 2, 1, "redis-publish-error-lease")
	if err := client.Set(ctx, queue.streamKey, "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err == nil {
		t.Fatal("publish with wrong stream type succeeded")
	}
	if exists, err := client.Exists(ctx, queue.dedupeKey(envelope.DedupeKey())).Result(); err != nil || exists != 0 {
		t.Fatalf("dedupe key survived failed publish: exists=%d err=%v", exists, err)
	}
}

func TestRedisAIJobDeliveryQueueConcurrentConsumersClaimUniqueEntries(t *testing.T) {
	ctx := context.Background()
	queue, client := newRedisAIJobDeliveryQueueTest(t, time.Second)
	const total = 20
	for index := 0; index < total; index++ {
		envelope := testAIJobDeliveryEnvelope("redis-concurrent-"+strconv.Itoa(index), 2, 1, "redis-concurrent-lease-"+strconv.Itoa(index))
		if err := queue.Publish(ctx, envelope, envelope.DedupeKey(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	deliveryIDs := make(chan string, total)
	errorsSeen := make(chan error, 4)
	var wait sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			deliveries, err := queue.Receive(ctx, "worker-"+strconv.Itoa(worker), 5, time.Second)
			if err != nil {
				errorsSeen <- err
				return
			}
			if len(deliveries) != 5 {
				errorsSeen <- fmt.Errorf("worker %d received %d deliveries", worker, len(deliveries))
				return
			}
			for _, delivery := range deliveries {
				if err := queue.Ack(ctx, delivery); err != nil {
					errorsSeen <- err
					return
				}
				deliveryIDs <- delivery.Envelope.DeliveryID
			}
		}(worker)
	}
	wait.Wait()
	close(errorsSeen)
	close(deliveryIDs)
	for err := range errorsSeen {
		t.Errorf("concurrent consumer: %v", err)
	}
	unique := map[string]struct{}{}
	for id := range deliveryIDs {
		if _, exists := unique[id]; exists {
			t.Errorf("delivery %s was claimed more than once", id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) != total {
		t.Fatalf("unique deliveries=%d want=%d", len(unique), total)
	}
	if length, err := client.XLen(ctx, queue.streamKey).Result(); err != nil || length != 0 {
		t.Fatalf("stream length=%d err=%v", length, err)
	}
	if tokens, err := client.HLen(ctx, queue.leaseTokenKey).Result(); err != nil || tokens != 0 {
		t.Fatalf("lease tokens=%d err=%v", tokens, err)
	}
	if deadlines, err := client.ZCard(ctx, queue.leaseUntilKey).Result(); err != nil || deadlines != 0 {
		t.Fatalf("lease deadlines=%d err=%v", deadlines, err)
	}
	if attempts, err := client.HLen(ctx, queue.attemptKey).Result(); err != nil || attempts != 0 {
		t.Fatalf("attempt entries=%d err=%v", attempts, err)
	}
}

func TestRedisAIJobDeliveryQueueRebuildsFromAuthoritativeJobState(t *testing.T) {
	ctx := context.Background()
	queue, client := newRedisAIJobDeliveryQueueTest(t, time.Second)
	svc := NewService(NewMemoryRepository(), "/v1", "redis-rebuild-secret")
	setupDurableWorkerRoutes(t, svc)
	job := beginDurableWorkerJob(t, svc, "redis-authoritative-rebuild")
	if report, err := svc.RunDurableAIJobSchedulerOnce(ctx, "scheduler-before-loss", time.Minute, 1, queue); err != nil || report.Published != 1 {
		t.Fatalf("initial scheduler report=%+v err=%v", report, err)
	}
	if err := deleteRedisAIJobDeliveryTestNamespace(ctx, client, queue); err != nil {
		t.Fatalf("simulate Redis loss: %v", err)
	}
	rebuilt, err := svc.RebuildDurableAIJobDeliveriesOnce(ctx, "delivery-rebuilder", time.Minute, 10, queue)
	if err != nil || rebuilt.Scanned != 1 || rebuilt.Republished != 1 || rebuilt.Published != 1 || rebuilt.Errors != 0 {
		t.Fatalf("rebuild report=%+v err=%v", rebuilt, err)
	}
	adapter := &durableAIJobAdapterStub{dispatchSteps: []durableDispatchStep{{result: ProviderDispatchResult{
		Outcome: ProviderDispatchOutcomeAccepted,
		Task:    ProviderTaskReference{ProviderTaskID: "redis-rebuilt-task", Status: "running"},
	}}}}
	report, err := svc.RunDurableAIJobDeliveryWorkerOnce(ctx, "redis-delivery-worker", 1, time.Second, queue, adapter)
	if err != nil || report.Received != 1 || report.Acked != 1 || report.Accepted != 1 || report.Errors != 0 || adapter.DispatchCalls() != 1 {
		t.Fatalf("delivery worker report=%+v calls=%d err=%v", report, adapter.DispatchCalls(), err)
	}
	assertAIJobStatus(t, svc, job.ID, AIJobStatusRunning)
}

func newRedisAIJobDeliveryQueueTest(t *testing.T, deliveryLease time.Duration) (*RedisAIJobDeliveryQueue, *redis.Client) {
	t.Helper()
	rawURL := strings.TrimSpace(os.Getenv("ASTER_TEST_REDIS_URL"))
	if rawURL == "" {
		t.Skip("ASTER_TEST_REDIS_URL is not set")
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatalf("parse ASTER_TEST_REDIS_URL: %v", err)
	}
	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("test Redis is unavailable: %v", err)
	}
	queue, err := NewRedisAIJobDeliveryQueue(client, RedisAIJobDeliveryQueueConfig{
		Namespace: "test_" + randomID(12), ConsumerGroup: "test-workers", DeliveryLease: deliveryLease,
		DedupeTTL: time.Hour, PromotionBatch: 20,
	})
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = deleteRedisAIJobDeliveryTestNamespace(cleanupCtx, client, queue)
		_ = client.Close()
	})
	return queue, client
}

func deleteRedisAIJobDeliveryTestNamespace(ctx context.Context, client *redis.Client, queue *RedisAIJobDeliveryQueue) error {
	prefix := strings.TrimSuffix(queue.streamKey, ":stream")
	iterator := client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	keys := make([]string, 0)
	for iterator.Next(ctx) {
		keys = append(keys, iterator.Val())
	}
	if err := iterator.Err(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return client.Del(ctx, keys...).Err()
}
