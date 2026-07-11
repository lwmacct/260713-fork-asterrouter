package settings

import "time"

const (
	KeySiteName          = "site_name"
	KeySiteSubtitle      = "site_subtitle"
	KeyPublicBaseURL     = "public_base_url"
	KeyDefaultLocale     = "default_locale"
	KeyEnabledLocales    = "enabled_locales"
	KeyDefaultProfile    = "default_profile"
	KeyEnabledProfiles   = "enabled_profiles"
	KeySetupCompleted    = "setup_completed"
	KeyGatewayBasePath   = "gateway_base_path"
	KeyOIDCEnabled       = "oidc_enabled"
	KeyOIDCProviderName  = "oidc_provider_name"
	KeyOIDCIssuerURL     = "oidc_issuer_url"
	KeyOIDCClientID      = "oidc_client_id"
	KeyDataRetentionDays = "data_retention_days"
	KeyPromptLoggingMode = "prompt_logging_mode"
	KeyUpdateChannel     = "update_channel"
	KeyServiceCenterMode = "service_center_mode"
)

type Entry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PublicSettings struct {
	SiteName          string   `json:"site_name"`
	SiteSubtitle      string   `json:"site_subtitle"`
	PublicBaseURL     string   `json:"public_base_url"`
	APIBaseURL        string   `json:"api_base_url"`
	GatewayBasePath   string   `json:"gateway_base_path"`
	DefaultProfile    string   `json:"default_profile"`
	EnabledProfiles   []string `json:"enabled_profiles"`
	SetupCompleted    bool     `json:"setup_completed"`
	DefaultLocale     string   `json:"default_locale"`
	EnabledLocales    []string `json:"enabled_locales"`
	OIDCEnabled       bool     `json:"oidc_enabled"`
	OIDCProviderName  string   `json:"oidc_provider_name"`
	ServiceCenterMode string   `json:"service_center_mode"`
	Version           string   `json:"version"`
	ServerTimezone    string   `json:"server_timezone"`
	ServerUTCOffset   string   `json:"server_utc_offset"`
	StorageMode       string   `json:"storage_mode"`
}

type AdminSettings struct {
	PublicSettings
	OIDCIssuerURL     string `json:"oidc_issuer_url"`
	OIDCClientID      string `json:"oidc_client_id"`
	DataRetentionDays int    `json:"data_retention_days"`
	PromptLoggingMode string `json:"prompt_logging_mode"`
	UpdateChannel     string `json:"update_channel"`
}

type LocaleInfo struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Native string `json:"native"`
}

var SupportedLocales = []LocaleInfo{
	{Code: "en-US", Name: "English", Native: "English"},
	{Code: "zh-CN", Name: "Simplified Chinese", Native: "简体中文"},
}
