package system

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	// archives are bounded to prevent accidental disk exhaustion
	defaultArchiveLimit = 2 << 30
	backupPrefix        = "asterrouter-backup-"
	diagnosticPrefix    = "asterrouter-diagnostic-"
)

var (
	ErrBackupConfirmation = errors.New("backup restore requires explicit confirmation")
	ErrBackupNotFound     = errors.New("backup archive not found")
	ErrBackupToolMissing  = errors.New("postgres backup tools are not available")
	ErrBackupDatabase     = errors.New("PostgreSQL is required for persistent backups")
	ErrBackupInvalid      = errors.New("backup archive is invalid")
)

var archiveIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,199}$`)

type BackupInfo struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type RestoreRequest struct {
	BackupID string `json:"backup_id"`
	Confirm  bool   `json:"confirm"`
}

type RestoreResult struct {
	OperationID string `json:"operation_id"`
	BackupID    string `json:"backup_id"`
	NeedRestart bool   `json:"need_restart"`
	Message     string `json:"message"`
}

type DiagnosticInfo struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type archiveManifest struct {
	SchemaVersion      string    `json:"schema_version"`
	Kind               string    `json:"kind"`
	CreatedAt          time.Time `json:"created_at"`
	DatabaseFormat     string    `json:"database_format,omitempty"`
	DatabaseIncluded   bool      `json:"database_included"`
	PluginCacheCopied  bool      `json:"plugin_cache_included"`
	PluginActiveCopied bool      `json:"plugin_active_included"`
}

type archiveLimitWriter struct {
	written int64
	limit   int64
}

func (w *archiveLimitWriter) Write(p []byte) (int, error) {
	if w.limit > 0 && w.written+int64(len(p)) > w.limit {
		return 0, fmt.Errorf("archive exceeds %d bytes", w.limit)
	}
	w.written += int64(len(p))
	return len(p), nil
}

func defaultArchiveBytes(value int64) int64 {
	if value <= 0 {
		return defaultArchiveLimit
	}
	return value
}

func (s *Service) CreateBackup(ctx context.Context, _ string) (BackupInfo, error) {
	if s.databaseURL == "" {
		return BackupInfo{}, ErrBackupDatabase
	}
	now := time.Now().UTC()
	backupDir := s.archiveDirectory("backup")
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return BackupInfo{}, err
	}
	workDir, err := os.MkdirTemp(backupDir, ".backup-work-")
	if err != nil {
		return BackupInfo{}, err
	}
	defer os.RemoveAll(workDir)

	manifest := archiveManifest{
		SchemaVersion: "asterrouter.archive.v1",
		Kind:          "backup",
		CreatedAt:     now,
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return BackupInfo{}, fmt.Errorf("%w: pg_dump", ErrBackupToolMissing)
	}
	dumpPath := filepath.Join(workDir, "database.dump")
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-acl", "--file", dumpPath, "--dbname", s.databaseURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		return BackupInfo{}, fmt.Errorf("pg_dump failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	manifest.DatabaseFormat = "pg_dump_custom"
	manifest.DatabaseIncluded = true

	if s.pluginCacheDir != "" {
		if err := copyArchiveTree(s.pluginCacheDir, filepath.Join(workDir, "plugins", "cache")); err != nil {
			return BackupInfo{}, err
		}
		manifest.PluginCacheCopied = directoryExists(s.pluginCacheDir)
	}
	if s.pluginActiveDir != "" {
		if err := copyArchiveTree(s.pluginActiveDir, filepath.Join(workDir, "plugins", "active")); err != nil {
			return BackupInfo{}, err
		}
		manifest.PluginActiveCopied = directoryExists(s.pluginActiveDir)
	}
	if err := writeJSONFile(filepath.Join(workDir, "manifest.json"), manifest); err != nil {
		return BackupInfo{}, err
	}

	id := archiveID(backupPrefix, now)
	target := filepath.Join(backupDir, id+".tar.gz")
	if err := createTarGzip(target, workDir, s.maxArchiveBytes); err != nil {
		_ = os.Remove(target)
		return BackupInfo{}, err
	}
	return fileArchiveInfo(id, target)
}

func (s *Service) ListBackups(_ context.Context) ([]BackupInfo, error) {
	dir := s.archiveDirectory("backup")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]BackupInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), backupPrefix) || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".tar.gz")
		info, err := fileArchiveInfo(id, filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) CleanupBackups(ctx context.Context, retentionDays, maxRetained int, now time.Time) (int, error) {
	if retentionDays < 1 || maxRetained < 1 {
		return 0, errors.New("backup retention settings must be positive")
	}
	backups, err := s.ListBackups(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	deleted := 0
	for index, backup := range backups {
		if index < maxRetained && !backup.CreatedAt.Before(cutoff) {
			continue
		}
		path, err := s.findArchive("backup", backup.ID)
		if err != nil {
			return deleted, err
		}
		if err := os.Remove(path); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *Service) RestoreBackup(ctx context.Context, operationID string, request RestoreRequest) (RestoreResult, error) {
	if !request.Confirm {
		return RestoreResult{}, ErrBackupConfirmation
	}
	path, err := s.findArchive("backup", request.BackupID)
	if err != nil {
		return RestoreResult{}, err
	}
	workDir, err := os.MkdirTemp(s.archiveDirectory("backup"), ".restore-work-")
	if err != nil {
		return RestoreResult{}, err
	}
	defer os.RemoveAll(workDir)
	if err := extractTarGzip(path, workDir, s.maxArchiveBytes); err != nil {
		return RestoreResult{}, fmt.Errorf("%w: %v", ErrBackupInvalid, err)
	}
	var manifest archiveManifest
	if err := readJSONFile(filepath.Join(workDir, "manifest.json"), &manifest); err != nil || manifest.SchemaVersion != "asterrouter.archive.v1" || manifest.Kind != "backup" {
		return RestoreResult{}, ErrBackupInvalid
	}
	if manifest.DatabaseIncluded {
		if s.databaseURL == "" || manifest.DatabaseFormat != "pg_dump_custom" {
			return RestoreResult{}, fmt.Errorf("%w: database restore requires PostgreSQL", ErrBackupInvalid)
		}
		if _, err := exec.LookPath("pg_restore"); err != nil {
			return RestoreResult{}, fmt.Errorf("%w: pg_restore", ErrBackupToolMissing)
		}
		dumpPath := filepath.Join(workDir, "database.dump")
		cmd := exec.CommandContext(ctx, "pg_restore", "--clean", "--if-exists", "--no-owner", "--no-acl", "--dbname", s.databaseURL, dumpPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return RestoreResult{}, fmt.Errorf("pg_restore failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if manifest.PluginCacheCopied && s.pluginCacheDir != "" {
		if err := replaceDirectory(filepath.Join(workDir, "plugins", "cache"), s.pluginCacheDir); err != nil {
			return RestoreResult{}, err
		}
	}
	if manifest.PluginActiveCopied && s.pluginActiveDir != "" {
		if err := replaceDirectory(filepath.Join(workDir, "plugins", "active"), s.pluginActiveDir); err != nil {
			return RestoreResult{}, err
		}
	}
	return RestoreResult{OperationID: operationID, BackupID: request.BackupID, NeedRestart: true, Message: "Backup restored. Restart the service before serving traffic."}, nil
}

func (s *Service) CreateDiagnosticBundle(_ context.Context, _ string, details map[string]any) (DiagnosticInfo, error) {
	now := time.Now().UTC()
	dir := s.archiveDirectory("diagnostic")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return DiagnosticInfo{}, err
	}
	workDir, err := os.MkdirTemp(dir, ".diagnostic-work-")
	if err != nil {
		return DiagnosticInfo{}, err
	}
	defer os.RemoveAll(workDir)
	if details == nil {
		details = map[string]any{}
	}
	redacted := map[string]any{
		"schema_version":      "asterrouter.diagnostic.v1",
		"created_at":          now,
		"version":             s.version,
		"build_type":          s.buildType,
		"platform":            runtime.GOOS + "/" + runtime.GOARCH,
		"database_configured": s.databaseURL != "",
		"details":             details,
	}
	if err := writeJSONFile(filepath.Join(workDir, "diagnostic.json"), redacted); err != nil {
		return DiagnosticInfo{}, err
	}
	id := archiveID(diagnosticPrefix, now)
	target := filepath.Join(dir, id+".tar.gz")
	if err := createTarGzip(target, workDir, s.maxArchiveBytes); err != nil {
		_ = os.Remove(target)
		return DiagnosticInfo{}, err
	}
	info, err := fileArchiveInfo(id, target)
	if err != nil {
		return DiagnosticInfo{}, err
	}
	return DiagnosticInfo{ID: info.ID, Path: info.Path, SizeBytes: info.SizeBytes, CreatedAt: info.CreatedAt}, nil
}

func (s *Service) BackupArchivePath(id string) (string, error) {
	return s.findArchive("backup", id)
}

func (s *Service) StoreBackupArchive(id string, source io.Reader) (BackupInfo, error) {
	if !validArchiveID(id, backupPrefix) {
		return BackupInfo{}, ErrBackupInvalid
	}
	dir := s.archiveDirectory("backup")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return BackupInfo{}, err
	}
	temp, err := os.CreateTemp(dir, ".s3-import-*.tar.gz")
	if err != nil {
		return BackupInfo{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, copyErr := io.Copy(temp, io.LimitReader(source, s.maxArchiveBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return BackupInfo{}, copyErr
	}
	if closeErr != nil {
		return BackupInfo{}, closeErr
	}
	if written > s.maxArchiveBytes {
		return BackupInfo{}, ErrBackupInvalid
	}
	target := filepath.Join(dir, id+".tar.gz")
	if err := os.Rename(tempPath, target); err != nil {
		return BackupInfo{}, err
	}
	return fileArchiveInfo(id, target)
}

func (s *Service) DiagnosticArchivePath(id string) (string, error) {
	return s.findArchive("diagnostic", id)
}

func (s *Service) archiveDirectory(kind string) string {
	if kind == "diagnostic" {
		if s.diagnosticDir != "" {
			return s.diagnosticDir
		}
	}
	if s.backupDir != "" {
		if kind == "diagnostic" {
			return filepath.Join(s.backupDir, "diagnostics")
		}
		return s.backupDir
	}
	if kind == "diagnostic" {
		return filepath.Join("data", "diagnostics")
	}
	return filepath.Join("data", "backups")
}

func (s *Service) findArchive(kind, id string) (string, error) {
	prefix := backupPrefix
	if kind == "diagnostic" {
		prefix = diagnosticPrefix
	}
	if !validArchiveID(id, prefix) {
		return "", ErrBackupNotFound
	}
	path := filepath.Join(s.archiveDirectory(kind), id+".tar.gz")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", ErrBackupNotFound
		}
		return "", err
	}
	return path, nil
}

func archiveID(prefix string, now time.Time) string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return prefix + now.UTC().Format("20060102T150405Z") + fmt.Sprintf("-%d", now.UnixNano())
	}
	return prefix + now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random)
}

func validArchiveID(id, prefix string) bool {
	return strings.HasPrefix(id, prefix) && archiveIDPattern.MatchString(id)
}

func fileArchiveInfo(id, path string) (BackupInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{ID: id, Path: filepath.Base(path), SizeBytes: stat.Size(), CreatedAt: stat.ModTime().UTC()}, nil
}

func writeJSONFile(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readJSONFile(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(target)
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyArchiveTree(source, target string) error {
	if !directoryExists(source) {
		return nil
	}
	return copyTree(source, target)
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive source contains symlink: %s", rel)
		}
		if info.IsDir() {
			return os.MkdirAll(destination, 0750)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archive source contains unsupported file: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0750); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeInputErr := input.Close()
		closeOutputErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInputErr != nil {
			return closeInputErr
		}
		return closeOutputErr
	})
}

func createTarGzip(target, source string, limit int64) error {
	temp := target + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	limited := &archiveLimitWriter{limit: limit}
	tee := io.MultiWriter(limited, file)
	gzipWriter := gzip.NewWriter(tee)
	tarWriter := tar.NewWriter(gzipWriter)
	err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("archive contains unsupported file: %s", rel)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Rename(temp, target)
}

func extractTarGzip(source, target string, limit int64) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(io.LimitReader(gzipReader, limit+1))
	if err := os.MkdirAll(target, 0750); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	var extracted int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !filepath.IsLocal(header.Name) {
			return fmt.Errorf("unsafe archive path")
		}
		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0750); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size < 0 || limit > 0 && extracted+header.Size > limit {
				return fmt.Errorf("archive exceeds limit")
			}
			if err := root.MkdirAll(filepath.Dir(name), 0750); err != nil {
				return err
			}
			output, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(output, io.LimitReader(reader, header.Size))
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written != header.Size {
				return io.ErrUnexpectedEOF
			}
			extracted += written
		default:
			return fmt.Errorf("archive contains unsupported entry type")
		}
	}
}

func replaceDirectory(source, destination string) error {
	if !directoryExists(source) {
		return nil
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	return copyTree(source, destination)
}
