package config

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

const localDevelopmentSecret = "asterrouter-local-development-secret"

type Config struct {
	Addr                string
	AdminToken          string
	AdminUsername       string
	AdminPassword       string
	DatabaseURL         string
	DeploymentRole      string
	FrontendDir         string
	DefaultProfile      string
	Profiles            []string
	PublicBase          string
	SecretKey           string
	Version             string
	BuildType           string
	UpdateManifestURL   string
	CatalogMode         string
	CatalogBootstrapURL string
	CatalogURL          string
	OfficialServicesURL string
	CatalogKeyID        string
	CatalogPublicKey    string
	LicenseURL          string
	RedeemURL           string
	LicenseKeyID        string
	LicensePublicKey    string
	InstanceID          string
	InstanceFingerprint string
	InstanceDisplayName string
	PluginCacheDir      string
	PluginActiveDir     string
	PluginHostURL       string
	BackupDir           string
	DiagnosticDir       string
	MaxArchiveBytes     int64
	AllowRestart        bool
	DemoMode            bool
}

func Load() Config {
	addr := getEnv("ASTER_ADDR", ":8080")
	profiles := normalizeProfiles(os.Getenv("ASTER_PROFILES"))
	defaultProfile := normalizeProfile(os.Getenv("ASTER_DEFAULT_PROFILE"))
	deploymentRole := strings.TrimSpace(os.Getenv("ASTER_DEPLOYMENT_ROLE"))
	if normalizedRole := normalizeProfile(deploymentRole); normalizedRole != "" && len(profiles) == 0 {
		profiles = []string{normalizedRole}
		if defaultProfile == "" {
			defaultProfile = normalizedRole
		}
	}
	if defaultProfile == "" && len(profiles) > 0 {
		defaultProfile = profiles[0]
	}
	pluginCacheDir := getEnv("ASTER_PLUGIN_CACHE_DIR", "data/plugin-cache")
	pluginActiveDir := getEnv("ASTER_PLUGIN_ACTIVE_DIR", filepath.Join(filepath.Dir(pluginCacheDir), "plugin-active"))
	return Config{
		Addr:                addr,
		AdminToken:          strings.TrimSpace(os.Getenv("ASTER_ADMIN_TOKEN")),
		AdminUsername:       getEnv("ASTER_ADMIN_USERNAME", "admin"),
		AdminPassword:       strings.TrimSpace(os.Getenv("ASTER_ADMIN_PASSWORD")),
		DatabaseURL:         strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DeploymentRole:      deploymentRole,
		FrontendDir:         getEnv("ASTER_FRONTEND_DIR", "../frontend/dist"),
		DefaultProfile:      defaultProfile,
		Profiles:            profiles,
		PublicBase:          strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		SecretKey:           getEnv("ASTER_SECRET_KEY", localDevelopmentSecret),
		Version:             getEnv("ASTER_VERSION", buildinfo.Version),
		BuildType:           getEnv("ASTER_BUILD_TYPE", buildinfo.BuildType),
		UpdateManifestURL:   strings.TrimSpace(os.Getenv("ASTER_UPDATE_MANIFEST_URL")),
		CatalogMode:         getEnv("ASTER_CATALOG_MODE", "disabled"),
		CatalogBootstrapURL: strings.TrimSpace(os.Getenv("ASTER_CATALOG_BOOTSTRAP_URL")),
		CatalogURL:          strings.TrimSpace(os.Getenv("ASTER_CATALOG_URL")),
		OfficialServicesURL: strings.TrimSpace(os.Getenv("ASTER_OFFICIAL_SERVICES_URL")),
		CatalogKeyID:        strings.TrimSpace(os.Getenv("ASTER_CATALOG_KEY_ID")),
		CatalogPublicKey:    strings.TrimSpace(os.Getenv("ASTER_CATALOG_PUBLIC_KEY")),
		LicenseURL:          strings.TrimSpace(os.Getenv("ASTER_LICENSE_URL")),
		RedeemURL:           strings.TrimSpace(os.Getenv("ASTER_REDEEM_URL")),
		LicenseKeyID:        strings.TrimSpace(os.Getenv("ASTER_LICENSE_KEY_ID")),
		LicensePublicKey:    strings.TrimSpace(os.Getenv("ASTER_LICENSE_PUBLIC_KEY")),
		InstanceID:          strings.TrimSpace(os.Getenv("ASTER_INSTANCE_ID")),
		InstanceFingerprint: strings.TrimSpace(os.Getenv("ASTER_INSTANCE_FINGERPRINT")),
		InstanceDisplayName: strings.TrimSpace(os.Getenv("ASTER_INSTANCE_DISPLAY_NAME")),
		PluginCacheDir:      pluginCacheDir,
		PluginActiveDir:     pluginActiveDir,
		PluginHostURL:       defaultString(strings.TrimSpace(os.Getenv("ASTER_PLUGIN_HOST_URL")), defaultPluginHostURL(addr)),
		BackupDir:           getEnv("ASTER_BACKUP_DIR", "data/backups"),
		DiagnosticDir:       getEnv("ASTER_DIAGNOSTIC_DIR", "data/diagnostics"),
		MaxArchiveBytes:     getInt64Env("ASTER_MAX_ARCHIVE_BYTES", 2<<30),
		AllowRestart:        getBoolEnv("ASTER_ALLOW_RESTART"),
		DemoMode:            getBoolEnv("ASTER_DEMO_MODE"),
	}
}

func defaultPluginHostURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			port = strings.TrimPrefix(addr, ":")
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

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func ValidateRuntime(cfg Config) error {
	if strings.TrimSpace(cfg.DeploymentRole) != "" {
		deploymentRole := normalizeProfile(cfg.DeploymentRole)
		if !isDeploymentProfile(deploymentRole) {
			return errors.New("ASTER_DEPLOYMENT_ROLE must be one of personal, relay_operator, enterprise, or platform")
		}
		if len(cfg.Profiles) != 1 || cfg.Profiles[0] != deploymentRole || cfg.DefaultProfile != deploymentRole {
			return errors.New("ASTER_DEPLOYMENT_ROLE must match the legacy ASTER_PROFILES and ASTER_DEFAULT_PROFILE values when they are also set")
		}
	}
	for _, profile := range cfg.Profiles {
		if !isDeploymentProfile(profile) {
			return errors.New("ASTER_PROFILES must contain only personal, relay_operator, enterprise, or platform")
		}
	}
	if cfg.DefaultProfile != "" && !isDeploymentProfile(cfg.DefaultProfile) {
		return errors.New("ASTER_DEFAULT_PROFILE must be one of personal, relay_operator, enterprise, or platform")
	}
	if len(cfg.Profiles) > 1 {
		return errors.New("ASTER_PROFILES accepts one bootstrap profile; deploy a separate instance for a different business model")
	}
	if len(cfg.Profiles) == 1 && cfg.DefaultProfile != "" && cfg.DefaultProfile != cfg.Profiles[0] {
		return errors.New("ASTER_DEFAULT_PROFILE must match the single ASTER_PROFILES bootstrap profile")
	}
	if cfg.BuildType != "release" {
		return nil
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return errors.New("DATABASE_URL is required for release deployments")
	}
	if strings.TrimSpace(cfg.SecretKey) == localDevelopmentSecret {
		return errors.New("ASTER_SECRET_KEY must be set to a stable production secret")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" && strings.TrimSpace(cfg.AdminToken) == "" && !cfg.DemoMode {
		return errors.New("ASTER_ADMIN_PASSWORD or ASTER_ADMIN_TOKEN is required for release deployments")
	}
	switch strings.TrimSpace(cfg.CatalogMode) {
	case "online", "private_mirror":
		if strings.TrimSpace(cfg.CatalogBootstrapURL) == "" {
			if strings.TrimSpace(cfg.CatalogURL) == "" {
				return errors.New("ASTER_CATALOG_URL or ASTER_CATALOG_BOOTSTRAP_URL is required when ASTER_CATALOG_MODE=online or private_mirror")
			}
			if strings.TrimSpace(cfg.CatalogKeyID) == "" || strings.TrimSpace(cfg.CatalogPublicKey) == "" {
				return errors.New("ASTER_CATALOG_KEY_ID and ASTER_CATALOG_PUBLIC_KEY are required when ASTER_CATALOG_BOOTSTRAP_URL is not set")
			}
		}
		if (strings.TrimSpace(cfg.LicenseKeyID) == "") != (strings.TrimSpace(cfg.LicensePublicKey) == "") {
			return errors.New("ASTER_LICENSE_KEY_ID and ASTER_LICENSE_PUBLIC_KEY must be set together")
		}
	case "offline":
		if strings.TrimSpace(cfg.CatalogKeyID) == "" || strings.TrimSpace(cfg.CatalogPublicKey) == "" {
			return errors.New("ASTER_CATALOG_KEY_ID and ASTER_CATALOG_PUBLIC_KEY are required when ASTER_CATALOG_MODE=offline")
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeProfile(value string) string {
	return strings.TrimSpace(value)
}

func isDeploymentProfile(value string) bool {
	switch strings.TrimSpace(value) {
	case "personal", "relay_operator", "enterprise", "platform":
		return true
	default:
		return false
	}
}

func normalizeProfiles(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		profile := normalizeProfile(field)
		if seen[profile] {
			continue
		}
		seen[profile] = true
		out = append(out, profile)
	}
	return out
}

func getBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func getInt64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
