package migrations_test

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

//go:embed *.sql
var migrationFiles embed.FS

var migrationNamePattern = regexp.MustCompile(`^(\d{3})_[a-z0-9_]+\.sql$`)

func TestMigrationSnapshotSequence(t *testing.T) {
	names := migrationNames(t)
	previous := 0
	missing := []int{}
	for _, name := range names {
		match := migrationNamePattern.FindStringSubmatch(name)
		if match == nil {
			t.Fatalf("invalid migration filename %q", name)
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration number from %q: %v", name, err)
		}
		if number <= previous {
			t.Fatalf("migration sequence is not strictly increasing at %q", name)
		}
		for candidate := previous + 1; candidate < number; candidate++ {
			missing = append(missing, candidate)
		}
		previous = number
	}
	if !reflect.DeepEqual(missing, []int{19}) {
		t.Fatalf("unexpected migration gaps: %v; only the documented historical 019 gap is allowed", missing)
	}
	if previous < 42 {
		t.Fatalf("latest migration snapshot = %03d, want at least 042", previous)
	}
}

func TestMigrationSnapshotsApplyToRuntimePostgres(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	db := testutil.OpenPostgres(t, schema.URL)
	applyMigrationSnapshots(t, db)

	var tableCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM information_schema.tables WHERE table_schema = $1`, schema.Name).Scan(&tableCount); err != nil {
		t.Fatalf("count migrated tables: %v", err)
	}
	if tableCount < 25 {
		t.Fatalf("migrated table count = %d, want at least 25", tableCount)
	}
}

func TestAIAttemptDispatchMigrationUpgradesExistingAttempts(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	db := testutil.OpenPostgres(t, schema.URL)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE ai_attempts (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL,
  attempt_number INTEGER NOT NULL,
  provider_id TEXT NOT NULL DEFAULT '',
  provider_account_id TEXT NOT NULL DEFAULT '',
  route_id TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error_type TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  UNIQUE(operation_id, attempt_number)
);
INSERT INTO ai_attempts(id, operation_id, attempt_number, provider_account_id, status, created_at, updated_at)
VALUES('attempt-old-1','operation-old',1,'account-old','running',NOW(),NOW()),
      ('attempt-old-2','operation-old',2,'account-old','running',NOW(),NOW());
`); err != nil {
		t.Fatalf("create pre-053 ai_attempts: %v", err)
	}
	body, err := migrationFiles.ReadFile("053_ai_attempt_provider_dispatch.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply 053 run %d: %v", run+1, err)
		}
	}
	var state, key string
	var version int
	if err := db.QueryRowContext(ctx, `SELECT dispatch_state, dispatch_version, dispatch_key FROM ai_attempts WHERE id='attempt-old-1'`).Scan(&state, &version, &key); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || version != 0 || key != "attempt-old-1" {
		t.Fatalf("upgraded dispatch state=%q version=%d key=%q", state, version, key)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ai_attempts SET provider_task_id='task-shared' WHERE id='attempt-old-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ai_attempts SET provider_task_id='task-shared' WHERE id='attempt-old-2'`); err == nil {
		t.Fatal("provider task uniqueness constraint accepted duplicate task binding")
	}
}

func TestUsageDimensionsMigrationIsIdempotent(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	db := testutil.OpenPostgres(t, schema.URL)
	body, err := migrationFiles.ReadFile("060_usage_dimensions.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatalf("apply 060 run %d: %v", run+1, err)
		}
	}
	columns := schemaColumns(t, db, schema.Name)
	if !strings.HasPrefix(columns["usage_records.usage_dimensions"], "jsonb|jsonb|NO|") || !strings.HasPrefix(columns["billing_holds.reserved_usage_dimensions"], "jsonb|jsonb|NO|") {
		t.Fatalf("usage dimension columns=%+v", columns)
	}
}

func TestProviderBillingSourcesMigrationIsIdempotentAndEnforcesConstraints(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	db := testutil.OpenPostgres(t, schema.URL)
	body, err := migrationFiles.ReadFile("062_provider_billing_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatalf("apply 062 run %d: %v", run+1, err)
		}
	}

	ctx := context.Background()
	now := time.Date(2026, time.July, 16, 2, 0, 0, 0, time.UTC)
	repo, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.SaveProvider(ctx, controlplane.ProviderConnection{ID: "migration-billing-provider", Name: "Migration billing provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: controlplane.ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProviderAccount(ctx, controlplane.ProviderAccount{ID: "migration-billing-account", ProviderID: "migration-billing-provider", Name: "Migration billing account", Platform: "openai_compatible", AuthType: "api_key", Status: controlplane.AccountStatusActive, SecretConfigured: true, SecretCiphertext: "ciphertext", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_billing_sources(id,provider_id,provider_account_id,adapter_id,status,sync_interval_seconds,created_at,updated_at) VALUES('migration-billing-source','migration-billing-provider','migration-billing-account','sub2api_compatible','observe_only',3600,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"invalid source status":   `UPDATE provider_billing_sources SET status='invalid' WHERE id='migration-billing-source'`,
		"invalid source interval": `UPDATE provider_billing_sources SET sync_interval_seconds=59 WHERE id='migration-billing-source'`,
		"duplicate account":       `INSERT INTO provider_billing_sources(id,provider_id,provider_account_id,adapter_id,status,sync_interval_seconds,created_at,updated_at) VALUES('migration-billing-source-duplicate','migration-billing-provider','migration-billing-account','sub2api_compatible','observe_only',3600,NOW(),NOW())`,
		"invalid run status":      `INSERT INTO provider_billing_sync_runs(id,source_id,provider_id,provider_account_id,trigger,adapter_id,status,started_at,created_at) VALUES('migration-billing-run-invalid','migration-billing-source','migration-billing-provider','migration-billing-account','manual','sub2api_compatible','invalid',NOW(),NOW())`,
	} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("%s constraint accepted invalid data", name)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_billing_sync_runs(id,source_id,provider_id,provider_account_id,trigger,adapter_id,status,started_at,created_at) VALUES('migration-billing-run','migration-billing-source','migration-billing-provider','migration-billing-account','manual','sub2api_compatible','running',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_usage_aggregate_snapshots(id,source_id,sync_run_id,provider_account_id,scope,request_count,currency,observed_at,created_at) VALUES('migration-aggregate-invalid','migration-billing-source','migration-billing-run','migration-billing-account','total',-1,'USD',$1,$1)`, now); err == nil {
		t.Fatal("aggregate non-negative constraint accepted invalid data")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_balance_snapshots(id,source_id,sync_run_id,provider_account_id,kind,amount_micros,currency,observed_at,created_at) VALUES('migration-balance-invalid','migration-billing-source','migration-billing-run','migration-billing-account','wallet_balance',1,'usd',$1,$1)`, now); err == nil {
		t.Fatal("balance currency constraint accepted invalid data")
	}
}

func TestAIJobProgressMigrationIsIdempotentAndEnforcesConstraints(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	db := testutil.OpenPostgres(t, schema.URL)
	body, err := migrationFiles.ReadFile("063_ai_job_progress_events.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatalf("apply 063 run %d: %v", run+1, err)
		}
	}

	ctx := context.Background()
	repo, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc := controlplane.NewService(repo, "/v1", "progress-migration-secret")
	if _, err := svc.CreateGatewayModel(ctx, "migration", controlplane.GatewayModelRequest{
		ModelID: "progress-migration-model", Name: "Progress migration", Modality: controlplane.GatewayModalityVideo, Status: controlplane.GatewayModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	job, created, err := svc.BeginDurableAIJob(ctx, gatewaycore.CanonicalAuthContext{
		CredentialSource: gatewaycore.CredentialSourceAPIKey, CredentialID: "progress-migration-key", ProfileScope: controlplane.ProfileScopePlatform,
		TenantID: "progress-migration-tenant", PrincipalType: controlplane.APIKeyTypeService, PrincipalID: "progress-migration-principal",
		ArtifactPolicy: controlplane.GatewayArtifactPolicyTemporary,
	}, gatewaycore.CanonicalRequest{
		ClientRequestID: "progress-migration-request", Fingerprint: "progress-migration-fingerprint", IdempotencyKey: "progress-migration-idem",
		Protocol: gatewaycore.ProtocolAsterJobs, Operation: controlplane.GatewayOperationVideoGeneration, Modality: controlplane.GatewayModalityVideo,
		Lane: gatewaycore.LaneDurable, Model: "progress-migration-model", Payload: []byte(`{"input":{"prompt":"synthetic"}}`),
	})
	if err != nil || !created {
		t.Fatalf("job=%+v created=%t err=%v", job, created, err)
	}
	attempt, err := svc.BeginAIAttempt(ctx, job.OperationID, 1, controlplane.GatewayProvider{ID: "progress-provider", AccountID: "progress-account"})
	if err != nil {
		t.Fatal(err)
	}

	for name, statement := range map[string]string{
		"invalid sequence": `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-invalid-sequence',$1,$2,'task',0,10,'rendering',NOW())`,
		"invalid percent":  `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-invalid-percent',$1,$2,'task',1,101,'rendering',NOW())`,
		"unsafe stage":     `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-invalid-stage',$1,$2,'task',1,10,'unsafe stage',NOW())`,
		"empty fact":       `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-empty',$1,$2,'task',1,NULL,'',NOW())`,
	} {
		if _, err := db.ExecContext(ctx, statement, job.ID, attempt.ID); err == nil {
			t.Fatalf("%s constraint accepted invalid progress", name)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-valid',$1,$2,'task',1,10,'rendering',NOW())`, job.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ai_job_progress_events(id,job_id,attempt_id,provider_task_id,provider_sequence,percent,stage,created_at) VALUES('progress-duplicate',$1,$2,'task',1,20,'rendering',NOW())`, job.ID, attempt.ID); err == nil {
		t.Fatal("progress sequence uniqueness accepted a duplicate")
	}
}

func TestProviderAccountModelInventoryMigrationIsIdempotentAndPersists(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	db := testutil.OpenPostgres(t, schema.URL)
	body, err := migrationFiles.ReadFile("065_provider_account_model_inventory.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatalf("apply 065 run %d: %v", run+1, err)
		}
	}

	ctx := context.Background()
	now := time.Date(2026, time.July, 16, 4, 0, 0, 0, time.UTC)
	repo, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProvider(ctx, controlplane.ProviderConnection{ID: "inventory-provider", Name: "Inventory provider", Type: "openai_compatible", BaseURL: "https://provider.example/v1", Status: controlplane.ProviderStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	account := controlplane.ProviderAccount{ID: "inventory-account", ProviderID: "inventory-provider", Name: "Inventory account", Platform: "openai_compatible", AuthType: "api_key", Status: controlplane.AccountStatusActive, Models: []string{"model-a"}, AutoEnableNewModels: true, CreatedAt: now, UpdatedAt: now}
	lastSeen := now
	models := []controlplane.ProviderAccountModel{{ProviderAccountID: account.ID, ModelID: "model-a", Source: controlplane.ProviderAccountModelSourceDiscovered, Enabled: true, Availability: controlplane.ProviderAccountModelAvailabilityAvailable, FirstSeenAt: now, LastSeenAt: &lastSeen, UpdatedAt: now}}
	if err := repo.SaveProviderAccountWithModels(ctx, account, models); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	accounts, err := reopened.ListProviderAccounts(ctx)
	if err != nil || len(accounts) != 1 || !accounts[0].AutoEnableNewModels {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	persisted, err := reopened.ListProviderAccountModels(ctx, account.ID)
	if err != nil || len(persisted) != 1 || persisted[0].ModelID != "model-a" || persisted[0].LastSeenAt == nil {
		t.Fatalf("models=%+v err=%v", persisted, err)
	}
	failedAccount := accounts[0]
	failedAccount.Name = "must roll back"
	failedAccount.Models = []string{"invalid-model"}
	invalidInventory := []controlplane.ProviderAccountModel{{
		ProviderAccountID: failedAccount.ID,
		ModelID:           "invalid-model",
		Source:            "invalid",
		Enabled:           true,
		Availability:      controlplane.ProviderAccountModelAvailabilityAvailable,
		FirstSeenAt:       now,
		UpdatedAt:         now,
	}}
	if err := reopened.SaveProviderAccountWithModels(ctx, failedAccount, invalidInventory); err == nil {
		t.Fatal("invalid inventory unexpectedly committed")
	}
	accounts, err = reopened.ListProviderAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].Name != account.Name || !reflect.DeepEqual(accounts[0].Models, account.Models) {
		t.Fatalf("account update was not rolled back: accounts=%+v err=%v", accounts, err)
	}
	persisted, err = reopened.ListProviderAccountModels(ctx, account.ID)
	if err != nil || len(persisted) != 1 || persisted[0].ModelID != "model-a" {
		t.Fatalf("inventory replacement was not rolled back: models=%+v err=%v", persisted, err)
	}
	for name, statement := range map[string]string{
		"invalid source":       `INSERT INTO provider_account_models(provider_account_id,model_id,source,enabled,availability,first_seen_at,updated_at) VALUES('inventory-account','bad-source','invalid',TRUE,'available',NOW(),NOW())`,
		"invalid availability": `INSERT INTO provider_account_models(provider_account_id,model_id,source,enabled,availability,first_seen_at,updated_at) VALUES('inventory-account','bad-status','manual',TRUE,'invalid',NOW(),NOW())`,
	} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("%s constraint accepted invalid data", name)
		}
	}
}

func TestRuntimeSchemaMatchesMigrationSnapshots(t *testing.T) {
	snapshotSchema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, snapshotSchema.URL)
	snapshotDB := testutil.OpenPostgres(t, snapshotSchema.URL)
	applyMigrationSnapshots(t, snapshotDB)

	runtimeSchema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, runtimeSchema.URL)
	runtimeDB := testutil.OpenPostgres(t, runtimeSchema.URL)

	snapshotColumns := schemaColumns(t, snapshotDB, snapshotSchema.Name)
	runtimeColumns := schemaColumns(t, runtimeDB, runtimeSchema.Name)
	assertStringMapEqual(t, "columns", snapshotColumns, runtimeColumns)

	snapshotIndexes := schemaIndexes(t, snapshotDB, snapshotSchema.Name)
	runtimeIndexes := schemaIndexes(t, runtimeDB, runtimeSchema.Name)
	assertStringMapEqual(t, "indexes", snapshotIndexes, runtimeIndexes)
}

func TestPublishedPricingRuleVersionIsDatabaseImmutable(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	initializeRuntimeSchema(t, schema.URL)
	ctx := context.Background()
	repo, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := controlplane.NewService(repo, "/v1")
	detail, err := service.CreatePricingRule(ctx, "migration-test", controlplane.PricingRuleCreateRequest{
		Name: "Immutable", Purpose: controlplane.PricingPurposeUsageCost, ScopeType: controlplane.PricingScopeGlobal,
		Model: "*", Currency: "USD", AuthoringMode: controlplane.PricingAuthoringRaw,
		Expression: `v1: fixed_line("request", "request", 1)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = service.PublishPricingRule(ctx, "migration-test", detail.Rule.ID, controlplane.PricingPublishRequest{
		DraftVersionID: detail.Draft.ID, ExpectedLockVersion: detail.Rule.LockVersion,
		ExpressionHash: detail.Draft.ExpressionHash,
	})
	if err != nil || detail.ActiveVersion == nil {
		t.Fatalf("publish detail=%+v err=%v", detail, err)
	}
	db := testutil.OpenPostgres(t, schema.URL)
	for name, statement := range map[string]string{
		"update": `UPDATE pricing_rule_versions SET expression='v1: fixed_line("request", "request", 2)' WHERE id=$1`,
		"delete": `DELETE FROM pricing_rule_versions WHERE id=$1`,
	} {
		if _, err := db.ExecContext(ctx, statement, detail.ActiveVersion.ID); err == nil {
			t.Fatalf("published pricing version %s was not rejected", name)
		}
	}
}

func migrationNames(t testing.TB) []string {
	t.Helper()
	names, err := fs.Glob(migrationFiles, "*.sql")
	if err != nil {
		t.Fatalf("list migration snapshots: %v", err)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no migration snapshots found")
	}
	return names
}

func applyMigrationSnapshots(t testing.TB, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, name := range migrationNames(t) {
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}

func initializeRuntimeSchema(t testing.TB, databaseURL string) {
	t.Helper()
	ctx := context.Background()
	settingsRepo, _, err := settings.NewRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize settings schema: %v", err)
	}
	defer settingsRepo.Close()
	controlRepo, _, err := controlplane.NewRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize controlplane schema: %v", err)
	}
	defer controlRepo.Close()
	operatorRepo, err := operator.NewRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize operator schema: %v", err)
	}
	defer operatorRepo.Close()
	pluginRepo, _, err := plugins.NewRepository(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize plugin schema: %v", err)
	}
	defer pluginRepo.Close()
	exportStore, err := server.NewCSVExportJobStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("initialize export schema: %v", err)
	}
	defer exportStore.Close()
}

func schemaColumns(t testing.TB, db *sql.DB, schema string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
SELECT table_name, column_name, data_type, udt_name, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = $1
ORDER BY table_name, ordinal_position`, schema)
	if err != nil {
		t.Fatalf("query schema columns: %v", err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var tableName, columnName, dataType, udtName, nullable, defaultValue string
		if err := rows.Scan(&tableName, &columnName, &dataType, &udtName, &nullable, &defaultValue); err != nil {
			t.Fatalf("scan schema column: %v", err)
		}
		result[tableName+"."+columnName] = strings.Join([]string{dataType, udtName, nullable, normalizeSQL(defaultValue)}, "|")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema columns: %v", err)
	}
	return result
}

func schemaIndexes(t testing.TB, db *sql.DB, schema string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
SELECT tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname = $1
ORDER BY tablename, indexname`, schema)
	if err != nil {
		t.Fatalf("query schema indexes: %v", err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var tableName, indexName, definition string
		if err := rows.Scan(&tableName, &indexName, &definition); err != nil {
			t.Fatalf("scan schema index: %v", err)
		}
		definition = strings.ReplaceAll(definition, `"`+schema+`".`, "")
		definition = strings.ReplaceAll(definition, schema+".", "")
		result[tableName+"."+indexName] = normalizeSQL(definition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema indexes: %v", err)
	}
	return result
}

func assertStringMapEqual(t testing.TB, label string, want, got map[string]string) {
	t.Helper()
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Errorf("runtime schema missing %s %s", label, key)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("runtime %s %s differs\n snapshot: %s\n runtime:  %s", label, key, wantValue, gotValue)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("runtime schema has undocumented %s %s", label, key)
		}
	}
}

func normalizeSQL(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func Example_migrationSnapshotsAreOrdered() {
	fmt.Println("001_settings.sql ... 042_customer_notifications.sql")
	// Output: 001_settings.sql ... 042_customer_notifications.sql
}
