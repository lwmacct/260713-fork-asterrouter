package plugins

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type sidecarManifest struct {
	ID         string            `json:"id"`
	Version    string            `json:"version"`
	Runtime    string            `json:"runtime"`
	Entrypoint map[string]string `json:"entrypoint"`
}

func inspectPackageRuntime(cachePath string) (string, bool, error) {
	file, err := os.Open(cachePath)
	if err != nil {
		return "", false, fmt.Errorf("open plugin package: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", false, nil
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("read plugin archive: %w", err)
		}
		if filepath.Clean(header.Name) != "plugin.json" || header.Typeflag != tar.TypeReg {
			continue
		}
		raw, err := io.ReadAll(reader)
		if err != nil {
			return "", false, fmt.Errorf("read plugin manifest: %w", err)
		}
		var manifest sidecarManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return "", false, fmt.Errorf("decode plugin manifest: %w", err)
		}
		return strings.TrimSpace(manifest.Runtime), true, nil
	}
}

func (s *Service) activatePackage(record packageRecord, cachePath string) (string, error) {
	activeDir := s.activePackageDir(record.PluginID, record.Version)
	stageDir := activeDir + ".staging"
	if err := os.RemoveAll(stageDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stageDir, 0750); err != nil {
		return "", fmt.Errorf("create plugin staging directory: %w", err)
	}
	if err := extractTarGzip(cachePath, stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", err
	}
	manifest, err := readSidecarManifest(filepath.Join(stageDir, "plugin.json"))
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return "", err
	}
	if manifest.ID != record.PluginID {
		_ = os.RemoveAll(stageDir)
		return "", fmt.Errorf("plugin manifest id mismatch")
	}
	if manifest.Version != record.Version {
		_ = os.RemoveAll(stageDir)
		return "", fmt.Errorf("plugin manifest version mismatch")
	}
	if manifest.Runtime == "sidecar" {
		entrypoint, err := s.sidecarEntrypointFromManifest(stageDir, manifest)
		if err != nil {
			_ = os.RemoveAll(stageDir)
			return "", err
		}
		if err := os.Chmod(entrypoint, 0750); err != nil {
			_ = os.RemoveAll(stageDir)
			return "", fmt.Errorf("mark plugin sidecar executable: %w", err)
		}
	}
	if err := os.RemoveAll(activeDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(activeDir), 0750); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", err
	}
	if err := os.Rename(stageDir, activeDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return "", fmt.Errorf("activate plugin package: %w", err)
	}
	return manifest.Runtime, nil
}

func (s *Service) activePackageDir(pluginID string, version string) string {
	return filepath.Join(s.packageActiveDir, sanitizePathSegment(pluginID), sanitizePathSegment(version))
}

func (s *Service) sidecarEntrypointFromManifest(baseDir string, manifest sidecarManifest) (string, error) {
	if manifest.Runtime != "sidecar" {
		return "", fmt.Errorf("plugin runtime must be sidecar")
	}
	target := s.targetOS + "-" + s.targetArch
	rel := strings.TrimSpace(manifest.Entrypoint[target])
	if rel == "" {
		return "", fmt.Errorf("plugin package does not include entrypoint for %s", target)
	}
	path := filepath.Clean(filepath.Join(baseDir, rel))
	if !strings.HasPrefix(path, filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("plugin entrypoint escapes package directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("plugin entrypoint is missing: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("plugin entrypoint is a directory")
	}
	return path, nil
}

func readSidecarManifest(path string) (sidecarManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return sidecarManifest{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	var manifest sidecarManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return sidecarManifest{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if strings.TrimSpace(manifest.ID) == "" || strings.TrimSpace(manifest.Version) == "" {
		return sidecarManifest{}, fmt.Errorf("plugin manifest is incomplete")
	}
	return manifest, nil
}

func extractTarGzip(source string, targetDir string) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open plugin package: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("plugin package must be a tar.gz archive: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	cleanTarget := filepath.Clean(targetDir)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read plugin archive: %w", err)
		}
		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("plugin archive contains unsafe path")
		}
		path := filepath.Join(cleanTarget, name)
		if !strings.HasPrefix(path, cleanTarget+string(os.PathSeparator)) && path != cleanTarget {
			return fmt.Errorf("plugin archive path escapes target directory")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0770)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, reader)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("plugin archive contains unsupported entry type")
		}
	}
}
