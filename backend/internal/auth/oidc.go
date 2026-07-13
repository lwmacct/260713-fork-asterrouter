package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrOIDCDisabled       = errors.New("oidc is disabled")
	ErrOIDCInvalidConfig  = errors.New("invalid oidc configuration")
	ErrOIDCInvalidState   = errors.New("invalid oidc state")
	ErrOIDCInvalidProfile = errors.New("oidc profile is missing a subject")
)

type OIDCConfig struct {
	Enabled              bool
	RequireVerifiedEmail bool
	IssuerURL            string
	ClientID             string
	RedirectURL          string
	StateTTL             time.Duration
}

type OIDCState struct {
	Value       string
	Verifier    string
	Nonce       string
	CreatedAt   time.Time
	RedirectURL string
}

type OIDCProfile struct {
	Subject     string
	Email       string
	DisplayName string
	Department  string
}

type OIDCService struct {
	config      OIDCConfig
	mu          sync.Mutex
	states      map[string]OIDCState
	provider    *oidc.Provider
	oauthConfig *oauth2.Config
	verifier    *oidc.IDTokenVerifier
}

func (s *OIDCService) Initialize(ctx context.Context) error {
	if s == nil || !s.config.Enabled {
		return ErrOIDCDisabled
	}
	provider, err := oidc.NewProvider(ctx, s.config.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	s.provider = provider
	s.oauthConfig = &oauth2.Config{ClientID: s.config.ClientID, Endpoint: provider.Endpoint(), RedirectURL: s.config.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}
	s.verifier = provider.Verifier(&oidc.Config{ClientID: s.config.ClientID})
	return nil
}

func (s *OIDCService) OAuthConfig() (*oauth2.Config, error) {
	if s == nil || !s.config.Enabled {
		return nil, ErrOIDCDisabled
	}
	if s.oauthConfig == nil {
		return nil, errors.New("oidc provider is not initialized")
	}
	copy := *s.oauthConfig
	return &copy, nil
}

func (s *OIDCService) IssuerURL() string {
	if s == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(s.config.IssuerURL), "/")
}

func (s *OIDCService) VerifyIDToken(ctx context.Context, raw string, nonce string) (OIDCProfile, error) {
	if s == nil || !s.config.Enabled {
		return OIDCProfile{}, ErrOIDCDisabled
	}
	if s.verifier == nil {
		return OIDCProfile{}, errors.New("oidc provider is not initialized")
	}
	token, err := s.verifier.Verify(ctx, raw)
	if err != nil {
		return OIDCProfile{}, fmt.Errorf("verify oidc id token: %w", err)
	}
	if nonce != "" && token.Nonce != nonce {
		return OIDCProfile{}, errors.New("oidc nonce mismatch")
	}
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return OIDCProfile{}, fmt.Errorf("decode oidc claims: %w", err)
	}
	if s.config.RequireVerifiedEmail {
		if !oidcEmailVerified(claims) {
			return OIDCProfile{}, errors.New("oidc email must be verified")
		}
	}
	return MapOIDCProfile(claims)
}

func oidcEmailVerified(claims map[string]any) bool {
	verified, ok := claims["email_verified"].(bool)
	return ok && verified
}

func NewOIDCService(cfg OIDCConfig) (*OIDCService, error) {
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = 10 * time.Minute
	}
	if cfg.Enabled {
		issuer, err := url.Parse(strings.TrimSpace(cfg.IssuerURL))
		if err != nil || issuer.Scheme != "https" || issuer.Host == "" {
			return nil, fmt.Errorf("%w: issuer_url must be an https URL", ErrOIDCInvalidConfig)
		}
		if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
			return nil, fmt.Errorf("%w: client_id and redirect_url are required", ErrOIDCInvalidConfig)
		}
		redirect, err := url.Parse(cfg.RedirectURL)
		if err != nil || redirect.Scheme != "https" || redirect.Host == "" {
			return nil, fmt.Errorf("%w: redirect_url must be an https URL", ErrOIDCInvalidConfig)
		}
	}
	return &OIDCService{config: cfg, states: make(map[string]OIDCState)}, nil
}

func (s *OIDCService) Begin(now time.Time) (OIDCState, error) {
	if s == nil || !s.config.Enabled {
		return OIDCState{}, ErrOIDCDisabled
	}
	state, err := randomURLToken(32)
	if err != nil {
		return OIDCState{}, err
	}
	verifier, err := randomURLToken(32)
	if err != nil {
		return OIDCState{}, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return OIDCState{}, err
	}
	entry := OIDCState{Value: state, Verifier: verifier, Nonce: nonce, CreatedAt: now.UTC(), RedirectURL: s.config.RedirectURL}
	s.mu.Lock()
	s.states[state] = entry
	s.pruneLocked(now.UTC())
	s.mu.Unlock()
	return entry, nil
}

func (s *OIDCService) Consume(state string, now time.Time) (OIDCState, error) {
	if s == nil || !s.config.Enabled {
		return OIDCState{}, ErrOIDCDisabled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.states[strings.TrimSpace(state)]
	delete(s.states, strings.TrimSpace(state))
	if !ok || now.UTC().Sub(entry.CreatedAt) > s.config.StateTTL {
		return OIDCState{}, ErrOIDCInvalidState
	}
	return entry, nil
}

func (s *OIDCService) AuthorizationURL(entry OIDCState) string {
	if s.oauthConfig != nil {
		return s.oauthConfig.AuthCodeURL(entry.Value, oidc.Nonce(entry.Nonce), oauth2.S256ChallengeOption(entry.Verifier))
	}
	base := strings.TrimRight(s.config.IssuerURL, "/") + "/authorize"
	v := url.Values{}
	v.Set("client_id", s.config.ClientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", entry.RedirectURL)
	v.Set("scope", "openid profile email")
	v.Set("state", entry.Value)
	v.Set("code_challenge", codeChallenge(entry.Verifier))
	v.Set("code_challenge_method", "S256")
	v.Set("nonce", entry.Nonce)
	return base + "?" + v.Encode()
}

func (s *OIDCService) Complete(ctx context.Context, state, code string, now time.Time) (OIDCProfile, error) {
	entry, err := s.Consume(state, now)
	if err != nil {
		return OIDCProfile{}, err
	}
	config, err := s.OAuthConfig()
	if err != nil {
		return OIDCProfile{}, err
	}
	token, err := config.Exchange(ctx, strings.TrimSpace(code), oauth2.VerifierOption(entry.Verifier))
	if err != nil {
		return OIDCProfile{}, fmt.Errorf("exchange oidc authorization code: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return OIDCProfile{}, errors.New("oidc token response is missing id_token")
	}
	return s.VerifyIDToken(ctx, raw, entry.Nonce)
}

func MapOIDCProfile(claims map[string]any) (OIDCProfile, error) {
	sub, _ := claims["sub"].(string)
	if strings.TrimSpace(sub) == "" {
		return OIDCProfile{}, ErrOIDCInvalidProfile
	}
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	if name == "" {
		name, _ = claims["preferred_username"].(string)
	}
	department, _ := claims["department"].(string)
	return OIDCProfile{Subject: strings.TrimSpace(sub), Email: strings.ToLower(strings.TrimSpace(email)), DisplayName: strings.TrimSpace(name), Department: strings.TrimSpace(department)}, nil
}

func (s *OIDCService) pruneLocked(now time.Time) {
	for key, entry := range s.states {
		if now.Sub(entry.CreatedAt) > s.config.StateTTL {
			delete(s.states, key)
		}
	}
}

func randomURLToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func PKCEChallenge(verifier string) string { return codeChallenge(verifier) }
func RandomToken(size int) (string, error) { return randomURLToken(size) }
