import { apiClient } from './client'
import {
  listOrEmpty,
  normalizeAIJobAdminDetail,
  normalizeAPIKeyCreateResponse,
  normalizeAPIKeyRecord,
  normalizeArtifactAdminDetail,
  normalizeDashboard,
  normalizeEffectivePricingDecision,
  normalizeEffectivePricingDecisionEvaluation,
  normalizeEffectivePricingReport,
  normalizeGatewayPolicyExplanation,
  normalizeProviderBillingSource,
  normalizeProviderBillingSourceInspection,
  normalizeProviderBillingSyncResult,
  normalizeUsageReport,
  stringListOrEmpty,
  type AIJobAdminDetailPayload,
  type APIKeyCreateResponsePayload,
  type APIKeyRecordPayload,
  type ArtifactAdminDetailPayload,
  type DashboardPayload,
  type EffectivePricingDecisionEvaluationPayload,
  type EffectivePricingDecisionPayload,
  type EffectivePricingReportPayload,
  type ProviderBillingSourceInspectionPayload,
  type ProviderBillingSourcePayload,
  type ProviderBillingSyncResultPayload,
  type UsageReportPayload
} from './normalizers'
import type {
  APIKeyCreateRequest,
  APIKeyCreateResponse,
  APIKeyRecord,
  APIKeyUpdateRequest,
  AIAttemptReconcileScheduleResult,
  AIJobAdminActionResult,
  AIJobAdminDetail,
  AIJobAdminRecord,
  AIJobListQuery,
  AIJobRuntimeStatus,
  AIJobSummary,
  ArtifactAdminDetail,
  ArtifactAdminRecord,
  ArtifactDeliveryRetryResult,
  ArtifactListQuery,
  ArtifactRuntime,
  ArtifactSummary,
  AlertEvent,
  AlertSummary,
  AuditLog,
  AuditLogSummary,
  CacheProbeRequest,
  CostAllocationReport,
  Department,
  DepartmentRequest,
  Dashboard,
  EffectivePricingDecision,
  EffectivePricingDecisionEvaluation,
  EffectivePricingDecisionEvaluationRequest,
  EffectivePricingPolicy,
  EffectivePricingPolicyRequest,
  EffectivePricingReport,
  ExportJob,
  ExportJobKind,
  GatewayPolicyExplanation,
	GatewayModel,
	GatewayModelRequest,
	GatewaySimulation,
  GatewayTrace,
  GatewayTraceSummary,
  GovernancePolicy,
  GovernancePolicyRequest,
  ModelPricing,
  ModelPricingRequest,
	OrganizationGroup,
	OrganizationGroupRequest,
	ModelRoute,
	ModelRouteBulkCreateRequest,
	ModelRouteBulkCreateResult,
	ModelRouteRequest,
  PortalWorkspace,
  RecordListQuery,
  RoleBinding,
  RoleBindingRequest,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderAccountModelDiscovery,
  ProviderAccountModelInventory,
  ProviderAccountModelSyncRequest,
  ProviderAccountModelSyncResult,
  ProviderAccountRequest,
  ProviderBillingLine,
  ProviderBillingLineRequest,
  ProviderBillingSource,
  ProviderBillingSourceEvidence,
  ProviderBillingSourceInspection,
  ProviderBillingSourceRequest,
  ProviderBillingSyncResult,
  ProviderCacheCapability,
  ProviderCacheCapabilityRequest,
  ProviderCacheProbeRun,
  ProviderHealthCheck,
  ProviderConnection,
  ProviderRequest,
  ProcurementPrice,
  ProcurementPriceRequest,
  RoutingGroup,
  RoutingGroupRequest,
  UsageReport,
  WorkspaceUser,
  WorkspaceUserRequest
} from '@/types'

type ProviderConnectionPayload = Omit<ProviderConnection, 'models'> & { models?: string[] | null }
type ProviderHealthCheckPayload = Omit<ProviderHealthCheck, 'models'> & { models?: string[] | null }
type ProviderAccountPayload = Omit<ProviderAccount, 'models' | 'group_ids' | 'temp_unschedulable_rules'> & {
  models?: string[] | null
  group_ids?: string[] | null
  temp_unschedulable_rules?: ProviderAccount['temp_unschedulable_rules'] | null
}
type ProviderAccountHealthCheckPayload = Omit<ProviderAccountHealthCheck, 'models'> & { models?: string[] | null }
type OrganizationGroupPayload = Omit<OrganizationGroup, 'member_ids'> & { member_ids?: string[] | null }
type GovernancePolicyPayload = Omit<GovernancePolicy, 'model_allowlist' | 'model_denylist'> & {
  model_allowlist?: string[] | null
  model_denylist?: string[] | null
}
type GatewaySimulationPayload = Omit<GatewaySimulation, 'candidates'> & { candidates?: GatewaySimulation['candidates'] | null }
type CostAllocationReportPayload = Omit<CostAllocationReport, 'rows'> & { rows?: CostAllocationReport['rows'] | null }
type ProviderBillingSourceEvidencePayload = Omit<ProviderBillingSourceEvidence, 'source' | 'runs' | 'balances' | 'aggregates'> & {
  source: ProviderBillingSourcePayload
  runs?: ProviderBillingSourceEvidence['runs'] | null
  balances?: ProviderBillingSourceEvidence['balances'] | null
  aggregates?: ProviderBillingSourceEvidence['aggregates'] | null
}

function normalizeProvider(provider: ProviderConnectionPayload): ProviderConnection {
  return {
    ...provider,
    models: stringListOrEmpty(provider.models)
  }
}

function normalizeProviderHealthCheck(check: ProviderHealthCheckPayload): ProviderHealthCheck {
  return {
    ...check,
    models: stringListOrEmpty(check.models)
  }
}

function normalizeProviderAccount(account: ProviderAccountPayload): ProviderAccount {
  return {
    ...account,
    models: stringListOrEmpty(account.models),
    auto_enable_new_models: account.auto_enable_new_models === true,
    group_ids: stringListOrEmpty(account.group_ids),
    temp_unschedulable_rules: listOrEmpty(account.temp_unschedulable_rules)
  }
}

function normalizeProviderAccountHealthCheck(check: ProviderAccountHealthCheckPayload): ProviderAccountHealthCheck {
  return {
    ...check,
    models: stringListOrEmpty(check.models)
  }
}

function normalizeOrganizationGroup(group: OrganizationGroupPayload): OrganizationGroup {
  return { ...group, member_ids: stringListOrEmpty(group.member_ids) }
}

function normalizeGovernancePolicy(policy: GovernancePolicyPayload): GovernancePolicy {
  return {
    ...policy,
    model_allowlist: stringListOrEmpty(policy.model_allowlist),
    model_denylist: stringListOrEmpty(policy.model_denylist)
  }
}

export async function getDashboard(): Promise<Dashboard> {
  const response = await apiClient.get<DashboardPayload>('/admin/dashboard')
  return normalizeDashboard(response.data)
}

export async function getProviders(): Promise<ProviderConnection[]> {
  const response = await apiClient.get<ProviderConnectionPayload[] | null>('/admin/providers')
  return listOrEmpty(response.data).map(normalizeProvider)
}

export async function getProviderHealthChecks(): Promise<ProviderHealthCheck[]> {
  const response = await apiClient.get<ProviderHealthCheckPayload[] | null>('/admin/provider-health-checks')
  return listOrEmpty(response.data).map(normalizeProviderHealthCheck)
}

export async function createProvider(payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.post<ProviderConnectionPayload>('/admin/providers', payload)
  return normalizeProvider(response.data)
}

export async function updateProvider(id: string, payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.put<ProviderConnectionPayload>(`/admin/providers/${id}`, payload)
  return normalizeProvider(response.data)
}

export async function checkProvider(id: string): Promise<ProviderHealthCheck> {
  const response = await apiClient.post<ProviderHealthCheckPayload>(`/admin/providers/${id}/check`)
  return normalizeProviderHealthCheck(response.data)
}

export async function getDepartments(): Promise<Department[]> {
  const response = await apiClient.get<Department[] | null>('/admin/departments')
  return listOrEmpty(response.data)
}

export async function getOrganizationGroups(): Promise<OrganizationGroup[]> {
		const response = await apiClient.get<OrganizationGroupPayload[] | null>('/admin/organization-groups')
		return listOrEmpty(response.data).map(normalizeOrganizationGroup)
}

export async function createOrganizationGroup(payload: OrganizationGroupRequest): Promise<OrganizationGroup> {
	return normalizeOrganizationGroup((await apiClient.post<OrganizationGroupPayload>('/admin/organization-groups', payload)).data)
}

export async function updateOrganizationGroup(id: string, payload: OrganizationGroupRequest): Promise<OrganizationGroup> {
	return normalizeOrganizationGroup((await apiClient.put<OrganizationGroupPayload>(`/admin/organization-groups/${id}`, payload)).data)
}

export async function deleteOrganizationGroup(id: string): Promise<void> {
	await apiClient.delete(`/admin/organization-groups/${id}`)
}

export async function createDepartment(payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.post<Department>('/admin/departments', payload)
  return response.data
}

export async function updateDepartment(id: string, payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.put<Department>(`/admin/departments/${id}`, payload)
  return response.data
}

export async function getGovernancePolicies(): Promise<GovernancePolicy[]> {
  const response = await apiClient.get<GovernancePolicyPayload[] | null>('/admin/policies')
  return listOrEmpty(response.data).map(normalizeGovernancePolicy)
}

export async function createGovernancePolicy(payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.post<GovernancePolicyPayload>('/admin/policies', payload)
  return normalizeGovernancePolicy(response.data)
}

export async function updateGovernancePolicy(id: string, payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.put<GovernancePolicyPayload>(`/admin/policies/${id}`, payload)
  return normalizeGovernancePolicy(response.data)
}

export async function getWorkspaceUsers(): Promise<WorkspaceUser[]> {
  const response = await apiClient.get<WorkspaceUser[] | null>('/admin/users')
  return listOrEmpty(response.data)
}

export async function createWorkspaceUser(payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.post<WorkspaceUser>('/admin/users', payload)
  return response.data
}

export async function updateWorkspaceUser(id: string, payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.put<WorkspaceUser>(`/admin/users/${id}`, payload)
  return response.data
}

export async function getRoleBindings(): Promise<RoleBinding[]> {
  const response = await apiClient.get<RoleBinding[] | null>('/admin/role-bindings')
  return listOrEmpty(response.data)
}

export async function createRoleBinding(payload: RoleBindingRequest): Promise<RoleBinding> {
  const response = await apiClient.post<RoleBinding>('/admin/role-bindings', payload)
  return response.data
}

export async function deleteRoleBinding(id: string): Promise<void> {
  await apiClient.delete(`/admin/role-bindings/${id}`)
}

export async function getRoutingGroups(): Promise<RoutingGroup[]> {
  const response = await apiClient.get<RoutingGroup[] | null>('/admin/routing-groups')
  return listOrEmpty(response.data)
}

export async function createRoutingGroup(payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.post<RoutingGroup>('/admin/routing-groups', payload)
  return response.data
}

export async function updateRoutingGroup(id: string, payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.put<RoutingGroup>(`/admin/routing-groups/${id}`, payload)
  return response.data
}

export async function getProviderAccounts(): Promise<ProviderAccount[]> {
  const response = await apiClient.get<ProviderAccountPayload[] | null>('/admin/provider-accounts')
  return listOrEmpty(response.data).map(normalizeProviderAccount)
}

export async function getProviderAccountHealthChecks(): Promise<ProviderAccountHealthCheck[]> {
  const response = await apiClient.get<ProviderAccountHealthCheckPayload[] | null>('/admin/provider-account-health-checks')
  return listOrEmpty(response.data).map(normalizeProviderAccountHealthCheck)
}

export async function createProviderAccount(payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.post<ProviderAccountPayload>('/admin/provider-accounts', payload)
  return normalizeProviderAccount(response.data)
}

export async function updateProviderAccount(id: string, payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.put<ProviderAccountPayload>(`/admin/provider-accounts/${id}`, payload)
  return normalizeProviderAccount(response.data)
}

export async function checkProviderAccount(id: string): Promise<ProviderAccountHealthCheck> {
  const response = await apiClient.post<ProviderAccountHealthCheckPayload>(`/admin/provider-accounts/${id}/check`)
  return normalizeProviderAccountHealthCheck(response.data)
}

export async function getProviderAccountModelInventory(id: string): Promise<ProviderAccountModelInventory> {
  const response = await apiClient.get<ProviderAccountModelInventory>(`/admin/provider-accounts/${id}/models`)
  return { ...response.data, models: listOrEmpty(response.data.models) }
}

export async function discoverProviderAccountModels(id: string): Promise<ProviderAccountModelDiscovery> {
  const response = await apiClient.post<ProviderAccountModelDiscovery>(`/admin/provider-accounts/${id}/models/discover`)
  return {
    ...response.data,
    models: listOrEmpty(response.data.models),
    added_models: stringListOrEmpty(response.data.added_models),
    missing_models: stringListOrEmpty(response.data.missing_models),
    unchanged_models: stringListOrEmpty(response.data.unchanged_models),
    affected_route_ids: stringListOrEmpty(response.data.affected_route_ids)
  }
}

export async function syncProviderAccountModels(id: string, payload: ProviderAccountModelSyncRequest): Promise<ProviderAccountModelSyncResult> {
  const response = await apiClient.post<ProviderAccountModelSyncResult>(`/admin/provider-accounts/${id}/models/sync`, payload)
  return {
    ...response.data,
    account: normalizeProviderAccount(response.data.account),
    inventory: {
      ...response.data.inventory,
      models: listOrEmpty(response.data.inventory.models)
    },
    discovery: {
      ...response.data.discovery,
      models: listOrEmpty(response.data.discovery.models),
      added_models: stringListOrEmpty(response.data.discovery.added_models),
      missing_models: stringListOrEmpty(response.data.discovery.missing_models),
      unchanged_models: stringListOrEmpty(response.data.discovery.unchanged_models),
      affected_route_ids: stringListOrEmpty(response.data.discovery.affected_route_ids)
    }
  }
}

export async function clearProviderAccountCooldown(id: string): Promise<ProviderAccount> {
  const response = await apiClient.post<ProviderAccountPayload>(`/admin/provider-accounts/${id}/clear-cooldown`)
  return normalizeProviderAccount(response.data)
}

export async function getGatewayModels(): Promise<GatewayModel[]> {
  const response = await apiClient.get<GatewayModel[] | null>('/admin/gateway-models')
  return listOrEmpty(response.data)
}

export async function createGatewayModel(payload: GatewayModelRequest): Promise<GatewayModel> {
  const response = await apiClient.post<GatewayModel>('/admin/gateway-models', payload)
  return response.data
}

export async function updateGatewayModel(id: string, payload: GatewayModelRequest): Promise<GatewayModel> {
  const response = await apiClient.put<GatewayModel>(`/admin/gateway-models/${id}`, payload)
  return response.data
}

export async function deleteGatewayModel(id: string): Promise<void> {
  await apiClient.delete(`/admin/gateway-models/${id}`)
}

export async function getModelRoutes(): Promise<ModelRoute[]> {
  const response = await apiClient.get<ModelRoute[] | null>('/admin/model-routes')
  return listOrEmpty(response.data)
}

export async function createModelRoute(payload: ModelRouteRequest): Promise<ModelRoute> {
  const response = await apiClient.post<ModelRoute>('/admin/model-routes', payload)
  return response.data
}

export async function bulkCreateModelRoutes(payload: ModelRouteBulkCreateRequest): Promise<ModelRouteBulkCreateResult> {
  const response = await apiClient.post<Omit<ModelRouteBulkCreateResult, 'routes'> & { routes?: ModelRoute[] | null }>('/admin/model-routes/bulk', payload)
  return { ...response.data, routes: listOrEmpty(response.data.routes) }
}

export async function updateModelRoute(id: string, payload: ModelRouteRequest): Promise<ModelRoute> {
  const response = await apiClient.put<ModelRoute>(`/admin/model-routes/${id}`, payload)
  return response.data
}

export async function deleteModelRoute(id: string): Promise<void> {
  await apiClient.delete(`/admin/model-routes/${id}`)
}

export async function simulateGatewayRouting(model: string, estimatedTokens: number): Promise<GatewaySimulation> {
  const response = await apiClient.post<GatewaySimulationPayload>('/admin/gateway-simulator', {
    model,
    estimated_tokens: estimatedTokens
  })
  return { ...response.data, candidates: listOrEmpty(response.data.candidates) }
}

export async function getModelPricings(): Promise<ModelPricing[]> {
  const response = await apiClient.get<ModelPricing[] | null>('/admin/model-pricings')
  return listOrEmpty(response.data)
}

export async function createModelPricing(payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.post<ModelPricing>('/admin/model-pricings', payload)
  return response.data
}

export async function updateModelPricing(id: string, payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.put<ModelPricing>(`/admin/model-pricings/${id}`, payload)
  return response.data
}

export async function getEffectivePricingReport(params?: { model?: string; protocol?: string; window_hours?: number }): Promise<EffectivePricingReport> {
  const response = await apiClient.get<EffectivePricingReportPayload>('/admin/effective-pricing/report', { params })
  return normalizeEffectivePricingReport(response.data)
}

export async function getEffectivePricingPolicy(): Promise<EffectivePricingPolicy> {
  const response = await apiClient.get<EffectivePricingPolicy>('/admin/effective-pricing/policy')
  return response.data
}

export async function updateEffectivePricingPolicy(payload: EffectivePricingPolicyRequest): Promise<EffectivePricingPolicy> {
  const response = await apiClient.put<EffectivePricingPolicy>('/admin/effective-pricing/policy', payload)
  return response.data
}

export async function getProcurementPrices(): Promise<ProcurementPrice[]> {
  const response = await apiClient.get<ProcurementPrice[] | null>('/admin/procurement-prices')
  return listOrEmpty(response.data)
}

export async function createProcurementPrice(payload: ProcurementPriceRequest): Promise<ProcurementPrice> {
  const response = await apiClient.post<ProcurementPrice>('/admin/procurement-prices', payload)
  return response.data
}

export async function updateProcurementPrice(id: string, payload: ProcurementPriceRequest): Promise<ProcurementPrice> {
  const response = await apiClient.put<ProcurementPrice>(`/admin/procurement-prices/${id}`, payload)
  return response.data
}

export async function getProviderBillingLines(): Promise<ProviderBillingLine[]> {
  const response = await apiClient.get<ProviderBillingLine[] | null>('/admin/provider-billing-lines')
  return listOrEmpty(response.data)
}

export async function createProviderBillingLine(payload: ProviderBillingLineRequest): Promise<ProviderBillingLine> {
  const response = await apiClient.post<ProviderBillingLine>('/admin/provider-billing-lines', payload)
  return response.data
}

export async function inspectProviderBillingSource(providerAccountID: string, adapterID = 'auto'): Promise<ProviderBillingSourceInspection> {
  const response = await apiClient.post<ProviderBillingSourceInspectionPayload>('/admin/provider-billing-sources/inspect', {
    provider_account_id: providerAccountID,
    adapter_id: adapterID
  })
  return normalizeProviderBillingSourceInspection(response.data)
}

export async function getProviderBillingSources(): Promise<ProviderBillingSource[]> {
  const response = await apiClient.get<ProviderBillingSourcePayload[] | null>('/admin/provider-billing-sources')
  return listOrEmpty(response.data).map(normalizeProviderBillingSource)
}

export async function updateProviderBillingSource(payload: ProviderBillingSourceRequest): Promise<ProviderBillingSource> {
  const response = await apiClient.put<ProviderBillingSourcePayload>('/admin/provider-billing-sources', payload)
  return normalizeProviderBillingSource(response.data)
}

export async function syncProviderBillingSource(id: string): Promise<ProviderBillingSyncResult> {
  const response = await apiClient.post<ProviderBillingSyncResultPayload>(`/admin/provider-billing-sources/${id}/sync`)
  return normalizeProviderBillingSyncResult(response.data)
}

export async function getProviderBillingSourceEvidence(id: string, limit = 100): Promise<ProviderBillingSourceEvidence> {
  const response = await apiClient.get<ProviderBillingSourceEvidencePayload>(`/admin/provider-billing-sources/${id}/evidence`, { params: { limit } })
  return {
    ...response.data,
    source: normalizeProviderBillingSource(response.data.source),
    runs: listOrEmpty(response.data.runs),
    balances: listOrEmpty(response.data.balances),
    aggregates: listOrEmpty(response.data.aggregates)
  }
}

export async function getProviderCacheCapabilities(): Promise<ProviderCacheCapability[]> {
  const response = await apiClient.get<ProviderCacheCapability[] | null>('/admin/provider-cache-capabilities')
  return listOrEmpty(response.data)
}

export async function updateProviderCacheCapability(payload: ProviderCacheCapabilityRequest): Promise<ProviderCacheCapability> {
  const response = await apiClient.put<ProviderCacheCapability>('/admin/provider-cache-capabilities', payload)
  return response.data
}

export async function getProviderCacheProbeRuns(limit = 100): Promise<ProviderCacheProbeRun[]> {
  const response = await apiClient.get<ProviderCacheProbeRun[] | null>('/admin/provider-cache-probes', { params: { limit } })
  return listOrEmpty(response.data)
}

export async function runProviderCacheProbe(payload: CacheProbeRequest): Promise<ProviderCacheProbeRun> {
  const response = await apiClient.post<ProviderCacheProbeRun>('/admin/provider-cache-probes', payload)
  return response.data
}

export async function getEffectivePricingDecisions(): Promise<EffectivePricingDecision[]> {
  const response = await apiClient.get<EffectivePricingDecisionPayload[] | null>('/admin/effective-pricing/decisions')
  return listOrEmpty(response.data).map(normalizeEffectivePricingDecision)
}

export async function getEffectivePricingDecisionEvaluations(id: string, limit = 100): Promise<EffectivePricingDecisionEvaluation[]> {
  const response = await apiClient.get<EffectivePricingDecisionEvaluationPayload[] | null>(`/admin/effective-pricing/decisions/${id}/evaluations`, { params: { limit } })
  return listOrEmpty(response.data).map(normalizeEffectivePricingDecisionEvaluation)
}

export async function evaluateEffectivePricingDecision(payload: EffectivePricingDecisionEvaluationRequest): Promise<EffectivePricingDecision> {
  const response = await apiClient.post<EffectivePricingDecisionPayload>('/admin/effective-pricing/decisions/evaluate', payload)
  return normalizeEffectivePricingDecision(response.data)
}

export async function actOnEffectivePricingDecision(id: string, action: string, canaryPercent = 0): Promise<EffectivePricingDecision> {
  const response = await apiClient.post<EffectivePricingDecisionPayload>(`/admin/effective-pricing/decisions/${id}/action`, { action, canary_percent: canaryPercent })
  return normalizeEffectivePricingDecision(response.data)
}

export async function getAPIKeys(): Promise<APIKeyRecord[]> {
  const response = await apiClient.get<APIKeyRecord[] | null>('/admin/api-keys')
  return listOrEmpty(response.data).map((record) => normalizeAPIKeyRecord(record as APIKeyRecordPayload))
}

export async function getAPIKeyPolicyExplanation(id: string): Promise<GatewayPolicyExplanation> {
  const response = await apiClient.get<GatewayPolicyExplanation>(`/admin/api-keys/${id}/policy-explanation`)
  return normalizeGatewayPolicyExplanation(response.data)
}

export async function createAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponsePayload>('/admin/api-keys', payload)
  return normalizeAPIKeyCreateResponse(response.data)
}

export async function updateAPIKey(id: string, payload: APIKeyUpdateRequest): Promise<APIKeyRecord> {
  const response = await apiClient.put<APIKeyRecordPayload>(`/admin/api-keys/${id}`, payload)
  return normalizeAPIKeyRecord(response.data)
}

export async function rotateAPIKey(id: string, gracePeriodSeconds = 0): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>(`/admin/api-keys/${id}/rotate`, { grace_period_seconds: gracePeriodSeconds })
  return normalizeAPIKeyCreateResponse(response.data)
}

export async function disableAPIKey(id: string): Promise<void> {
  await apiClient.post(`/admin/api-keys/${id}/disable`)
}

export async function getAuditLogs(params?: RecordListQuery): Promise<AuditLog[]> {
  const response = await apiClient.get<AuditLog[] | null>('/admin/audit-logs', { params })
  return listOrEmpty(response.data)
}

export async function getAuditLogSummary(params?: RecordListQuery): Promise<AuditLogSummary> {
  const response = await apiClient.get<AuditLogSummary>('/admin/audit-logs/summary', { params })
  return response.data
}

export async function getAlerts(params?: RecordListQuery): Promise<AlertEvent[]> {
  const response = await apiClient.get<AlertEvent[] | null>('/admin/alerts', { params })
  return listOrEmpty(response.data)
}

export async function getAlertSummary(params?: RecordListQuery): Promise<AlertSummary> {
  const response = await apiClient.get<AlertSummary>('/admin/alerts/summary', { params })
  return response.data
}

export async function acknowledgeAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/admin/alerts/${id}/acknowledge`)
  return response.data
}

export async function resolveAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/admin/alerts/${id}/resolve`)
  return response.data
}

export async function exportAuditLogsCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/audit-logs/export', `audit-${Date.now()}.csv`, params)
}

export async function getUsageReport(params?: RecordListQuery): Promise<UsageReport> {
  const response = await apiClient.get<UsageReportPayload>('/admin/usage', { params })
  return normalizeUsageReport(response.data)
}

export async function exportUsageCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/usage/export', `usage-${Date.now()}.csv`, params)
}

export async function getCostAllocationReport(params?: RecordListQuery): Promise<CostAllocationReport> {
  const response = await apiClient.get<CostAllocationReportPayload>('/admin/cost-allocation', { params })
  return { ...response.data, rows: listOrEmpty(response.data.rows) }
}

export async function exportCostAllocationCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/cost-allocation/export', `cost-allocation-${Date.now()}.csv`, params)
}

export async function getGatewayTraces(params?: RecordListQuery): Promise<GatewayTrace[]> {
  const response = await apiClient.get<GatewayTrace[] | null>('/admin/gateway-traces', { params })
  return listOrEmpty(response.data)
}

export async function getGatewayTraceSummary(params?: RecordListQuery): Promise<GatewayTraceSummary> {
  const response = await apiClient.get<GatewayTraceSummary>('/admin/gateway-traces/summary', { params })
  return response.data
}

export async function getArtifacts(params?: ArtifactListQuery): Promise<ArtifactAdminRecord[]> {
  const response = await apiClient.get<ArtifactAdminRecord[] | null>('/admin/artifacts', { params })
  return listOrEmpty(response.data)
}

export async function getArtifactSummary(params?: ArtifactListQuery): Promise<ArtifactSummary> {
  const response = await apiClient.get<ArtifactSummary>('/admin/artifacts/summary', { params })
  return response.data
}

export async function getArtifact(id: string): Promise<ArtifactAdminDetail> {
  const response = await apiClient.get<ArtifactAdminDetailPayload>(`/admin/artifacts/${id}`)
  return normalizeArtifactAdminDetail(response.data)
}

export async function getArtifactRuntimes(): Promise<ArtifactRuntime[]> {
  const response = await apiClient.get<ArtifactRuntime[] | null>('/admin/artifact-runtimes')
  return listOrEmpty(response.data)
}

export async function retryArtifactDelivery(id: string): Promise<ArtifactDeliveryRetryResult> {
  const response = await apiClient.post<ArtifactDeliveryRetryResult>(`/admin/artifacts/${id}/retry-delivery`)
  return response.data
}

export async function getAIJobs(params?: AIJobListQuery): Promise<AIJobAdminRecord[]> {
  const response = await apiClient.get<AIJobAdminRecord[] | null>('/admin/ai-jobs', { params })
  return listOrEmpty(response.data)
}

export async function getAIJobSummary(params?: AIJobListQuery): Promise<AIJobSummary> {
  const response = await apiClient.get<AIJobSummary>('/admin/ai-jobs/summary', { params })
  return response.data
}

export async function getAIJobRuntime(): Promise<AIJobRuntimeStatus> {
  const response = await apiClient.get<AIJobRuntimeStatus>('/admin/ai-jobs/runtime')
  return response.data
}

export async function getAIJob(id: string): Promise<AIJobAdminDetail> {
  const response = await apiClient.get<AIJobAdminDetailPayload>(`/admin/ai-jobs/${id}`)
  return normalizeAIJobAdminDetail(response.data)
}

export async function cancelAIJob(id: string): Promise<AIJobAdminActionResult> {
  const response = await apiClient.post<AIJobAdminActionResult>(`/admin/ai-jobs/${id}/cancel`)
  return response.data
}

export async function scheduleAIJobAttemptReconciliation(jobID: string, attemptID: string): Promise<AIAttemptReconcileScheduleResult> {
  const response = await apiClient.post<AIAttemptReconcileScheduleResult>(`/admin/ai-jobs/${jobID}/attempts/${attemptID}/reconcile`)
  return response.data
}

export async function exportGatewayTracesCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/gateway-traces/export', `gateway-traces-${Date.now()}.csv`, params)
}

export async function createExportJob(kind: ExportJobKind, params?: RecordListQuery): Promise<ExportJob> {
  const response = await apiClient.post<ExportJob>('/admin/export-jobs', null, { params: { ...params, kind } })
  return response.data
}

export async function getExportJobs(limit = 50): Promise<ExportJob[]> {
  const response = await apiClient.get<ExportJob[] | null>('/admin/export-jobs', { params: { limit } })
  return listOrEmpty(response.data)
}

export async function getExportJob(id: string): Promise<ExportJob> {
  const response = await apiClient.get<ExportJob>(`/admin/export-jobs/${id}`)
  return response.data
}

export async function downloadExportJob(job: ExportJob): Promise<void> {
  await downloadCSV(`/admin/export-jobs/${job.id}/download`, job.filename)
}

export async function getPortalWorkspace(): Promise<PortalWorkspace> {
		const response = await apiClient.get<PortalWorkspace>(`${selfServiceAPIBase()}/workspace`)
		const payload = response.data ?? {} as PortalWorkspace
		return {
			...payload,
			api_keys: listOrEmpty(payload.api_keys).map((record) => normalizeAPIKeyRecord(record as APIKeyRecordPayload)),
			usage: normalizeUsageReport(payload.usage as UsageReportPayload),
			recent_traces: listOrEmpty(payload.recent_traces),
			alerts: listOrEmpty(payload.alerts),
			models: stringListOrEmpty(payload.models)
		}
}

export async function createPortalAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>(`${selfServiceAPIBase()}/api-keys`, payload)
		return normalizeAPIKeyCreateResponse(response.data)
}

export async function rotatePortalAPIKey(id: string, gracePeriodSeconds = 0): Promise<APIKeyCreateResponse> {
		const response = await apiClient.post<APIKeyCreateResponsePayload>(`${selfServiceAPIBase()}/api-keys/${id}/rotate`, { grace_period_seconds: gracePeriodSeconds })
		return normalizeAPIKeyCreateResponse(response.data)
}

export async function disablePortalAPIKey(id: string): Promise<void> {
	await apiClient.post(`${selfServiceAPIBase()}/api-keys/${id}/disable`)
}

function selfServiceAPIBase(): '/portal' | '/customer' {
	return window.location.pathname.startsWith('/customer') ? '/customer' : '/portal'
}

async function downloadCSV(path: string, filename: string, params?: RecordListQuery): Promise<void> {
  const response = await apiClient.get<Blob>(path, { params, responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
