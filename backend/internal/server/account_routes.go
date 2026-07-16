package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/astercloud/asterrouter/backend/internal/settings"
	"github.com/gin-gonic/gin"
)

type accountLoginMethod struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Bound     bool   `json:"bound"`
	Detail    string `json:"detail,omitempty"`
}

type accountProfileResponse struct {
	controlplane.AccountProfile
	LoginMethods  []accountLoginMethod `json:"login_methods"`
	TOTPAvailable bool                 `json:"totp_available"`
}

type accountSecurityResponse struct {
	AccessToken string    `json:"access_token,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Changed     bool      `json:"changed,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	Codes       []string  `json:"codes,omitempty"`
}

func registerAccountRoutes(api *gin.RouterGroup, opts Options) {
	account := api.Group("/account")
	account.Use(requireAdminAuth(opts.Runtime.AdminToken, opts.AuthService))

	account.GET("/profile", func(c *gin.Context) {
		data, err := currentAccountProfile(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	account.PUT("/profile", func(c *gin.Context) {
		var req controlplane.AccountProfileUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if _, err := opts.ControlService.UpdateCurrentAccountProfile(c.Request.Context(), actor(c), req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1331, err.Error())
			return
		}
		data, err := currentAccountProfile(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	account.PUT("/password", func(c *gin.Context) {
		var req controlplane.AccountPasswordUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		if err := opts.ControlService.ChangeCurrentAccountPassword(c.Request.Context(), actor(c), req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1332, err.Error())
			return
		}
		if opts.AuthService != nil && opts.AuthService.IsLocalPrincipal(actor(c)) {
			passwordHash, err := opts.ControlService.CurrentAccountPasswordHash(c.Request.Context(), actor(c))
			if err != nil {
				httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
				return
			}
			opts.AuthService.SetPasswordHash(passwordHash)
		}
		response, err := replacementAccountSession(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		response.Changed = true
		httpx.OK(c, response)
	})
	account.DELETE("/identities/:provider", func(c *gin.Context) {
		if err := opts.ControlService.UnbindCurrentAuthIdentity(c.Request.Context(), actor(c), c.Param("provider")); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1335, err.Error())
			return
		}
		data, err := currentAccountProfile(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	account.POST("/identities/:provider/bind", func(c *gin.Context) {
		var req struct {
			ReturnPath string `json:"return_path"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1400, "invalid request")
			return
		}
		provider := strings.ToLower(strings.TrimSpace(c.Param("provider")))
		now := time.Now().UTC()
		var state auth.OIDCState
		var authorizationURL string
		var err error
		switch provider {
		case "oidc":
			if opts.OIDCService == nil {
				err = errors.New("oidc is not configured")
				break
			}
			state, err = opts.OIDCService.Begin(now)
			if err == nil {
				authorizationURL = opts.OIDCService.AuthorizationURL(state)
			}
		case "feishu":
			if opts.FeishuService == nil {
				err = errors.New("feishu is not configured")
				break
			}
			state, err = opts.FeishuService.Begin(now)
			if err == nil {
				authorizationURL = opts.FeishuService.AuthorizationURL(state.Value, auth.PKCEChallenge(state.Verifier))
			}
		case "dingtalk":
			if opts.DingTalkService == nil {
				err = errors.New("DingTalk is not configured")
				break
			}
			state, err = opts.DingTalkService.Begin(now)
			if err == nil {
				authorizationURL = opts.DingTalkService.AuthorizationURL(state)
			}
		case "github", "google":
			social := opts.GitHubOAuthService
			if provider == "google" {
				social = opts.GoogleOAuthService
			}
			if social == nil {
				err = errors.New(provider + " is not configured")
				break
			}
			state, err = social.Begin(now)
			if err == nil {
				authorizationURL = social.AuthorizationURL(state)
			}
		default:
			err = errors.New("unsupported authentication provider")
		}
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1336, err.Error())
			return
		}
		if err := opts.authBindingStore.Save(state.Value, actor(c), provider, req.ReturnPath, now); err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1337, err.Error())
			return
		}
		httpx.OK(c, gin.H{"authorization_url": authorizationURL})
	})
	account.POST("/totp/setup", func(c *gin.Context) {
		public, err := opts.SettingsService.Public(c.Request.Context())
		if err != nil {
			httpx.Error(c, http.StatusServiceUnavailable, 1002, err.Error())
			return
		}
		if !public.TOTPEnabled {
			httpx.Error(c, http.StatusForbidden, 1333, "TOTP is disabled by the administrator")
			return
		}
		data, err := opts.ControlService.BeginTOTPSetup(c.Request.Context(), actor(c))
		if err != nil {
			httpx.Error(c, http.StatusBadRequest, 1312, err.Error())
			return
		}
		httpx.OK(c, data)
	})
	account.POST("/totp/confirm", func(c *gin.Context) {
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
	account.POST("/totp/recovery-codes", func(c *gin.Context) {
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
	account.DELETE("/totp", func(c *gin.Context) {
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
	account.POST("/sessions/revoke-others", func(c *gin.Context) {
		if err := opts.ControlService.RevokeAccountSessions(c.Request.Context(), actor(c)); err != nil {
			httpx.Error(c, http.StatusBadRequest, 1338, err.Error())
			return
		}
		response, err := replacementAccountSession(c, opts)
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1330, err.Error())
			return
		}
		response.Changed = true
		httpx.OK(c, response)
	})
}

func replacementAccountSession(c *gin.Context, opts Options) (accountSecurityResponse, error) {
	if opts.AuthService == nil {
		return accountSecurityResponse{}, nil
	}
	result, err := opts.AuthService.LoginOIDC(actor(c), role(c))
	if err != nil {
		return accountSecurityResponse{}, err
	}
	return accountSecurityResponse{AccessToken: result.AccessToken, ExpiresAt: result.ExpiresAt}, nil
}

func currentAccountProfile(c *gin.Context, opts Options) (accountProfileResponse, error) {
	public, err := opts.SettingsService.Public(c.Request.Context())
	if err != nil {
		return accountProfileResponse{}, err
	}
	profile, err := opts.ControlService.CurrentAccountProfile(c.Request.Context(), actor(c))
	isLocalPrincipal := opts.AuthService != nil && opts.AuthService.IsLocalPrincipal(actor(c))
	if opts.AuthService == nil && actor(c) == "local-admin" {
		isLocalPrincipal = true
	}
	if errors.Is(err, controlplane.ErrDeploymentManagedAccount) && isLocalPrincipal {
		profile = controlplane.AccountProfile{
			ID: actor(c), DisplayName: actor(c), Status: controlplane.WorkspaceUserStatusActive,
			Role: role(c), PasswordEnabled: true, ManagedByConfig: true,
		}
	} else if err != nil {
		return accountProfileResponse{}, err
	}
	if isLocalPrincipal && strings.HasSuffix(profile.Email, "@local.invalid") {
		profile.Email = ""
	}
	return accountProfileResponse{
		AccountProfile: profile,
		LoginMethods:   accountLoginMethods(profile, public, isLocalPrincipal),
		TOTPAvailable:  public.TOTPEnabled && !profile.ManagedByConfig,
	}, nil
}

func accountLoginMethods(profile controlplane.AccountProfile, public settings.PublicSettings, localPrincipal bool) []accountLoginMethod {
	issuer := strings.ToLower(strings.TrimSpace(profile.ExternalIssuer))
	issuers := map[string]bool{}
	for _, identity := range profile.AuthIdentities {
		issuers[strings.ToLower(strings.TrimSpace(identity.Issuer))] = true
	}
	if issuer != "" {
		issuers[issuer] = true
	}
	bound := func(provider string) bool {
		if provider == "feishu" {
			for value := range issuers {
				if strings.HasPrefix(value, "feishu:") {
					return true
				}
			}
			return false
		}
		return issuers[provider]
	}
	knownExternal := bound("github") || bound("google") || bound("dingtalk") || bound("feishu")
	oidcLabel := strings.TrimSpace(public.OIDCProviderName)
	if oidcLabel == "" {
		oidcLabel = "OIDC"
	}
	feishuLabel := "Feishu"
	if public.FeishuRegion == "global" {
		feishuLabel = "Lark"
	}
	methods := []accountLoginMethod{
		{ID: "email", Label: "Email", Available: profile.PasswordEnabled, Bound: profile.PasswordEnabled, Detail: profile.Email},
		{ID: "oidc", Label: oidcLabel, Available: public.OIDCEnabled, Bound: len(issuers) > 0 && !knownExternal, Detail: externalDetail(issuer, profile.Email)},
		{ID: "feishu", Label: feishuLabel, Available: public.FeishuEnabled, Bound: bound("feishu"), Detail: externalDetail(issuer, profile.Email)},
		{ID: "github", Label: "GitHub", Available: public.GitHubOAuthEnabled, Bound: bound("github"), Detail: externalDetail(issuer, profile.Email)},
		{ID: "google", Label: "Google", Available: public.GoogleOAuthEnabled, Bound: bound("google"), Detail: externalDetail(issuer, profile.Email)},
		{ID: "dingtalk", Label: "DingTalk", Available: public.DingTalkEnabled, Bound: bound("dingtalk"), Detail: externalDetail(issuer, profile.Email)},
	}
	if localPrincipal {
		methods[0] = accountLoginMethod{ID: "local", Label: "Local administrator", Available: true, Bound: true, Detail: "Built-in administrator account"}
	}
	return methods
}

func externalDetail(issuer, email string) string {
	if issuer == "" {
		return ""
	}
	return email
}

func currentAuthUser(c *gin.Context, opts Options) gin.H {
	allowedSurfaces := allowedSurfacesForActor(c.Request.Context(), opts.ControlService, actor(c))
	data, err := currentAccountProfile(c, opts)
	if err != nil {
		return gin.H{"username": actor(c), "role": role(c), "allowed_surfaces": allowedSurfaces}
	}
	username := data.Email
	if username == "" {
		username = data.DisplayName
	}
	return gin.H{
		"username": username, "role": data.Role, "display_name": data.DisplayName,
		"email": data.Email, "avatar_data_url": data.AvatarDataURL, "allowed_surfaces": allowedSurfaces,
	}
}

func allowedSurfacesForActor(ctx context.Context, control *controlplane.Service, actor string) []string {
	if control == nil {
		return nil
	}
	surfaces := []string{
		controlplane.SurfacePersonal,
		controlplane.SurfaceRelayOperator,
		controlplane.SurfaceEnterprise,
		controlplane.SurfacePlatform,
		controlplane.SurfacePortal,
		controlplane.SurfaceCustomer,
	}
	allowed := make([]string, 0, len(surfaces))
	for _, surface := range surfaces {
		ok, err := control.ActorCanSurface(ctx, actor, surface)
		if err != nil {
			return nil
		}
		if ok {
			allowed = append(allowed, surface)
		}
	}
	return allowed
}
