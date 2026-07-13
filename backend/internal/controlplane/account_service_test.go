package controlplane

import (
	"context"
	"strings"
	"testing"
)

func TestCurrentAccountProfileUpdateAndPasswordChange(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "profile@example.test", "current-password", "Profile User", false)
	if err != nil {
		t.Fatalf("RegisterWorkspaceUser(): %v", err)
	}

	profile, err := svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil {
		t.Fatalf("CurrentAccountProfile(): %v", err)
	}
	if profile.Email != user.Email || !profile.PasswordEnabled || profile.ManagedByConfig {
		t.Fatalf("profile mismatch: %+v", profile)
	}

	avatar := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ"
	profile, err = svc.UpdateCurrentAccountProfile(ctx, user.ID, AccountProfileUpdateRequest{DisplayName: "Updated User", AvatarDataURL: avatar})
	if err != nil {
		t.Fatalf("UpdateCurrentAccountProfile(): %v", err)
	}
	if profile.DisplayName != "Updated User" || profile.AvatarDataURL != avatar {
		t.Fatalf("updated profile mismatch: %+v", profile)
	}

	if _, err := svc.UpdateCurrentAccountProfile(ctx, user.ID, AccountProfileUpdateRequest{DisplayName: "Updated User", AvatarDataURL: "data:text/plain;base64,dGVzdA=="}); err == nil {
		t.Fatal("expected invalid avatar type to fail")
	}
	if _, err := svc.UpdateCurrentAccountProfile(ctx, user.ID, AccountProfileUpdateRequest{DisplayName: "Updated User", AvatarDataURL: "data:image/png;base64," + strings.Repeat("A", maxAvatarDataURLBytes)}); err == nil {
		t.Fatal("expected oversized avatar to fail")
	}

	if err := svc.ChangeCurrentAccountPassword(ctx, user.ID, AccountPasswordUpdateRequest{CurrentPassword: "wrong-password", NewPassword: "new-password-value"}); err == nil {
		t.Fatal("expected incorrect current password to fail")
	}
	if err := svc.ChangeCurrentAccountPassword(ctx, user.ID, AccountPasswordUpdateRequest{CurrentPassword: "current-password", NewPassword: "new-password-value"}); err != nil {
		t.Fatalf("ChangeCurrentAccountPassword(): %v", err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(ctx, user.Email, "new-password-value", false); err != nil {
		t.Fatalf("AuthenticateWorkspaceUser(new password): %v", err)
	}
}

func TestCurrentAccountProfileRejectsDeploymentManagedActor(t *testing.T) {
	svc := NewService(NewMemoryRepository(), "/v1")
	if _, err := svc.CurrentAccountProfile(context.Background(), "admin"); err != ErrDeploymentManagedAccount {
		t.Fatalf("CurrentAccountProfile(admin) error = %v", err)
	}
}

func TestExternalAccountCanEnableLocalPasswordLogin(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	user, err := svc.ProvisionOIDCUser(ctx, "feishu:cn", "union-1", "user@example.test", "Enterprise User", "")
	if err != nil {
		t.Fatalf("ProvisionOIDCUser(): %v", err)
	}
	profile, err := svc.CurrentAccountProfile(ctx, user.ID)
	if err != nil || profile.PasswordEnabled {
		t.Fatalf("unexpected initial profile=%+v err=%v", profile, err)
	}
	if err := svc.ChangeCurrentAccountPassword(ctx, user.ID, AccountPasswordUpdateRequest{NewPassword: "local-password-value"}); err != nil {
		t.Fatalf("ChangeCurrentAccountPassword(enable): %v", err)
	}
	if _, err := svc.AuthenticateWorkspaceUser(ctx, user.Email, "local-password-value", false); err != nil {
		t.Fatalf("AuthenticateWorkspaceUser(): %v", err)
	}
	if err := svc.ChangeCurrentAccountPassword(ctx, user.ID, AccountPasswordUpdateRequest{NewPassword: "replacement-password"}); err == nil {
		t.Fatal("changing an enabled local password must require the current password")
	}
}

func TestEnsureLocalAdminPersistsAccountState(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1")
	admin, err := svc.EnsureLocalAdmin(ctx, "admin", "bootstrap-password")
	if err != nil {
		t.Fatalf("EnsureLocalAdmin(): %v", err)
	}
	if admin.ID != "admin" || admin.Role != RoleSuperAdmin || admin.PasswordHash == "" {
		t.Fatalf("local admin mismatch: %+v", admin)
	}
	updated, err := svc.UpdateCurrentAccountProfile(ctx, admin.ID, AccountProfileUpdateRequest{DisplayName: "Router Admin"})
	if err != nil {
		t.Fatalf("UpdateCurrentAccountProfile(): %v", err)
	}
	if _, err := svc.EnsureLocalAdmin(ctx, "admin", "different-bootstrap-password"); err != nil {
		t.Fatalf("EnsureLocalAdmin(second): %v", err)
	}
	profile, err := svc.CurrentAccountProfile(ctx, admin.ID)
	if err != nil {
		t.Fatalf("CurrentAccountProfile(): %v", err)
	}
	if profile.DisplayName != updated.DisplayName {
		t.Fatalf("display name was overwritten: %+v", profile)
	}
	if _, err := svc.AuthenticateWorkspaceUser(ctx, admin.Email, "bootstrap-password", false); err != nil {
		t.Fatalf("bootstrap password changed unexpectedly: %v", err)
	}
}
