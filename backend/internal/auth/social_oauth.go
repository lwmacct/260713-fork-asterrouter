package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SocialOAuthConfig struct {
	Provider, ClientID, ClientSecret, RedirectURL string
	Enabled                                       bool
}
type SocialOAuthProfile struct{ Subject, Email, DisplayName string }
type SocialOAuthService struct {
	config SocialOAuthConfig
	state  *OIDCService
	client *http.Client
}

func NewSocialOAuthService(config SocialOAuthConfig) (*SocialOAuthService, error) {
	config.Provider = strings.ToLower(strings.TrimSpace(config.Provider))
	if config.Provider != "github" && config.Provider != "google" {
		return nil, errors.New("social OAuth provider must be github or google")
	}
	if config.Enabled && (config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "") {
		return nil, fmt.Errorf("%s OAuth configuration is incomplete", config.Provider)
	}
	state, _ := NewOIDCService(OIDCConfig{Enabled: config.Enabled, IssuerURL: "https://" + config.Provider + ".com", ClientID: config.ClientID, RedirectURL: config.RedirectURL})
	return &SocialOAuthService{config: config, state: state, client: http.DefaultClient}, nil
}

func (s *SocialOAuthService) Begin(now time.Time) (OIDCState, error) { return s.state.Begin(now) }
func (s *SocialOAuthService) Provider() string                       { return s.config.Provider }
func (s *SocialOAuthService) AuthorizationURL(entry OIDCState) string {
	base, scope := "https://github.com/login/oauth/authorize", "read:user user:email"
	if s.config.Provider == "google" {
		base, scope = "https://accounts.google.com/o/oauth2/v2/auth", "openid email profile"
	}
	values := url.Values{"client_id": {s.config.ClientID}, "redirect_uri": {s.config.RedirectURL}, "response_type": {"code"}, "scope": {scope}, "state": {entry.Value}, "code_challenge": {PKCEChallenge(entry.Verifier)}, "code_challenge_method": {"S256"}}
	return base + "?" + values.Encode()
}

func (s *SocialOAuthService) Complete(ctx context.Context, state, code string, now time.Time) (SocialOAuthProfile, error) {
	entry, err := s.state.Consume(state, now)
	if err != nil {
		return SocialOAuthProfile{}, err
	}
	tokenURL := "https://github.com/login/oauth/access_token"
	if s.config.Provider == "google" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{"client_id": {s.config.ClientID}, "client_secret": {s.config.ClientSecret}, "redirect_uri": {s.config.RedirectURL}, "code": {code}, "grant_type": {"authorization_code"}, "code_verifier": {entry.Verifier}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return SocialOAuthProfile{}, err
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := decodeOAuthJSON(resp, &token); err != nil || token.AccessToken == "" {
		return SocialOAuthProfile{}, errors.New("OAuth token exchange failed")
	}
	if s.config.Provider == "google" {
		return s.googleProfile(ctx, token.AccessToken)
	}
	return s.githubProfile(ctx, token.AccessToken)
}

func (s *SocialOAuthService) googleProfile(ctx context.Context, token string) (SocialOAuthProfile, error) {
	var info struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := s.getJSON(ctx, "https://openidconnect.googleapis.com/v1/userinfo", token, &info); err != nil {
		return SocialOAuthProfile{}, err
	}
	if info.Subject == "" || info.Email == "" || !info.EmailVerified {
		return SocialOAuthProfile{}, errors.New("Google account must expose a verified email")
	}
	return SocialOAuthProfile{Subject: info.Subject, Email: strings.ToLower(info.Email), DisplayName: info.Name}, nil
}

func (s *SocialOAuthService) githubProfile(ctx context.Context, token string) (SocialOAuthProfile, error) {
	var user struct {
		ID          int64 `json:"id"`
		Login, Name string
	}
	if err := s.getJSON(ctx, "https://api.github.com/user", token, &user); err != nil {
		return SocialOAuthProfile{}, err
	}
	var emails []struct {
		Email             string
		Primary, Verified bool
	}
	if err := s.getJSON(ctx, "https://api.github.com/user/emails", token, &emails); err != nil {
		return SocialOAuthProfile{}, err
	}
	email := ""
	for _, item := range emails {
		if item.Verified && (item.Primary || email == "") {
			email = item.Email
			if item.Primary {
				break
			}
		}
	}
	if user.ID == 0 || email == "" {
		return SocialOAuthProfile{}, errors.New("GitHub account must expose a verified email")
	}
	name := user.Name
	if name == "" {
		name = user.Login
	}
	return SocialOAuthProfile{Subject: fmt.Sprintf("%d", user.ID), Email: strings.ToLower(email), DisplayName: name}, nil
}

func (s *SocialOAuthService) getJSON(ctx context.Context, endpoint, token string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeOAuthJSON(resp, out)
}
func decodeOAuthJSON(response *http.Response, out any) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("OAuth provider returned HTTP %d", response.StatusCode)
	}
	return json.Unmarshal(raw, out)
}
