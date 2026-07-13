package controlplane

import (
	"context"
	"strings"
)

const (
	PermissionAdminRead      = "admin:read"
	PermissionAdminWrite     = "admin:write"
	PermissionAdminAudit     = "admin:audit"
	PermissionPluginManage   = "plugins:manage"
	PermissionExportManage   = "exports:manage"
	PermissionSystemManage   = "system:manage"
	PermissionSettingsManage = "settings:manage"
)

type PrincipalAccess struct {
	Actor         string   `json:"actor"`
	Role          string   `json:"role"`
	Global        bool     `json:"global"`
	Permissions   []string `json:"permissions"`
	ResolvedFrom  string   `json:"resolved_from"`
	Resource      string   `json:"resource,omitempty"`
	DepartmentIDs []string `json:"department_ids,omitempty"`
}

func (s *Service) PrincipalAccess(ctx context.Context, actor string) (PrincipalAccess, error) {
	return s.principalAccessForResource(ctx, actor, "")
}

func (s *Service) principalAccessForResource(ctx context.Context, actor string, resource string) (PrincipalAccess, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	if isLocalAdminActor(actor) {
		return PrincipalAccess{
			Actor:        actor,
			Role:         RoleSuperAdmin,
			Global:       true,
			Permissions:  permissionsForRole(RoleSuperAdmin, resource),
			ResolvedFrom: "local_admin",
		}, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return PrincipalAccess{}, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return PrincipalAccess{Actor: actor, Role: RoleDeveloper, ResolvedFrom: "unmatched"}, nil
	}
	access := PrincipalAccess{
		Actor:        actor,
		Role:         user.Role,
		Global:       user.Role != RoleDeveloper,
		Permissions:  permissionsForRole(user.Role, resource),
		ResolvedFrom: "workspace_user",
		Resource:     resource,
	}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return PrincipalAccess{}, err
	}
	for _, binding := range bindings {
		if binding.UserID != user.ID {
			continue
		}
		if binding.ScopeType != RoleScopeGlobal && binding.ScopeType != RoleScopeDepartment && (binding.ScopeType != RoleScopeResource || binding.ScopeID != resource) {
			continue
		}
		access.Permissions = mergePermissions(access.Permissions, permissionsForRole(binding.Role, resource))
		if roleRank(binding.Role) > roleRank(access.Role) {
			access.Role = binding.Role
		}
		if binding.ScopeType == RoleScopeGlobal {
			access.Global = true
		} else if binding.ScopeType == RoleScopeDepartment && !contains(access.DepartmentIDs, binding.ScopeID) {
			access.DepartmentIDs = append(access.DepartmentIDs, binding.ScopeID)
		}
	}
	return access, nil
}

func (s *Service) ActorCan(ctx context.Context, actor string, permission string) (bool, PrincipalAccess, error) {
	return s.ActorCanResource(ctx, actor, permission, "")
}

func (s *Service) ActorCanResource(ctx context.Context, actor string, permission string, resource string) (bool, PrincipalAccess, error) {
	access, err := s.principalAccessForResource(ctx, actor, resource)
	if err != nil {
		return false, PrincipalAccess{}, err
	}
	return contains(access.Permissions, permission), access, nil
}

func (s *Service) ActorCanSurface(ctx context.Context, actor string, surface string) (bool, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	if isLocalAdminActor(actor) {
		return true, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return false, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return false, nil
	}
	if surface == SurfacePortal || surface == SurfaceCustomer || user.Role == RoleSuperAdmin {
		return true, nil
	}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return false, err
	}
	if surface == SurfaceEnterprise {
		if user.Role != RoleDeveloper {
			return true, nil
		}
		for _, binding := range bindings {
			if binding.UserID == user.ID && (binding.ScopeType == RoleScopeGlobal || binding.ScopeType == RoleScopeResource || binding.ScopeType == RoleScopeDepartment || (binding.ScopeType == RoleScopeSurface && binding.ScopeID == surface)) && binding.Role != RoleDeveloper {
				return true, nil
			}
		}
		return false, nil
	}
	for _, binding := range bindings {
		if binding.UserID == user.ID && binding.ScopeType == RoleScopeSurface && binding.ScopeID == surface && (binding.Role == RoleSuperAdmin || binding.Role == RolePlatformAdmin) {
			return true, nil
		}
	}
	return false, nil
}

func permissionsForRole(role string, resource string) []string {
	switch role {
	case RoleSuperAdmin:
		return []string{
			PermissionAdminRead,
			PermissionAdminWrite,
			PermissionAdminAudit,
			PermissionPluginManage,
			PermissionExportManage,
			PermissionSystemManage,
			PermissionSettingsManage,
		}
	case RolePlatformAdmin:
		return []string{
			PermissionAdminRead,
			PermissionAdminWrite,
			PermissionAdminAudit,
			PermissionPluginManage,
			PermissionExportManage,
			PermissionSettingsManage,
		}
	case RoleKeyManager:
		switch resource {
		case RBACResourceAPIKeys:
			return []string{PermissionAdminRead, PermissionAdminWrite}
		case RBACResourceUsage, RBACResourceTraces:
			return []string{PermissionAdminRead}
		default:
			return []string{}
		}
	case RoleReadOnlyAuditor:
		return []string{PermissionAdminRead, PermissionAdminAudit, PermissionExportManage}
	case RoleDeveloper:
		return []string{}
	default:
		return []string{}
	}
}

func mergePermissions(current []string, next []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(current)+len(next))
	for _, permission := range append(current, next...) {
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		out = append(out, permission)
	}
	return out
}

func roleRank(role string) int {
	switch role {
	case RoleSuperAdmin:
		return 5
	case RolePlatformAdmin:
		return 4
	case RoleKeyManager:
		return 3
	case RoleReadOnlyAuditor:
		return 2
	case RoleDeveloper:
		return 1
	default:
		return 0
	}
}
