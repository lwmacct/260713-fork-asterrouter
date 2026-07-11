package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("asterrouter %s\ncommit: %s\nbuilt: %s\nbuild_type: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, buildinfo.BuildType)
		return
	}

	cfg := config.Load()
	if err := config.ValidateRuntime(cfg); err != nil {
		log.Fatalf("invalid runtime configuration: %v", err)
	}
	repo, storageMode, err := settings.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize settings repository: %v", err)
	}
	defer repo.Close()

	controlRepo, _, err := controlplane.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize control plane repository: %v", err)
	}
	defer controlRepo.Close()
	pluginRepo, _, err := plugins.NewRepository(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize plugin repository: %v", err)
	}
	defer pluginRepo.Close()
	exportJobStore, err := server.NewCSVExportJobStore(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("initialize export job store: %v", err)
	}
	defer exportJobStore.Close()

	settingsService := settings.NewService(repo, settings.ServiceOptions{
		Version:         cfg.Version,
		EnabledProfiles: cfg.Profiles,
		DefaultProfile:  cfg.DefaultProfile,
		StorageMode:     storageMode,
	})
	authService := auth.NewService(auth.Config{
		Username:         cfg.AdminUsername,
		Password:         cfg.AdminPassword,
		LegacyAdminToken: cfg.AdminToken,
		SecretKey:        cfg.SecretKey,
	})
	controlService := controlplane.NewService(controlRepo, "/v1", cfg.SecretKey)
	if err := controlService.EnsureSeedData(context.Background()); err != nil {
		log.Fatalf("seed control plane repository: %v", err)
	}
	pluginService := plugins.NewServiceWithOptions(pluginRepo, plugins.ServiceOptions{
		SecretKey: cfg.SecretKey,
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode:            cfg.CatalogMode,
			BootstrapURL:    cfg.CatalogBootstrapURL,
			URL:             cfg.CatalogURL,
			LicenseURL:      cfg.LicenseURL,
			PublicKeyID:     cfg.CatalogKeyID,
			PublicKeyBase64: cfg.CatalogPublicKey,
		},
		OfficialLicense: plugins.OfficialLicenseConfig{
			URL:             cfg.LicenseURL,
			PublicKeyID:     cfg.LicenseKeyID,
			PublicKeyBase64: cfg.LicensePublicKey,
			InstanceID:      cfg.InstanceID,
			Fingerprint:     cfg.InstanceFingerprint,
			DisplayName:     cfg.InstanceDisplayName,
		},
		PackageCacheDir: cfg.PluginCacheDir,
		PluginActiveDir: cfg.PluginActiveDir,
		CoreVersion:     cfg.Version,
	})
	if err := pluginService.EnsureSeedData(context.Background()); err != nil {
		log.Fatalf("seed plugin repository: %v", err)
	}
	if err := pluginService.StartEnabledSidecars(context.Background()); err != nil {
		log.Fatalf("start enabled plugin sidecars: %v", err)
	}
	officialCatalogURL := ""
	officialCatalogKeyID := ""
	officialCatalogPublicKey := ""
	if cfg.CatalogMode == "online" {
		officialCatalogURL = cfg.CatalogURL
		if officialCatalogURL == "" {
			officialCatalogURL = cfg.CatalogBootstrapURL
		}
		officialCatalogKeyID = cfg.CatalogKeyID
		officialCatalogPublicKey = cfg.CatalogPublicKey
	}
	systemService := system.NewService(system.Config{
		Version:            cfg.Version,
		BuildType:          cfg.BuildType,
		ManifestURL:        cfg.UpdateManifestURL,
		OfficialCatalogURL: officialCatalogURL,
		OfficialKeyID:      officialCatalogKeyID,
		OfficialPublicKey:  officialCatalogPublicKey,
		AllowRestart:       cfg.AllowRestart,
	})

	router := server.New(server.Options{
		Config:          cfg,
		AuthService:     authService,
		SettingsService: settingsService,
		ControlService:  controlService,
		PluginService:   pluginService,
		SystemService:   systemService,
		ExportJobStore:  exportJobStore,
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("AsterRouter listening on %s (storage=%s)", cfg.Addr, storageMode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("server shutdown failed: %v", err)
	}
	if err := pluginService.Shutdown(context.Background()); err != nil {
		log.Printf("plugin shutdown failed: %v", err)
	}
}
