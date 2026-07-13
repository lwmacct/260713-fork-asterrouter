package server

import (
	"context"
	"os"
	"testing"
)

func TestCSVExportJobStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newCSVExportJobStore()
	defer store.Close()

	job, err := store.create(ctx, "tester", "audit_logs", "audit-logs.csv", map[string]string{"limit": "100", "q": "marker"})
	if err != nil {
		t.Fatalf("create(): %v", err)
	}
	if job.ID == "" || job.Owner != "tester" || job.Status != exportJobStatusQueued || job.Parameters["q"] != "marker" {
		t.Fatalf("unexpected job after create: %+v", job)
	}

	if err := store.markRunning(ctx, job.ID); err != nil {
		t.Fatalf("markRunning(): %v", err)
	}
	if err := store.markSucceeded(ctx, job.ID, 1, []byte("time,actor\n")); err != nil {
		t.Fatalf("markSucceeded(): %v", err)
	}

	got, ok, err := store.get(ctx, job.ID)
	if err != nil || !ok {
		t.Fatalf("get() ok=%t err=%v", ok, err)
	}
	if got.Status != exportJobStatusSucceeded || got.RowCount != 1 || got.SizeBytes == 0 {
		t.Fatalf("unexpected job after success: %+v", got)
	}

	downloadJob, body, ok, err := store.getDownload(ctx, job.ID)
	if err != nil || !ok {
		t.Fatalf("getDownload() ok=%t err=%v", ok, err)
	}
	if downloadJob.ID != job.ID || string(body) != "time,actor\n" {
		t.Fatalf("unexpected download job=%+v body=%q", downloadJob, string(body))
	}

	jobs, err := store.list(ctx, 10)
	if err != nil {
		t.Fatalf("list(): %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected list: %+v", jobs)
	}
}

func TestPostgresCSVExportJobStorePersistsResult(t *testing.T) {
	databaseURL := os.Getenv("ASTER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ASTER_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	store, err := NewCSVExportJobStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewCSVExportJobStore(): %v", err)
	}
	job, err := store.create(ctx, "tester", "usage", "usage-records.csv", map[string]string{"limit": "7"})
	if err != nil {
		t.Fatalf("create(): %v", err)
	}
	if err := store.markSucceeded(ctx, job.ID, 1, []byte("time,model\n")); err != nil {
		t.Fatalf("markSucceeded(): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	reopened, err := NewCSVExportJobStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen NewCSVExportJobStore(): %v", err)
	}
	defer reopened.Close()

	got, body, ok, err := reopened.getDownload(ctx, job.ID)
	if err != nil || !ok {
		t.Fatalf("reopened getDownload() ok=%t err=%v", ok, err)
	}
	if got.ID != job.ID || got.Parameters["limit"] != "7" || string(body) != "time,model\n" {
		t.Fatalf("unexpected persisted job=%+v body=%q", got, string(body))
	}
}
