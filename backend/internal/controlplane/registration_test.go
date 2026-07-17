package controlplane

import (
	"context"
	"testing"
)

func TestWorkspaceRegistrationVerificationAndAuthentication(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, token, err := svc.RegisterWorkspaceUser(context.Background(), "User@Example.test", "long-password", "User", true)
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailVerified || token == "" {
		t.Fatalf("user=%+v token=%q", user, token)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err == nil {
		t.Fatal("unverified user must be rejected")
	}
	if err := svc.VerifyWorkspaceUserEmail(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyWorkspaceUserEmail(context.Background(), token); err == nil {
		t.Fatal("verification token must be single use")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "wrong-password", true); err == nil {
		t.Fatal("wrong password must be rejected")
	}
	_, resetToken, err := svc.BeginPasswordReset(context.Background(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CompletePasswordReset(context.Background(), resetToken, "new-long-password"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompletePasswordReset(context.Background(), resetToken, "another-password"); err == nil {
		t.Fatal("reset token must be single use")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "long-password", true); err == nil {
		t.Fatal("old password must be invalid")
	}
	if _, err := svc.AuthenticateWorkspaceUser(context.Background(), user.Email, "new-long-password", true); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterWorkspaceUserAppliesDefaults(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	user, _, err := svc.RegisterWorkspaceUser(context.Background(), "defaults@example.test", "long-password", "Defaults", false, WorkspaceUserDefaults{BalanceMicros: 1200, ConcurrencyLimit: 8, RPMLimit: 90})
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser() error = %v", err)
	}
	if user.BalanceMicros != 1200 || user.ConcurrencyLimit != 8 || user.RPMLimit != 90 {
		t.Fatalf("user defaults not applied: %+v", user)
	}
}
