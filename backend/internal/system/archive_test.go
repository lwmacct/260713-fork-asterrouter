package system

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreBackupArchiveValidatesIDAndSize(t *testing.T) {
	svc := NewService(Config{BackupDir: t.TempDir(), MaxArchiveBytes: 8})
	if _, err := svc.StoreBackupArchive("../escape", bytes.NewReader([]byte("data"))); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("invalid id error = %v", err)
	}
	if _, err := svc.StoreBackupArchive("asterrouter-backup-valid.tar.gz", bytes.NewReader([]byte("data"))); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("invalid extension error = %v", err)
	}
	if _, err := svc.StoreBackupArchive("asterrouter-backup-valid", bytes.NewReader([]byte("0123456789"))); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("oversized error = %v", err)
	}
	info, err := svc.StoreBackupArchive("asterrouter-backup-valid", bytes.NewReader([]byte("archive")))
	if err != nil || info.ID != "asterrouter-backup-valid" {
		t.Fatalf("StoreBackupArchive() = %+v, %v", info, err)
	}
}

func TestS3BackupUploadRejectsInvalidArchiveIDBeforeAccessingStorage(t *testing.T) {
	store := &S3BackupStore{}
	if _, err := store.Upload(context.Background(), "../secrets", bytes.NewReader([]byte("data"))); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("Upload() error = %v, want ErrBackupInvalid", err)
	}
}

func TestS3BackupUploadUsesArchiveIDAndReader(t *testing.T) {
	const id = "asterrouter-backup-20260713T010203Z-0011223344556677"
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/test-bucket/backups/"+id+".tar.gz" {
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL.Path)
		}
		var err error
		uploaded, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upload body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := NewS3BackupStore(context.Background(), S3BackupConfig{
		Endpoint: server.URL, Region: "test", Bucket: "test-bucket", Prefix: "backups",
		AccessKey: "test-access", SecretKey: "test-secret", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.Upload(context.Background(), id, bytes.NewReader([]byte("archive-content")))
	if err != nil {
		t.Fatal(err)
	}
	if key != "backups/"+id+".tar.gz" || string(uploaded) != "archive-content" {
		t.Fatalf("Upload() key=%q body=%q", key, uploaded)
	}
}

func TestCleanupBackupsEnforcesAgeAndMaximumCount(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(Config{BackupDir: dir})
	now := time.Now().UTC()
	for index, age := range []time.Duration{time.Hour, 2 * time.Hour, 40 * 24 * time.Hour} {
		name := fmt.Sprintf("asterrouter-backup-20260101T00000%dZ-test.tar.gz", index)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("backup"), 0600); err != nil {
			t.Fatal(err)
		}
		createdAt := now.Add(-age)
		if err := os.Chtimes(path, createdAt, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := svc.CleanupBackups(context.Background(), 30, 1, now)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	remaining, err := svc.ListBackups(context.Background())
	if err != nil || len(remaining) != 1 {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
}

func TestS3BackupDownloadRejectsObjectsOutsideBackupNamespace(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
	}{
		{name: "arbitrary object without prefix", key: "secrets.txt"},
		{name: "nested object without prefix", key: "other/asterrouter-backup-20260101.tar.gz"},
		{name: "outside configured prefix", prefix: "backups", key: "other/asterrouter-backup-20260101.tar.gz"},
		{name: "nested below configured prefix", prefix: "backups", key: "backups/archive/asterrouter-backup-20260101.tar.gz"},
		{name: "path traversal", prefix: "backups", key: "backups/../asterrouter-backup-20260101.tar.gz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &S3BackupStore{config: S3BackupConfig{Prefix: test.prefix}}
			if _, err := store.Download(context.Background(), test.key); err == nil {
				t.Fatalf("Download(%q) unexpectedly succeeded", test.key)
			}
		})
	}
}

func TestBackupRestoreIncludesPluginAssetsAndRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	installFakePostgresTools(t, root)
	cacheDir := filepath.Join(root, "plugin-cache")
	activeDir := filepath.Join(root, "plugin-active")
	if err := os.MkdirAll(filepath.Join(cacheDir, "sample"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(activeDir, "sample", "1.0.0"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "sample", "package.pkg"), []byte("cache-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "sample", "1.0.0", "plugin.json"), []byte(`{"id":"sample"}`), 0600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(Config{
		Version:         "0.1.0",
		DatabaseURL:     "postgres://test.invalid/router",
		BackupDir:       filepath.Join(root, "backups"),
		DiagnosticDir:   filepath.Join(root, "diagnostics"),
		PluginCacheDir:  cacheDir,
		PluginActiveDir: activeDir,
	})
	created, err := svc.CreateBackup(context.Background(), "test-backup")
	if err != nil {
		t.Fatalf("CreateBackup(): %v", err)
	}
	if created.ID == "" || strings.Contains(created.Path, root) {
		t.Fatalf("backup info leaks or misses path metadata: %+v", created)
	}
	listed, err := svc.ListBackups(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListBackups() = %+v, err=%v", listed, err)
	}
	if _, err := svc.RestoreBackup(context.Background(), "restore-denied", RestoreRequest{BackupID: created.ID}); !errors.Is(err, ErrBackupConfirmation) {
		t.Fatalf("RestoreBackup() error = %v, want ErrBackupConfirmation", err)
	}

	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(activeDir); err != nil {
		t.Fatal(err)
	}
	restored, err := svc.RestoreBackup(context.Background(), "restore-ok", RestoreRequest{BackupID: created.ID, Confirm: true})
	if err != nil {
		t.Fatalf("RestoreBackup(): %v", err)
	}
	if !restored.NeedRestart || restored.BackupID != created.ID {
		t.Fatalf("restore result = %+v", restored)
	}
	cacheContent, err := os.ReadFile(filepath.Join(cacheDir, "sample", "package.pkg"))
	if err != nil || string(cacheContent) != "cache-content" {
		t.Fatalf("restored cache = %q, err=%v", cacheContent, err)
	}
	if _, err := os.Stat(filepath.Join(activeDir, "sample", "1.0.0", "plugin.json")); err != nil {
		t.Fatalf("restored active asset: %v", err)
	}
}

func TestBackupRequiresPostgreSQL(t *testing.T) {
	svc := NewService(Config{BackupDir: t.TempDir()})
	if _, err := svc.CreateBackup(context.Background(), "memory"); !errors.Is(err, ErrBackupDatabase) {
		t.Fatalf("CreateBackup() error = %v, want ErrBackupDatabase", err)
	}
}

func TestRecoveryDatabaseURLRequiresDedicatedDatabase(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://user:pass@localhost/postgres",
		"postgres://user:pass@localhost/production",
		"postgres://user:pass@localhost/production_asterrouter_recovery_test",
		"mysql://user:pass@localhost/asterrouter_recovery_test",
	} {
		if err := validateRecoveryTestDatabaseURL(databaseURL); err == nil {
			t.Fatalf("validateRecoveryTestDatabaseURL(%q) unexpectedly succeeded", databaseURL)
		}
	}
	if err := validateRecoveryTestDatabaseURL("postgres://user:pass@localhost/asterrouter_recovery_test"); err != nil {
		t.Fatalf("validateRecoveryTestDatabaseURL() = %v", err)
	}
	if err := validateRecoveryTestDatabaseURL("postgres://user:pass@localhost/asterrouter_recovery_test_ci"); err != nil {
		t.Fatalf("validateRecoveryTestDatabaseURL() with suffix = %v", err)
	}
}

func TestDiagnosticBundleIsRedacted(t *testing.T) {
	root := t.TempDir()
	svc := NewService(Config{
		Version:       "0.1.0",
		BuildType:     "release",
		DatabaseURL:   "postgres://user:secret@example.invalid/router",
		DiagnosticDir: filepath.Join(root, "diagnostics"),
	})
	info, err := svc.CreateDiagnosticBundle(context.Background(), "../../attacker-controlled", map[string]any{"storage_mode": "postgres"})
	if err != nil {
		t.Fatalf("CreateDiagnosticBundle(): %v", err)
	}
	if strings.Contains(info.ID, "attacker") || !validArchiveID(info.ID, diagnosticPrefix) {
		t.Fatalf("diagnostic id contains request data or is invalid: %q", info.ID)
	}
	archivePath, err := svc.DiagnosticArchivePath(info.ID)
	if err != nil {
		t.Fatalf("DiagnosticArchivePath(): %v", err)
	}
	extracted := filepath.Join(root, "extracted")
	if err := os.MkdirAll(extracted, 0750); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGzip(archivePath, extracted, defaultArchiveLimit); err != nil {
		t.Fatalf("extractTarGzip(): %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(extracted, "diagnostic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "postgres://") {
		t.Fatalf("diagnostic bundle contains sensitive configuration: %s", raw)
	}
	if !strings.Contains(string(raw), `"database_configured": true`) {
		t.Fatalf("diagnostic bundle misses safe database state: %s", raw)
	}
}

func TestExtractTarGzipRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "unsafe.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("unsafe")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGzip(archivePath, filepath.Join(root, "target"), defaultArchiveLimit); err == nil {
		t.Fatal("extractTarGzip accepted path traversal")
	}
}

func TestExtractTarGzipRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(target, "link")); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "unsafe-symlink.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("unsafe")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "link/escape", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGzip(archivePath, target, defaultArchiveLimit); err == nil {
		t.Fatal("extractTarGzip followed a symlink outside the target")
	}
}

func installFakePostgresTools(t *testing.T, root string) {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0750); err != nil {
		t.Fatal(err)
	}
	pgDump := `#!/bin/sh
set -eu
output=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--file" ]; then output="$arg"; break; fi
  previous="$arg"
done
test -n "$output"
printf 'database-dump' > "$output"
`
	pgRestore := "#!/bin/sh\nset -eu\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "pg_dump"), []byte(pgDump), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pg_restore"), []byte(pgRestore), 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
