package settings

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
)

type ServiceOptions struct {
	Version         string
	EnabledProfiles []string
	DefaultProfile  string
	StorageMode     string
	DemoMode        bool
}

type Service struct {
	repo            Repository
	version         string
	enabledProfiles []string
	defaultProfile  string
	storageMode     string
	demoMode        bool
	inviteMu        sync.Mutex
}

func NewService(repo Repository, opts ServiceOptions) *Service {
	version := opts.Version
	if version == "" {
		version = buildinfo.Version
	}
	storageMode := opts.StorageMode
	if storageMode == "" {
		storageMode = "unknown"
	}
	return &Service{
		repo:            repo,
		version:         version,
		enabledProfiles: normalizeProfiles(opts.EnabledProfiles),
		defaultProfile:  strings.TrimSpace(opts.DefaultProfile),
		storageMode:     storageMode,
		demoMode:        opts.DemoMode,
	}
}

func (s *Service) Public(ctx context.Context) (PublicSettings, error) {
	settings, err := s.Admin(ctx)
	if err != nil {
		return PublicSettings{}, err
	}
	return settings.PublicSettings, nil
}

func (s *Service) Admin(ctx context.Context) (AdminSettings, error) {
	raw, err := s.repo.GetAll(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	merged := defaults()
	for key, value := range raw {
		merged[key] = value
	}
	if len(s.enabledProfiles) > 0 && raw[KeyEnabledProfiles] == "" && raw[KeyDefaultProfile] == "" {
		defaultProfile := s.defaultProfile
		if defaultProfile == "" {
			defaultProfile = s.enabledProfiles[0]
		}
		if !containsString(s.enabledProfiles, defaultProfile) {
			defaultProfile = s.enabledProfiles[0]
		}
		encodedProfiles, _ := json.Marshal(s.enabledProfiles)
		merged[KeyEnabledProfiles] = string(encodedProfiles)
		merged[KeyDefaultProfile] = defaultProfile
		merged[KeySetupCompleted] = "true"
	}
	if s.demoMode && len(s.enabledProfiles) == 0 && raw[KeyEnabledProfiles] == "" && raw[KeyDefaultProfile] == "" {
		merged[KeyEnabledProfiles] = `["personal","relay_operator","enterprise","platform"]`
		merged[KeyDefaultProfile] = "personal"
		merged[KeySetupCompleted] = "true"
	}
	return s.parse(merged), nil
}

func (s *Service) Update(ctx context.Context, in AdminSettings) (AdminSettings, error) {
	current, err := s.Admin(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	if profileConfigurationChanged(current, in) {
		return AdminSettings{}, errors.New("deployment profile is fixed after installation; create a separate instance for a different business model")
	}
	values, err := valuesFromAdminSettings(in)
	if err != nil {
		return AdminSettings{}, err
	}
	if strings.TrimSpace(in.FeishuAppSecret) == "" {
		if existing, getErr := s.repo.GetAll(ctx); getErr == nil && strings.TrimSpace(existing[KeyFeishuAppSecret]) != "" {
			values[KeyFeishuAppSecret] = existing[KeyFeishuAppSecret]
		}
	}
	if existing, getErr := s.repo.GetAll(ctx); getErr == nil {
		if strings.TrimSpace(in.TurnstileSecretKey) == "" && existing[KeyTurnstileSecretKey] != "" {
			values[KeyTurnstileSecretKey] = existing[KeyTurnstileSecretKey]
		}
		if strings.TrimSpace(in.SMTPPassword) == "" && existing[KeySMTPPassword] != "" {
			values[KeySMTPPassword] = existing[KeySMTPPassword]
		}
		if strings.TrimSpace(in.GitHubOAuthClientSecret) == "" && existing[KeyGitHubOAuthSecret] != "" {
			values[KeyGitHubOAuthSecret] = existing[KeyGitHubOAuthSecret]
		}
		if strings.TrimSpace(in.GoogleOAuthClientSecret) == "" && existing[KeyGoogleOAuthSecret] != "" {
			values[KeyGoogleOAuthSecret] = existing[KeyGoogleOAuthSecret]
		}
		if strings.TrimSpace(in.DingTalkClientSecret) == "" && existing[KeyDingTalkClientSecret] != "" {
			values[KeyDingTalkClientSecret] = existing[KeyDingTalkClientSecret]
		}
		if strings.TrimSpace(in.BackupS3SecretKey) == "" && existing[KeyBackupS3SecretKey] != "" {
			values[KeyBackupS3SecretKey] = existing[KeyBackupS3SecretKey]
		}
	}
	if err := s.repo.SetMultiple(ctx, values); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func profileConfigurationChanged(current, next AdminSettings) bool {
	if strings.TrimSpace(current.DefaultProfile) != strings.TrimSpace(next.DefaultProfile) {
		return true
	}
	return !sameProfiles(normalizeProfiles(current.EnabledProfiles), normalizeProfiles(next.EnabledProfiles))
}

func (s *Service) ApplyProfiles(ctx context.Context, profiles []string, defaultProfile string) (AdminSettings, error) {
	enabledProfiles := normalizeProfiles(profiles)
	if len(enabledProfiles) == 0 {
		return AdminSettings{}, errors.New("at least one profile is required")
	}
	if !s.demoMode && len(enabledProfiles) != 1 {
		return AdminSettings{}, errors.New("exactly one deployment profile is supported; migrate to a separate instance before changing profiles")
	}
	defaultProfile = strings.TrimSpace(defaultProfile)
	if defaultProfile == "" {
		defaultProfile = enabledProfiles[0]
	}
	if !containsString(enabledProfiles, defaultProfile) {
		return AdminSettings{}, fmt.Errorf("default profile %q is not enabled", defaultProfile)
	}
	if !s.demoMode {
		if err := s.repo.InitializeDeploymentProfile(ctx, defaultProfile); err == nil {
			return s.Admin(ctx)
		} else if !errors.Is(err, ErrDeploymentProfileInitialized) {
			return AdminSettings{}, err
		}
		raw, err := s.repo.GetAll(ctx)
		if err != nil {
			return AdminSettings{}, err
		}
		persistedProfile, err := persistedDeploymentProfile(raw)
		if err != nil {
			return AdminSettings{}, err
		}
		if persistedProfile != defaultProfile {
			return AdminSettings{}, ErrDeploymentProfileInitialized
		}
		return s.Admin(ctx)
	}
	encodedProfiles, _ := json.Marshal(enabledProfiles)
	if err := s.repo.SetMultiple(ctx, map[string]string{
		KeyDefaultProfile:  defaultProfile,
		KeyEnabledProfiles: string(encodedProfiles),
		KeySetupCompleted:  "true",
	}); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

func sameProfiles(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// ApplyInitialProfile completes a fresh installation with one primary product
// profile. A deployment profile is immutable once setup is complete because
// current Core resources do not carry an end-to-end profile scope.
func (s *Service) ApplyInitialProfile(ctx context.Context, profile string) (AdminSettings, error) {
	profile = strings.TrimSpace(profile)
	if !isProfile(profile) {
		return AdminSettings{}, fmt.Errorf("%w %q", ErrUnsupportedDeploymentProfile, profile)
	}
	if err := s.repo.InitializeDeploymentProfile(ctx, profile); err != nil {
		return AdminSettings{}, err
	}
	return s.Admin(ctx)
}

// BootstrapProfile persists the configured non-interactive installation
// profile exactly once. Environment configuration is bootstrap input only;
// PostgreSQL becomes the source of truth immediately afterwards.
func (s *Service) BootstrapProfile(ctx context.Context) error {
	if s.defaultProfile != "" && !isProfile(s.defaultProfile) {
		return fmt.Errorf("%w %q", ErrUnsupportedDeploymentProfile, s.defaultProfile)
	}
	if len(s.enabledProfiles) == 0 {
		return nil
	}
	if len(s.enabledProfiles) != 1 {
		return errors.New("exactly one bootstrap profile is required")
	}
	profile := s.enabledProfiles[0]
	if s.defaultProfile != "" && s.defaultProfile != profile {
		return errors.New("bootstrap default profile must match the enabled profile")
	}
	if err := s.repo.InitializeDeploymentProfile(ctx, profile); err == nil {
		return nil
	} else if !errors.Is(err, ErrDeploymentProfileInitialized) {
		return err
	}
	raw, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}
	persistedProfile, err := persistedDeploymentProfile(raw)
	if err != nil {
		return err
	}
	if persistedProfile != profile {
		return fmt.Errorf("configured deployment role %q does not match persisted deployment profile %q; deploy a separate instance for a different business model", profile, persistedProfile)
	}
	return nil
}

func persistedDeploymentProfile(raw map[string]string) (string, error) {
	profiles := normalizeProfiles(parseStringList(raw[KeyEnabledProfiles], nil))
	defaultProfile := strings.TrimSpace(raw[KeyDefaultProfile])
	if !parseBool(raw[KeySetupCompleted]) || len(profiles) != 1 || defaultProfile == "" || profiles[0] != defaultProfile {
		return "", errors.New("persisted deployment profile is incomplete or invalid; repair the stored state explicitly instead of overwriting it")
	}
	return defaultProfile, nil
}

func (s *Service) Health(ctx context.Context) error {
	return s.repo.Health(ctx)
}

func (s *Service) FeishuSecret(ctx context.Context) (string, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyFeishuAppSecret], nil
}

func (s *Service) SocialOAuthSecrets(ctx context.Context) (github, google string, err error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", "", err
	}
	return values[KeyGitHubOAuthSecret], values[KeyGoogleOAuthSecret], nil
}

func (s *Service) DingTalkSecret(ctx context.Context) (string, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", err
	}
	return values[KeyDingTalkClientSecret], nil
}

type LoginSecuritySettings struct {
	TurnstileEnabled bool
	TurnstileSecret  string
}

type RegistrationPolicy struct {
	Enabled, EmailVerification, InvitationRequired bool
	AllowedDomains, InvitationCodes                []string
}

func (s *Service) SMTPConfig(ctx context.Context) (host string, port int, username, password, from string, err error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", 0, "", "", "", err
	}
	return values[KeySMTPHost], parseInt(values[KeySMTPPort], 587), values[KeySMTPUsername], values[KeySMTPPassword], values[KeySMTPFrom], nil
}

func (s *Service) RegistrationPolicy(ctx context.Context) (RegistrationPolicy, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	return RegistrationPolicy{Enabled: parseBool(values[KeyRegistrationEnabled]), EmailVerification: parseBool(values[KeyEmailVerifyEnabled]), InvitationRequired: parseBool(values[KeyInvitationRequired]), AllowedDomains: parseStringList(values[KeyAllowedEmailDomains], []string{}), InvitationCodes: parseStringList(values[KeyInvitationCodes], []string{})}, nil
}

func (s *Service) ConsumeInvitationCode(ctx context.Context, code string) error {
	s.inviteMu.Lock()
	defer s.inviteMu.Unlock()
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("invitation code is required")
	}
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}
	codes := parseStringList(values[KeyInvitationCodes], []string{})
	for index, candidate := range codes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			codes = append(codes[:index], codes[index+1:]...)
			raw, _ := json.Marshal(codes)
			return s.repo.SetMultiple(ctx, map[string]string{KeyInvitationCodes: string(raw)})
		}
	}
	return errors.New("invitation code is invalid")
}

func (s *Service) LoginSecurity(ctx context.Context) (LoginSecuritySettings, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return LoginSecuritySettings{}, err
	}
	return LoginSecuritySettings{TurnstileEnabled: parseBool(values[KeyTurnstileEnabled]), TurnstileSecret: values[KeyTurnstileSecretKey]}, nil
}

func (s *Service) parse(values map[string]string) AdminSettings {
	_, offset := time.Now().Zone()
	enabledProfiles := parseProfileList(values[KeyEnabledProfiles])
	defaultProfile := strings.TrimSpace(values[KeyDefaultProfile])
	if defaultProfile == "" && len(enabledProfiles) > 0 {
		defaultProfile = enabledProfiles[0]
	}
	if defaultProfile != "" && !containsString(enabledProfiles, defaultProfile) {
		enabledProfiles = normalizeProfiles(append([]string{defaultProfile}, enabledProfiles...))
	}
	return AdminSettings{
		PublicSettings: PublicSettings{
			SiteName:                 values[KeySiteName],
			SiteSubtitle:             values[KeySiteSubtitle],
			SiteLogo:                 values[KeySiteLogo],
			PublicBaseURL:            values[KeyPublicBaseURL],
			APIBaseURL:               "/api/v1",
			GatewayBasePath:          values[KeyGatewayBasePath],
			DefaultProfile:           defaultProfile,
			EnabledProfiles:          enabledProfiles,
			SetupCompleted:           parseBool(values[KeySetupCompleted]),
			DefaultLocale:            values[KeyDefaultLocale],
			EnabledLocales:           parseStringList(values[KeyEnabledLocales], []string{"en-US", "zh-CN"}),
			OIDCEnabled:              parseBool(values[KeyOIDCEnabled]),
			OIDCProviderName:         values[KeyOIDCProviderName],
			OIDCRequireVerifiedEmail: parseBool(values[KeyOIDCRequireVerifiedEmail]),
			FeishuEnabled:            parseBool(values[KeyFeishuEnabled]),
			FeishuRegion:             values[KeyFeishuRegion],
			GitHubOAuthEnabled:       parseBool(values[KeyGitHubOAuthEnabled]), GoogleOAuthEnabled: parseBool(values[KeyGoogleOAuthEnabled]),
			DingTalkEnabled:       parseBool(values[KeyDingTalkEnabled]),
			RegistrationEnabled:   parseBool(values[KeyRegistrationEnabled]),
			EmailVerifyEnabled:    parseBool(values[KeyEmailVerifyEnabled]),
			TOTPEnabled:           parseBool(values[KeyTOTPEnabled]),
			TurnstileEnabled:      parseBool(values[KeyTurnstileEnabled]),
			TurnstileSiteKey:      values[KeyTurnstileSiteKey],
			InvitationRequired:    parseBool(values[KeyInvitationRequired]),
			LoginAgreementEnabled: parseBool(values[KeyLoginAgreementEnabled]),
			LoginAgreementMode:    values[KeyLoginAgreementMode], LoginAgreementUpdatedAt: values[KeyLoginAgreementUpdatedAt], LegalDocuments: parseLegalDocuments(values[KeyLegalDocuments]), BackendMode: parseBool(values[KeyBackendMode]), SupportContact: values[KeySupportContact], DocumentationURL: values[KeyDocumentationURL],
			CustomEndpoints: parseCustomEndpoints(values[KeyCustomEndpoints]), CustomMenuItems: parseCustomMenuItems(values[KeyCustomMenuItems]),
			ServiceCenterMode:     values[KeyServiceCenterMode],
			ChannelMonitorEnabled: parseBool(values[KeyChannelMonitorEnabled]), AvailableChannelsEnabled: parseBool(values[KeyAvailableChannels]), RiskControlEnabled: parseBool(values[KeyRiskControlEnabled]), CyberSessionBlockEnabled: parseBool(values[KeyCyberSessionBlock]),
			BackupS3Enabled: parseBool(values[KeyBackupS3Enabled]),
			Version:         s.version,
			ServerTimezone:  timezoneName(),
			ServerUTCOffset: formatUTCOffset(offset),
			StorageMode:     s.storageMode,
			DemoMode:        s.demoMode,
		},
		OIDCIssuerURL:       values[KeyOIDCIssuerURL],
		OIDCClientID:        values[KeyOIDCClientID],
		FeishuAppID:         values[KeyFeishuAppID],
		FeishuConfigured:    strings.TrimSpace(values[KeyFeishuAppSecret]) != "",
		GitHubOAuthClientID: values[KeyGitHubOAuthClientID], GitHubOAuthConfigured: strings.TrimSpace(values[KeyGitHubOAuthSecret]) != "", GoogleOAuthClientID: values[KeyGoogleOAuthClientID], GoogleOAuthConfigured: strings.TrimSpace(values[KeyGoogleOAuthSecret]) != "",
		DingTalkClientID: values[KeyDingTalkClientID], DingTalkConfigured: strings.TrimSpace(values[KeyDingTalkClientSecret]) != "",
		AllowedEmailDomains: parseStringList(values[KeyAllowedEmailDomains], []string{}),
		InvitationCodes:     parseStringList(values[KeyInvitationCodes], []string{}),
		TrustedProxyHeaders: parseBool(values[KeyTrustedProxyHeaders]),
		TurnstileConfigured: strings.TrimSpace(values[KeyTurnstileSecretKey]) != "",
		DefaultBalanceCents: parseInt(values[KeyDefaultBalanceCents], 0),
		DefaultConcurrency:  parseInt(values[KeyDefaultConcurrency], 5),
		DefaultRPM:          parseInt(values[KeyDefaultRPM], 0),
		AuthSourceDefaults:  parseAuthSourceDefaults(values[KeyAuthSourceDefaults]),
		SMTPHost:            values[KeySMTPHost], SMTPPort: parseInt(values[KeySMTPPort], 587), SMTPUsername: values[KeySMTPUsername], SMTPFrom: values[KeySMTPFrom], SMTPConfigured: strings.TrimSpace(values[KeySMTPPassword]) != "",
		EmailTemplates:      parseEmailTemplates(values[KeyEmailTemplates]),
		LoginAgreementTitle: values[KeyLoginAgreementTitle], LoginAgreementContent: values[KeyLoginAgreementContent],
		DefaultPageSize: parseInt(values[KeyDefaultPageSize], 20), PageSizeOptions: parseIntList(values[KeyPageSizeOptions], []int{10, 20, 50}), HomeContent: values[KeyHomeContent], HideImportButton: parseBool(values[KeyHideImportButton]),
		ChannelMonitorIntervalSeconds: parseInt(values[KeyChannelMonitorInterval], 300), CyberSessionBlockTTLSeconds: parseInt(values[KeyCyberSessionBlockTTL], 3600),
		BackupS3Endpoint: values[KeyBackupS3Endpoint], BackupS3Region: values[KeyBackupS3Region], BackupS3Bucket: values[KeyBackupS3Bucket], BackupS3Prefix: values[KeyBackupS3Prefix], BackupS3AccessKey: values[KeyBackupS3AccessKey], BackupS3Configured: strings.TrimSpace(values[KeyBackupS3SecretKey]) != "", BackupS3PathStyle: parseBool(values[KeyBackupS3PathStyle]), BackupRetentionDays: parseInt(values[KeyBackupRetentionDays], 30), BackupMaxRetained: parseInt(values[KeyBackupMaxRetained], 10), BackupScheduleEnabled: parseBool(values[KeyBackupScheduleEnabled]), BackupIntervalHours: parseInt(values[KeyBackupIntervalHours], 24),
		DataRetentionDays: parseInt(values[KeyDataRetentionDays], 30),
		PromptLoggingMode: values[KeyPromptLoggingMode],
		UpdateChannel:     values[KeyUpdateChannel],
	}
}

type BackupS3Config struct {
	Enabled, PathStyle               bool
	Endpoint, Region, Bucket, Prefix string
	AccessKey, SecretKey             string
	RetentionDays, MaxRetained       int
}

func (s *Service) BackupS3Config(ctx context.Context) (BackupS3Config, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return BackupS3Config{}, err
	}
	return BackupS3Config{Enabled: parseBool(values[KeyBackupS3Enabled]), PathStyle: parseBool(values[KeyBackupS3PathStyle]), Endpoint: values[KeyBackupS3Endpoint], Region: values[KeyBackupS3Region], Bucket: values[KeyBackupS3Bucket], Prefix: values[KeyBackupS3Prefix], AccessKey: values[KeyBackupS3AccessKey], SecretKey: values[KeyBackupS3SecretKey], RetentionDays: parseInt(values[KeyBackupRetentionDays], 30), MaxRetained: parseInt(values[KeyBackupMaxRetained], 10)}, nil
}

func defaults() map[string]string {
	return map[string]string{
		KeySiteName:                 "AsterRouter",
		KeySiteSubtitle:             "AI Gateway Control Plane",
		KeySiteLogo:                 "",
		KeyPublicBaseURL:            "",
		KeyDefaultLocale:            "en-US",
		KeyEnabledLocales:           `["en-US","zh-CN"]`,
		KeyDefaultProfile:           "",
		KeyEnabledProfiles:          "[]",
		KeySetupCompleted:           "false",
		KeyGatewayBasePath:          "/v1",
		KeyOIDCEnabled:              "false",
		KeyOIDCProviderName:         "OIDC",
		KeyOIDCIssuerURL:            "",
		KeyOIDCClientID:             "",
		KeyOIDCRequireVerifiedEmail: "true",
		KeyFeishuEnabled:            "false",
		KeyFeishuRegion:             "cn",
		KeyFeishuAppID:              "",
		KeyFeishuAppSecret:          "",
		KeyGitHubOAuthEnabled:       "false", KeyGitHubOAuthClientID: "", KeyGitHubOAuthSecret: "", KeyGoogleOAuthEnabled: "false", KeyGoogleOAuthClientID: "", KeyGoogleOAuthSecret: "",
		KeyDingTalkEnabled: "false", KeyDingTalkClientID: "", KeyDingTalkClientSecret: "",
		KeyRegistrationEnabled: "false", KeyEmailVerifyEnabled: "false", KeyAllowedEmailDomains: "[]", KeyInvitationRequired: "false", KeyInvitationCodes: "[]", KeyTOTPEnabled: "false", KeyTrustedProxyHeaders: "false", KeyTurnstileEnabled: "false", KeyTurnstileSiteKey: "", KeyTurnstileSecretKey: "", KeyDefaultBalanceCents: "0", KeyDefaultConcurrency: "5", KeyDefaultRPM: "0", KeySMTPHost: "", KeySMTPPort: "587", KeySMTPUsername: "", KeySMTPPassword: "", KeySMTPFrom: "", KeyLoginAgreementEnabled: "false", KeyLoginAgreementTitle: "Terms of Service", KeyLoginAgreementContent: "",
		KeyAuthSourceDefaults: "{}",
		KeyEmailTemplates:     "[]",
		KeyBackendMode:        "false", KeyDefaultPageSize: "20", KeyPageSizeOptions: "[10,20,50]", KeySupportContact: "", KeyDocumentationURL: "", KeyHomeContent: "", KeyHideImportButton: "false", KeyLoginAgreementMode: "modal", KeyLoginAgreementUpdatedAt: "", KeyLegalDocuments: "[]",
		KeyCustomEndpoints: "[]", KeyCustomMenuItems: "[]",
		KeyChannelMonitorEnabled: "true", KeyChannelMonitorInterval: "300", KeyAvailableChannels: "true", KeyRiskControlEnabled: "true", KeyCyberSessionBlock: "true", KeyCyberSessionBlockTTL: "3600",
		KeyBackupS3Enabled: "false", KeyBackupS3Endpoint: "", KeyBackupS3Region: "auto", KeyBackupS3Bucket: "", KeyBackupS3Prefix: "asterrouter", KeyBackupS3AccessKey: "", KeyBackupS3SecretKey: "", KeyBackupS3PathStyle: "false", KeyBackupRetentionDays: "30", KeyBackupMaxRetained: "10", KeyBackupScheduleEnabled: "false", KeyBackupIntervalHours: "24",
		KeyDataRetentionDays: "30",
		KeyPromptLoggingMode: "metadata_only",
		KeyUpdateChannel:     "stable",
		KeyServiceCenterMode: "disabled",
	}
}

func valuesFromAdminSettings(in AdminSettings) (map[string]string, error) {
	if strings.TrimSpace(in.SiteName) == "" {
		return nil, errors.New("site_name is required")
	}
	if !isLocale(in.DefaultLocale) {
		return nil, errors.New("default_locale must be en-US or zh-CN")
	}
	if len(in.EnabledLocales) == 0 {
		return nil, errors.New("enabled_locales must not be empty")
	}
	for _, locale := range in.EnabledLocales {
		if !isLocale(locale) {
			return nil, fmt.Errorf("unsupported locale %q", locale)
		}
	}
	enabledProfiles := normalizeProfiles(in.EnabledProfiles)
	defaultProfile := strings.TrimSpace(in.DefaultProfile)
	if defaultProfile == "" && len(enabledProfiles) > 0 {
		defaultProfile = enabledProfiles[0]
	}
	if defaultProfile != "" && !containsString(enabledProfiles, defaultProfile) {
		return nil, fmt.Errorf("default profile %q is not enabled", defaultProfile)
	}
	if in.GatewayBasePath == "" || !strings.HasPrefix(in.GatewayBasePath, "/") {
		return nil, errors.New("gateway_base_path must start with /")
	}
	if in.DataRetentionDays < 1 || in.DataRetentionDays > 3650 {
		return nil, errors.New("data_retention_days must be between 1 and 3650")
	}
	if !oneOf(in.PromptLoggingMode, "disabled", "metadata_only", "full") {
		return nil, errors.New("prompt_logging_mode must be disabled, metadata_only, or full")
	}
	if !oneOf(in.UpdateChannel, "stable", "beta", "manual") {
		return nil, errors.New("update_channel must be stable, beta, or manual")
	}
	if !oneOf(in.ServiceCenterMode, "disabled", "online", "private_mirror", "offline") {
		return nil, errors.New("service_center_mode must be disabled, online, private_mirror, or offline")
	}
	if !oneOf(strings.TrimSpace(in.FeishuRegion), "cn", "global") {
		return nil, errors.New("feishu_region must be cn or global")
	}
	if in.FeishuEnabled && strings.TrimSpace(in.FeishuAppID) == "" {
		return nil, errors.New("feishu_app_id is required when feishu login is enabled")
	}
	if in.GitHubOAuthEnabled && strings.TrimSpace(in.GitHubOAuthClientID) == "" {
		return nil, errors.New("github_oauth_client_id is required")
	}
	if in.GitHubOAuthEnabled && !in.GitHubOAuthConfigured && strings.TrimSpace(in.GitHubOAuthClientSecret) == "" {
		return nil, errors.New("github_oauth_client_secret is required")
	}
	if in.GoogleOAuthEnabled && strings.TrimSpace(in.GoogleOAuthClientID) == "" {
		return nil, errors.New("google_oauth_client_id is required")
	}
	if in.GoogleOAuthEnabled && !in.GoogleOAuthConfigured && strings.TrimSpace(in.GoogleOAuthClientSecret) == "" {
		return nil, errors.New("google_oauth_client_secret is required")
	}
	if in.DingTalkEnabled && strings.TrimSpace(in.DingTalkClientID) == "" {
		return nil, errors.New("dingtalk_client_id is required")
	}
	if in.DefaultBalanceCents < 0 || in.DefaultConcurrency < 0 || in.DefaultRPM < 0 {
		return nil, errors.New("default user limits cannot be negative")
	}
	if err := validateAuthSourceDefaults(in.AuthSourceDefaults); err != nil {
		return nil, err
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return nil, errors.New("smtp_port must be between 1 and 65535")
	}
	if in.DefaultPageSize < 5 || in.DefaultPageSize > 1000 {
		return nil, errors.New("default_page_size must be between 5 and 1000")
	}
	if len(in.PageSizeOptions) == 0 {
		return nil, errors.New("page_size_options must not be empty")
	}
	pageSizeSeen := make(map[int]struct{}, len(in.PageSizeOptions))
	for _, size := range in.PageSizeOptions {
		if size < 5 || size > 1000 {
			return nil, errors.New("page_size_options must be between 5 and 1000")
		}
		if _, exists := pageSizeSeen[size]; exists {
			return nil, errors.New("page_size_options must not contain duplicates")
		}
		pageSizeSeen[size] = struct{}{}
	}
	if _, exists := pageSizeSeen[in.DefaultPageSize]; !exists {
		return nil, errors.New("default_page_size must be included in page_size_options")
	}
	if err := validateOptionalHTTPURL("public_base_url", in.PublicBaseURL); err != nil {
		return nil, err
	}
	if err := validateOptionalHTTPURL("documentation_url", in.DocumentationURL); err != nil {
		return nil, err
	}
	if !oneOf(in.LoginAgreementMode, "modal", "checkbox") {
		return nil, errors.New("login_agreement_mode must be modal or checkbox")
	}
	if err := validateLegalDocuments(in.LegalDocuments, in.LoginAgreementEnabled); err != nil {
		return nil, err
	}
	if err := validateSiteLogo(in.SiteLogo); err != nil {
		return nil, err
	}
	if err := validateCustomNavigation(in.CustomEndpoints, in.CustomMenuItems); err != nil {
		return nil, err
	}
	if in.ChannelMonitorIntervalSeconds < 30 || in.ChannelMonitorIntervalSeconds > 86400 {
		return nil, errors.New("channel_monitor_interval_seconds must be between 30 and 86400")
	}
	if in.CyberSessionBlockTTLSeconds < 60 || in.CyberSessionBlockTTLSeconds > 2592000 {
		return nil, errors.New("cyber_session_block_ttl_seconds must be between 60 and 2592000")
	}
	if in.BackupS3Enabled && (strings.TrimSpace(in.BackupS3Bucket) == "" || strings.TrimSpace(in.BackupS3AccessKey) == "" || (!in.BackupS3Configured && strings.TrimSpace(in.BackupS3SecretKey) == "")) {
		return nil, errors.New("S3 bucket, access key, and secret key are required when S3 backup is enabled")
	}
	if err := validateOptionalHTTPURL("backup_s3_endpoint", in.BackupS3Endpoint); err != nil {
		return nil, err
	}
	if in.BackupRetentionDays < 1 || in.BackupRetentionDays > 3650 || in.BackupMaxRetained < 1 || in.BackupMaxRetained > 1000 {
		return nil, errors.New("backup retention settings are out of range")
	}
	if in.BackupIntervalHours < 1 || in.BackupIntervalHours > 24*30 {
		return nil, errors.New("backup interval must be between 1 and 720 hours")
	}
	for _, domain := range in.AllowedEmailDomains {
		if strings.TrimSpace(domain) == "" || strings.Contains(domain, "@") {
			return nil, errors.New("allowed_email_domains must contain domain names")
		}
	}
	locales, _ := json.Marshal(in.EnabledLocales)
	profiles, _ := json.Marshal(enabledProfiles)
	domains, _ := json.Marshal(in.AllowedEmailDomains)
	invitationCodes, _ := json.Marshal(in.InvitationCodes)
	pageSizes, _ := json.Marshal(in.PageSizeOptions)
	legalDocuments, _ := json.Marshal(in.LegalDocuments)
	customEndpoints, _ := json.Marshal(in.CustomEndpoints)
	customMenuItems, _ := json.Marshal(in.CustomMenuItems)
	emailTemplates, err := validateAndMarshalEmailTemplates(in.EmailTemplates)
	if err != nil {
		return nil, err
	}
	authSourceDefaults, _ := json.Marshal(in.AuthSourceDefaults)
	return map[string]string{
		KeySiteName:                 strings.TrimSpace(in.SiteName),
		KeySiteSubtitle:             strings.TrimSpace(in.SiteSubtitle),
		KeySiteLogo:                 strings.TrimSpace(in.SiteLogo),
		KeyPublicBaseURL:            strings.TrimSpace(in.PublicBaseURL),
		KeyDefaultLocale:            in.DefaultLocale,
		KeyEnabledLocales:           string(locales),
		KeyDefaultProfile:           defaultProfile,
		KeyEnabledProfiles:          string(profiles),
		KeySetupCompleted:           strconv.FormatBool(in.SetupCompleted),
		KeyGatewayBasePath:          in.GatewayBasePath,
		KeyOIDCEnabled:              strconv.FormatBool(in.OIDCEnabled),
		KeyOIDCProviderName:         strings.TrimSpace(in.OIDCProviderName),
		KeyOIDCIssuerURL:            strings.TrimSpace(in.OIDCIssuerURL),
		KeyOIDCClientID:             strings.TrimSpace(in.OIDCClientID),
		KeyOIDCRequireVerifiedEmail: strconv.FormatBool(in.OIDCRequireVerifiedEmail),
		KeyFeishuEnabled:            strconv.FormatBool(in.FeishuEnabled),
		KeyFeishuRegion:             strings.TrimSpace(in.FeishuRegion),
		KeyFeishuAppID:              strings.TrimSpace(in.FeishuAppID),
		KeyFeishuAppSecret:          strings.TrimSpace(in.FeishuAppSecret),
		KeyGitHubOAuthEnabled:       strconv.FormatBool(in.GitHubOAuthEnabled), KeyGitHubOAuthClientID: strings.TrimSpace(in.GitHubOAuthClientID), KeyGitHubOAuthSecret: strings.TrimSpace(in.GitHubOAuthClientSecret), KeyGoogleOAuthEnabled: strconv.FormatBool(in.GoogleOAuthEnabled), KeyGoogleOAuthClientID: strings.TrimSpace(in.GoogleOAuthClientID), KeyGoogleOAuthSecret: strings.TrimSpace(in.GoogleOAuthClientSecret),
		KeyDingTalkEnabled: strconv.FormatBool(in.DingTalkEnabled), KeyDingTalkClientID: strings.TrimSpace(in.DingTalkClientID), KeyDingTalkClientSecret: strings.TrimSpace(in.DingTalkClientSecret),
		KeyRegistrationEnabled: strconv.FormatBool(in.RegistrationEnabled), KeyEmailVerifyEnabled: strconv.FormatBool(in.EmailVerifyEnabled), KeyAllowedEmailDomains: string(domains), KeyInvitationRequired: strconv.FormatBool(in.InvitationRequired), KeyInvitationCodes: string(invitationCodes), KeyTOTPEnabled: strconv.FormatBool(in.TOTPEnabled), KeyTrustedProxyHeaders: strconv.FormatBool(in.TrustedProxyHeaders), KeyTurnstileEnabled: strconv.FormatBool(in.TurnstileEnabled), KeyTurnstileSiteKey: strings.TrimSpace(in.TurnstileSiteKey), KeyTurnstileSecretKey: strings.TrimSpace(in.TurnstileSecretKey), KeyDefaultBalanceCents: strconv.Itoa(in.DefaultBalanceCents), KeyDefaultConcurrency: strconv.Itoa(in.DefaultConcurrency), KeyDefaultRPM: strconv.Itoa(in.DefaultRPM), KeySMTPHost: strings.TrimSpace(in.SMTPHost), KeySMTPPort: strconv.Itoa(in.SMTPPort), KeySMTPUsername: strings.TrimSpace(in.SMTPUsername), KeySMTPPassword: strings.TrimSpace(in.SMTPPassword), KeySMTPFrom: strings.TrimSpace(in.SMTPFrom), KeyLoginAgreementEnabled: strconv.FormatBool(in.LoginAgreementEnabled), KeyLoginAgreementTitle: strings.TrimSpace(in.LoginAgreementTitle), KeyLoginAgreementContent: strings.TrimSpace(in.LoginAgreementContent),
		KeyEmailTemplates:     string(emailTemplates),
		KeyAuthSourceDefaults: string(authSourceDefaults),
		KeyBackendMode:        strconv.FormatBool(in.BackendMode), KeyDefaultPageSize: strconv.Itoa(in.DefaultPageSize), KeyPageSizeOptions: string(pageSizes), KeySupportContact: strings.TrimSpace(in.SupportContact), KeyDocumentationURL: strings.TrimSpace(in.DocumentationURL), KeyHomeContent: in.HomeContent, KeyHideImportButton: strconv.FormatBool(in.HideImportButton), KeyLoginAgreementMode: strings.TrimSpace(in.LoginAgreementMode), KeyLoginAgreementUpdatedAt: strings.TrimSpace(in.LoginAgreementUpdatedAt), KeyLegalDocuments: string(legalDocuments),
		KeyCustomEndpoints: string(customEndpoints), KeyCustomMenuItems: string(customMenuItems),
		KeyChannelMonitorEnabled: strconv.FormatBool(in.ChannelMonitorEnabled), KeyChannelMonitorInterval: strconv.Itoa(in.ChannelMonitorIntervalSeconds), KeyAvailableChannels: strconv.FormatBool(in.AvailableChannelsEnabled), KeyRiskControlEnabled: strconv.FormatBool(in.RiskControlEnabled), KeyCyberSessionBlock: strconv.FormatBool(in.CyberSessionBlockEnabled), KeyCyberSessionBlockTTL: strconv.Itoa(in.CyberSessionBlockTTLSeconds),
		KeyBackupS3Enabled: strconv.FormatBool(in.BackupS3Enabled), KeyBackupS3Endpoint: strings.TrimSpace(in.BackupS3Endpoint), KeyBackupS3Region: strings.TrimSpace(in.BackupS3Region), KeyBackupS3Bucket: strings.TrimSpace(in.BackupS3Bucket), KeyBackupS3Prefix: strings.Trim(strings.TrimSpace(in.BackupS3Prefix), "/"), KeyBackupS3AccessKey: strings.TrimSpace(in.BackupS3AccessKey), KeyBackupS3SecretKey: strings.TrimSpace(in.BackupS3SecretKey), KeyBackupS3PathStyle: strconv.FormatBool(in.BackupS3PathStyle), KeyBackupRetentionDays: strconv.Itoa(in.BackupRetentionDays), KeyBackupMaxRetained: strconv.Itoa(in.BackupMaxRetained), KeyBackupScheduleEnabled: strconv.FormatBool(in.BackupScheduleEnabled), KeyBackupIntervalHours: strconv.Itoa(in.BackupIntervalHours),
		KeyDataRetentionDays: strconv.Itoa(in.DataRetentionDays),
		KeyPromptLoggingMode: in.PromptLoggingMode,
		KeyUpdateChannel:     in.UpdateChannel,
		KeyServiceCenterMode: in.ServiceCenterMode,
	}, nil
}

func parseBool(value string) bool {
	return strings.EqualFold(value, "true") || value == "1"
}

func parseInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func parseStringList(value string, fallback []string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil || len(out) == 0 {
		return fallback
	}
	return out
}

func parseIntList(value string, fallback []int) []int {
	var out []int
	if err := json.Unmarshal([]byte(value), &out); err != nil || len(out) == 0 {
		return fallback
	}
	return out
}

func parseLegalDocuments(value string) []LegalDocument {
	var out []LegalDocument
	if err := json.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return []LegalDocument{}
	}
	return out
}

func parseEmailTemplates(value string) []EmailTemplate {
	var templates []EmailTemplate
	if json.Unmarshal([]byte(value), &templates) != nil || templates == nil {
		return []EmailTemplate{}
	}
	return templates
}

func parseCustomEndpoints(value string) []CustomEndpoint {
	var out []CustomEndpoint
	if json.Unmarshal([]byte(value), &out) != nil || out == nil {
		return []CustomEndpoint{}
	}
	return out
}
func parseCustomMenuItems(value string) []CustomMenuItem {
	var out []CustomMenuItem
	if json.Unmarshal([]byte(value), &out) != nil || out == nil {
		return []CustomMenuItem{}
	}
	return out
}

func parseAuthSourceDefaults(value string) map[string]AuthSourceDefault {
	out := map[string]AuthSourceDefault{}
	if json.Unmarshal([]byte(value), &out) != nil {
		return map[string]AuthSourceDefault{}
	}
	return out
}

func validateAuthSourceDefaults(values map[string]AuthSourceDefault) error {
	allowed := map[string]bool{"local": true, "oidc": true, "feishu": true, "dingtalk": true, "github": true, "google": true}
	for source, value := range values {
		if !allowed[source] {
			return fmt.Errorf("unsupported auth source default %q", source)
		}
		if value.BalanceCents < 0 || value.Concurrency < 0 || value.RPM < 0 {
			return fmt.Errorf("auth source default %q cannot be negative", source)
		}
	}
	return nil
}

func validateSiteLogo(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || (!strings.HasPrefix(parts[0], "data:image/png;base64") && !strings.HasPrefix(parts[0], "data:image/jpeg;base64") && !strings.HasPrefix(parts[0], "data:image/webp;base64")) {
		return errors.New("site_logo must be a PNG, JPEG, or WebP data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(raw) > 1024*1024 {
		return errors.New("site_logo must be valid and no larger than 1 MiB")
	}
	return nil
}

func validateCustomNavigation(endpoints []CustomEndpoint, items []CustomMenuItem) error {
	names := map[string]bool{}
	for _, endpoint := range endpoints {
		name := strings.TrimSpace(endpoint.Name)
		if name == "" || strings.TrimSpace(endpoint.Endpoint) == "" {
			return errors.New("custom endpoints require name and endpoint")
		}
		if names[name] {
			return errors.New("custom endpoint names must be unique")
		}
		names[name] = true
	}
	ids := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.URL) == "" {
			return errors.New("custom menu items require id, label, and URL")
		}
		if ids[item.ID] {
			return errors.New("custom menu item ids must be unique")
		}
		ids[item.ID] = true
		if !strings.HasPrefix(item.URL, "/") {
			if err := validateOptionalHTTPURL("custom menu URL", item.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAndMarshalEmailTemplates(templates []EmailTemplate) ([]byte, error) {
	allowedEvents := map[string]bool{"email_verification": true, "password_reset": true, "balance_low": true, "quota_limit": true, "subscription_expiry": true}
	seen := map[string]bool{}
	for _, item := range templates {
		key := item.Event + ":" + item.Locale
		if !allowedEvents[item.Event] || !isLocale(item.Locale) || strings.TrimSpace(item.Subject) == "" || strings.TrimSpace(item.HTML) == "" {
			return nil, fmt.Errorf("invalid email template %q", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate email template %q", key)
		}
		seen[key] = true
	}
	return json.Marshal(templates)
}

var legalSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateLegalDocuments(documents []LegalDocument, required bool) error {
	if required && len(documents) == 0 {
		return errors.New("legal_documents must not be empty when login agreement is enabled")
	}
	ids := make(map[string]struct{}, len(documents))
	slugs := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		id := strings.TrimSpace(document.ID)
		name := strings.TrimSpace(document.Name)
		slug := strings.TrimSpace(document.Slug)
		if id == "" || name == "" || slug == "" || strings.TrimSpace(document.Content) == "" {
			return errors.New("legal document id, name, slug, and content are required")
		}
		if !legalSlugPattern.MatchString(slug) {
			return fmt.Errorf("legal document slug %q must contain lowercase letters, numbers, and hyphens only", slug)
		}
		if _, exists := ids[id]; exists {
			return fmt.Errorf("duplicate legal document id %q", id)
		}
		if _, exists := slugs[slug]; exists {
			return fmt.Errorf("duplicate legal document slug %q", slug)
		}
		ids[id] = struct{}{}
		slugs[slug] = struct{}{}
	}
	return nil
}

func validateOptionalHTTPURL(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be an http or https URL", field)
	}
	return nil
}

func parseProfileList(value string) []string {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err == nil {
		if normalized := normalizeProfiles(out); len(normalized) > 0 {
			return normalized
		}
	}
	return []string{}
}

func isLocale(value string) bool {
	return value == "en-US" || value == "zh-CN"
}

func isProfile(value string) bool {
	return value == "personal" || value == "relay_operator" || value == "enterprise" || value == "platform"
}

func normalizeProfiles(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		profile := strings.TrimSpace(value)
		if !isProfile(profile) || seen[profile] {
			continue
		}
		seen[profile] = true
		out = append(out, profile)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func timezoneName() string {
	name, _ := time.Now().Zone()
	if name == "" {
		return "Local"
	}
	return name
}

func formatUTCOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}
