package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	operatorcore "github.com/astercloud/asterrouter/backend/internal/operator"
	"github.com/astercloud/asterrouter/backend/internal/plugins"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/astercloud/asterrouter/backend/internal/system"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Runtime            RuntimeConfig
	AuthService        *auth.Service
	OIDCService        *auth.OIDCService
	FeishuService      *auth.FeishuService
	GitHubOAuthService *auth.SocialOAuthService
	GoogleOAuthService *auth.SocialOAuthService
	DingTalkService    *auth.DingTalkService
	SettingsService    *settings.Service
	ControlService     *controlplane.Service
	OperatorService    *operatorcore.Service
	PluginService      *plugins.Service
	SystemService      *system.Service
	ExportJobStore     CSVExportJobStore
	DurableAIJobs      DurableAIJobAdmission
	AIJobRuntime       AIJobRuntimeStatusProvider
	authBindingStore   *authBindingStore
}

type RuntimeConfig struct {
	AdminToken  string
	DemoMode    bool
	FrontendDir string
}

type AIJobRuntimeStatusProvider interface {
	Status() controlplane.DurableAIJobRuntimeStatus
}

func New(opts Options) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	authLimiter := newAuthAttemptLimiter(10, 5*time.Minute)
	if opts.authBindingStore == nil {
		opts.authBindingStore = newAuthBindingStore()
	}
	exportJobStore := opts.ExportJobStore
	if exportJobStore == nil {
		exportJobStore = newCSVExportJobStore()
	}
	if opts.ControlService != nil {
		dispatchers := multiAlertDispatcher{}
		if opts.PluginService != nil {
			dispatchers = append(dispatchers, opts.PluginService)
		}
		if opts.SettingsService != nil {
			dispatchers = append(dispatchers, emailAlertDispatcher{control: opts.ControlService, settings: opts.SettingsService})
			opts.ControlService.SetCustomerNotificationDispatcher(customerEmailNotificationDispatcher{settings: opts.SettingsService})
		}
		opts.ControlService.SetAlertDispatcher(dispatchers)
		if opts.AuthService != nil {
			opts.AuthService.SetSessionVersionResolver(func(subject string) (int64, bool) {
				return opts.ControlService.SessionVersion(context.Background(), subject)
			})
		}
	}
	if opts.ControlService != nil && opts.SettingsService != nil {
		if current, err := opts.SettingsService.Admin(context.Background()); err == nil && current.DataRetentionDays > 0 {
			_, _ = opts.ControlService.CleanupRetainedData(context.Background(), "system:startup", retentionCutoff(current.DataRetentionDays))
		}
	}

	r.GET("/health", func(c *gin.Context) {
		httpx.OK(c, gin.H{"status": "ok"})
	})

	r.GET("/ready", func(c *gin.Context) {
		if err := opts.SettingsService.Health(c.Request.Context()); err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
			return
		}
		if opts.ControlService != nil {
			if err := opts.ControlService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if opts.OperatorService != nil {
			if err := opts.OperatorService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if opts.PluginService != nil {
			if err := opts.PluginService.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		if exportJobStore != nil {
			if err := exportJobStore.Health(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
				return
			}
		}
		httpx.OK(c, gin.H{"status": "ready"})
	})

	api := r.Group("/api/v1")
	api.GET("/settings/public", func(c *gin.Context) {
		data, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1002, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.GET("/legal/:slug", func(c *gin.Context) {
		public, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1001, err.Error())
			return
		}
		for _, document := range public.LegalDocuments {
			if document.Slug == c.Param("slug") {
				httpx.OK(c, document)
				return
			}
		}
		httpx.Error(c, http.StatusNotFound, 1404, "legal document not found")
	})
	api.GET("/i18n/locales", func(c *gin.Context) {
		httpx.OK(c, settings.SupportedLocales)
	})
	api.GET("/setup/status", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1003, err.Error())
			return
		}
		httpx.OK(c, gin.H{
			"default_profile":  data.DefaultProfile,
			"enabled_profiles": data.EnabledProfiles,
			"setup_completed":  data.SetupCompleted,
		})
	})
	api.POST("/setup/profiles", func(c *gin.Context) {
		var req struct {
			Profile string `json:"profile"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		profile := strings.TrimSpace(req.Profile)
		data, err := opts.SettingsService.ApplyInitialProfile(c.Request.Context(), profile)
		if err != nil {
			if errors.Is(err, settings.ErrUnsupportedDeploymentProfile) {
				httpx.Error(c, http.StatusBadRequest, 1401, err.Error())
				return
			}
			if !errors.Is(err, settings.ErrDeploymentProfileInitialized) {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1401, "failed to initialize deployment profile")
				return
			}
			data, err = opts.SettingsService.Admin(c.Request.Context())
			if err != nil {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1401, "failed to load deployment profile")
				return
			}
			if !data.SetupCompleted || len(data.EnabledProfiles) != 1 || data.EnabledProfiles[0] != profile || data.DefaultProfile != profile {
				httpx.Error(c, http.StatusBadRequest, 1401, settings.ErrDeploymentProfileInitialized.Error())
				return
			}
		}
		if profile == controlplane.ProfileScopePlatform && opts.ControlService != nil {
			if err := opts.ControlService.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
				_ = c.Error(err)
				httpx.Error(c, http.StatusInternalServerError, 1402, "failed to initialize platform domain")
				return
			}
		}
		httpx.OK(c, data.PublicSettings)
	})
	api.POST("/auth/login", func(c *gin.Context) {
		if !authLimiter.Allow(c.ClientIP(), time.Now().UTC()) {
			httpx.Error(c, http.StatusTooManyRequests, 1429, "too many login attempts")
			return
		}
		if opts.AuthService == nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1300, "auth service is not available")
			return
		}
		var req struct {
			Username          string `json:"username"`
			Password          string `json:"password"`
			TurnstileToken    string `json:"turnstile_token"`
			AgreementAccepted bool   `json:"agreement_accepted"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1301, "invalid login payload")
			return
		}
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, req.AgreementAccepted) {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		security, err := opts.SettingsService.LoginSecurity(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		if security.TurnstileEnabled {
			if err := (auth.TurnstileVerifier{}).Verify(c.Request.Context(), security.TurnstileSecret, req.TurnstileToken, c.ClientIP()); err != nil {
				httpx.Error(c, http.StatusForbidden, 1311, "turnstile verification failed")
				return
			}
		}
		result, err := opts.AuthService.Login(c.Request.Context(), req.Username, req.Password)
		if err == nil && opts.ControlService != nil && opts.AuthService.IsLocalPrincipal(req.Username) {
			if profile, profileErr := opts.ControlService.CurrentAccountProfile(c.Request.Context(), req.Username); profileErr == nil && profile.TOTPEnabled {
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(profile.ID, profile.Role)
				if challengeErr != nil {
					httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
					return
				}
				httpx.OK(c, gin.H{"mfa_required": true, "challenge": challenge, "expires_at": expires})
				return
			}
		}
		if err != nil {
			policy, policyErr := opts.SettingsService.RegistrationPolicy(c.Request.Context())
			if policyErr == nil && opts.ControlService != nil {
				if user, userErr := opts.ControlService.AuthenticateWorkspaceUser(c.Request.Context(), req.Username, req.Password, policy.EmailVerification); userErr == nil {
					if user.TOTPEnabled {
						challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
						if challengeErr != nil {
							httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
							return
						}
						httpx.OK(c, gin.H{"mfa_required": true, "challenge": challenge, "expires_at": expires})
						return
					}
					result, err = opts.AuthService.LoginOIDC(user.ID, user.Role)
				}
			}
		}
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				httpx.Error(c, http.StatusUnauthorized, 1302, "invalid username or password")
				return
			}
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		authLimiter.Reset(c.ClientIP())
		httpx.OK(c, enrichLoginResult(c.Request.Context(), opts.ControlService, result))
	})
	api.POST("/auth/register", func(c *gin.Context) {
		policy, err := opts.SettingsService.RegistrationPolicy(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1303, err.Error())
			return
		}
		if !policy.Enabled {
			httpx.Error(c, http.StatusForbidden, 1320, "registration is disabled")
			return
		}
		var req struct {
			Email             string `json:"email"`
			Password          string `json:"password"`
			DisplayName       string `json:"display_name"`
			InvitationCode    string `json:"invitation_code"`
			AgreementAccepted bool   `json:"agreement_accepted"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, req.AgreementAccepted) {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		domain := ""
		if at := strings.LastIndex(req.Email, "@"); at >= 0 {
			domain = strings.ToLower(req.Email[at+1:])
		}
		if len(policy.AllowedDomains) > 0 {
			if !emailDomainAllowed(policy.AllowedDomains, domain) {
				httpx.Error(c, http.StatusForbidden, 1321, "email domain is not allowed")
				return
			}
		}
		if policy.InvitationRequired {
			if len(req.Password) < 10 {
				httpx.Error(c, http.StatusBadRequest, 1322, "password must contain at least 10 characters")
				return
			}
			if err := opts.SettingsService.ConsumeInvitationCode(c.Request.Context(), req.InvitationCode); err != nil {
				httpx.Error(c, http.StatusForbidden, 1326, err.Error())
				return
			}
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "local")
		user, token, err := opts.ControlService.RegisterWorkspaceUser(c.Request.Context(), req.Email, req.Password, req.DisplayName, policy.EmailVerification, defaults)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1322, err.Error())
			return
		}
		if policy.EmailVerification && !opts.Runtime.DemoMode {
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?verify=" + url.QueryEscape(token)
			if mailErr := sendConfiguredEmail(c.Request.Context(), opts.SettingsService, "email_verification", user.Email, user.DisplayName, verifyURL); mailErr != nil {
				httpx.Error(c, http.StatusBadGateway, 1324, "verification email could not be sent")
				return
			}
		}
		data := gin.H{"user_id": user.ID, "verification_required": policy.EmailVerification}
		if policy.EmailVerification && opts.Runtime.DemoMode {
			data["verification_token"] = token
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/verify-email", func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.VerifyWorkspaceUserEmail(c.Request.Context(), req.Token); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1323, err.Error())
			return
		}
		httpx.OK(c, gin.H{"verified": true})
	})
	api.POST("/auth/resend-verification", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		user, token, err := opts.ControlService.RenewEmailVerification(c.Request.Context(), req.Email)
		if err == nil {
			public, _ := opts.SettingsService.Public(c.Request.Context())
			verifyURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?verify=" + url.QueryEscape(token)
			_ = sendConfiguredEmail(c.Request.Context(), opts.SettingsService, "email_verification", user.Email, user.DisplayName, verifyURL)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/forgot-password", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		user, token, err := opts.ControlService.BeginPasswordReset(c.Request.Context(), req.Email)
		if err == nil {
			public, _ := opts.SettingsService.Public(c.Request.Context())
			resetURL := strings.TrimRight(public.PublicBaseURL, "/") + "/login?reset=" + url.QueryEscape(token)
			_ = sendConfiguredEmail(c.Request.Context(), opts.SettingsService, "password_reset", user.Email, user.DisplayName, resetURL)
		}
		httpx.OK(c, gin.H{"accepted": true})
	})
	api.POST("/auth/reset-password", func(c *gin.Context) {
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.CompletePasswordReset(c.Request.Context(), req.Token, req.Password); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1325, err.Error())
			return
		}
		httpx.OK(c, gin.H{"reset": true})
	})
	api.POST("/auth/logout", func(c *gin.Context) {
		if opts.AuthService != nil && opts.ControlService != nil {
			provided := bearerToken(c)
			if provided == "" {
				provided, _ = c.Cookie("asterrouter_session")
			}
			if principal, ok := opts.AuthService.Verify(provided); ok {
				_ = opts.ControlService.RevokeAccountSessions(c.Request.Context(), principal.Subject)
			}
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", "", -1, "/", "", true, true)
		httpx.OK(c, gin.H{"logged_out": true})
	})
	api.GET("/auth/oidc", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.OIDCService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "oidc is not configured")
			return
		}
		entry, err := opts.OIDCService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1304, err.Error())
			return
		}
		c.Redirect(http.StatusFound, opts.OIDCService.AuthorizationURL(entry))
	})
	api.GET("/auth/feishu", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.FeishuService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "feishu login is not configured")
			return
		}
		entry, err := opts.FeishuService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
			return
		}
		c.Redirect(http.StatusFound, opts.FeishuService.AuthorizationURL(entry.Value, auth.PKCEChallenge(entry.Verifier)))
	})
	api.GET("/auth/dingtalk", func(c *gin.Context) {
		if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
			httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
			return
		}
		if opts.DingTalkService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "DingTalk login is not configured")
			return
		}
		entry, err := opts.DingTalkService.Begin(time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
			return
		}
		c.Redirect(http.StatusFound, opts.DingTalkService.AuthorizationURL(entry))
	})
	api.GET("/auth/oidc/callback", func(c *gin.Context) {
		if opts.OIDCService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "oidc is not configured")
			return
		}
		profile, err := opts.OIDCService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1305, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "oidc", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, opts.OIDCService.IssuerURL(), profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "oidc", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "oidc")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), opts.OIDCService.IssuerURL(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		c.Redirect(http.StatusFound, "/login?oidc=success")
	})
	api.GET("/auth/feishu/callback", func(c *gin.Context) {
		if opts.FeishuService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "feishu login is not configured")
			return
		}
		entry, err := opts.FeishuService.Consume(c.Query("state"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1309, err.Error())
			return
		}
		profile, err := opts.FeishuService.Complete(c.Request.Context(), c.Query("code"), entry.Verifier)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1310, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "feishu", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "feishu", ""))
			return
		}
		adminSettings, settingsErr := opts.SettingsService.Admin(c.Request.Context())
		if settingsErr != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "feishu")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "feishu:"+opts.FeishuService.Region(), profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, err := opts.AuthService.BeginMFA(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, err.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		c.Redirect(http.StatusFound, "/login?provider=feishu")
	})
	api.GET("/auth/dingtalk/callback", func(c *gin.Context) {
		if opts.DingTalkService == nil || opts.AuthService == nil || opts.ControlService == nil {
			httpx.Error(c, http.StatusNotFound, 1404, "DingTalk login is not configured")
			return
		}
		profile, err := opts.DingTalkService.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1310, err.Error())
			return
		}
		if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), "dingtalk", time.Now().UTC()); binding {
			if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, "dingtalk", profile.Subject, profile.Email); err != nil {
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
				return
			}
			c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", "dingtalk", ""))
			return
		}
		adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
			return
		}
		defaults := workspaceUserDefaults(adminSettings, "dingtalk")
		user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), "dingtalk", profile.Subject, profile.Email, profile.DisplayName, profile.Department, defaults)
		if err != nil {
			httpx.Error(c, http.StatusForbidden, 1306, err.Error())
			return
		}
		if user.TOTPEnabled {
			challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
			if challengeErr != nil {
				httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
				return
			}
			c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
			return
		}
		result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		c.Redirect(http.StatusFound, "/login?provider=dingtalk")
	})
	for _, social := range []*auth.SocialOAuthService{opts.GitHubOAuthService, opts.GoogleOAuthService} {
		if social == nil {
			continue
		}
		provider := social.Provider()
		api.GET("/auth/oauth/"+provider, func(c *gin.Context) {
			if !agreementAccepted(c.Request.Context(), opts.SettingsService, c.Query("agreement_accepted") == "true") {
				httpx.Error(c, http.StatusForbidden, 1328, "login agreement must be accepted")
				return
			}
			entry, err := social.Begin(time.Now().UTC())
			if err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1308, err.Error())
				return
			}
			c.Redirect(http.StatusFound, social.AuthorizationURL(entry))
		})
		api.GET("/auth/oauth/"+provider+"/callback", func(c *gin.Context) {
			profile, err := social.Complete(c.Request.Context(), c.Query("state"), c.Query("code"), time.Now().UTC())
			if err != nil {
				httpx.Error(c, http.StatusUnauthorized, 1305, err.Error())
				return
			}
			if transaction, binding := opts.authBindingStore.Consume(c.Query("state"), provider, time.Now().UTC()); binding {
				if err := opts.ControlService.BindCurrentAuthIdentity(c.Request.Context(), transaction.UserID, provider, profile.Subject, profile.Email); err != nil {
					c.Redirect(http.StatusFound, authBindingRedirect(transaction, "error", "", err.Error()))
					return
				}
				c.Redirect(http.StatusFound, authBindingRedirect(transaction, "success", provider, ""))
				return
			}
			if err := authorizeSocialProvision(c.Request.Context(), opts.SettingsService, opts.ControlService, provider, profile.Subject, profile.Email); err != nil {
				httpx.Error(c, http.StatusForbidden, 1329, err.Error())
				return
			}
			adminSettings, err := opts.SettingsService.Admin(c.Request.Context())
			if err != nil {
				httpx.Error(c, http.StatusServiceUnavailable, 1327, "user defaults are unavailable")
				return
			}
			defaults := workspaceUserDefaults(adminSettings, provider)
			user, err := opts.ControlService.ProvisionOIDCUser(c.Request.Context(), provider, profile.Subject, profile.Email, profile.DisplayName, "", defaults)
			if err != nil {
				httpx.Error(c, http.StatusForbidden, 1306, err.Error())
				return
			}
			if user.TOTPEnabled {
				challenge, expires, challengeErr := opts.AuthService.BeginMFA(user.ID, user.Role)
				if challengeErr != nil {
					httpx.Error(c, http.StatusInternalServerError, 1315, challengeErr.Error())
					return
				}
				c.Redirect(http.StatusFound, "/login?mfa="+url.QueryEscape(challenge)+"&expires="+url.QueryEscape(expires.Format(time.RFC3339)))
				return
			}
			result, err := opts.AuthService.LoginOIDC(user.ID, user.Role)
			if err != nil {
				httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
				return
			}
			c.SetSameSite(http.SameSiteLaxMode)
			c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
			c.Redirect(http.StatusFound, "/login?oauth="+url.QueryEscape(provider)+"&status=success")
		})
	}
	api.POST("/auth/totp/login", func(c *gin.Context) {
		var req struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		userID, role, ok := opts.AuthService.ConsumeMFA(req.Challenge)
		if !ok {
			httpx.Error(c, http.StatusUnauthorized, 1316, "MFA challenge is invalid or expired")
			return
		}
		if _, err := opts.ControlService.VerifyUserTOTP(c.Request.Context(), userID, req.Code); err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1317, "invalid TOTP code")
			return
		}
		result, err := opts.AuthService.LoginOIDC(userID, role)
		if err != nil {
			httpx.Error(c, http.StatusUnauthorized, 1307, err.Error())
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("asterrouter_session", result.AccessToken, int(time.Until(result.ExpiresAt).Seconds()), "/", "", true, true)
		httpx.OK(c, enrichLoginResult(c.Request.Context(), opts.ControlService, result))
	})

	r.GET("/api/iam/get-captcha-code", func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"captchaOnOff": false,
			"img":          "",
			"uuid":         "",
		})
	})
	api.GET("/auth/me", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		httpx.OK(c, currentAuthUser(c, opts))
	})
	registerAccountRoutes(api, opts)
	api.POST("/auth/totp/setup", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		data, err := opts.ControlService.BeginTOTPSetup(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1312, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	api.POST("/auth/totp/confirm", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		codes, err := opts.ControlService.ConfirmTOTPWithRecoveryCodes(c.Request.Context(), actor(c), req.Code)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1313, err.Error())
			return
		}
		response, err := replacementAccountSession(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		enabled := true
		response.Enabled = &enabled
		response.Codes = codes
		httpx.OK(c, response)
	})
	api.POST("/auth/totp/disable", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.DisableTOTP(c.Request.Context(), actor(c), req.Code); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1314, err.Error())
			return
		}
		response, err := replacementAccountSession(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		enabled := false
		response.Enabled = &enabled
		httpx.OK(c, response)
	})
	api.POST("/auth/totp/recovery-codes", requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService), func(c *gin.Context) {
		codes, err := opts.ControlService.GenerateTOTPRecoveryCodes(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1318, err.Error())
			return
		}
		response, err := replacementAccountSession(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		response.Codes = codes
		httpx.OK(c, response)
	})
	registerPluginOpenRoutes(api.Group("/open/plugins"), opts.PluginService, opts.ControlService)
	registerPluginHostRoutes(api.Group("/plugin-host"), opts.PluginService, opts.ControlService)
	systemAPI := api.Group("/system")
	systemAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	systemAPI.Use(requireSystemAdministrator(opts.ControlService))
	systemAPI.GET("/profiles", func(c *gin.Context) {
		current, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, profileBundleResponse(current))
	})
	systemAPI.PUT("/profiles", func(c *gin.Context) {
		var req profileBundleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid profile bundle payload")
			return
		}
		current, err := opts.SettingsService.ApplyProfiles(c.Request.Context(), req.EnabledProfiles, req.DefaultProfile)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		if current.DefaultProfile == controlplane.ProfileScopePlatform && opts.ControlService != nil {
			if err := opts.ControlService.EnsurePlatformBootstrap(c.Request.Context()); err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1404, "failed to initialize platform domain")
				return
			}
		}
		httpx.OK(c, profileBundleResponse(current))
	})

	admin := api.Group("/admin")
	admin.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	admin.Use(requireProfile(opts.SettingsService, "enterprise"))
	admin.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceEnterprise))
	admin.Use(requireRBAC(opts.ControlService))
	registerAdminRoutes(admin, opts.ControlService, exportJobStore, opts.AIJobRuntime)
	registerPluginRoutes(admin.Group("/plugins"), opts.PluginService, opts.ControlService, "enterprise")
	registerSystemRoutes(admin.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	admin.GET("/settings", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	admin.PUT("/settings", func(c *gin.Context) {
		var req settings.AdminSettings
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid settings payload")
			return
		}
		previous, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		if !requireProfileBundleChange(c, opts.ControlService, previous, req) {
			return
		}
		data, err := opts.SettingsService.Update(c.Request.Context(), req)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1403, err.Error())
			return
		}
		data.RuntimeRestartReasons = authenticationRestartReasons(previous, data)
		if strings.TrimSpace(req.FeishuAppSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "feishu_secret")
		}
		if strings.TrimSpace(req.DingTalkClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "dingtalk_secret")
		}
		if strings.TrimSpace(req.GitHubOAuthClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "github_secret")
		}
		if strings.TrimSpace(req.GoogleOAuthClientSecret) != "" {
			data.RuntimeRestartReasons = append(data.RuntimeRestartReasons, "google_secret")
		}
		data.RuntimeRestartRequired = len(data.RuntimeRestartReasons) > 0
		httpx.OK(c, data)
	})
	admin.POST("/settings/retention/cleanup", func(c *gin.Context) {
		data, err := opts.SettingsService.Admin(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1004, err.Error())
			return
		}
		result, err := opts.ControlService.CleanupRetainedData(c.Request.Context(), actor(c), retentionCutoff(data.DataRetentionDays))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1404, err.Error())
			return
		}
		httpx.OK(c, result)
	})
	admin.POST("/settings/smtp/test", func(c *gin.Context) {
		var req struct {
			Recipient string `json:"recipient"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || !strings.Contains(req.Recipient, "@") {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		host, port, username, password, from, err := opts.SettingsService.SMTPConfig(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1501, err.Error())
			return
		}
		mailer := auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}
		if err := mailer.Send(c.Request.Context(), strings.TrimSpace(req.Recipient), "AsterRouter SMTP test", "SMTP configuration is working."); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})
	admin.GET("/settings/email-templates/defaults", func(c *gin.Context) { httpx.OK(c, auth.DefaultEmailTemplates()) })
	admin.POST("/settings/email-templates/preview", func(c *gin.Context) {
		var req struct {
			Subject string `json:"subject"`
			HTML    string `json:"html"`
		}
		if c.ShouldBindJSON(&req) != nil {
			httpx.Error(c, http.StatusBadRequest, 1402, "invalid template payload")
			return
		}
		data := auth.EmailTemplateData{SiteName: "AsterRouter", UserName: "Enterprise User", ActionURL: "https://example.test/action", Amount: "100.00", Limit: "100000", Period: "monthly", Message: "Access expires in 7 days."}
		subject, htmlBody, err := auth.RenderEmailTemplate(req.Subject, req.HTML, data)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		httpx.OK(c, gin.H{"subject": subject, "html": htmlBody})
	})
	admin.POST("/settings/email-templates/test", func(c *gin.Context) {
		var req struct{ Recipient, Subject, HTML string }
		if c.ShouldBindJSON(&req) != nil || !strings.Contains(req.Recipient, "@") {
			httpx.Error(c, http.StatusBadRequest, 1402, "valid recipient is required")
			return
		}
		data := auth.EmailTemplateData{SiteName: "AsterRouter", UserName: "Enterprise User", ActionURL: "https://example.test/action", Amount: "100.00", Limit: "100000", Period: "monthly", Message: "Access expires in 7 days."}
		subject, htmlBody, err := auth.RenderEmailTemplate(req.Subject, req.HTML, data)
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1420, err.Error())
			return
		}
		host, port, username, password, from, err := opts.SettingsService.SMTPConfig(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1501, err.Error())
			return
		}
		if err := (auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}).SendHTML(c.Request.Context(), req.Recipient, subject, htmlBody); err != nil {
			httpx.Error(c, http.StatusBadGateway, 1502, err.Error())
			return
		}
		httpx.OK(c, gin.H{"sent": true})
	})

	portal := api.Group("/portal")
	portal.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	portal.Use(requireProfile(opts.SettingsService, "enterprise"))
	portal.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePortal))
	registerPortalRoutes(portal, opts.ControlService, opts.SettingsService)

	customer := api.Group("/customer")
	customer.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	customer.Use(requireProfile(opts.SettingsService, "relay_operator"))
	customer.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceCustomer))
	registerPortalRoutes(customer, opts.ControlService, opts.SettingsService)
	registerCustomerRoutes(customer, opts.ControlService)

	operatorAPI := api.Group("/operator")
	operatorAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	operatorAPI.Use(requireProfile(opts.SettingsService, "relay_operator"))
	operatorAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfaceRelayOperator))
	registerOperatorRoutes(operatorAPI, opts.OperatorService)
	registerSharedCoreRoutes(operatorAPI, opts.ControlService, false)
	registerSurfaceSettings(operatorAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(operatorAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(operatorAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "relay_operator")

	consoleAPI := api.Group("/console")
	consoleAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	consoleAPI.Use(requireProfile(opts.SettingsService, "personal"))
	consoleAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePersonal))
	registerSharedCoreRoutes(consoleAPI, opts.ControlService, true)
	consoleAPI.GET("/dashboard", func(c *gin.Context) {
		data, err := opts.ControlService.Dashboard(c.Request.Context())
		sharedCoreResponse(c, data, err)
	})
	registerSurfaceSettings(consoleAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(consoleAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerPluginRoutes(consoleAPI.Group("/plugins"), opts.PluginService, opts.ControlService, "personal")

	platformAPI := api.Group("/platform")
	platformAPI.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))
	platformAPI.Use(requireProfile(opts.SettingsService, "platform"))
	platformAPI.Use(requireSurfaceAccess(opts.ControlService, controlplane.SurfacePlatform))
	platformAPI.Use(requireSurfaceRBAC(opts.ControlService, controlplane.SurfacePlatform))
	registerPlatformRoutes(platformAPI, opts.ControlService, opts.PluginService, opts.AIJobRuntime)
	registerSurfaceSettings(platformAPI, opts.SettingsService, opts.ControlService)
	registerSystemRoutes(platformAPI.Group("/system"), opts.SystemService, opts.SettingsService, opts.ControlService)
	registerGatewayRoutes(r, opts.ControlService, opts.DurableAIJobs, opts.PluginService)

	serveSPA(r, opts.Runtime.FrontendDir)
	return r
}

func enrichLoginResult(ctx context.Context, control *controlplane.Service, result auth.LoginResult) auth.LoginResult {
	result.User.AllowedSurfaces = allowedSurfacesForActor(ctx, control, result.User.Username)
	return result
}

type profileBundleRequest struct {
	EnabledProfiles []string `json:"enabled_profiles"`
	DefaultProfile  string   `json:"default_profile"`
}

func profileBundleResponse(current settings.AdminSettings) profileBundleRequest {
	return profileBundleRequest{
		EnabledProfiles: current.EnabledProfiles,
		DefaultProfile:  current.DefaultProfile,
	}
}

func agreementAccepted(ctx context.Context, service *settings.Service, accepted bool) bool {
	public, err := service.Public(ctx)
	return err == nil && (!public.LoginAgreementEnabled || accepted)
}

func authorizeSocialProvision(ctx context.Context, settingsService *settings.Service, control *controlplane.Service, issuer, subject, email string) error {
	exists, err := control.ExternalIdentityExists(ctx, issuer, subject)
	if err != nil || exists {
		return err
	}
	policy, err := settingsService.RegistrationPolicy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return errors.New("registration is disabled for new social login accounts")
	}
	if policy.InvitationRequired {
		return errors.New("an invitation is required before creating a social login account")
	}
	domain := ""
	if at := strings.LastIndex(strings.ToLower(strings.TrimSpace(email)), "@"); at >= 0 {
		domain = strings.TrimSpace(strings.ToLower(email[at+1:]))
	}
	if len(policy.AllowedDomains) > 0 && !emailDomainAllowed(policy.AllowedDomains, domain) {
		return errors.New("email domain is not allowed")
	}
	return nil
}

func emailDomainAllowed(allowedDomains []string, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, value := range allowedDomains {
		candidate := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "*.")
		if candidate != "" && (domain == candidate || strings.HasSuffix(domain, "."+candidate)) {
			return true
		}
	}
	return false
}

func authenticationRestartReasons(previous, current settings.AdminSettings) []string {
	var reasons []string
	if previous.PublicBaseURL != current.PublicBaseURL {
		reasons = append(reasons, "public_base_url")
	}
	if previous.OIDCEnabled != current.OIDCEnabled || previous.OIDCIssuerURL != current.OIDCIssuerURL || previous.OIDCClientID != current.OIDCClientID || previous.OIDCRequireVerifiedEmail != current.OIDCRequireVerifiedEmail {
		reasons = append(reasons, "oidc")
	}
	if previous.FeishuEnabled != current.FeishuEnabled || previous.FeishuRegion != current.FeishuRegion || previous.FeishuAppID != current.FeishuAppID || previous.FeishuConfigured != current.FeishuConfigured {
		reasons = append(reasons, "feishu")
	}
	if previous.DingTalkEnabled != current.DingTalkEnabled || previous.DingTalkClientID != current.DingTalkClientID || previous.DingTalkConfigured != current.DingTalkConfigured {
		reasons = append(reasons, "dingtalk")
	}
	if previous.GitHubOAuthEnabled != current.GitHubOAuthEnabled || previous.GitHubOAuthClientID != current.GitHubOAuthClientID || previous.GitHubOAuthConfigured != current.GitHubOAuthConfigured {
		reasons = append(reasons, "github")
	}
	if previous.GoogleOAuthEnabled != current.GoogleOAuthEnabled || previous.GoogleOAuthClientID != current.GoogleOAuthClientID || previous.GoogleOAuthConfigured != current.GoogleOAuthConfigured {
		reasons = append(reasons, "google")
	}
	return reasons
}

func retentionCutoff(days int) time.Time {
	if days < 1 {
		days = 1
	}
	return time.Now().UTC().AddDate(0, 0, -days)
}

func workspaceUserDefaults(admin settings.AdminSettings, source string) controlplane.WorkspaceUserDefaults {
	result := controlplane.WorkspaceUserDefaults{BalanceCents: admin.DefaultBalanceCents, ConcurrencyLimit: admin.DefaultConcurrency, RPMLimit: admin.DefaultRPM}
	if override, ok := admin.AuthSourceDefaults[source]; ok && override.Enabled {
		result = controlplane.WorkspaceUserDefaults{BalanceCents: override.BalanceCents, ConcurrencyLimit: override.Concurrency, RPMLimit: override.RPM}
	}
	return result
}

func sendConfiguredEmail(ctx context.Context, service *settings.Service, event, recipient, userName, actionURL string) error {
	return sendConfiguredEmailData(ctx, service, event, recipient, auth.EmailTemplateData{UserName: userName, ActionURL: actionURL})
}

func sendConfiguredEmailData(ctx context.Context, service *settings.Service, event, recipient string, data auth.EmailTemplateData) error {
	admin, err := service.Admin(ctx)
	if err != nil {
		return err
	}
	locale := admin.DefaultLocale
	var subject, htmlBody string
	for _, item := range admin.EmailTemplates {
		if item.Event == event && item.Locale == locale {
			subject, htmlBody = item.Subject, item.HTML
			break
		}
	}
	if subject == "" {
		for _, item := range auth.DefaultEmailTemplates() {
			if item.Event == event && item.Locale == locale {
				subject, htmlBody = item.Subject, item.HTML
				break
			}
		}
	}
	if subject == "" {
		return errors.New("email template is not configured")
	}
	data.SiteName = admin.SiteName
	subject, htmlBody, err = auth.RenderEmailTemplate(subject, htmlBody, data)
	if err != nil {
		return err
	}
	host, port, username, password, from, err := service.SMTPConfig(ctx)
	if err != nil {
		return err
	}
	return (auth.SMTPMailer{Config: auth.SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}}).SendHTML(ctx, recipient, subject, htmlBody)
}
