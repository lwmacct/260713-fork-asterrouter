package server

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

type backupScheduleConfig struct {
	Enabled  bool
	Interval time.Duration
}

func RunBackupScheduler(ctx context.Context, svc *system.Service, settingsSvc *settings.Service, control *controlplane.Service, onError func(error)) {
	runBackupScheduler(ctx, func(ctx context.Context) (backupScheduleConfig, error) {
		current, err := settingsSvc.Admin(ctx)
		if err != nil {
			return backupScheduleConfig{}, err
		}
		return backupScheduleConfig{Enabled: current.BackupScheduleEnabled, Interval: time.Duration(current.BackupIntervalHours) * time.Hour}, nil
	}, func(ctx context.Context) error {
		operationID := fmt.Sprintf("scheduled-%d", time.Now().UTC().Unix())
		backup, err := createManagedBackup(ctx, svc, settingsSvc, operationID)
		if err != nil {
			_ = control.RecordSystemEvent(ctx, "system:backup-scheduler", "backup_failed", operationID, err.Error())
			return err
		}
		return control.RecordSystemEvent(ctx, "system:backup-scheduler", "backup", backup.ID, fmt.Sprintf("Created scheduled backup %s", backup.ID))
	}, onError)
}

func runBackupScheduler(ctx context.Context, config func(context.Context) (backupScheduleConfig, error), run func(context.Context) error, onError func(error)) {
	var nextRun time.Time
	var configuredInterval time.Duration
	for {
		current, err := config(ctx)
		if err != nil {
			if onError != nil {
				onError(err)
			}
			current = backupScheduleConfig{Interval: time.Minute}
		}
		if current.Interval <= 0 {
			current.Interval = time.Minute
		}
		now := time.Now()
		if !current.Enabled {
			nextRun = time.Time{}
			configuredInterval = 0
		} else if nextRun.IsZero() || configuredInterval != current.Interval {
			nextRun = now.Add(current.Interval)
			configuredInterval = current.Interval
		}
		wait := time.Minute
		if current.Enabled && time.Until(nextRun) < wait {
			wait = max(time.Until(nextRun), time.Millisecond)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if current.Enabled && !time.Now().Before(nextRun) {
			if err := run(ctx); err != nil && onError != nil {
				onError(err)
			}
			nextRun = time.Now().Add(current.Interval)
		}
	}
}

func createManagedBackup(ctx context.Context, svc *system.Service, settingsSvc *settings.Service, operationID string) (system.BackupInfo, error) {
	data, err := svc.CreateBackup(ctx, operationID)
	if err != nil {
		return system.BackupInfo{}, err
	}
	config, err := settingsSvc.BackupS3Config(ctx)
	if err != nil {
		return system.BackupInfo{}, err
	}
	if _, err := svc.CleanupBackups(ctx, config.RetentionDays, config.MaxRetained, time.Now().UTC()); err != nil {
		return system.BackupInfo{}, err
	}
	if !config.Enabled {
		return data, nil
	}
	store, err := system.NewS3BackupStore(ctx, system.S3BackupConfig{Endpoint: config.Endpoint, Region: config.Region, Bucket: config.Bucket, Prefix: config.Prefix, AccessKey: config.AccessKey, SecretKey: config.SecretKey, PathStyle: config.PathStyle, RetentionDays: config.RetentionDays, MaxRetained: config.MaxRetained})
	if err != nil {
		return system.BackupInfo{}, err
	}
	archivePath, err := svc.BackupArchivePath(data.ID)
	if err != nil {
		return system.BackupInfo{}, err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return system.BackupInfo{}, err
	}
	defer archive.Close()
	if _, err := store.Upload(ctx, data.ID, archive); err != nil {
		return system.BackupInfo{}, err
	}
	if err := store.Cleanup(ctx, time.Now().UTC()); err != nil {
		return system.BackupInfo{}, err
	}
	return data, nil
}
