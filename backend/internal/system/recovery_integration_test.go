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
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
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
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION CURRENT_USER`); err != nil {
		t.Fatalf("reset dedicated recovery database: %v", err)
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
	user, _, err := controlService.RegisterWorkspaceUser(ctx, "recovery-user@example.test", "synthetic-password-123", "Recovery User", false)
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}
	key, err := controlService.CreateAPIKey(ctx, "recovery-test", controlplane.APIKeyCreateRequest{
		Name:           "Recovery workspace key",
		KeyType:        controlplane.APIKeyTypeUser,
		OwnerUserID:    user.ID,
		ModelAllowlist: []string{"recovery-model"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(): %v", err)
	}
	auth, err := controlService.AuthenticateGatewayKey(ctx, key.Key)
	if err != nil {
		t.Fatalf("AuthenticateGatewayKey(): %v", err)
	}
	if err := controlService.RecordGatewayUsage(ctx, auth, controlplane.GatewayUsageInput{
		Model: "recovery-model", Status: "forwarded", ProviderID: provider.ID, InputTokens: 7, OutputTokens: 11,
	}); err != nil {
		t.Fatalf("RecordGatewayUsage(): %v", err)
	}
	if err := controlService.RecordGatewayTrace(ctx, auth, controlplane.GatewayTraceInput{
		Model: "recovery-model", ProviderID: provider.ID, Status: "forwarded", HTTPStatus: 200, InputTokens: 7, OutputTokens: 11,
		RequestSummary: "synthetic recovery request", ResponseSummary: "synthetic recovery response", RouteAttempts: "[]",
	}); err != nil {
		t.Fatalf("RecordGatewayTrace(): %v", err)
	}
	if err := controlService.RecordRiskRuleAlert(ctx, key.Record.ID, "recovery-rule", "Recovery rule", "Synthetic recovery alert", 100, 50); err != nil {
		t.Fatalf("RecordRiskRuleAlert(): %v", err)
	}
	pluginRepo, err := plugins.NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open plugin repository: %v", err)
	}
	plugin := plugins.Plugin{
		ID: "plugin-recovery", PluginID: "recovery.plugin", Name: "Recovery Plugin", Category: "testing", Type: "builtin",
		Tier: plugins.TierFreeCore, Version: "1.0.0", Vendor: "AsterRouter", Status: plugins.StatusEnabled,
		EntitlementStatus: plugins.EntitlementFree, Surfaces: []string{"admin"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := pluginRepo.SavePlugin(ctx, plugin); err != nil {
		t.Fatalf("SavePlugin(): %v", err)
	}
	if err := pluginRepo.Close(); err != nil {
		t.Fatalf("close plugin repository before backup: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS csv_export_jobs (
  id TEXT PRIMARY KEY, owner TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, status TEXT NOT NULL, filename TEXT NOT NULL,
  content_type TEXT NOT NULL, row_count INTEGER NOT NULL DEFAULT 0, size_bytes INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
  parameters TEXT NOT NULL DEFAULT '{}', body BYTEA, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL
)
`); err != nil {
		t.Fatalf("create recovery export table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO csv_export_jobs(id, owner, kind, status, filename, content_type, row_count, size_bytes, parameters, body, created_at, updated_at, expires_at)
VALUES('export-recovery', $1, 'usage', 'succeeded', 'recovery.csv', 'text/csv', 1, 17, '{"run":"recovery"}', $2, $3, $3, $4)
ON CONFLICT(id) DO UPDATE SET body = EXCLUDED.body, updated_at = EXCLUDED.updated_at
`, user.ID, []byte("model,tokens\nrecovery-model,18\n"), time.Now().UTC(), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("create recovery export fixture: %v", err)
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
		Version:         "0.3.0-recovery-fixture",
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

	candidateService := NewService(Config{
		Version:         "0.4.0-candidate-fixture",
		DatabaseURL:     databaseURL,
		BackupDir:       filepath.Join(root, "backups"),
		PluginCacheDir:  cacheDir,
		PluginActiveDir: activeDir,
	})
	restored, err := candidateService.RestoreBackup(ctx, "recovery-test", RestoreRequest{BackupID: backup.ID, Confirm: true})
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
	users, err := restoredControl.ListWorkspaceUsers(ctx)
	if err != nil || len(users) != 1 || users[0].ID != user.ID || users[0].Email != user.Email {
		t.Fatalf("restored users=%#v err=%v", users, err)
	}
	restoredAuth, err := restoredControl.AuthenticateGatewayKey(ctx, key.Key)
	if err != nil || restoredAuth.APIKey.ID != key.Record.ID || restoredAuth.APIKey.OwnerUserID != user.ID {
		t.Fatalf("restored workspace key auth=%#v err=%v", restoredAuth, err)
	}
	usage, err := restoredControl.UsageReportQuery(ctx, controlplane.UsageQuery{APIKeyID: key.Record.ID, Limit: 10})
	if err != nil || usage.TotalRequests != 1 || usage.TotalTokens != 18 || usage.UnpricedRequests != 1 || usage.Recent[0].PricingStatus != "unpriced" {
		t.Fatalf("restored usage=%#v err=%v", usage, err)
	}
	traces, err := restoredControl.ListGatewayTraces(ctx, 10)
	if err != nil || len(traces) != 1 || traces[0].APIKeyID != key.Record.ID || traces[0].HTTPStatus != 200 {
		t.Fatalf("restored traces=%#v err=%v", traces, err)
	}
	alerts, err := restoredControl.ListAlertEventsQuery(ctx, controlplane.AlertQuery{ResourceIDs: []string{key.Record.ID}})
	if err != nil || len(alerts) != 1 || alerts[0].Type != controlplane.AlertTypeRiskRule {
		t.Fatalf("restored alerts=%#v err=%v", alerts, err)
	}
	audit, err := restoredControl.ListAuditLogs(ctx, 100)
	if err != nil || !containsAuditResource(audit, "api_key", key.Record.ID) || !containsAuditResource(audit, "workspace_user", user.ID) {
		t.Fatalf("restored audit=%#v err=%v", audit, err)
	}
	restoredPluginRepo, err := plugins.NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen plugin repository: %v", err)
	}
	defer restoredPluginRepo.Close()
	restoredPlugin, ok, err := restoredPluginRepo.FindPlugin(ctx, plugin.ID)
	if err != nil || !ok || restoredPlugin.Status != plugins.StatusEnabled || restoredPlugin.Version != plugin.Version {
		t.Fatalf("restored plugin=%#v ok=%t err=%v", restoredPlugin, ok, err)
	}
	var exportStatus string
	var exportBody []byte
	if err := db.QueryRowContext(ctx, `SELECT status, body FROM csv_export_jobs WHERE id = 'export-recovery'`).Scan(&exportStatus, &exportBody); err != nil || exportStatus != "succeeded" || string(exportBody) != "model,tokens\nrecovery-model,18\n" {
		t.Fatalf("restored export status=%q body=%q err=%v", exportStatus, exportBody, err)
	}
	content, err := os.ReadFile(filepath.Join(cacheDir, "evidence.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before-backup" {
		t.Fatalf("restored plugin evidence = %q", content)
	}

}

func containsAuditResource(events []controlplane.AuditLog, resourceType, resourceID string) bool {
	for _, event := range events {
		if event.ResourceType == resourceType && event.ResourceID == resourceID {
			return true
		}
	}
	return false
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
