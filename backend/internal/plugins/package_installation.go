package plugins

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (s *Service) InstallPackage(ctx context.Context, pluginID string, packageID string) (PackageInstallation, error) {
	pluginID = strings.TrimSpace(pluginID)
	packageID = strings.TrimSpace(packageID)
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	record, ok, err := s.repo.FindPackage(ctx, packageID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || record.PluginID != pluginID {
		return PackageInstallation{}, ErrPackageNotFound
	}
	view, err := s.packageView(ctx, record)
	if err != nil {
		return PackageInstallation{}, err
	}
	if view.Revoked {
		return PackageInstallation{}, ErrPackageRevoked
	}
	if !view.Compatible {
		return PackageInstallation{}, fmt.Errorf("%w: %s", ErrPackageIncompatible, view.CompatibilityError)
	}
	if record.RequiredEntitlement {
		if _, _, ok, err := s.localLicenseForPackage(ctx, record); err != nil {
			return PackageInstallation{}, err
		} else if !ok {
			return PackageInstallation{}, ErrPluginLocked
		}
	}
	cache, ok, err := s.repo.FindPackageCache(ctx, record.PackageID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || cache.Status != PackageCacheStatusCached || strings.TrimSpace(cache.CachePath) == "" {
		return PackageInstallation{}, ErrPackageNotCached
	}
	previous, hadPrevious, err := s.repo.FindPackageInstallation(ctx, record.PluginID)
	if err != nil {
		return PackageInstallation{}, err
	}
	runtime := ""
	var finalizeActivation func() error
	var rollbackActivation func() error
	if inspectedRuntime, ok, err := inspectPackageRuntime(cache.CachePath); err != nil {
		return PackageInstallation{}, err
	} else if ok {
		if err := s.stopSidecarSupervisor(ctx, record.PluginID); err != nil {
			return PackageInstallation{}, fmt.Errorf("stop existing plugin runtime: %w", err)
		}
		activatedRuntime, finalize, rollback, err := s.activatePackage(record, cache.CachePath)
		if err != nil {
			return PackageInstallation{}, err
		}
		if inspectedRuntime != activatedRuntime {
			_ = rollback()
			return PackageInstallation{}, fmt.Errorf("plugin package runtime changed during activation")
		}
		runtime = activatedRuntime
		finalizeActivation = finalize
		rollbackActivation = rollback
	}
	now := s.now().UTC()
	installation := packageInstallationRecord{
		PluginID:    record.PluginID,
		PackageID:   record.PackageID,
		Version:     record.Version,
		OS:          record.OS,
		Arch:        record.Arch,
		CachePath:   cache.CachePath,
		Status:      PackageInstallInstalled,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.repo.SavePackageInstallation(ctx, installation); err != nil {
		if rollbackActivation != nil {
			_ = rollbackActivation()
		}
		if hadPrevious && previous.Status == PackageInstallInstalled {
			_ = s.ensureSidecarSupervisor(ctx, record.PluginID)
		}
		return PackageInstallation{}, err
	}
	if runtime == "sidecar" {
		plugin, ok, err := s.repo.FindPlugin(ctx, record.PluginID)
		if err != nil {
			_ = s.restoreInstallationAfterFailure(ctx, record.PluginID, previous, hadPrevious, rollbackActivation)
			return PackageInstallation{}, err
		}
		if ok && plugin.Status == StatusEnabled {
			if err := s.ensureSidecarSupervisor(ctx, record.PluginID); err != nil {
				_ = s.restoreInstallationAfterFailure(ctx, record.PluginID, previous, hadPrevious, rollbackActivation)
				return PackageInstallation{}, err
			}
		}
	}
	if finalizeActivation != nil {
		if err := finalizeActivation(); err != nil {
			return PackageInstallation{}, fmt.Errorf("finalize plugin activation: %w", err)
		}
	}
	return packageInstallationFromRecord(installation), nil
}

func (s *Service) UninstallPackage(ctx context.Context, pluginID string, packageID string) (PackageInstallation, error) {
	pluginID = strings.TrimSpace(pluginID)
	packageID = strings.TrimSpace(packageID)
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	installation, ok, err := s.repo.FindPackageInstallation(ctx, pluginID)
	if err != nil {
		return PackageInstallation{}, err
	}
	if !ok || installation.Status != PackageInstallInstalled || installation.PackageID != packageID {
		return PackageInstallation{}, ErrPackageNotInstalled
	}
	if err := s.stopSidecarSupervisor(ctx, pluginID); err != nil {
		return PackageInstallation{}, fmt.Errorf("stop existing plugin runtime: %w", err)
	}
	activeDir, err := s.activePackageDir(pluginID, installation.Version)
	if err != nil {
		return PackageInstallation{}, err
	}
	backupDir := activeDir + ".uninstall-" + fmt.Sprint(s.now().UnixNano())
	hadActive := false
	if _, statErr := os.Stat(activeDir); statErr == nil {
		if err := os.Rename(activeDir, backupDir); err != nil {
			return PackageInstallation{}, fmt.Errorf("stage plugin uninstall: %w", err)
		}
		hadActive = true
	} else if !os.IsNotExist(statErr) {
		return PackageInstallation{}, statErr
	}
	installation.Status = PackageInstallUninstalled
	installation.UpdatedAt = s.now().UTC()
	if err := s.repo.SavePackageInstallation(ctx, installation); err != nil {
		if hadActive {
			_ = os.Rename(backupDir, activeDir)
		}
		_ = s.ensureSidecarSupervisor(ctx, pluginID)
		return PackageInstallation{}, err
	}
	if hadActive {
		if err := os.RemoveAll(backupDir); err != nil {
			return PackageInstallation{}, fmt.Errorf("remove uninstalled plugin package: %w", err)
		}
	}
	return packageInstallationFromRecord(installation), nil
}

func (s *Service) restoreInstallationAfterFailure(ctx context.Context, pluginID string, previous packageInstallationRecord, hadPrevious bool, rollback func() error) error {
	if rollback != nil {
		_ = rollback()
	}
	if hadPrevious {
		if err := s.repo.SavePackageInstallation(ctx, previous); err != nil {
			return err
		}
		if previous.Status == PackageInstallInstalled {
			_ = s.ensureSidecarSupervisor(ctx, pluginID)
		}
		return nil
	}
	if current, ok, err := s.repo.FindPackageInstallation(ctx, pluginID); err == nil && ok {
		current.Status = PackageInstallUninstalled
		current.UpdatedAt = s.now().UTC()
		if err := s.repo.SavePackageInstallation(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

func packageInstallationFromRecord(record packageInstallationRecord) PackageInstallation {
	return PackageInstallation{
		PluginID:    record.PluginID,
		PackageID:   record.PackageID,
		Version:     record.Version,
		OS:          record.OS,
		Arch:        record.Arch,
		CachePath:   record.CachePath,
		Status:      record.Status,
		InstalledAt: record.InstalledAt,
		UpdatedAt:   record.UpdatedAt,
	}
}
