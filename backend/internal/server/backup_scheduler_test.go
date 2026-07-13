package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackupSchedulerRunsAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runBackupScheduler(ctx, func(context.Context) (backupScheduleConfig, error) {
			return backupScheduleConfig{Enabled: true, Interval: 5 * time.Millisecond}, nil
		}, func(context.Context) error {
			if runs.Add(1) == 1 {
				cancel()
			}
			return nil
		}, nil)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
	if runs.Load() != 1 {
		t.Fatalf("runs=%d", runs.Load())
	}
}

func TestBackupSchedulerDoesNotRunWhenDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var runs atomic.Int32
	runBackupScheduler(ctx, func(context.Context) (backupScheduleConfig, error) {
		return backupScheduleConfig{Enabled: false, Interval: 5 * time.Millisecond}, nil
	}, func(context.Context) error {
		runs.Add(1)
		return nil
	}, nil)
	if runs.Load() != 0 {
		t.Fatalf("disabled scheduler runs=%d", runs.Load())
	}
}
