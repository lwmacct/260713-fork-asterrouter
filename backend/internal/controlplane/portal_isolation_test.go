package controlplane

import (
	"context"
	"testing"
)

func TestPortalWorkspaceIsolatesKeysUsageTracesAndMutationsByOwner(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	first, _, err := svc.RegisterWorkspaceUser(ctx, "first-portal@example.test", "long-password", "First", false)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.RegisterWorkspaceUser(ctx, "second-portal@example.test", "long-password", "Second", false)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, err := svc.CreatePortalAPIKey(ctx, first.ID, APIKeyCreateRequest{Name: "First key", ModelAllowlist: []string{"model-a"}})
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := svc.CreatePortalAPIKey(ctx, second.ID, APIKeyCreateRequest{Name: "Second key", ModelAllowlist: []string{"model-a"}})
	if err != nil {
		t.Fatal(err)
	}
	firstAuth, err := svc.AuthorizeGatewayModel(ctx, firstKey.Key, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, firstAuth, GatewayUsageInput{Model: "model-a", InputTokens: 10, OutputTokens: 5, Status: "forwarded"}); err != nil {
		t.Fatal(err)
	}
	secondAuth, err := svc.AuthorizeGatewayModel(ctx, secondKey.Key, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordGatewayUsage(ctx, secondAuth, GatewayUsageInput{Model: "model-a", InputTokens: 20, OutputTokens: 5, Status: "forwarded"}); err != nil {
		t.Fatal(err)
	}

	workspace, err := svc.PortalWorkspace(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.APIKeys) != 1 || workspace.APIKeys[0].ID != firstKey.Record.ID {
		t.Fatalf("portal keys leaked: %+v", workspace.APIKeys)
	}
	if workspace.Usage.TotalRequests != 1 || workspace.Usage.TotalTokens != 15 {
		t.Fatalf("portal usage leaked: %+v", workspace.Usage)
	}
	for _, trace := range workspace.RecentTraces {
		if trace.APIKeyID != firstKey.Record.ID {
			t.Fatalf("portal trace leaked: %+v", trace)
		}
	}
	if _, err := svc.RotatePortalAPIKey(ctx, first.ID, secondKey.Record.ID); err == nil {
		t.Fatal("user must not rotate another user's key")
	}
	if err := svc.DisablePortalAPIKey(ctx, first.ID, secondKey.Record.ID); err == nil {
		t.Fatal("user must not disable another user's key")
	}
}
