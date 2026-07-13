package auth

import (
	"strings"
	"testing"
	"time"
)

func TestOIDCStateIsSingleUseAndPKCEURLIsBound(t *testing.T) {
	svc, err := NewOIDCService(OIDCConfig{Enabled: true, IssuerURL: "https://id.example.test", ClientID: "client", RedirectURL: "https://router.example.test/api/v1/auth/oidc/callback"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state, err := svc.Begin(now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svc.AuthorizationURL(state), "code_challenge_method=S256") {
		t.Fatal("missing PKCE method")
	}
	if _, err := svc.Consume(state.Value, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Consume(state.Value, now.Add(time.Minute)); err != ErrOIDCInvalidState {
		t.Fatalf("second consume error = %v", err)
	}
}

func TestMapOIDCProfileUsesStableSubjectAndFallbackName(t *testing.T) {
	profile, err := MapOIDCProfile(map[string]any{"sub": "id-1", "email": "USER@EXAMPLE.TEST", "preferred_username": "user", "department": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Subject != "id-1" || profile.Email != "user@example.test" || profile.DisplayName != "user" || profile.Department != "eng" {
		t.Fatalf("profile = %+v", profile)
	}
	if _, err := MapOIDCProfile(map[string]any{}); err != ErrOIDCInvalidProfile {
		t.Fatalf("missing subject error = %v", err)
	}
}

func TestOIDCEmailVerifiedClaimRequiresBooleanTrue(t *testing.T) {
	if !oidcEmailVerified(map[string]any{"email_verified": true}) {
		t.Fatal("boolean true must be accepted")
	}
	for _, claims := range []map[string]any{{}, {"email_verified": false}, {"email_verified": "true"}, {"email_verified": 1}} {
		if oidcEmailVerified(claims) {
			t.Fatalf("unexpected verified claim: %#v", claims)
		}
	}
}
