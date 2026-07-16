package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	httpserver "github.com/astercloud/asterrouter/backend/internal/server"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
)

type runtime struct {
	storageMode string
	handler     http.Handler

	settingsRepo settings.Repository
	controlRepo  controlplane.Repository
	operatorRepo operatorcore.Repository
	pluginRepo   plugins.Repository
	exportStore  httpserver.CSVExportJobStore

	settingsService *settings.Service
	controlService  *controlplane.Service
	pluginService   *plugins.Service
	systemService   *system.Service
	durableJobs     *controlplane.DurableAIJobRuntime

	closeInfrastructure func()
	cancel              context.CancelFunc
	backgroundErrors    chan error
	waitGroup           sync.WaitGroup
	closeOnce           sync.Once
	closeErr            error
}

func newRuntime(ctx context.Context, cfg *config.Server) (_ *runtime, err error) {
	rt := &runtime{closeInfrastructure: func() {}, backgroundErrors: make(chan error, 1)}
	defer func() {
		if err != nil {
			_ = rt.Close(context.Background())
		}
	}()

	if rt.settingsRepo, rt.storageMode, err = settings.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize settings repository: %w", err)
	}
	if rt.controlRepo, _, err = controlplane.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize control plane repository: %w", err)
	}
	if rt.operatorRepo, err = operatorcore.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize operator repository: %w", err)
	}
	if rt.pluginRepo, _, err = plugins.NewRepository(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize plugin repository: %w", err)
	}
	if rt.exportStore, err = httpserver.NewCSVExportJobStore(ctx, cfg.Storage.DatabaseURL); err != nil {
		return nil, fmt.Errorf("initialize export job store: %w", err)
	}

	profiles := []string(nil)
	defaultProfile := ""
	if cfg.Bootstrap.DeploymentRole != "" {
		profiles = []string{cfg.Bootstrap.DeploymentRole}
		defaultProfile = cfg.Bootstrap.DeploymentRole
	}
	rt.settingsService = settings.NewService(rt.settingsRepo, settings.ServiceOptions{
		Version: buildinfo.Version, EnabledProfiles: profiles, DefaultProfile: defaultProfile,
		StorageMode: rt.storageMode, DemoMode: cfg.Bootstrap.DemoMode,
	})
	if err = rt.settingsService.BootstrapProfile(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap profile: %w", err)
	}
	adminSettings, err := rt.settingsService.Admin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	oidcService, feishuService, githubOAuthService, googleOAuthService, dingTalkService, err := newIdentityServices(ctx, rt.settingsService, adminSettings)
	if err != nil {
		return nil, err
	}

	rt.controlService = controlplane.NewService(rt.controlRepo, "/v1", cfg.Security.SecretKey)
	if err = configureArtifactStore(ctx, cfg.Artifacts, rt.controlService); err != nil {
		return nil, err
	}
	if err = rt.controlService.SetAIJobAdmissionLimits(controlplane.AIJobAdmissionLimits{
		Profile: cfg.Jobs.Queue.Limits.Profile, Tenant: cfg.Jobs.Queue.Limits.Tenant, Principal: cfg.Jobs.Queue.Limits.Principal,
	}); err != nil {
		return nil, fmt.Errorf("configure durable AI job admission: %w", err)
	}
	var deliveryQueue controlplane.AIJobDeliveryQueue
	deliveryQueue, rt.closeInfrastructure, err = configureAIJobInfrastructure(ctx, cfg.Jobs, cfg.Storage.Redis, rt.controlService)
	if err != nil {
		return nil, fmt.Errorf("initialize durable AI job infrastructure: %w", err)
	}
	if err = rt.controlService.EnsureSeedData(ctx); err != nil {
		return nil, fmt.Errorf("seed control plane repository: %w", err)
	}
	if adminSettings.DefaultProfile == controlplane.ProfileScopePlatform {
		if err = rt.controlService.EnsurePlatformBootstrap(ctx); err != nil {
			return nil, fmt.Errorf("initialize platform domain: %w", err)
		}
	}

	authService := auth.NewService(auth.Config{
		Username: cfg.Security.Admin.Username, Password: cfg.Security.Admin.Password,
		LegacyAdminToken: cfg.Security.Admin.Token, SecretKey: cfg.Security.SecretKey, DemoMode: cfg.Bootstrap.DemoMode,
	})
	localAdminUsername, localAdminPassword := authService.BootstrapIdentity()
	localAdminDefaults := controlplane.WorkspaceUserDefaults{
		BalanceCents: adminSettings.DefaultBalanceCents, ConcurrencyLimit: adminSettings.DefaultConcurrency, RPMLimit: adminSettings.DefaultRPM,
	}
	localAdmin, err := rt.controlService.EnsureLocalAdmin(ctx, localAdminUsername, localAdminPassword, localAdminDefaults)
	if err != nil {
		return nil, fmt.Errorf("initialize local administrator account: %w", err)
	}
	authService.SetPasswordHash(localAdmin.PasswordHash)

	operatorService := operatorcore.NewService(rt.operatorRepo, rt.controlService)
	operatorService.SetRiskConfigProvider(func(ctx context.Context) (operatorcore.RiskRuntimeConfig, error) {
		current, err := rt.settingsService.Admin(ctx)
		if err != nil {
			return operatorcore.RiskRuntimeConfig{}, err
		}
		return operatorcore.RiskRuntimeConfig{
			Enabled: current.RiskControlEnabled, AutoBlock: current.CyberSessionBlockEnabled,
			BlockTimeout: time.Duration(current.CyberSessionBlockTTLSeconds) * time.Second,
		}, nil
	})
	rt.controlService.SetUsageObserver(operatorService)

	rt.pluginService = plugins.NewServiceWithOptions(rt.pluginRepo, plugins.ServiceOptions{
		SecretKey: cfg.Security.SecretKey,
		OfficialCatalog: plugins.OfficialCatalogConfig{
			Mode: cfg.Official.Catalog.Mode, BootstrapURL: cfg.Official.Catalog.BootstrapURL,
			URL: cfg.Official.Catalog.URL, ServicesURL: cfg.Official.Catalog.ServicesURL,
			LicenseURL: cfg.Official.License.URL, RedeemURL: cfg.Official.License.RedeemURL,
			PublicKeyID: cfg.Official.Catalog.KeyID, PublicKeyBase64: cfg.Official.Catalog.PublicKey,
		},
		OfficialLicense: plugins.OfficialLicenseConfig{
			URL: cfg.Official.License.URL, RedeemURL: cfg.Official.License.RedeemURL,
			PublicKeyID: cfg.Official.License.KeyID, PublicKeyBase64: cfg.Official.License.PublicKey,
			InstanceID: cfg.Official.Instance.ID, Fingerprint: cfg.Official.Instance.Fingerprint,
			DisplayName: cfg.Official.Instance.DisplayName,
		},
		PackageCacheDir: cfg.Plugins.CacheDir, PluginActiveDir: cfg.Plugins.ActiveDir,
		PluginHostURL: cfg.Plugins.HostURL, CoreVersion: buildinfo.Version, ArtifactSinkRegistry: rt.controlService,
	})
	if err = rt.pluginService.EnsureSeedData(ctx); err != nil {
		return nil, fmt.Errorf("seed plugin repository: %w", err)
	}
	if err = rt.pluginService.StartEnabledSidecars(ctx); err != nil {
		return nil, fmt.Errorf("start enabled plugin sidecars: %w", err)
	}
	if err = rt.pluginService.StartEnabledArtifactSinks(ctx); err != nil {
		return nil, fmt.Errorf("start enabled artifact sinks: %w", err)
	}
	if rt.durableJobs, err = controlplane.NewDurableAIJobRuntime(rt.controlService, deliveryQueue, rt.pluginService, controlplane.DurableAIJobRuntimeConfig{}); err != nil {
		return nil, fmt.Errorf("initialize durable AI job runtime: %w", err)
	}

	officialCatalogURL, officialCatalogKeyID, officialCatalogPublicKey := systemCatalog(cfg.Official.Catalog)
	rt.systemService = system.NewService(system.Config{
		Version: buildinfo.Version, BuildType: buildinfo.BuildType, ManifestURL: cfg.Official.UpdateManifestURL,
		OfficialCatalogURL: officialCatalogURL, OfficialKeyID: officialCatalogKeyID, OfficialPublicKey: officialCatalogPublicKey,
		AllowRestart: cfg.Maintenance.AllowRestart, DatabaseURL: cfg.Storage.DatabaseURL,
		PluginCacheDir: cfg.Plugins.CacheDir, PluginActiveDir: cfg.Plugins.ActiveDir,
		BackupDir: cfg.Maintenance.BackupDir, DiagnosticDir: cfg.Maintenance.DiagnosticDir,
		MaxArchiveBytes: cfg.Maintenance.MaxArchiveBytes,
	})

	rt.handler = httpserver.New(httpserver.Options{
		Runtime: httpserver.RuntimeConfig{
			AdminToken: cfg.Security.Admin.Token, DemoMode: cfg.Bootstrap.DemoMode, FrontendDir: cfg.HTTP.FrontendDir,
		},
		AuthService: authService, OIDCService: oidcService, FeishuService: feishuService,
		GitHubOAuthService: githubOAuthService, GoogleOAuthService: googleOAuthService, DingTalkService: dingTalkService,
		SettingsService: rt.settingsService, ControlService: rt.controlService, OperatorService: operatorService,
		PluginService: rt.pluginService, SystemService: rt.systemService, ExportJobStore: rt.exportStore,
		DurableAIJobs: rt.durableJobs, AIJobRuntime: rt.durableJobs,
	})
	return rt, nil
}

func (rt *runtime) Start(ctx context.Context) {
	backgroundCtx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel
	rt.startBackground(backgroundCtx)
}

func (rt *runtime) Errors() <-chan error {
	return rt.backgroundErrors
}

func (rt *runtime) Close(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	rt.closeOnce.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		waitDone := make(chan struct{})
		go func() {
			rt.waitGroup.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-ctx.Done():
			rt.closeErr = errors.Join(rt.closeErr, ctx.Err())
		}
		if rt.pluginService != nil {
			rt.closeErr = errors.Join(rt.closeErr, rt.pluginService.Shutdown(context.WithoutCancel(ctx)))
		}
		if rt.closeInfrastructure != nil {
			rt.closeInfrastructure()
		}
		for _, closeResource := range []func() error{
			closeCSVExportStore(rt.exportStore), closePluginRepository(rt.pluginRepo), closeOperatorRepository(rt.operatorRepo),
			closeControlRepository(rt.controlRepo), closeSettingsRepository(rt.settingsRepo),
		} {
			rt.closeErr = errors.Join(rt.closeErr, closeResource())
		}
	})
	return rt.closeErr
}

func (rt *runtime) startBackground(ctx context.Context) {
	rt.launch(func() {
		rt.controlService.RunChannelMonitor(ctx, func(ctx context.Context) (controlplane.ChannelMonitorConfig, error) {
			current, err := rt.settingsService.Admin(ctx)
			if err != nil {
				return controlplane.ChannelMonitorConfig{}, err
			}
			return controlplane.ChannelMonitorConfig{
				Enabled: current.ChannelMonitorEnabled, Interval: time.Duration(current.ChannelMonitorIntervalSeconds) * time.Second,
			}, nil
		}, func(operation string, err error) {
			slog.Error("channel monitor failed", "operation", operation, "error", err)
		})
	})
	rt.launch(func() {
		rt.controlService.RunCustomerNotificationScheduler(ctx, logBackgroundError("customer notification scheduler"))
	})
	rt.launch(func() {
		rt.controlService.RunPlatformUsageDeliveryScheduler(ctx, logBackgroundError("platform usage delivery scheduler"))
	})
	rt.launch(func() {
		rt.controlService.RunArtifactLifecycleScheduler(ctx, 30*time.Second, 100, logBackgroundError("artifact lifecycle scheduler"))
	})
	rt.launch(func() {
		rt.controlService.RunEffectivePricingDecisionMonitor(ctx, time.Minute, logBackgroundError("effective pricing decision monitor"))
	})
	rt.launch(func() {
		rt.controlService.RunProviderBillingSyncScheduler(ctx, time.Minute, logBackgroundError("provider billing sync scheduler"))
	})
	rt.launch(func() {
		httpserver.RunBackupScheduler(ctx, rt.systemService, rt.settingsService, rt.controlService, logBackgroundError("backup scheduler"))
	})
	rt.launch(func() {
		if err := rt.durableJobs.Run(ctx, func(component string, err error) {
			slog.Error("durable AI job runtime component failed", "component", component, "error", err)
		}); err != nil && !errors.Is(err, context.Canceled) {
			select {
			case rt.backgroundErrors <- err:
			default:
			}
		}
	})
}

func (rt *runtime) launch(run func()) {
	rt.waitGroup.Add(1)
	go func() {
		defer rt.waitGroup.Done()
		run()
	}()
}

func logBackgroundError(component string) func(error) {
	return func(err error) { slog.Error("background component failed", "component", component, "error", err) }
}

func systemCatalog(cfg config.OfficialCatalog) (string, string, string) {
	if cfg.Mode != "online" {
		return "", "", ""
	}
	url := cfg.URL
	if url == "" {
		url = cfg.BootstrapURL
	}
	return url, cfg.KeyID, cfg.PublicKey
}

func closeCSVExportStore(store httpserver.CSVExportJobStore) func() error {
	return func() error {
		if store == nil {
			return nil
		}
		return store.Close()
	}
}

func closePluginRepository(repo plugins.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}

func closeOperatorRepository(repo operatorcore.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}

func closeControlRepository(repo controlplane.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}

func closeSettingsRepository(repo settings.Repository) func() error {
	return func() error {
		if repo == nil {
			return nil
		}
		return repo.Close()
	}
}
