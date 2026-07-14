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
	"github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/testutil"
)

//go:embed *.sql testdata/*.sql
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

func TestV030LegacySchemaUpgradesWithCandidateRuntime(t *testing.T) {
	schema := testutil.NewPostgresSchema(t)
	legacyDB := testutil.OpenPostgres(t, schema.URL)
	applyV030LegacySchema(t, legacyDB)

	ctx := context.Background()
	createdAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	seedStatements := []string{
		`
	INSERT INTO provider_connections(id,name,type,base_url,status,models,priority,secret_configured,secret_hint,secret_ciphertext,created_at,updated_at)
	VALUES('provider-v030','v0.3 provider','openai_compatible','https://provider.example/v1','active','["legacy-model"]',10,TRUE,'...legacy','ciphertext', $1, $1)`,
		`
	INSERT INTO workspace_users(id,email,display_name,status,role,balance_cents,concurrency_limit,rpm_limit,created_at,updated_at)
	VALUES('user-v030','v030@example.test','v0.3 user','active','developer',700,5,0,$1,$1)`,
		`
	INSERT INTO api_keys(id,name,key_hash,fingerprint,prefix,status,key_type,customer_id,owner_user_id,policy_id,model_allowlist,qps_limit,monthly_token_limit,created_at,updated_at)
	VALUES('key-v030','v0.3 key','v030-key-hash','v030fingerprint','ast_v030','active','user','','user-v030','','["legacy-model"]',10,1000,$1,$1)`,
		`
	INSERT INTO usage_records(id,api_key_id,customer_id,api_fingerprint,model,upstream_model,provider_id,provider_account_id,status,error_type,latency_ms,input_tokens,output_tokens,cost_cents,created_at)
	VALUES('usage-v030','key-v030','','v030fingerprint','legacy-model','legacy-model','provider-v030','','forwarded','',12,7,11,9,$1)`,
	}
	for _, statement := range seedStatements {
		if _, err := legacyDB.ExecContext(ctx, statement, createdAt); err != nil {
			t.Fatalf("seed v0.3.0 fixture: %v", err)
		}
	}

	// Opening the current repository is the candidate upgrade step. The v0.3.0
	// fixture intentionally lacks session_version and all notification tables.
	candidate, err := controlplane.NewPostgresRepository(ctx, schema.URL)
	if err != nil {
		t.Fatalf("upgrade v0.3.0 schema with candidate runtime: %v", err)
	}
	defer candidate.Close()

	providers, err := candidate.ListProviders(ctx)
	if err != nil || len(providers) != 1 || providers[0].ID != "provider-v030" || providers[0].SecretCiphertext != "ciphertext" {
		t.Fatalf("upgraded providers=%+v err=%v", providers, err)
	}
	users, err := candidate.ListWorkspaceUsers(ctx)
	if err != nil || len(users) != 1 || users[0].ID != "user-v030" || users[0].SessionVersion != 1 || users[0].BalanceCents != 700 {
		t.Fatalf("upgraded users=%+v err=%v", users, err)
	}
	key, found, err := candidate.FindAPIKeyByHash(ctx, "v030-key-hash")
	if err != nil || !found || key.ID != "key-v030" || key.OwnerUserID != "user-v030" ||
		!reflect.DeepEqual(key.Scopes, []string{controlplane.GatewayScopeInvoke, controlplane.GatewayScopeModelsRead}) ||
		!reflect.DeepEqual(key.AllowedModalities, []string{controlplane.GatewayModalityMetadata, controlplane.GatewayModalityText}) ||
		!reflect.DeepEqual(key.AllowedOperations, []string{controlplane.GatewayOperationListModels, controlplane.GatewayOperationChatCompletion}) ||
		key.LanePolicy != controlplane.GatewayLanePolicyDirectOnly || key.ArtifactPolicy != controlplane.GatewayArtifactPolicyProxyOnly {
		t.Fatalf("upgraded key=%+v found=%t err=%v", key, found, err)
	}
	usage, err := candidate.QueryUsageRecords(ctx, controlplane.UsageQuery{APIKeyID: "key-v030", Limit: 10})
	if err != nil || len(usage) != 1 || usage[0].ID != "usage-v030" || usage[0].InputTokens != 7 || usage[0].OutputTokens != 11 {
		t.Fatalf("upgraded usage=%+v err=%v", usage, err)
	}

	service := controlplane.NewService(candidate, "/v1", "candidate-upgrade-test-secret")
	settings, err := service.CustomerNotificationSettings(ctx, "v030@example.test")
	if err != nil || len(settings.Preferences) != 9 {
		t.Fatalf("candidate notification defaults=%+v err=%v", settings, err)
	}
	if _, err := service.UpdateCustomerNotificationSettings(ctx, "v030@example.test", controlplane.CustomerNotificationSettingsRequest{Preferences: settings.Preferences}); err != nil {
		t.Fatalf("persist candidate notification preferences: %v", err)
	}
	var preferenceCount int
	if err := legacyDB.QueryRowContext(ctx, `SELECT count(*) FROM customer_notification_preferences WHERE user_id = 'user-v030'`).Scan(&preferenceCount); err != nil || preferenceCount != len(settings.Preferences) {
		t.Fatalf("upgraded notification preferences=%d err=%v", preferenceCount, err)
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

func applyV030LegacySchema(t testing.TB, db *sql.DB) {
	t.Helper()
	body, err := migrationFiles.ReadFile("testdata/v0.3.0_legacy_schema.sql")
	if err != nil {
		t.Fatalf("read v0.3.0 legacy schema fixture: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatalf("apply v0.3.0 legacy schema fixture: %v", err)
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
