package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
)

func TestAdminIdentityUserAndRoleBindingEndpoints(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})

	createBody := bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Dev User","status":"active","role":"developer"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create user status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Data controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create user: %v", err)
	}
	if createResp.Data.ID == "" || createResp.Data.Email != "dev@example.com" || createResp.Data.Role != controlplane.RoleDeveloper {
		t.Fatalf("create user mismatch: %+v", createResp.Data)
	}

	updateBody := bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Developer User","status":"active","role":"key_manager"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+createResp.Data.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update user status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Data controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update user: %v", err)
	}
	if updateResp.Data.DisplayName != "Developer User" || updateResp.Data.Role != controlplane.RoleKeyManager {
		t.Fatalf("update user mismatch: %+v", updateResp.Data)
	}

	bindingBody := bytes.NewBufferString(`{"user_id":"` + createResp.Data.ID + `","role":"key_manager","scope_type":"global"}`)
	bindingReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/role-bindings", bindingBody)
	bindingReq.Header.Set("Content-Type", "application/json")
	bindingRec := httptest.NewRecorder()
	handler.ServeHTTP(bindingRec, bindingReq)
	if bindingRec.Code != http.StatusOK {
		t.Fatalf("create role binding status = %d body=%s", bindingRec.Code, bindingRec.Body.String())
	}
	var bindingResp struct {
		Data controlplane.RoleBinding `json:"data"`
	}
	if err := json.Unmarshal(bindingRec.Body.Bytes(), &bindingResp); err != nil {
		t.Fatalf("decode role binding: %v", err)
	}
	if bindingResp.Data.UserID != createResp.Data.ID || bindingResp.Data.ScopeType != controlplane.RoleScopeGlobal || bindingResp.Data.ScopeID != "" {
		t.Fatalf("role binding mismatch: %+v", bindingResp.Data)
	}

	usersReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	usersRec := httptest.NewRecorder()
	handler.ServeHTTP(usersRec, usersReq)
	if usersRec.Code != http.StatusOK {
		t.Fatalf("list users status = %d body=%s", usersRec.Code, usersRec.Body.String())
	}
	var usersResp struct {
		Data []controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(usersRec.Body.Bytes(), &usersResp); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	var createdUser *controlplane.WorkspaceUser
	for index := range usersResp.Data {
		if usersResp.Data[index].ID == createResp.Data.ID {
			createdUser = &usersResp.Data[index]
			break
		}
	}
	if createdUser == nil || createdUser.Role != controlplane.RoleKeyManager {
		t.Fatalf("users list mismatch: %+v", usersResp.Data)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/role-bindings/"+bindingResp.Data.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete binding status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	audit, err := control.ListAuditLogs(context.Background(), 20)
	if err != nil {
		t.Fatalf("ListAuditLogs(): %v", err)
	}
	var seenCreateUser, seenGrant, seenRevoke bool
	for _, event := range audit {
		seenCreateUser = seenCreateUser || event.ResourceType == "workspace_user" && event.Action == "create"
		seenGrant = seenGrant || event.ResourceType == "role_binding" && event.Action == "grant_role"
		seenRevoke = seenRevoke || event.ResourceType == "role_binding" && event.Action == "revoke_role"
	}
	if !seenCreateUser || !seenGrant || !seenRevoke {
		t.Fatalf("identity audit events missing create=%v grant=%v revoke=%v audit=%+v", seenCreateUser, seenGrant, seenRevoke, audit)
	}

	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(`{"email":"dev@example.com","display_name":"Duplicate","status":"active","role":"developer"}`))
	duplicateReq.Header.Set("Content-Type", "application/json")
	duplicateRec := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRec, duplicateReq)
	if duplicateRec.Code != http.StatusBadRequest || !strings.Contains(duplicateRec.Body.String(), "already exists") {
		t.Fatalf("duplicate user should be rejected status=%d body=%s", duplicateRec.Code, duplicateRec.Body.String())
	}
}

func TestAdminOrganizationGroupLifecycle(t *testing.T) {
	handler, control := newTestRuntime(t, config.Config{})
	user, err := control.CreateWorkspaceUser(t.Context(), "tester", controlplane.WorkspaceUserRequest{Email: "group-member@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper})
	if err != nil {
		t.Fatal(err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/organization-groups", bytes.NewBufferString(`{"name":"AI Platform","status":"active","member_ids":["`+user.ID+`"]}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	var createResponse struct {
		Data controlplane.OrganizationGroup `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil || createRec.Code != http.StatusOK || len(createResponse.Data.MemberIDs) != 1 {
		t.Fatalf("create status=%d body=%s err=%v", createRec.Code, createRec.Body.String(), err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/organization-groups", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), createResponse.Data.ID) {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/organization-groups/"+createResponse.Data.ID, nil)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}
