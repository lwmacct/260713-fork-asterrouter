package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestPortalWorkspaceOnlyReturnsOwnedUserKeyData(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email:       "dev@example.com",
		DisplayName: "Developer",
		Status:      WorkspaceUserStatusActive,
		Role:        RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	other, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email: "other@example.com", Status: WorkspaceUserStatusActive, Role: RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(other): %v", err)
	}
	owned, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Developer Key", ModelAllowlist: []string{"gpt-4o-mini"}, KeyType: APIKeyTypeUser, OwnerUserID: user.ID,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(owned): %v", err)
	}
	foreign, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Other Key", ModelAllowlist: []string{"gpt-4o-mini"}, KeyType: APIKeyTypeUser, OwnerUserID: other.ID,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(foreign): %v", err)
	}
	if _, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{Name: "Workspace Key", ModelAllowlist: []string{"gpt-4o-mini"}}); err != nil {
		t.Fatalf("CreateAPIKey(workspace): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: owned.Record}, GatewayUsageInput{Model: "gpt-4o-mini", Status: "forwarded", InputTokens: 10}); err != nil {
		t.Fatalf("RecordGatewayUsage(owned): %v", err)
	}
	if err := svc.RecordGatewayUsage(ctx, GatewayAuthContext{APIKey: foreign.Record}, GatewayUsageInput{Model: "gpt-4o-mini", Status: "forwarded", InputTokens: 20}); err != nil {
		t.Fatalf("RecordGatewayUsage(foreign): %v", err)
	}
	now := time.Now().UTC()
	if err := repo.SaveGatewayTrace(ctx, GatewayTrace{ID: "trace_owned", APIKeyID: owned.Record.ID, Status: "forwarded", CreatedAt: now}); err != nil {
		t.Fatalf("SaveGatewayTrace(owned): %v", err)
	}
	if err := repo.SaveGatewayTrace(ctx, GatewayTrace{ID: "trace_foreign", APIKeyID: foreign.Record.ID, Status: "forwarded", CreatedAt: now}); err != nil {
		t.Fatalf("SaveGatewayTrace(foreign): %v", err)
	}
	if err := repo.SaveAlertEvent(ctx, AlertEvent{ID: "alert_owned", Type: AlertTypeAPIKeyQuota, Severity: AlertSeverityWarning, Status: AlertStatusActive, ResourceType: "api_key", ResourceID: owned.Record.ID, DedupeKey: "owned", FirstSeenAt: now, LastSeenAt: now}); err != nil {
		t.Fatalf("SaveAlertEvent(owned): %v", err)
	}
	if err := repo.SaveAlertEvent(ctx, AlertEvent{ID: "alert_foreign", Type: AlertTypeAPIKeyQuota, Severity: AlertSeverityWarning, Status: AlertStatusActive, ResourceType: "api_key", ResourceID: foreign.Record.ID, DedupeKey: "foreign", FirstSeenAt: now, LastSeenAt: now}); err != nil {
		t.Fatalf("SaveAlertEvent(foreign): %v", err)
	}
	workspace, err := svc.PortalWorkspace(ctx, user.Email)
	if err != nil {
		t.Fatalf("PortalWorkspace(): %v", err)
	}
	if !workspace.CanManageKeys {
		t.Fatal("developer must be able to manage owned personal keys")
	}
	if len(workspace.APIKeys) != 1 || workspace.APIKeys[0].ID != owned.Record.ID {
		t.Fatalf("unexpected portal keys: %+v", workspace.APIKeys)
	}
	if workspace.Usage.TotalRequests != 1 || len(workspace.Usage.Recent) != 1 || workspace.Usage.Recent[0].APIKeyID != owned.Record.ID {
		t.Fatalf("unexpected portal usage: %+v", workspace.Usage)
	}
	if len(workspace.RecentTraces) != 1 || workspace.RecentTraces[0].ID != "trace_owned" {
		t.Fatalf("unexpected portal traces: %+v", workspace.RecentTraces)
	}
	if len(workspace.Alerts) != 1 || workspace.Alerts[0].ID != "alert_owned" {
		t.Fatalf("unexpected portal alerts: %+v", workspace.Alerts)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, user.Email, foreign.Record.ID); err == nil {
		t.Fatal("portal must not rotate another user's key")
	}
}

func TestPortalWorkspaceLocalAdminCanAccessAllKeys(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email: "developer@example.com", Status: WorkspaceUserStatusActive, Role: RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	workspaceKey, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "Workspace Key", ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(workspace): %v", err)
	}
	userKey, err := svc.CreateAPIKey(ctx, "tester", APIKeyCreateRequest{
		Name: "User Key", ModelAllowlist: []string{"gpt-4o-mini"}, KeyType: APIKeyTypeUser, OwnerUserID: user.ID,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(user): %v", err)
	}
	workspace, err := svc.PortalWorkspace(ctx, "admin")
	if err != nil {
		t.Fatalf("PortalWorkspace(admin): %v", err)
	}
	if !workspace.CanManageKeys || len(workspace.APIKeys) != 2 {
		t.Fatalf("local admin portal access mismatch: %+v", workspace.APIKeys)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, "admin", workspaceKey.Record.ID); err != nil {
		t.Fatalf("RotatePortalAPIKey(workspace): %v", err)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, "admin", userKey.Record.ID); err != nil {
		t.Fatalf("RotatePortalAPIKey(user): %v", err)
	}
}

func TestPortalKeyManagementRequiresKeyManagerRole(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	svc := NewService(repo, "/v1")
	user, err := svc.CreateWorkspaceUser(ctx, "tester", WorkspaceUserRequest{
		Email:       "keys@example.com",
		DisplayName: "Key Manager",
		Status:      WorkspaceUserStatusActive,
		Role:        RoleKeyManager,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	created, err := svc.CreatePortalAPIKey(ctx, user.Email, APIKeyCreateRequest{
		Name:           "CLI Key",
		ModelAllowlist: []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("CreatePortalAPIKey(): %v", err)
	}
	if created.Key == "" || created.Record.Name != "CLI Key" {
		t.Fatalf("unexpected created key: %+v", created)
	}
	if created.Record.KeyType != APIKeyTypeUser || created.Record.OwnerUserID != user.ID {
		t.Fatalf("portal key ownership mismatch: %+v", created.Record)
	}
	if _, err := svc.RotatePortalAPIKey(ctx, user.Email, created.Record.ID); err != nil {
		t.Fatalf("RotatePortalAPIKey(): %v", err)
	}
	if err := svc.DisablePortalAPIKey(ctx, user.Email, created.Record.ID); err != nil {
		t.Fatalf("DisablePortalAPIKey(): %v", err)
	}
}
