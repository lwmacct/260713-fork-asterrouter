package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/httpx"
	"github.com/gin-gonic/gin"
)

func requireRBAC(control *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			c.Next()
			return
		}
		permission := permissionForRequest(c)
		if permission == "" {
			c.Next()
			return
		}
		allowed, access, err := control.ActorCanResource(c.Request.Context(), actor(c), permission, resourceForRequest(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1450, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1451, "permission denied")
			c.Abort()
			return
		}
		if !access.Global && len(access.DepartmentIDs) > 0 && resourceForRequest(c) == controlplane.RBACResourceAudit {
			httpx.Error(c, http.StatusForbidden, 1451, "department-scoped access does not include global audit logs")
			c.Abort()
			return
		}
		if !access.Global && len(access.DepartmentIDs) > 0 && resourceForRequest(c) == controlplane.RBACResourceArtifacts {
			httpx.Error(c, http.StatusForbidden, 1451, "artifact administration requires global access")
			c.Abort()
			return
		}
		if !access.Global && len(access.DepartmentIDs) > 0 && resourceForRequest(c) == controlplane.RBACResourceAIJobs {
			httpx.Error(c, http.StatusForbidden, 1451, "AI job administration requires global access")
			c.Abort()
			return
		}
		if !access.Global && len(access.DepartmentIDs) > 0 && strings.HasPrefix(strings.TrimPrefix(c.FullPath(), "/api/v1/admin"), "/organization-groups") {
			httpx.Error(c, http.StatusForbidden, 1451, "organization group management requires global identity access")
			c.Abort()
			return
		}
		c.Set("principal_access", access)
		c.Next()
	}
}

func requireSurfaceRBAC(control *controlplane.Service, surface string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if control == nil {
			c.Next()
			return
		}
		permission := permissionForRequest(c)
		if permission == "" {
			c.Next()
			return
		}
		allowed, access, err := control.ActorCanSurfaceResource(c.Request.Context(), actor(c), surface, permission, resourceForRequest(c))
		if err != nil {
			httpx.Error(c, http.StatusInternalServerError, 1450, err.Error())
			c.Abort()
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusForbidden, 1451, "permission denied")
			c.Abort()
			return
		}
		c.Set("principal_access", access)
		c.Next()
	}
}

func requireUserInAccess(ctx context.Context, control *controlplane.Service, userID string, access controlplane.PrincipalAccess) error {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return nil
	}
	users, err := control.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range filterUsersForAccess(users, access) {
		if user.ID == userID {
			return nil
		}
	}
	return errors.New("resource is outside the authorized department scope")
}

func requireDepartmentAssignmentInAccess(departmentID *string, access controlplane.PrincipalAccess, allowOmitted bool) error {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return nil
	}
	if departmentID == nil {
		if allowOmitted {
			return nil
		}
		return errors.New("department-scoped administrators must assign a user to an authorized department")
	}
	requested := strings.TrimSpace(*departmentID)
	for _, allowed := range access.DepartmentIDs {
		if requested == allowed {
			return nil
		}
	}
	return errors.New("department assignment is outside the authorized department scope")
}

func requireAPIKeyInAccess(ctx context.Context, control *controlplane.Service, keyID string, access controlplane.PrincipalAccess) error {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return nil
	}
	keys, err := control.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	users, err := control.ListWorkspaceUsers(ctx)
	if err != nil {
		return err
	}
	for _, key := range filterAPIKeysForAccess(keys, users, access) {
		if key.ID == keyID {
			return nil
		}
	}
	return errors.New("resource is outside the authorized department scope")
}

func departmentAPIKeyIDs(ctx context.Context, control *controlplane.Service, access controlplane.PrincipalAccess) ([]string, error) {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return nil, nil
	}
	keys, err := control.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	users, err := control.ListWorkspaceUsers(ctx)
	if err != nil {
		return nil, err
	}
	visible := filterAPIKeysForAccess(keys, users, access)
	ids := make([]string, 0, len(visible))
	for _, key := range visible {
		ids = append(ids, key.ID)
	}
	if len(ids) == 0 {
		ids = append(ids, "__no_authorized_department_key__")
	}
	return ids, nil
}

func scopeUsageQuery(ctx context.Context, control *controlplane.Service, access controlplane.PrincipalAccess, query controlplane.UsageQuery) (controlplane.UsageQuery, error) {
	ids, err := departmentAPIKeyIDs(ctx, control, access)
	if err != nil || ids == nil {
		return query, err
	}
	query.APIKeyIDs = ids
	if query.APIKeyID != "" && !containsString(ids, query.APIKeyID) {
		query.APIKeyID = "__no_authorized_department_key__"
	}
	return query, nil
}

func scopeGatewayTraceQuery(ctx context.Context, control *controlplane.Service, access controlplane.PrincipalAccess, query controlplane.GatewayTraceQuery) (controlplane.GatewayTraceQuery, error) {
	ids, err := departmentAPIKeyIDs(ctx, control, access)
	if err != nil || ids == nil {
		return query, err
	}
	query.APIKeyIDs = ids
	if query.APIKeyID != "" && !containsString(ids, query.APIKeyID) {
		query.APIKeyID = "__no_authorized_department_key__"
	}
	return query, nil
}

func scopeAlertQuery(ctx context.Context, control *controlplane.Service, access controlplane.PrincipalAccess, query controlplane.AlertQuery) (controlplane.AlertQuery, error) {
	ids, err := departmentAPIKeyIDs(ctx, control, access)
	if err != nil || ids == nil {
		return query, err
	}
	query.ResourceType = "api_key"
	query.ResourceIDs = ids
	return query, nil
}

func requireAlertInAccess(ctx context.Context, control *controlplane.Service, alertID string, access controlplane.PrincipalAccess) error {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return nil
	}
	alert, err := control.AlertEventByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert.ResourceType == "api_key" {
		return requireAPIKeyInAccess(ctx, control, alert.ResourceID, access)
	}
	return errors.New("resource is outside the authorized department scope")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func resourceForRequest(c *gin.Context) string {
	path := controlPath(c)
	switch {
	case path == "/dashboard":
		return controlplane.RBACResourceDashboard
	case strings.HasPrefix(path, "/gateway-models"), strings.HasPrefix(path, "/model-routes"), strings.HasPrefix(path, "/gateway-simulator"), strings.HasPrefix(path, "/routing-groups"):
		return controlplane.RBACResourceRouting
	case strings.HasPrefix(path, "/providers"), strings.HasPrefix(path, "/provider-"):
		return controlplane.RBACResourceProviders
	case strings.HasPrefix(path, "/api-keys"):
		return controlplane.RBACResourceAPIKeys
	case strings.HasPrefix(path, "/usage"), strings.HasPrefix(path, "/cost-allocation"), strings.HasPrefix(path, "/pricing-rules"), strings.HasPrefix(path, "/pricing-evaluations"):
		return controlplane.RBACResourceUsage
	case strings.HasPrefix(path, "/gateway-traces"):
		return controlplane.RBACResourceTraces
	case strings.HasPrefix(path, "/ai-jobs"):
		return controlplane.RBACResourceAIJobs
	case strings.HasPrefix(path, "/artifacts"), strings.HasPrefix(path, "/artifact-runtimes"):
		return controlplane.RBACResourceArtifacts
	case strings.HasPrefix(path, "/alerts"):
		return controlplane.RBACResourceAlerts
	case strings.HasPrefix(path, "/users"), strings.HasPrefix(path, "/role-bindings"), strings.HasPrefix(path, "/departments"), strings.HasPrefix(path, "/organization-groups"):
		return controlplane.RBACResourceIdentity
	case strings.HasPrefix(path, "/tenants"), strings.HasPrefix(path, "/gateway-principals"):
		return controlplane.RBACResourcePlatformTenants
	case strings.HasPrefix(path, "/policies"):
		return controlplane.RBACResourcePolicies
	case strings.HasPrefix(path, "/audit-logs"):
		return controlplane.RBACResourceAudit
	case strings.HasPrefix(path, "/export-jobs"):
		return controlplane.RBACResourceExports
	case strings.HasPrefix(path, "/plugins"):
		return controlplane.RBACResourcePlugins
	case strings.HasPrefix(path, "/settings"):
		return controlplane.RBACResourceSettings
	case strings.HasPrefix(path, "/system"):
		return controlplane.RBACResourceSystem
	default:
		return ""
	}
}

func permissionForRequest(c *gin.Context) string {
	path := controlPath(c)
	method := c.Request.Method
	if strings.HasPrefix(path, "/plugins") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionPluginManage
	}
	if strings.HasPrefix(path, "/system") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionSystemManage
	}
	if strings.HasPrefix(path, "/export-jobs") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminAudit
		}
		return controlplane.PermissionExportManage
	}
	if strings.HasPrefix(path, "/audit-logs") || strings.Contains(path, "/export") {
		return controlplane.PermissionAdminAudit
	}
	if strings.HasPrefix(path, "/settings") {
		if method == http.MethodGet {
			return controlplane.PermissionAdminRead
		}
		return controlplane.PermissionSettingsManage
	}
	if method == http.MethodGet {
		return controlplane.PermissionAdminRead
	}
	return controlplane.PermissionAdminWrite
}

func controlPath(c *gin.Context) string {
	for _, value := range []string{c.FullPath(), c.Request.URL.Path} {
		for _, prefix := range []string{"/api/v1/admin", "/api/v1/platform"} {
			if strings.HasPrefix(value, prefix) {
				return strings.TrimPrefix(value, prefix)
			}
		}
	}
	return ""
}

func principalAccess(c *gin.Context) controlplane.PrincipalAccess {
	if value, ok := c.Get("principal_access"); ok {
		if access, ok := value.(controlplane.PrincipalAccess); ok {
			return access
		}
	}
	return controlplane.PrincipalAccess{Global: true}
}

func filterUsersForAccess(users []controlplane.WorkspaceUser, access controlplane.PrincipalAccess) []controlplane.WorkspaceUser {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return users
	}
	allowed := make(map[string]struct{}, len(access.DepartmentIDs))
	for _, id := range access.DepartmentIDs {
		allowed[id] = struct{}{}
	}
	out := make([]controlplane.WorkspaceUser, 0, len(users))
	for _, user := range users {
		if _, ok := allowed[user.DepartmentID]; ok {
			out = append(out, user)
		}
	}
	return out
}

func filterAPIKeysForAccess(keys []controlplane.APIKeyRecord, users []controlplane.WorkspaceUser, access controlplane.PrincipalAccess) []controlplane.APIKeyRecord {
	if access.Global || len(access.DepartmentIDs) == 0 {
		return keys
	}
	visibleUsers := filterUsersForAccess(users, access)
	allowedOwners := make(map[string]struct{}, len(visibleUsers))
	for _, user := range visibleUsers {
		allowedOwners[user.ID] = struct{}{}
	}
	out := make([]controlplane.APIKeyRecord, 0, len(keys))
	for _, key := range keys {
		if _, ok := allowedOwners[key.OwnerUserID]; ok && key.KeyType == controlplane.APIKeyTypeUser {
			out = append(out, key)
		}
	}
	return out
}
