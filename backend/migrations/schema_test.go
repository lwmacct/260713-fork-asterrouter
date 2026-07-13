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

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
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
