package controlplane

import (
	"context"
	"testing"
)

func TestOrganizationGroupMembershipIsUniqueAndAudited(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "member@example.test", "long-password", "Member", false)
	if err != nil {
		t.Fatal(err)
	}
	group, err := svc.SaveOrganizationGroup(ctx, "admin", "", OrganizationGroupRequest{Name: "AI Platform", Status: WorkspaceUserStatusActive, MemberIDs: []string{user.ID, user.ID}})
	if err != nil || len(group.MemberIDs) != 1 {
		t.Fatalf("group=%+v err=%v", group, err)
	}
	if _, err := svc.SaveOrganizationGroup(ctx, "admin", "", OrganizationGroupRequest{Name: "Finance AI", Status: WorkspaceUserStatusActive, MemberIDs: []string{user.ID}}); err == nil {
		t.Fatal("user must not belong to multiple organization groups")
	}
	if err := svc.DeleteOrganizationGroup(ctx, "admin", group.ID); err != nil {
		t.Fatal(err)
	}
	logs, err := svc.ListAuditLogs(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	seenSave, seenDelete := false, false
	for _, log := range logs {
		seenSave = seenSave || log.ResourceType == "organization_group" && log.Action == "save"
		seenDelete = seenDelete || log.ResourceType == "organization_group" && log.Action == "delete"
	}
	if !seenSave || !seenDelete {
		t.Fatalf("organization group audit missing: %+v", logs)
	}
}

func TestCostAllocationReportAggregatesByOrganizationGroup(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryRepository(), "/v1", "secret")
	user, _, err := svc.RegisterWorkspaceUser(ctx, "group-cost@example.test", "long-password", "Group Cost User", false)
	if err != nil {
		t.Fatal(err)
	}
	group, err := svc.SaveOrganizationGroup(ctx, "admin", "", OrganizationGroupRequest{Name: "AI Cost Center", Status: WorkspaceUserStatusActive, MemberIDs: []string{user.ID}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateAPIKey(ctx, "admin", APIKeyCreateRequest{Name: "Group key", KeyType: APIKeyTypeUser, OwnerUserID: user.ID, ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	auth, _ := svc.AuthorizeGatewayModel(ctx, created.Key, "model")
	if err := svc.RecordGatewayUsage(ctx, auth, GatewayUsageInput{Model: "model", Status: "forwarded", CostCents: 50}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.CostAllocationReportQuery(ctx, CostAllocationByGroup, UsageQuery{})
	if err != nil || len(report.Rows) != 1 || report.Rows[0].ResourceID != group.ID || report.Rows[0].ResourceName != group.Name || report.Rows[0].TotalCostCents != 50 {
		t.Fatalf("group report=%+v err=%v", report, err)
	}
}
