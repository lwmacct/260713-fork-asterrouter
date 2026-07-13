package system

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
	_ "github.com/lib/pq"
)

func TestPostgresBackupRestoreRehearsal(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("ASTER_RECOVERY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("ASTER_RECOVERY_TEST_DATABASE_URL is not set")
	}
	if os.Getenv("ASTER_RECOVERY_TEST_ALLOW_DESTRUCTIVE") != "1" {
		t.Fatal("ASTER_RECOVERY_TEST_ALLOW_DESTRUCTIVE=1 is required because pg_restore --clean replaces database objects")
	}
	if err := validateRecoveryTestDatabaseURL(databaseURL); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open recovery database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping recovery database: %v", err)
	}

	upstream := testutil.NewFakeOpenAI(t)
	controlRepo, err := controlplane.NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open controlplane repository: %v", err)
	}
	const secretKey = "synthetic-recovery-encryption-key"
	controlService := controlplane.NewService(controlRepo, "/v1", secretKey)
	provider, err := controlService.CreateProvider(ctx, "recovery-test", controlplane.ProviderRequest{
		Name: "Recovery Provider", Type: "openai_compatible", BaseURL: upstream.BaseURL(),
		Status: controlplane.ProviderStatusActive, Models: []string{"upstream-model"}, APIKey: "synthetic-recovery-provider-secret",
	})
	if err != nil {
		t.Fatalf("CreateProvider(): %v", err)
	}
	if err := controlRepo.Close(); err != nil {
		t.Fatalf("close controlplane repository before backup: %v", err)
	}

	root := t.TempDir()
	cacheDir := filepath.Join(root, "plugin-cache")
	activeDir := filepath.Join(root, "plugin-active")
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "evidence.txt"), []byte("before-backup"), 0600); err != nil {
		t.Fatal(err)
	}

	service := NewService(Config{
		Version:         "recovery-test",
		DatabaseURL:     databaseURL,
		BackupDir:       filepath.Join(root, "backups"),
		PluginCacheDir:  cacheDir,
		PluginActiveDir: activeDir,
	})
	backup, err := service.CreateBackup(ctx, "recovery-test")
	if err != nil {
		t.Fatalf("CreateBackup(): %v", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM provider_connections`); err != nil {
		t.Fatalf("mutate recovery database: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "evidence.txt"), []byte("after-backup"), 0600); err != nil {
		t.Fatal(err)
	}

	restored, err := service.RestoreBackup(ctx, "recovery-test", RestoreRequest{BackupID: backup.ID, Confirm: true})
	if err != nil {
		t.Fatalf("RestoreBackup(): %v", err)
	}
	if !restored.NeedRestart || restored.BackupID != backup.ID {
		t.Fatalf("restore result = %+v", restored)
	}

	restoredRepo, err := controlplane.NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen restored controlplane repository: %v", err)
	}
	defer restoredRepo.Close()
	restoredControl := controlplane.NewService(restoredRepo, "/v1", secretKey)
	providers, err := restoredControl.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders(): %v", err)
	}
	if len(providers) != 1 || providers[0].ID != provider.ID || !providers[0].SecretConfigured {
		t.Fatalf("restored providers = %#v", providers)
	}
	check, err := restoredControl.CheckProvider(ctx, "recovery-test", provider.ID)
	if err != nil || check.Status != "ok" {
		t.Fatalf("CheckProvider() = %#v, err=%v", check, err)
	}
	requests := upstream.Requests()
	if len(requests) != 1 || requests[0].Authorization != "Bearer synthetic-recovery-provider-secret" {
		t.Fatalf("restored provider secret was not usable: %#v", requests)
	}
	content, err := os.ReadFile(filepath.Join(cacheDir, "evidence.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before-backup" {
		t.Fatalf("restored plugin evidence = %q", content)
	}

}

func validateRecoveryTestDatabaseURL(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse recovery database URL: %w", err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("recovery database URL must use postgres or postgresql")
	}
	if parsed.Hostname() == "" || (databaseName != "asterrouter_recovery_test" && !strings.HasPrefix(databaseName, "asterrouter_recovery_test_")) {
		return fmt.Errorf("recovery database name must be asterrouter_recovery_test or use that prefix")
	}
	return nil
}
