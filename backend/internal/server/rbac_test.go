package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/gin-gonic/gin"
)

func TestAdminRBACAllowsGlobalAuditorReadAndBlocksWrites(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "auditor@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleReadOnlyAuditor,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleReadOnlyAuditor,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	readReq.Header.Set("Authorization", "Bearer secret")
	readReq.Header.Set("X-Actor", "auditor@example.com")
	readRec := httptest.NewRecorder()
	handler.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("auditor read status=%d body=%s", readRec.Code, readRec.Body.String())
	}

	writeReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewBufferString(`{"site_name":"Blocked"}`))
	writeReq.Header.Set("Authorization", "Bearer secret")
	writeReq.Header.Set("X-Actor", "auditor@example.com")
	writeReq.Header.Set("Content-Type", "application/json")
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusForbidden {
		t.Fatalf("auditor write should be forbidden status=%d body=%s", writeRec.Code, writeRec.Body.String())
	}
}

func TestAdminRBACBlocksDeveloperButPortalStillWorks(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "dev@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleDeveloper,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	adminReq.Header.Set("Authorization", "Bearer secret")
	adminReq.Header.Set("X-Actor", "dev@example.com")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusForbidden {
		t.Fatalf("developer admin should be forbidden status=%d body=%s", adminRec.Code, adminRec.Body.String())
	}

	portalReq := httptest.NewRequest(http.MethodGet, "/api/v1/portal/workspace", nil)
	portalReq.Header.Set("Authorization", "Bearer secret")
	portalReq.Header.Set("X-Actor", "dev@example.com")
	portalRec := httptest.NewRecorder()
	handler.ServeHTTP(portalRec, portalReq)
	if portalRec.Code != http.StatusOK {
		t.Fatalf("developer portal should work status=%d body=%s", portalRec.Code, portalRec.Body.String())
	}

	for _, target := range []string{"/api/v1/operator/dashboard", "/api/v1/console/dashboard"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", user.Email)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("developer must not cross surface via %s status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminRBACResourceBindingOnlyGrantsMatchingResource(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "key-scope@example.com", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID: user.ID, Role: controlplane.RoleKeyManager, ScopeType: controlplane.RoleScopeResource, ScopeID: controlplane.RBACResourceAPIKeys,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}
	allowed, access, err := control.ActorCanResource(context.Background(), user.Email, controlplane.PermissionAdminRead, controlplane.RBACResourceAPIKeys)
	if err != nil || !allowed || access.Global || access.Resource != controlplane.RBACResourceAPIKeys {
		t.Fatalf("scoped access=%+v allowed=%v err=%v", access, allowed, err)
	}

	for _, target := range []string{"/api/v1/admin/api-keys", "/api/v1/admin/providers"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", user.Email)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		want := http.StatusOK
		if target == "/api/v1/admin/providers" {
			want = http.StatusForbidden
		}
		if rec.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", target, rec.Code, want, rec.Body.String())
		}
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys", bytes.NewBufferString(`{"name":"Scoped key","model_allowlist":["gpt-4o-mini"]}`))
	createReq.Header.Set("Authorization", "Bearer secret")
	createReq.Header.Set("X-Actor", user.Email)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("resource key manager create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
}

func TestDepartmentScopedAdministratorOnlySeesDepartmentUsersAndKeys(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	ctx := context.Background()
	engineering, err := control.CreateDepartment(ctx, "tester", controlplane.DepartmentRequest{Name: "Engineering", Code: "eng", Status: controlplane.DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	finance, err := control.CreateDepartment(ctx, "tester", controlplane.DepartmentRequest{Name: "Finance", Code: "fin", Status: controlplane.DepartmentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := control.CreateWorkspaceUser(ctx, "tester", controlplane.WorkspaceUserRequest{Email: "department-admin@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper})
	if err != nil {
		t.Fatal(err)
	}
	engUser, err := control.ProvisionOIDCUser(ctx, "https://id.example.test", "eng-user", "eng@example.test", "Engineer", "eng")
	if err != nil {
		t.Fatal(err)
	}
	finUser, err := control.ProvisionOIDCUser(ctx, "https://id.example.test", "fin-user", "fin@example.test", "Finance", "fin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateRoleBinding(ctx, "tester", controlplane.RoleBindingRequest{UserID: manager.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeDepartment, ScopeID: engineering.ID}); err != nil {
		t.Fatal(err)
	}
	engKey, err := control.CreateAPIKey(ctx, "tester", controlplane.APIKeyCreateRequest{Name: "Engineering key", KeyType: controlplane.APIKeyTypeUser, OwnerUserID: engUser.ID, ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	finKey, err := control.CreateAPIKey(ctx, "tester", controlplane.APIKeyCreateRequest{Name: "Finance key", KeyType: controlplane.APIKeyTypeUser, OwnerUserID: finUser.ID, ModelAllowlist: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key    controlplane.APIKeyRecord
		tokens int
	}{
		{key: engKey.Record, tokens: 10},
		{key: finKey.Record, tokens: 20},
	} {
		authContext := controlplane.GatewayAuthContext{APIKey: item.key}
		if err := control.RecordGatewayUsage(ctx, authContext, controlplane.GatewayUsageInput{Model: "model", InputTokens: item.tokens, Status: "forwarded"}); err != nil {
			t.Fatal(err)
		}
		if err := control.RecordGatewayTrace(ctx, authContext, controlplane.GatewayTraceInput{Model: "model", InputTokens: item.tokens, Status: "forwarded"}); err != nil {
			t.Fatal(err)
		}
		if err := control.RecordRiskRuleAlert(ctx, item.key.ID, "rule", "Review", "Scoped alert", 1, 1); err != nil {
			t.Fatal(err)
		}
	}

	usersReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	usersReq.Header.Set("Authorization", "Bearer secret")
	usersReq.Header.Set("X-Actor", manager.Email)
	usersRec := httptest.NewRecorder()
	handler.ServeHTTP(usersRec, usersReq)
	var usersResponse struct {
		Data []controlplane.WorkspaceUser `json:"data"`
	}
	if err := json.Unmarshal(usersRec.Body.Bytes(), &usersResponse); err != nil || usersRec.Code != http.StatusOK {
		t.Fatalf("users status=%d body=%s err=%v", usersRec.Code, usersRec.Body.String(), err)
	}
	if len(usersResponse.Data) != 1 || usersResponse.Data[0].ID != engUser.ID {
		t.Fatalf("department users leaked: %+v", usersResponse.Data)
	}

	keysReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/api-keys", nil)
	keysReq.Header.Set("Authorization", "Bearer secret")
	keysReq.Header.Set("X-Actor", manager.Email)
	keysRec := httptest.NewRecorder()
	handler.ServeHTTP(keysRec, keysReq)
	var keysResponse struct {
		Data []controlplane.APIKeyRecord `json:"data"`
	}
	if err := json.Unmarshal(keysRec.Body.Bytes(), &keysResponse); err != nil || keysRec.Code != http.StatusOK {
		t.Fatalf("keys status=%d body=%s err=%v", keysRec.Code, keysRec.Body.String(), err)
	}
	if len(keysResponse.Data) != 1 || keysResponse.Data[0].ID != engKey.Record.ID {
		t.Fatalf("department keys leaked: %+v", keysResponse.Data)
	}
	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage", nil)
	usageReq.Header.Set("Authorization", "Bearer secret")
	usageReq.Header.Set("X-Actor", manager.Email)
	usageRec := httptest.NewRecorder()
	handler.ServeHTTP(usageRec, usageReq)
	var usageResponse struct {
		Data controlplane.UsageReport `json:"data"`
	}
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usageResponse); err != nil || usageRec.Code != http.StatusOK || usageResponse.Data.TotalRequests != 1 || usageResponse.Data.TotalTokens != 10 {
		t.Fatalf("department usage leaked status=%d body=%s err=%v", usageRec.Code, usageRec.Body.String(), err)
	}
	bypassReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage?api_key_id="+finKey.Record.ID, nil)
	bypassReq.Header.Set("Authorization", "Bearer secret")
	bypassReq.Header.Set("X-Actor", manager.Email)
	bypassRec := httptest.NewRecorder()
	handler.ServeHTTP(bypassRec, bypassReq)
	var bypassResponse struct {
		Data controlplane.UsageReport `json:"data"`
	}
	_ = json.Unmarshal(bypassRec.Body.Bytes(), &bypassResponse)
	if bypassRec.Code != http.StatusOK || bypassResponse.Data.TotalRequests != 0 {
		t.Fatalf("api_key_id bypass status=%d body=%s", bypassRec.Code, bypassRec.Body.String())
	}
	tracesReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces", nil)
	tracesReq.Header.Set("Authorization", "Bearer secret")
	tracesReq.Header.Set("X-Actor", manager.Email)
	tracesRec := httptest.NewRecorder()
	handler.ServeHTTP(tracesRec, tracesReq)
	var tracesResponse struct {
		Data []controlplane.GatewayTrace `json:"data"`
	}
	_ = json.Unmarshal(tracesRec.Body.Bytes(), &tracesResponse)
	if tracesRec.Code != http.StatusOK || len(tracesResponse.Data) != 1 || tracesResponse.Data[0].APIKeyID != engKey.Record.ID {
		t.Fatalf("department traces leaked status=%d body=%s", tracesRec.Code, tracesRec.Body.String())
	}
	alertsReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts", nil)
	alertsReq.Header.Set("Authorization", "Bearer secret")
	alertsReq.Header.Set("X-Actor", manager.Email)
	alertsRec := httptest.NewRecorder()
	handler.ServeHTTP(alertsRec, alertsReq)
	var alertsResponse struct {
		Data []controlplane.AlertEvent `json:"data"`
	}
	_ = json.Unmarshal(alertsRec.Body.Bytes(), &alertsResponse)
	if alertsRec.Code != http.StatusOK || len(alertsResponse.Data) != 1 || alertsResponse.Data[0].ResourceID != engKey.Record.ID {
		t.Fatalf("department alerts leaked status=%d body=%s", alertsRec.Code, alertsRec.Body.String())
	}
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-traces/export", nil)
	exportReq.Header.Set("Authorization", "Bearer secret")
	exportReq.Header.Set("X-Actor", manager.Email)
	exportRec := httptest.NewRecorder()
	handler.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK || !strings.Contains(exportRec.Body.String(), engKey.Record.ID) || strings.Contains(exportRec.Body.String(), finKey.Record.ID) {
		t.Fatalf("department trace export leaked status=%d body=%s", exportRec.Code, exportRec.Body.String())
	}
	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	auditReq.Header.Set("Authorization", "Bearer secret")
	auditReq.Header.Set("X-Actor", manager.Email)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusForbidden {
		t.Fatalf("global audit status=%d body=%s", auditRec.Code, auditRec.Body.String())
	}
	exportJobReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/export-jobs?kind=usage&limit=10", nil)
	exportJobReq.Header.Set("Authorization", "Bearer secret")
	exportJobReq.Header.Set("X-Actor", manager.Email)
	exportJobRec := httptest.NewRecorder()
	handler.ServeHTTP(exportJobRec, exportJobReq)
	var exportJobResponse struct {
		Data csvExportJob `json:"data"`
	}
	if err := json.Unmarshal(exportJobRec.Body.Bytes(), &exportJobResponse); err != nil || exportJobRec.Code != http.StatusOK || exportJobResponse.Data.Owner != manager.Email {
		t.Fatalf("department export create status=%d body=%s err=%v", exportJobRec.Code, exportJobRec.Body.String(), err)
	}
	otherManager, err := control.CreateWorkspaceUser(ctx, "tester", controlplane.WorkspaceUserRequest{Email: "finance-admin@example.test", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.CreateRoleBinding(ctx, "tester", controlplane.RoleBindingRequest{UserID: otherManager.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeDepartment, ScopeID: finance.ID}); err != nil {
		t.Fatal(err)
	}
	foreignJobReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export-jobs/"+exportJobResponse.Data.ID, nil)
	foreignJobReq.Header.Set("Authorization", "Bearer secret")
	foreignJobReq.Header.Set("X-Actor", otherManager.Email)
	foreignJobRec := httptest.NewRecorder()
	handler.ServeHTTP(foreignJobRec, foreignJobReq)
	if foreignJobRec.Code != http.StatusNotFound {
		t.Fatalf("foreign export job status=%d body=%s", foreignJobRec.Code, foreignJobRec.Body.String())
	}
	for _, target := range []string{"/api/v1/admin/api-keys/" + finKey.Record.ID + "/rotate", "/api/v1/admin/api-keys/" + finKey.Record.ID + "/disable"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", manager.Email)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-department mutation %s status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
	sharedReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys", bytes.NewBufferString(`{"name":"Shared","model_allowlist":["model"]}`))
	sharedReq.Header.Set("Authorization", "Bearer secret")
	sharedReq.Header.Set("X-Actor", manager.Email)
	sharedReq.Header.Set("Content-Type", "application/json")
	sharedRec := httptest.NewRecorder()
	handler.ServeHTTP(sharedRec, sharedReq)
	if sharedRec.Code != http.StatusForbidden {
		t.Fatalf("department administrator created shared key status=%d body=%s", sharedRec.Code, sharedRec.Body.String())
	}
}

func TestSurfaceBindingExplicitlyGrantsOperatorAccess(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email: "operator@example.com", Status: controlplane.WorkspaceUserStatusActive, Role: controlplane.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID: user.ID, Role: controlplane.RolePlatformAdmin, ScopeType: controlplane.RoleScopeSurface, ScopeID: controlplane.SurfaceRelayOperator,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/dashboard", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Actor", user.Email)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator surface binding status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminRoutesResolveResourceDomains(t *testing.T) {
	tests := map[string]string{
		"/api/v1/admin/dashboard":           controlplane.RBACResourceDashboard,
		"/api/v1/admin/model-routes":        controlplane.RBACResourceRouting,
		"/api/v1/admin/provider-accounts":   controlplane.RBACResourceProviders,
		"/api/v1/admin/api-keys":            controlplane.RBACResourceAPIKeys,
		"/api/v1/admin/model-pricings":      controlplane.RBACResourceUsage,
		"/api/v1/admin/gateway-traces":      controlplane.RBACResourceTraces,
		"/api/v1/admin/alerts":              controlplane.RBACResourceAlerts,
		"/api/v1/admin/role-bindings":       controlplane.RBACResourceIdentity,
		"/api/v1/admin/organization-groups": controlplane.RBACResourceIdentity,
		"/api/v1/admin/policies":            controlplane.RBACResourcePolicies,
		"/api/v1/admin/audit-logs":          controlplane.RBACResourceAudit,
		"/api/v1/admin/export-jobs":         controlplane.RBACResourceExports,
		"/api/v1/admin/plugins":             controlplane.RBACResourcePlugins,
		"/api/v1/admin/settings/smtp/test":  controlplane.RBACResourceSettings,
		"/api/v1/admin/system/update":       controlplane.RBACResourceSystem,
	}
	for path, want := range tests {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, path, nil)
		if got := resourceForRequest(context); got != want {
			t.Fatalf("resourceForRequest(%s)=%q want=%q", path, got, want)
		}
	}
}

func TestAdminRBACProtectsPluginAndSystemWrites(t *testing.T) {
	handler, control := newTestRuntime(t, RuntimeConfig{AdminToken: "secret"})
	user, err := control.CreateWorkspaceUser(context.Background(), "tester", controlplane.WorkspaceUserRequest{
		Email:  "auditor@example.com",
		Status: controlplane.WorkspaceUserStatusActive,
		Role:   controlplane.RoleReadOnlyAuditor,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceUser(): %v", err)
	}
	if _, err := control.CreateRoleBinding(context.Background(), "tester", controlplane.RoleBindingRequest{
		UserID:    user.ID,
		Role:      controlplane.RoleReadOnlyAuditor,
		ScopeType: controlplane.RoleScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateRoleBinding(): %v", err)
	}

	for _, target := range []string{"/api/v1/admin/plugins/catalog-sync", "/api/v1/admin/system/update"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Actor", "auditor@example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s should be forbidden status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}
