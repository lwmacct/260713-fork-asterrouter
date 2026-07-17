package operator

import (
	"context"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

func TestPostgresRepositoryAppliesBalanceEntryAtomicallyAndIdempotently(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo, err := NewRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("NewRepository(): %v", err)
	}

	customer := Customer{ID: "customer-postgres", Name: "Customer", Status: StatusActive, BalanceMicros: 100, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveCustomer(ctx, customer); err != nil {
		t.Fatalf("SaveCustomer(): %v", err)
	}
	entry := BalanceEntry{ID: "entry-postgres", CustomerID: customer.ID, Kind: "allocation", AmountMicros: 50, Reference: "idempotency-1", Actor: "tester", CreatedAt: now}
	first, err := repo.ApplyBalanceEntry(ctx, entry)
	if err != nil {
		t.Fatalf("ApplyBalanceEntry(first): %v", err)
	}
	second := entry
	second.ID = "entry-postgres-retry"
	second, err = repo.ApplyBalanceEntry(ctx, second)
	if err != nil {
		t.Fatalf("ApplyBalanceEntry(retry): %v", err)
	}
	if first.ID != second.ID || first.BalanceAfter != 150 || second.BalanceAfter != 150 {
		t.Fatalf("idempotent entries first=%#v second=%#v", first, second)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("reopen NewRepository(): %v", err)
	}
	defer reopened.Close()
	customers, err := reopened.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("ListCustomers(): %v", err)
	}
	entries, err := reopened.ListBalanceEntries(ctx)
	if err != nil {
		t.Fatalf("ListBalanceEntries(): %v", err)
	}
	if len(customers) != 1 || customers[0].BalanceMicros != 150 || len(entries) != 1 {
		t.Fatalf("persisted customers=%#v entries=%#v", customers, entries)
	}
}
