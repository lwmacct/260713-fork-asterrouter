package controlplane

import (
	"context"
	"errors"
	"strings"
)

type portalScope struct {
	Actor         string
	AllProjects   bool
	ProjectIDs    map[string]struct{}
	CanManageKeys bool
}

func (s *Service) PortalWorkspace(ctx context.Context, actor string) (PortalWorkspace, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return PortalWorkspace{}, err
	}
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return PortalWorkspace{}, err
	}
	projects = filterProjectsByPortalScope(projects, scope)
	apps, err := s.repo.ListApplications(ctx, "")
	if err != nil {
		return PortalWorkspace{}, err
	}
	apps = filterApplicationsByPortalScope(apps, scope)
	keys, err := s.repo.ListAPIKeys(ctx)
	if err != nil {
		return PortalWorkspace{}, err
	}
	keys = filterAPIKeysByPortalScope(keys, scope)
	usage, err := s.portalUsageReport(ctx, scope, 20)
	if err != nil {
		return PortalWorkspace{}, err
	}
	traces, err := s.portalGatewayTraces(ctx, scope, 12)
	if err != nil {
		return PortalWorkspace{}, err
	}
	alerts, err := s.portalAlerts(ctx, scope, 12)
	if err != nil {
		return PortalWorkspace{}, err
	}
	models, err := s.GatewayModels(ctx)
	if err != nil {
		return PortalWorkspace{}, err
	}
	return PortalWorkspace{
		Projects:      projects,
		Applications:  apps,
		APIKeys:       keys,
		Usage:         usage,
		RecentTraces:  traces,
		Alerts:        alerts,
		Models:        models,
		GatewayPath:   s.gatewayPath,
		CanManageKeys: scope.CanManageKeys,
		Principal:     scope.Actor,
	}, nil
}

func (s *Service) CreatePortalAPIKey(ctx context.Context, actor string, req APIKeyCreateRequest) (APIKeyCreateResponse, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if !scope.CanManageKeys {
		return APIKeyCreateResponse{}, errors.New("portal principal cannot manage api keys")
	}
	if strings.TrimSpace(req.ApplicationID) != "" {
		if err := s.ensurePortalApplicationAccess(ctx, scope, req.ApplicationID); err != nil {
			return APIKeyCreateResponse{}, err
		}
	} else if strings.TrimSpace(req.ProjectID) != "" {
		if err := s.ensurePortalProjectAccess(scope, req.ProjectID); err != nil {
			return APIKeyCreateResponse{}, err
		}
	}
	return s.CreateAPIKey(ctx, portalActor(scope.Actor), req)
}

func (s *Service) RotatePortalAPIKey(ctx context.Context, actor string, id string) (APIKeyCreateResponse, error) {
	scope, key, err := s.portalAPIKeyAccess(ctx, actor, id)
	if err != nil {
		return APIKeyCreateResponse{}, err
	}
	if !scope.CanManageKeys {
		return APIKeyCreateResponse{}, errors.New("portal principal cannot manage api keys")
	}
	return s.RotateAPIKey(ctx, portalActor(scope.Actor), key.ID)
}

func (s *Service) DisablePortalAPIKey(ctx context.Context, actor string, id string) error {
	scope, key, err := s.portalAPIKeyAccess(ctx, actor, id)
	if err != nil {
		return err
	}
	if !scope.CanManageKeys {
		return errors.New("portal principal cannot manage api keys")
	}
	return s.DisableAPIKey(ctx, portalActor(scope.Actor), key.ID)
}

func (s *Service) portalAPIKeyAccess(ctx context.Context, actor string, id string) (portalScope, APIKeyRecord, error) {
	scope, err := s.portalScopeForActor(ctx, actor)
	if err != nil {
		return portalScope{}, APIKeyRecord{}, err
	}
	key, err := s.apiKeyByID(ctx, id)
	if err != nil {
		return portalScope{}, APIKeyRecord{}, err
	}
	if err := s.ensurePortalProjectAccess(scope, key.ProjectID); err != nil {
		return portalScope{}, APIKeyRecord{}, err
	}
	return scope, key, nil
}

func (s *Service) portalScopeForActor(ctx context.Context, actor string) (portalScope, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	scope := portalScope{Actor: actor, ProjectIDs: map[string]struct{}{}}
	if isLocalAdminActor(actor) {
		scope.AllProjects = true
		scope.CanManageKeys = true
		return scope, nil
	}
	users, err := s.repo.ListWorkspaceUsers(ctx)
	if err != nil {
		return portalScope{}, err
	}
	user, ok := workspaceUserByActor(users, actor)
	if !ok || user.Status != WorkspaceUserStatusActive {
		return scope, nil
	}
	if roleCanManageKeys(user.Role) {
		scope.CanManageKeys = true
	}
	bindings, err := s.repo.ListRoleBindings(ctx)
	if err != nil {
		return portalScope{}, err
	}
	for _, binding := range bindings {
		if binding.UserID != user.ID {
			continue
		}
		if roleCanManageKeys(binding.Role) {
			scope.CanManageKeys = true
		}
		if binding.ScopeType == RoleScopeGlobal {
			scope.AllProjects = true
		}
		if binding.ScopeType == RoleScopeProject && strings.TrimSpace(binding.ScopeID) != "" {
			scope.ProjectIDs[binding.ScopeID] = struct{}{}
		}
	}
	return scope, nil
}

func filterProjectsByPortalScope(projects []Project, scope portalScope) []Project {
	if scope.AllProjects {
		return projects
	}
	out := make([]Project, 0, len(projects))
	for _, project := range projects {
		if _, ok := scope.ProjectIDs[project.ID]; ok {
			out = append(out, project)
		}
	}
	return out
}

func filterApplicationsByPortalScope(apps []Application, scope portalScope) []Application {
	if scope.AllProjects {
		return apps
	}
	out := make([]Application, 0, len(apps))
	for _, app := range apps {
		if _, ok := scope.ProjectIDs[app.ProjectID]; ok {
			out = append(out, app)
		}
	}
	return out
}

func filterAPIKeysByPortalScope(keys []APIKeyRecord, scope portalScope) []APIKeyRecord {
	if scope.AllProjects {
		return keys
	}
	out := make([]APIKeyRecord, 0, len(keys))
	for _, key := range keys {
		if _, ok := scope.ProjectIDs[key.ProjectID]; ok {
			out = append(out, key)
		}
	}
	return out
}

func (s *Service) ensurePortalProjectAccess(scope portalScope, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("project_id is required")
	}
	if scope.AllProjects {
		return nil
	}
	if _, ok := scope.ProjectIDs[projectID]; ok {
		return nil
	}
	return errors.New("portal principal is not allowed to access this project")
}

func (s *Service) portalUsageReport(ctx context.Context, scope portalScope, limit int) (UsageReport, error) {
	if scope.AllProjects || len(scope.ProjectIDs) == 1 {
		query := UsageQuery{Limit: limit}
		if !scope.AllProjects {
			for id := range scope.ProjectIDs {
				query.ProjectID = id
			}
		}
		return s.UsageReportQuery(ctx, query)
	}
	records, err := s.repo.QueryUsageRecords(ctx, UsageQuery{Limit: 500})
	if err != nil {
		return UsageReport{}, err
	}
	filtered := make([]UsageRecord, 0, len(records))
	for _, record := range records {
		if _, ok := scope.ProjectIDs[record.ProjectID]; ok {
			filtered = append(filtered, record)
		}
	}
	aggregate := usageAggregateFromRecords(filtered)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return UsageReport{
		TotalRequests:  aggregate.TotalRequests,
		ErrorRequests:  aggregate.ErrorRequests,
		TotalTokens:    aggregate.TotalTokens,
		TotalCostCents: aggregate.TotalCostCents,
		AvgLatencyMS:   aggregate.AvgLatencyMS,
		ByModel:        aggregate.ByModel,
		Recent:         filtered,
	}, nil
}

func (s *Service) portalGatewayTraces(ctx context.Context, scope portalScope, limit int) ([]GatewayTrace, error) {
	if scope.AllProjects || len(scope.ProjectIDs) == 1 {
		query := GatewayTraceQuery{Limit: limit}
		if !scope.AllProjects {
			for id := range scope.ProjectIDs {
				query.ProjectID = id
			}
		}
		return s.ListGatewayTracesQuery(ctx, query)
	}
	traces, err := s.ListGatewayTracesQuery(ctx, GatewayTraceQuery{Limit: 500})
	if err != nil {
		return nil, err
	}
	filtered := make([]GatewayTrace, 0, len(traces))
	for _, trace := range traces {
		if _, ok := scope.ProjectIDs[trace.ProjectID]; ok {
			filtered = append(filtered, trace)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *Service) portalAlerts(ctx context.Context, scope portalScope, limit int) ([]AlertEvent, error) {
	if scope.AllProjects || len(scope.ProjectIDs) == 1 {
		query := AlertQuery{Limit: limit, Status: AlertStatusActive}
		if !scope.AllProjects {
			for id := range scope.ProjectIDs {
				query.ProjectID = id
			}
		}
		return s.ListAlertEventsQuery(ctx, query)
	}
	alerts, err := s.ListAlertEventsQuery(ctx, AlertQuery{Limit: 500, Status: AlertStatusActive})
	if err != nil {
		return nil, err
	}
	filtered := make([]AlertEvent, 0, len(alerts))
	for _, alert := range alerts {
		if _, ok := scope.ProjectIDs[alert.ProjectID]; ok {
			filtered = append(filtered, alert)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *Service) ensurePortalApplicationAccess(ctx context.Context, scope portalScope, applicationID string) error {
	app, err := s.applicationByID(ctx, applicationID)
	if err != nil {
		return err
	}
	return s.ensurePortalProjectAccess(scope, app.ProjectID)
}

func workspaceUserByActor(users []WorkspaceUser, actor string) (WorkspaceUser, bool) {
	actor = strings.ToLower(strings.TrimSpace(actor))
	for _, user := range users {
		if strings.ToLower(user.ID) == actor || strings.ToLower(user.Email) == actor {
			return user, true
		}
	}
	return WorkspaceUser{}, false
}

func roleCanManageKeys(role string) bool {
	switch role {
	case RoleSuperAdmin, RolePlatformAdmin, RoleProjectAdmin, RoleDeveloper:
		return true
	default:
		return false
	}
}

func isLocalAdminActor(actor string) bool {
	actor = strings.TrimSpace(actor)
	return actor == "" || actor == "local-admin" || actor == "admin"
}

func portalActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "local-admin"
	}
	return "portal:" + actor
}
