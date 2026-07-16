package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/auth"
	"github.com/astercloud/asterrouter/backend/internal/settings"
)

func newIdentityServices(ctx context.Context, service *settings.Service, current settings.AdminSettings) (*auth.OIDCService, *auth.FeishuService, *auth.SocialOAuthService, *auth.SocialOAuthService, *auth.DingTalkService, error) {
	baseURL := strings.TrimRight(current.PublicBaseURL, "/")
	oidcService, err := auth.NewOIDCService(auth.OIDCConfig{
		Enabled: current.OIDCEnabled, RequireVerifiedEmail: current.OIDCRequireVerifiedEmail,
		IssuerURL: current.OIDCIssuerURL, ClientID: current.OIDCClientID,
		RedirectURL: baseURL + "/api/v1/auth/oidc/callback",
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize OIDC: %w", err)
	}
	if current.OIDCEnabled {
		if err = oidcService.Initialize(ctx); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("initialize OIDC provider: %w", err)
		}
	}
	feishuSecret, err := service.FeishuSecret(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("load Feishu secret: %w", err)
	}
	feishuService, err := auth.NewFeishuService(auth.FeishuConfig{
		Enabled: current.FeishuEnabled, Region: current.FeishuRegion, AppID: current.FeishuAppID,
		AppSecret: feishuSecret, RedirectURL: baseURL + "/api/v1/auth/feishu/callback",
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize Feishu login: %w", err)
	}
	githubSecret, googleSecret, err := service.SocialOAuthSecrets(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("load social OAuth secrets: %w", err)
	}
	githubService, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{
		Provider: "github", Enabled: current.GitHubOAuthEnabled, ClientID: current.GitHubOAuthClientID,
		ClientSecret: githubSecret, RedirectURL: baseURL + "/api/v1/auth/oauth/github/callback",
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize GitHub OAuth: %w", err)
	}
	googleService, err := auth.NewSocialOAuthService(auth.SocialOAuthConfig{
		Provider: "google", Enabled: current.GoogleOAuthEnabled, ClientID: current.GoogleOAuthClientID,
		ClientSecret: googleSecret, RedirectURL: baseURL + "/api/v1/auth/oauth/google/callback",
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize Google OAuth: %w", err)
	}
	dingTalkSecret, err := service.DingTalkSecret(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("load DingTalk secret: %w", err)
	}
	dingTalkService, err := auth.NewDingTalkService(auth.DingTalkConfig{
		Enabled: current.DingTalkEnabled, ClientID: current.DingTalkClientID,
		ClientSecret: dingTalkSecret, RedirectURL: baseURL + "/api/v1/auth/dingtalk/callback",
	})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize DingTalk login: %w", err)
	}
	return oidcService, feishuService, githubService, googleService, dingTalkService, nil
}
