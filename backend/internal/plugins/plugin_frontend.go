package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPluginFrontendNotFound = errors.New("plugin frontend contribution is not available")

func (s *Service) PluginFrontendContribution(ctx context.Context, pluginID string) ([]byte, error) {
	frontendDir, err := s.pluginFrontendDir(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(frontendDir, "contribution.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginFrontendNotFound, err)
	}
	return raw, nil
}

func (s *Service) PluginFrontendAssetPath(ctx context.Context, pluginID string, assetPath string) (string, error) {
	frontendDir, err := s.pluginFrontendDir(ctx, pluginID)
	if err != nil {
		return "", err
	}
	assetPath = strings.TrimLeft(strings.TrimSpace(assetPath), "/")
	if assetPath == "" {
		return "", ErrPluginFrontendNotFound
	}
	clean := filepath.Clean(assetPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
		return "", ErrPluginFrontendNotFound
	}
	fullPath := filepath.Join(frontendDir, clean)
	if !strings.HasPrefix(fullPath, filepath.Clean(frontendDir)+string(os.PathSeparator)) {
		return "", ErrPluginFrontendNotFound
	}
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return "", ErrPluginFrontendNotFound
	}
	return fullPath, nil
}

func (s *Service) pluginFrontendDir(ctx context.Context, pluginID string) (string, error) {
	pluginID = strings.TrimSpace(pluginID)
	installation, ok, err := s.repo.FindPackageInstallation(ctx, pluginID)
	if err != nil {
		return "", err
	}
	if !ok || installation.Status != PackageInstallInstalled {
		return "", ErrPackageNotInstalled
	}
	return filepath.Join(s.activePackageDir(pluginID, installation.Version), "frontend"), nil
}
