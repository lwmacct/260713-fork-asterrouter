package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestTransactionalOutboxLeaseAndAggregateOrderingContract(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			base := time.Date(2026, time.July, 14, 10, 0, 0, 0, time.UTC)
			aggregateID := seedTransactionalOutboxVersions(t, repo, base, 1, 2)

			claimed, err := repo.ClaimDueTransactionalOutboxEvents(ctx, base, base.Add(transactionalOutboxLease), "lease-1", 10)
			if err != nil || len(claimed) != 1 || claimed[0].EventVersion != 1 || claimed[0].AttemptCount != 1 || claimed[0].Status != OutboxStatusPublishing {
				t.Fatalf("first claim=%+v err=%v", claimed, err)
			}
			if next, err := repo.ClaimDueTransactionalOutboxEvents(ctx, base.Add(time.Second), base.Add(time.Minute), "lease-other", 10); err != nil || len(next) != 0 {
				t.Fatalf("active lease claim=%+v err=%v", next, err)
			}
			if err := repo.CompleteTransactionalOutboxEvent(ctx, claimed[0].ID, "wrong-lease", base.Add(time.Second)); err == nil {
				t.Fatal("completion accepted a mismatched lease")
			}

			reclaimedAt := base.Add(transactionalOutboxLease + time.Second)
			reclaimed, err := repo.ClaimDueTransactionalOutboxEvents(ctx, reclaimedAt, reclaimedAt.Add(transactionalOutboxLease), "lease-2", 10)
			if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != claimed[0].ID || reclaimed[0].AttemptCount != 2 {
				t.Fatalf("expired lease claim=%+v err=%v", reclaimed, err)
			}
			if err := repo.CompleteTransactionalOutboxEvent(ctx, reclaimed[0].ID, "lease-2", reclaimedAt); err != nil {
				t.Fatal(err)
			}
			second, err := repo.ClaimDueTransactionalOutboxEvents(ctx, reclaimedAt, reclaimedAt.Add(transactionalOutboxLease), "lease-3", 10)
			if err != nil || len(second) != 1 || second[0].EventVersion != 2 {
				t.Fatalf("second version claim=%+v err=%v", second, err)
			}
			if err := repo.CompleteTransactionalOutboxEvent(ctx, second[0].ID, "lease-3", reclaimedAt); err != nil {
				t.Fatal(err)
			}
			events, err := repo.ListTransactionalOutboxEvents(ctx, aggregateID)
			if err != nil || len(events) != 2 || events[0].Status != OutboxStatusPublished || events[1].Status != OutboxStatusPublished {
				t.Fatalf("published events=%+v err=%v", events, err)
			}
		})
	}
}

func TestTransactionalOutboxPublisherRetriesDeadLettersAndRequeues(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Repository
	}{
		{name: "memory", open: func(*testing.T) Repository { return NewMemoryRepository() }},
		{name: "postgres", open: func(t *testing.T) Repository {
			schema := testutil.NewPostgresSchema(t)
			repo, err := NewPostgresRepository(context.Background(), schema.URL)
			if err != nil {
				t.Fatalf("NewPostgresRepository(): %v", err)
			}
			return repo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := test.open(t)
			t.Cleanup(func() { _ = repo.Close() })
			base := time.Date(2026, time.July, 14, 11, 0, 0, 0, time.UTC)
			svc := NewService(repo, "/v1")
			svc.now = func() time.Time { return base }
			aggregateID := seedTransactionalOutboxVersionsWithMax(t, repo, base, 2, 1)
			events, err := repo.ListTransactionalOutboxEvents(ctx, aggregateID)
			if err != nil || len(events) != 1 {
				t.Fatalf("seeded events=%+v err=%v", events, err)
			}
			event := events[0]

			publisher := &recordingTransactionalOutboxPublisher{failuresRemaining: 2}
			svc.SetTransactionalOutboxPublisher(publisher)
			if err := svc.PublishDueTransactionalOutbox(ctx, 10); err == nil {
				t.Fatal("first publisher failure was not reported")
			}
			events, _ = repo.ListTransactionalOutboxEvents(ctx, aggregateID)
			if events[0].Status != OutboxStatusPending || events[0].AttemptCount != 1 || events[0].LastError == "" || !events[0].AvailableAt.Equal(base.Add(5*time.Second)) {
				t.Fatalf("first retry event=%+v", events[0])
			}

			base = base.Add(5 * time.Second)
			if err := svc.PublishDueTransactionalOutbox(ctx, 10); err == nil {
				t.Fatal("terminal publisher failure was not reported")
			}
			events, _ = repo.ListTransactionalOutboxEvents(ctx, aggregateID)
			if events[0].Status != OutboxStatusDeadLetter || events[0].AttemptCount != 2 {
				t.Fatalf("dead-letter event=%+v", events[0])
			}
			if err := svc.RequeueTransactionalOutboxEvent(ctx, event.ID); err != nil {
				t.Fatal(err)
			}
			if err := svc.PublishDueTransactionalOutbox(ctx, 10); err != nil {
				t.Fatalf("requeued publish: %v", err)
			}
			events, _ = repo.ListTransactionalOutboxEvents(ctx, aggregateID)
			if events[0].Status != OutboxStatusPublished || events[0].PublishedAt == nil || events[0].AttemptCount != 1 || events[0].LastError != "" {
				t.Fatalf("published event=%+v", events[0])
			}
			if publisher.callCount.Load() != 3 {
				t.Fatalf("publisher calls=%d", publisher.callCount.Load())
			}
		})
	}
}

func TestTransactionalOutboxClaimIsAtomicAcrossPostgresInstances(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	repoA, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoA.Close()
	repoB, err := NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repoB.Close()
	base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	seedTransactionalOutboxVersions(t, repoA, base, 1)

	var claimedCount atomic.Int32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			repo := repoA
			if worker%2 == 1 {
				repo = repoB
			}
			claimed, err := repo.ClaimDueTransactionalOutboxEvents(ctx, base, base.Add(transactionalOutboxLease), fmt.Sprintf("lease-%d", worker), 1)
			if err != nil {
				errorsSeen <- err
				return
			}
			claimedCount.Add(int32(len(claimed)))
		}(index)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("claim: %v", err)
	}
	if claimedCount.Load() != 1 {
		t.Fatalf("claimed=%d want=1", claimedCount.Load())
	}
}

func TestTransactionalOutboxPublisherMustBeConfiguredBeforeClaim(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	base := time.Date(2026, time.July, 14, 13, 0, 0, 0, time.UTC)
	aggregateID := seedTransactionalOutboxVersions(t, repo, base, 1)
	if err := svc.PublishDueTransactionalOutbox(context.Background(), 10); !errors.Is(err, ErrTransactionalOutboxPublisherUnavailable) {
		t.Fatalf("publisher error=%v", err)
	}
	events, _ := repo.ListTransactionalOutboxEvents(context.Background(), aggregateID)
	if len(events) != 1 || events[0].AttemptCount != 0 || events[0].Status != OutboxStatusPending {
		t.Fatalf("unconfigured publisher mutated event=%+v", events)
	}
}

type recordingTransactionalOutboxPublisher struct {
	callCount         atomic.Int32
	failuresRemaining int
	mu                sync.Mutex
}

func (p *recordingTransactionalOutboxPublisher) PublishTransactionalOutbox(_ context.Context, event TransactionalOutboxEvent) error {
	p.callCount.Add(1)
	if event.ID == "" || event.PayloadJSON == "" || event.Status != OutboxStatusPublishing {
		return errors.New("publisher received an invalid event")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failuresRemaining > 0 {
		p.failuresRemaining--
		return errors.New("synthetic broker failure")
	}
	return nil
}

func seedTransactionalOutboxVersions(t *testing.T, repo Repository, base time.Time, versions ...int) string {
	return seedTransactionalOutboxVersionsWithMax(t, repo, base, OutboxDefaultMaxAttempts, versions...)
}

func seedTransactionalOutboxVersionsWithMax(t *testing.T, repo Repository, base time.Time, maxAttempts int, versions ...int) string {
	t.Helper()
	ctx := context.Background()
	svc := NewService(repo, "/v1")
	svc.now = func() time.Time { return base }
	publishTestUsagePricingRule(t, svc, `v1: unit_line("input", total_input_tokens, "token", 1)`)
	operation, created, err := svc.BeginCanonicalOperation(ctx, operationTestAuth(), operationTestRequest("", "outbox-fingerprint"))
	if err != nil || !created {
		t.Fatalf("BeginCanonicalOperation() created=%t err=%v", created, err)
	}
	attempt, err := svc.BeginAIAttempt(ctx, operation.ID, 1, GatewayProvider{})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range versions {
		record := UsageRecord{
			ID: "usage-seed", OperationID: operation.ID, AttemptID: attempt.ID, UsageVersion: version, UsageSource: "synthetic",
			RequestFingerprint: operation.RequestFingerprint, APIKeyID: "key-operation", APIFingerprint: "fingerprint-operation",
			Model: "model-a", Status: "forwarded", InputTokens: version, UsageCostCurrency: "USD", PricingStatus: "unpriced", CreatedAt: base,
		}
		record.ID = "usage_" + usageLedgerDigest(record)
		settlement, err := svc.buildUsageSettlement(ctx, record)
		if err != nil {
			t.Fatal(err)
		}
		for index := range settlement.OutboxEvents {
			settlement.OutboxEvents[index].MaxAttempts = maxAttempts
		}
		if applied, err := repo.ApplyUsageSettlement(ctx, settlement); err != nil || !applied {
			t.Fatalf("ApplyUsageSettlement(version=%d) applied=%t err=%v", version, applied, err)
		}
	}
	return operation.ID + ":" + attempt.ID
}
