package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type Config struct {
	Username         string
	Password         string
	PasswordHash     string
	LegacyAdminToken string
	SecretKey        string
	TokenTTL         time.Duration
	DemoMode         bool
}

type Service struct {
	username         string
	password         string
	passwordHash     string
	legacyAdminToken string
	secretKey        []byte
	tokenTTL         time.Duration
	demoMode         bool
	mfaMu            sync.Mutex
	mfaChallenges    map[string]mfaChallenge
	sessionVersion   func(string) (int64, bool)
}

type mfaChallenge struct {
	Subject, Role string
	ExpiresAt     time.Time
}

type Principal struct {
	Subject        string `json:"sub"`
	Role           string `json:"role"`
	Expires        int64  `json:"exp"`
	SessionVersion int64  `json:"sv,omitempty"`
}

func (s *Service) SetSessionVersionResolver(resolver func(string) (int64, bool)) {
	s.mfaMu.Lock()
	s.sessionVersion = resolver
	s.mfaMu.Unlock()
}

type LoginResult struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        User      `json:"user"`
}

type User struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func NewService(cfg Config) *Service {
	username := strings.TrimSpace(cfg.Username)
	if username == "" {
		username = "admin"
	}
	password := cfg.Password
	if password == "" && strings.TrimSpace(cfg.LegacyAdminToken) != "" {
		password = strings.TrimSpace(cfg.LegacyAdminToken)
	}
	if password == "" {
		password = "admin"
	}
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	secret := cfg.SecretKey
	if secret == "" {
		secret = "asterrouter-local-development-secret"
	}
	return &Service{
		username:         username,
		password:         password,
		passwordHash:     strings.TrimSpace(cfg.PasswordHash),
		legacyAdminToken: strings.TrimSpace(cfg.LegacyAdminToken),
		secretKey:        []byte(secret),
		tokenTTL:         ttl,
		demoMode:         cfg.DemoMode,
		mfaChallenges:    map[string]mfaChallenge{},
	}
}

func (s *Service) BeginMFA(subject, role string) (string, time.Time, error) {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(role) == "" {
		return "", time.Time{}, ErrInvalidCredentials
	}
	token, err := randomURLToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().UTC().Add(5 * time.Minute)
	s.mfaMu.Lock()
	s.mfaChallenges[token] = mfaChallenge{Subject: subject, Role: role, ExpiresAt: expires}
	for key, challenge := range s.mfaChallenges {
		if time.Now().UTC().After(challenge.ExpiresAt) {
			delete(s.mfaChallenges, key)
		}
	}
	s.mfaMu.Unlock()
	return token, expires, nil
}

func (s *Service) ConsumeMFA(token string) (string, string, bool) {
	s.mfaMu.Lock()
	defer s.mfaMu.Unlock()
	challenge, ok := s.mfaChallenges[strings.TrimSpace(token)]
	delete(s.mfaChallenges, strings.TrimSpace(token))
	if !ok || time.Now().UTC().After(challenge.ExpiresAt) {
		return "", "", false
	}
	return challenge.Subject, challenge.Role, true
}

func (s *Service) Login(_ context.Context, username string, password string) (LoginResult, error) {
	if s.demoMode && strings.TrimSpace(username) == "demo" && constantTimeEqual(password, "demo") {
		return s.loginFor("demo", "demo", "demo_admin")
	}
	if strings.TrimSpace(username) != s.username || !s.validLocalPassword(password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	return s.loginFor(s.username, s.username, "super_admin")
}

func (s *Service) BootstrapIdentity() (string, string) {
	return s.username, s.password
}

func (s *Service) SetPasswordHash(passwordHash string) {
	s.mfaMu.Lock()
	s.passwordHash = strings.TrimSpace(passwordHash)
	s.mfaMu.Unlock()
}

func (s *Service) validLocalPassword(password string) bool {
	s.mfaMu.Lock()
	passwordHash := s.passwordHash
	s.mfaMu.Unlock()
	if passwordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
	}
	return constantTimeEqual(password, s.password)
}

// LoginOIDC creates the same signed local session as password login after the
// caller has verified the upstream OIDC ID token and provisioned the user.
func (s *Service) LoginOIDC(subject, role string) (LoginResult, error) {
	subject = strings.TrimSpace(subject)
	role = strings.TrimSpace(role)
	if subject == "" || role == "" {
		return LoginResult{}, ErrInvalidCredentials
	}
	return s.loginFor(subject, subject, role)
}

func (s *Service) loginFor(username string, subject string, role string) (LoginResult, error) {
	expiresAt := time.Now().UTC().Add(s.tokenTTL)
	user := User{Username: username, Role: role}
	version := int64(0)
	s.mfaMu.Lock()
	resolver := s.sessionVersion
	s.mfaMu.Unlock()
	if resolver != nil {
		if current, ok := resolver(subject); ok {
			version = current
		}
	}
	token, err := s.sign(Principal{Subject: subject, Role: user.Role, Expires: expiresAt.Unix(), SessionVersion: version})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        user,
	}, nil
}

func (s *Service) Verify(token string) (Principal, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, false
	}
	if s.legacyAdminToken != "" && constantTimeEqual(token, s.legacyAdminToken) {
		return Principal{Subject: s.username, Role: "super_admin", Expires: time.Now().UTC().Add(s.tokenTTL).Unix()}, true
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Principal{}, false
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Principal{}, false
	}
	wantSig := s.signature(parts[0])
	if !constantTimeEqual(parts[1], wantSig) {
		return Principal{}, false
	}
	var principal Principal
	if err := json.Unmarshal(payloadRaw, &principal); err != nil {
		return Principal{}, false
	}
	if principal.Expires <= time.Now().UTC().Unix() {
		return Principal{}, false
	}
	s.mfaMu.Lock()
	resolver := s.sessionVersion
	s.mfaMu.Unlock()
	if resolver != nil {
		if current, exists := resolver(principal.Subject); exists && current != principal.SessionVersion {
			return Principal{}, false
		}
	}
	return principal, true
}

func (s *Service) IsLocalPrincipal(subject string) bool {
	subject = strings.TrimSpace(subject)
	return subject == s.username || (s.demoMode && subject == "demo")
}

func (s *Service) sign(principal Principal) (string, error) {
	payloadRaw, err := json.Marshal(principal)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	return payload + "." + s.signature(payload), nil
}

func (s *Service) signature(payload string) string {
	mac := hmac.New(sha256.New, s.secretKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func constantTimeEqual(a string, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}
