package settings

import "time"

const (
	KeySiteName                 = "site_name"
	KeySiteSubtitle             = "site_subtitle"
	KeySiteLogo                 = "site_logo"
	KeyPublicBaseURL            = "public_base_url"
	KeyDefaultLocale            = "default_locale"
	KeyEnabledLocales           = "enabled_locales"
	KeyDefaultProfile           = "default_profile"
	KeyEnabledProfiles          = "enabled_profiles"
	KeySetupCompleted           = "setup_completed"
	KeyGatewayBasePath          = "gateway_base_path"
	KeyOIDCEnabled              = "oidc_enabled"
	KeyOIDCProviderName         = "oidc_provider_name"
	KeyOIDCIssuerURL            = "oidc_issuer_url"
	KeyOIDCClientID             = "oidc_client_id"
	KeyOIDCRequireVerifiedEmail = "oidc_require_verified_email"
	KeyFeishuEnabled            = "feishu_enabled"
	KeyFeishuRegion             = "feishu_region"
	KeyFeishuAppID              = "feishu_app_id"
	KeyFeishuAppSecret          = "feishu_app_secret"
	KeyGitHubOAuthEnabled       = "github_oauth_enabled"
	KeyGitHubOAuthClientID      = "github_oauth_client_id"
	KeyGitHubOAuthSecret        = "github_oauth_client_secret"
	KeyGoogleOAuthEnabled       = "google_oauth_enabled"
	KeyGoogleOAuthClientID      = "google_oauth_client_id"
	KeyGoogleOAuthSecret        = "google_oauth_client_secret"
	KeyDingTalkEnabled          = "dingtalk_enabled"
	KeyDingTalkClientID         = "dingtalk_client_id"
	KeyDingTalkClientSecret     = "dingtalk_client_secret"
	KeyRegistrationEnabled      = "registration_enabled"
	KeyEmailVerifyEnabled       = "email_verify_enabled"
	KeyAllowedEmailDomains      = "allowed_email_domains"
	KeyInvitationRequired       = "invitation_required"
	KeyInvitationCodes          = "invitation_codes"
	KeyTOTPEnabled              = "totp_enabled"
	KeyTrustedProxyHeaders      = "trusted_proxy_headers"
	KeyTurnstileEnabled         = "turnstile_enabled"
	KeyTurnstileSiteKey         = "turnstile_site_key"
	KeyTurnstileSecretKey       = "turnstile_secret_key"
	KeyDefaultBalanceCents      = "default_balance_cents"
	KeyDefaultConcurrency       = "default_concurrency"
	KeyDefaultRPM               = "default_rpm"
	KeyAuthSourceDefaults       = "auth_source_defaults"
	KeySMTPHost                 = "smtp_host"
	KeySMTPPort                 = "smtp_port"
	KeySMTPUsername             = "smtp_username"
	KeySMTPPassword             = "smtp_password"
	KeySMTPFrom                 = "smtp_from"
	KeyEmailTemplates           = "email_templates"
	KeyLoginAgreementEnabled    = "login_agreement_enabled"
	KeyLoginAgreementTitle      = "login_agreement_title"
	KeyLoginAgreementContent    = "login_agreement_content"
	KeyBackendMode              = "backend_mode"
	KeyDefaultPageSize          = "default_page_size"
	KeyPageSizeOptions          = "page_size_options"
	KeySupportContact           = "support_contact"
	KeyDocumentationURL         = "documentation_url"
	KeyHomeContent              = "home_content"
	KeyCustomEndpoints          = "custom_endpoints"
	KeyCustomMenuItems          = "custom_menu_items"
	KeyHideImportButton         = "hide_import_button"
	KeyLoginAgreementMode       = "login_agreement_mode"
	KeyLoginAgreementUpdatedAt  = "login_agreement_updated_at"
	KeyLegalDocuments           = "legal_documents"
	KeyChannelMonitorEnabled    = "channel_monitor_enabled"
	KeyChannelMonitorInterval   = "channel_monitor_interval_seconds"
	KeyAvailableChannels        = "available_channels_enabled"
	KeyRiskControlEnabled       = "risk_control_enabled"
	KeyCyberSessionBlock        = "cyber_session_block_enabled"
	KeyCyberSessionBlockTTL     = "cyber_session_block_ttl_seconds"
	KeyBackupS3Enabled          = "backup_s3_enabled"
	KeyBackupS3Endpoint         = "backup_s3_endpoint"
	KeyBackupS3Region           = "backup_s3_region"
	KeyBackupS3Bucket           = "backup_s3_bucket"
	KeyBackupS3Prefix           = "backup_s3_prefix"
	KeyBackupS3AccessKey        = "backup_s3_access_key"
	KeyBackupS3SecretKey        = "backup_s3_secret_key"
	KeyBackupS3PathStyle        = "backup_s3_path_style"
	KeyBackupRetentionDays      = "backup_retention_days"
	KeyBackupMaxRetained        = "backup_max_retained"
	KeyBackupScheduleEnabled    = "backup_schedule_enabled"
	KeyBackupIntervalHours      = "backup_interval_hours"
	KeyDataRetentionDays        = "data_retention_days"
	KeyPromptLoggingMode        = "prompt_logging_mode"
	KeyUpdateChannel            = "update_channel"
	KeyServiceCenterMode        = "service_center_mode"
)

type Entry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PublicSettings struct {
	SiteName                 string           `json:"site_name"`
	SiteSubtitle             string           `json:"site_subtitle"`
	SiteLogo                 string           `json:"site_logo"`
	PublicBaseURL            string           `json:"public_base_url"`
	APIBaseURL               string           `json:"api_base_url"`
	GatewayBasePath          string           `json:"gateway_base_path"`
	DefaultProfile           string           `json:"default_profile"`
	EnabledProfiles          []string         `json:"enabled_profiles"`
	SetupCompleted           bool             `json:"setup_completed"`
	DefaultLocale            string           `json:"default_locale"`
	EnabledLocales           []string         `json:"enabled_locales"`
	OIDCEnabled              bool             `json:"oidc_enabled"`
	OIDCProviderName         string           `json:"oidc_provider_name"`
	OIDCRequireVerifiedEmail bool             `json:"oidc_require_verified_email"`
	FeishuEnabled            bool             `json:"feishu_enabled"`
	FeishuRegion             string           `json:"feishu_region"`
	GitHubOAuthEnabled       bool             `json:"github_oauth_enabled"`
	GoogleOAuthEnabled       bool             `json:"google_oauth_enabled"`
	DingTalkEnabled          bool             `json:"dingtalk_enabled"`
	RegistrationEnabled      bool             `json:"registration_enabled"`
	EmailVerifyEnabled       bool             `json:"email_verify_enabled"`
	TOTPEnabled              bool             `json:"totp_enabled"`
	TurnstileEnabled         bool             `json:"turnstile_enabled"`
	TurnstileSiteKey         string           `json:"turnstile_site_key"`
	InvitationRequired       bool             `json:"invitation_required"`
	LoginAgreementEnabled    bool             `json:"login_agreement_enabled"`
	LoginAgreementMode       string           `json:"login_agreement_mode"`
	LoginAgreementUpdatedAt  string           `json:"login_agreement_updated_at"`
	LegalDocuments           []LegalDocument  `json:"legal_documents"`
	BackendMode              bool             `json:"backend_mode"`
	SupportContact           string           `json:"support_contact"`
	DocumentationURL         string           `json:"documentation_url"`
	CustomEndpoints          []CustomEndpoint `json:"custom_endpoints"`
	CustomMenuItems          []CustomMenuItem `json:"custom_menu_items"`
	ChannelMonitorEnabled    bool             `json:"channel_monitor_enabled"`
	AvailableChannelsEnabled bool             `json:"available_channels_enabled"`
	RiskControlEnabled       bool             `json:"risk_control_enabled"`
	CyberSessionBlockEnabled bool             `json:"cyber_session_block_enabled"`
	BackupS3Enabled          bool             `json:"backup_s3_enabled"`
	ServiceCenterMode        string           `json:"service_center_mode"`
	Version                  string           `json:"version"`
	ServerTimezone           string           `json:"server_timezone"`
	ServerUTCOffset          string           `json:"server_utc_offset"`
	StorageMode              string           `json:"storage_mode"`
	DemoMode                 bool             `json:"demo_mode"`
}

type AdminSettings struct {
	PublicSettings
	RuntimeRestartRequired        bool                         `json:"runtime_restart_required"`
	RuntimeRestartReasons         []string                     `json:"runtime_restart_reasons"`
	OIDCIssuerURL                 string                       `json:"oidc_issuer_url"`
	OIDCClientID                  string                       `json:"oidc_client_id"`
	FeishuAppID                   string                       `json:"feishu_app_id"`
	FeishuAppSecret               string                       `json:"feishu_app_secret,omitempty"`
	FeishuConfigured              bool                         `json:"feishu_configured"`
	GitHubOAuthClientID           string                       `json:"github_oauth_client_id"`
	GitHubOAuthClientSecret       string                       `json:"github_oauth_client_secret,omitempty"`
	GitHubOAuthConfigured         bool                         `json:"github_oauth_configured"`
	GoogleOAuthClientID           string                       `json:"google_oauth_client_id"`
	GoogleOAuthClientSecret       string                       `json:"google_oauth_client_secret,omitempty"`
	GoogleOAuthConfigured         bool                         `json:"google_oauth_configured"`
	DingTalkClientID              string                       `json:"dingtalk_client_id"`
	DingTalkClientSecret          string                       `json:"dingtalk_client_secret,omitempty"`
	DingTalkConfigured            bool                         `json:"dingtalk_configured"`
	AllowedEmailDomains           []string                     `json:"allowed_email_domains"`
	InvitationCodes               []string                     `json:"invitation_codes"`
	TrustedProxyHeaders           bool                         `json:"trusted_proxy_headers"`
	TurnstileSecretKey            string                       `json:"turnstile_secret_key,omitempty"`
	TurnstileConfigured           bool                         `json:"turnstile_configured"`
	DefaultBalanceCents           int                          `json:"default_balance_cents"`
	DefaultConcurrency            int                          `json:"default_concurrency"`
	DefaultRPM                    int                          `json:"default_rpm"`
	AuthSourceDefaults            map[string]AuthSourceDefault `json:"auth_source_defaults"`
	SMTPHost                      string                       `json:"smtp_host"`
	SMTPPort                      int                          `json:"smtp_port"`
	SMTPUsername                  string                       `json:"smtp_username"`
	SMTPPassword                  string                       `json:"smtp_password,omitempty"`
	SMTPFrom                      string                       `json:"smtp_from"`
	SMTPConfigured                bool                         `json:"smtp_configured"`
	EmailTemplates                []EmailTemplate              `json:"email_templates"`
	LoginAgreementTitle           string                       `json:"login_agreement_title"`
	LoginAgreementContent         string                       `json:"login_agreement_content"`
	DefaultPageSize               int                          `json:"default_page_size"`
	PageSizeOptions               []int                        `json:"page_size_options"`
	HomeContent                   string                       `json:"home_content"`
	HideImportButton              bool                         `json:"hide_import_button"`
	ChannelMonitorIntervalSeconds int                          `json:"channel_monitor_interval_seconds"`
	CyberSessionBlockTTLSeconds   int                          `json:"cyber_session_block_ttl_seconds"`
	BackupS3Endpoint              string                       `json:"backup_s3_endpoint"`
	BackupS3Region                string                       `json:"backup_s3_region"`
	BackupS3Bucket                string                       `json:"backup_s3_bucket"`
	BackupS3Prefix                string                       `json:"backup_s3_prefix"`
	BackupS3AccessKey             string                       `json:"backup_s3_access_key"`
	BackupS3SecretKey             string                       `json:"backup_s3_secret_key,omitempty"`
	BackupS3Configured            bool                         `json:"backup_s3_configured"`
	BackupS3PathStyle             bool                         `json:"backup_s3_path_style"`
	BackupRetentionDays           int                          `json:"backup_retention_days"`
	BackupMaxRetained             int                          `json:"backup_max_retained"`
	BackupScheduleEnabled         bool                         `json:"backup_schedule_enabled"`
	BackupIntervalHours           int                          `json:"backup_interval_hours"`
	DataRetentionDays             int                          `json:"data_retention_days"`
	PromptLoggingMode             string                       `json:"prompt_logging_mode"`
	UpdateChannel                 string                       `json:"update_channel"`
}

type LegalDocument struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Content string `json:"content"`
}

type EmailTemplate struct {
	Event   string `json:"event"`
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type CustomEndpoint struct {
	Name        string `json:"name"`
	Endpoint    string `json:"endpoint"`
	Description string `json:"description"`
}

type CustomMenuItem struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	URL          string `json:"url"`
	OpenInNewTab bool   `json:"open_in_new_tab"`
}

type AuthSourceDefault struct {
	Enabled      bool `json:"enabled"`
	BalanceCents int  `json:"balance_cents"`
	Concurrency  int  `json:"concurrency"`
	RPM          int  `json:"rpm"`
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
