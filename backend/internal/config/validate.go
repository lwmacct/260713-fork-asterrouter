package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidHTTP        = errors.New("invalid HTTP configuration")
	ErrInvalidBootstrap   = errors.New("invalid bootstrap configuration")
	ErrInvalidSecurity    = errors.New("invalid security configuration")
	ErrInvalidStorage     = errors.New("invalid storage configuration")
	ErrInvalidOfficial    = errors.New("invalid official service configuration")
	ErrInvalidPlugins     = errors.New("invalid plugin configuration")
	ErrInvalidJobs        = errors.New("invalid AI job configuration")
	ErrInvalidArtifacts   = errors.New("invalid artifact configuration")
	ErrInvalidMaintenance = errors.New("invalid maintenance configuration")
)

func Validate(cfg Server, buildType string) (Server, error) {
	normalize(&cfg)
	if cfg.HTTP.Listen == "" || cfg.HTTP.FrontendDir == "" {
		return cfg, fmt.Errorf("%w: server.http.listen and server.http.frontend-dir are required", ErrInvalidHTTP)
	}
	if cfg.Bootstrap.DeploymentRole != "" && !isDeploymentRole(cfg.Bootstrap.DeploymentRole) {
		return cfg, fmt.Errorf("%w: server.bootstrap.deployment-role must be personal, relay_operator, enterprise, or platform", ErrInvalidBootstrap)
	}
	if cfg.Security.Admin.Username == "" {
		return cfg, fmt.Errorf("%w: server.security.admin.username is required", ErrInvalidSecurity)
	}
	if cfg.Storage.Redis.Namespace == "" || !validRuntimeNamespace(cfg.Storage.Redis.Namespace) {
		return cfg, fmt.Errorf("%w: server.storage.redis.namespace must contain only letters, numbers, dots, underscores, or hyphens", ErrInvalidStorage)
	}
	if err := validateJobs(cfg); err != nil {
		return cfg, err
	}
	if err := validateArtifacts(cfg.Artifacts); err != nil {
		return cfg, err
	}
	if err := validateOfficial(cfg.Official); err != nil {
		return cfg, err
	}
	if cfg.Plugins.CacheDir == "" || cfg.Plugins.ActiveDir == "" || cfg.Plugins.HostURL == "" {
		return cfg, fmt.Errorf("%w: server.plugins cache-dir, active-dir, and host-url must resolve to non-empty values", ErrInvalidPlugins)
	}
	if cfg.Maintenance.BackupDir == "" || cfg.Maintenance.DiagnosticDir == "" || cfg.Maintenance.MaxArchiveBytes <= 0 {
		return cfg, fmt.Errorf("%w: backup-dir, diagnostic-dir, and a positive max-archive-bytes are required", ErrInvalidMaintenance)
	}

	isRelease := strings.TrimSpace(buildType) == "release"
	if isRelease && cfg.Storage.DatabaseURL == "" {
		return cfg, fmt.Errorf("%w: server.storage.database-url is required in release builds", ErrInvalidStorage)
	}
	if (isRelease || cfg.Storage.DatabaseURL != "") && cfg.Security.SecretKey == "" {
		return cfg, fmt.Errorf("%w: server.security.secret-key is required for release or persistent storage", ErrInvalidSecurity)
	}
	if isRelease && cfg.Security.Admin.Password == "" && cfg.Security.Admin.Token == "" && !cfg.Bootstrap.DemoMode {
		return cfg, fmt.Errorf("%w: an admin password or token is required in release builds", ErrInvalidSecurity)
	}
	return cfg, nil
}

func normalize(cfg *Server) {
	cfg.HTTP.Listen = strings.TrimSpace(cfg.HTTP.Listen)
	cfg.HTTP.FrontendDir = strings.TrimSpace(cfg.HTTP.FrontendDir)
	cfg.Bootstrap.DeploymentRole = strings.TrimSpace(cfg.Bootstrap.DeploymentRole)
	cfg.Security.Admin.Username = strings.TrimSpace(cfg.Security.Admin.Username)
	cfg.Security.Admin.Password = strings.TrimSpace(cfg.Security.Admin.Password)
	cfg.Security.Admin.Token = strings.TrimSpace(cfg.Security.Admin.Token)
	cfg.Security.SecretKey = strings.TrimSpace(cfg.Security.SecretKey)
	cfg.Storage.DatabaseURL = strings.TrimSpace(cfg.Storage.DatabaseURL)
	cfg.Storage.Redis.URL = strings.TrimSpace(cfg.Storage.Redis.URL)
	cfg.Storage.Redis.Namespace = strings.TrimSpace(cfg.Storage.Redis.Namespace)
	cfg.Official.UpdateManifestURL = strings.TrimSpace(cfg.Official.UpdateManifestURL)
	normalizeOfficial(&cfg.Official)
	cfg.Plugins.CacheDir = strings.TrimSpace(cfg.Plugins.CacheDir)
	cfg.Plugins.ActiveDir = strings.TrimSpace(cfg.Plugins.ActiveDir)
	if cfg.Plugins.ActiveDir == "" && cfg.Plugins.CacheDir != "" {
		cfg.Plugins.ActiveDir = filepath.Join(filepath.Dir(cfg.Plugins.CacheDir), "plugin-active")
	}
	cfg.Plugins.HostURL = strings.TrimSpace(cfg.Plugins.HostURL)
	if cfg.Plugins.HostURL == "" {
		cfg.Plugins.HostURL = defaultPluginHostURL(cfg.HTTP.Listen)
	}
	cfg.Jobs.Queue.Driver = strings.TrimSpace(cfg.Jobs.Queue.Driver)
	cfg.Jobs.RoutingAffinityDriver = strings.TrimSpace(cfg.Jobs.RoutingAffinityDriver)
	cfg.Artifacts.Driver = strings.TrimSpace(cfg.Artifacts.Driver)
	cfg.Artifacts.LocalRoot = strings.TrimSpace(cfg.Artifacts.LocalRoot)
	cfg.Artifacts.S3.Endpoint = strings.TrimSpace(cfg.Artifacts.S3.Endpoint)
	cfg.Artifacts.S3.Region = strings.TrimSpace(cfg.Artifacts.S3.Region)
	cfg.Artifacts.S3.Bucket = strings.TrimSpace(cfg.Artifacts.S3.Bucket)
	cfg.Artifacts.S3.Prefix = strings.Trim(strings.TrimSpace(cfg.Artifacts.S3.Prefix), "/")
	cfg.Artifacts.S3.AccessKey = strings.TrimSpace(cfg.Artifacts.S3.AccessKey)
	cfg.Artifacts.S3.SecretKey = strings.TrimSpace(cfg.Artifacts.S3.SecretKey)
	cfg.Maintenance.BackupDir = strings.TrimSpace(cfg.Maintenance.BackupDir)
	cfg.Maintenance.DiagnosticDir = strings.TrimSpace(cfg.Maintenance.DiagnosticDir)
}

func normalizeOfficial(cfg *Official) {
	cfg.Catalog.Mode = strings.TrimSpace(cfg.Catalog.Mode)
	cfg.Catalog.BootstrapURL = strings.TrimSpace(cfg.Catalog.BootstrapURL)
	cfg.Catalog.URL = strings.TrimSpace(cfg.Catalog.URL)
	cfg.Catalog.ServicesURL = strings.TrimSpace(cfg.Catalog.ServicesURL)
	cfg.Catalog.KeyID = strings.TrimSpace(cfg.Catalog.KeyID)
	cfg.Catalog.PublicKey = strings.TrimSpace(cfg.Catalog.PublicKey)
	cfg.License.URL = strings.TrimSpace(cfg.License.URL)
	cfg.License.RedeemURL = strings.TrimSpace(cfg.License.RedeemURL)
	cfg.License.KeyID = strings.TrimSpace(cfg.License.KeyID)
	cfg.License.PublicKey = strings.TrimSpace(cfg.License.PublicKey)
	cfg.Instance.ID = strings.TrimSpace(cfg.Instance.ID)
	cfg.Instance.Fingerprint = strings.TrimSpace(cfg.Instance.Fingerprint)
	cfg.Instance.DisplayName = strings.TrimSpace(cfg.Instance.DisplayName)
}

func validateJobs(cfg Server) error {
	limits := cfg.Jobs.Queue.Limits
	if limits.Profile < 0 || limits.Tenant < 0 || limits.Principal < 0 {
		return fmt.Errorf("%w: server.jobs.queue.limits values must be non-negative", ErrInvalidJobs)
	}
	switch cfg.Jobs.Queue.Driver {
	case "memory":
	case "redis":
		if cfg.Storage.Redis.URL == "" {
			return fmt.Errorf("%w: server.storage.redis.url is required when the queue driver is redis", ErrInvalidJobs)
		}
	default:
		return fmt.Errorf("%w: server.jobs.queue.driver must be memory or redis", ErrInvalidJobs)
	}
	switch cfg.Jobs.RoutingAffinityDriver {
	case "repository":
	case "redis":
		if cfg.Storage.Redis.URL == "" {
			return fmt.Errorf("%w: server.storage.redis.url is required when routing affinity uses redis", ErrInvalidJobs)
		}
	default:
		return fmt.Errorf("%w: server.jobs.routing-affinity-driver must be repository or redis", ErrInvalidJobs)
	}
	return nil
}

func validateArtifacts(cfg Artifacts) error {
	switch cfg.Driver {
	case "none":
		return nil
	case "local":
		if cfg.LocalRoot == "" {
			return fmt.Errorf("%w: server.artifacts.local-root is required for the local driver", ErrInvalidArtifacts)
		}
		return nil
	case "s3":
		if cfg.S3.Bucket == "" || cfg.S3.AccessKey == "" || cfg.S3.SecretKey == "" {
			return fmt.Errorf("%w: S3 bucket, access-key, and secret-key are required", ErrInvalidArtifacts)
		}
		return nil
	default:
		return fmt.Errorf("%w: server.artifacts.driver must be none, local, or s3", ErrInvalidArtifacts)
	}
}

func validateOfficial(cfg Official) error {
	if (cfg.License.KeyID == "") != (cfg.License.PublicKey == "") {
		return fmt.Errorf("%w: license key-id and public-key must be configured together", ErrInvalidOfficial)
	}
	switch cfg.Catalog.Mode {
	case "disabled":
		return nil
	case "online", "private_mirror":
		if cfg.Catalog.BootstrapURL == "" {
			if cfg.Catalog.URL == "" || cfg.Catalog.KeyID == "" || cfg.Catalog.PublicKey == "" {
				return fmt.Errorf("%w: catalog URL and trust material are required without a bootstrap URL", ErrInvalidOfficial)
			}
		}
		return nil
	case "offline":
		if cfg.Catalog.KeyID == "" || cfg.Catalog.PublicKey == "" {
			return fmt.Errorf("%w: offline catalog mode requires key-id and public-key", ErrInvalidOfficial)
		}
		return nil
	default:
		return fmt.Errorf("%w: catalog mode must be disabled, online, private_mirror, or offline", ErrInvalidOfficial)
	}
}

func isDeploymentRole(value string) bool {
	switch value {
	case "personal", "relay_operator", "enterprise", "platform":
		return true
	default:
		return false
	}
}

func validRuntimeNamespace(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func defaultPluginHostURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		if strings.HasPrefix(listen, ":") {
			port = strings.TrimPrefix(listen, ":")
		} else {
			return ""
		}
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/v1/plugin-host"
}
