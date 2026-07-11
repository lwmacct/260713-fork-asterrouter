import { apiClient } from './client'
import type {
  APIKeyCreateRequest,
  APIKeyCreateResponse,
  APIKeyRecord,
  APIKeyUpdateRequest,
  AlertEvent,
  AlertSummary,
  AuditLog,
  AuditLogSummary,
  CostAllocationReport,
  Department,
  DepartmentRequest,
  Dashboard,
  ExportJob,
  ExportJobKind,
  GatewayPolicyExplanation,
  GatewayTrace,
  GatewayTraceSummary,
  GovernancePolicy,
  GovernancePolicyRequest,
  ModelPricing,
  ModelPricingRequest,
  PortalWorkspace,
  RecordListQuery,
  RoleBinding,
  RoleBindingRequest,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderAccountRequest,
  ProviderHealthCheck,
  ProviderConnection,
  ProviderRequest,
  RoutingGroup,
  RoutingGroupRequest,
  UsageReport,
  WorkspaceUser,
  WorkspaceUserRequest
} from '@/types'

type NullableList<T> = T[] | null | undefined
type ProviderConnectionPayload = Omit<ProviderConnection, 'models'> & { models?: string[] | null }
type ProviderHealthCheckPayload = Omit<ProviderHealthCheck, 'models'> & { models?: string[] | null }
type ProviderAccountPayload = Omit<ProviderAccount, 'models' | 'group_ids'> & {
  models?: string[] | null
  group_ids?: string[] | null
}
type ProviderAccountHealthCheckPayload = Omit<ProviderAccountHealthCheck, 'models'> & { models?: string[] | null }

function listOrEmpty<T>(value: NullableList<T>): T[] {
  return Array.isArray(value) ? value : []
}

function stringListOrEmpty(value: NullableList<string>): string[] {
  return listOrEmpty(value).filter((item): item is string => typeof item === 'string')
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
    group_ids: stringListOrEmpty(account.group_ids)
  }
}

function normalizeProviderAccountHealthCheck(check: ProviderAccountHealthCheckPayload): ProviderAccountHealthCheck {
  return {
    ...check,
    models: stringListOrEmpty(check.models)
  }
}

export async function getDashboard(): Promise<Dashboard> {
  const response = await apiClient.get<Dashboard>('/admin/dashboard')
  return response.data
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
  const response = await apiClient.post<ProviderConnection>('/admin/providers', payload)
  return response.data
}

export async function updateProvider(id: string, payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.put<ProviderConnection>(`/admin/providers/${id}`, payload)
  return response.data
}

export async function checkProvider(id: string): Promise<ProviderHealthCheck> {
  const response = await apiClient.post<ProviderHealthCheck>(`/admin/providers/${id}/check`)
  return response.data
}

export async function getDepartments(): Promise<Department[]> {
  const response = await apiClient.get<Department[]>('/admin/departments')
  return response.data
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
  const response = await apiClient.get<GovernancePolicy[]>('/admin/policies')
  return response.data
}

export async function createGovernancePolicy(payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.post<GovernancePolicy>('/admin/policies', payload)
  return response.data
}

export async function updateGovernancePolicy(id: string, payload: GovernancePolicyRequest): Promise<GovernancePolicy> {
  const response = await apiClient.put<GovernancePolicy>(`/admin/policies/${id}`, payload)
  return response.data
}

export async function getWorkspaceUsers(): Promise<WorkspaceUser[]> {
  const response = await apiClient.get<WorkspaceUser[]>('/admin/users')
  return response.data
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
  const response = await apiClient.get<RoleBinding[]>('/admin/role-bindings')
  return response.data
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
  const response = await apiClient.post<ProviderAccount>('/admin/provider-accounts', payload)
  return response.data
}

export async function updateProviderAccount(id: string, payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.put<ProviderAccount>(`/admin/provider-accounts/${id}`, payload)
  return response.data
}

export async function checkProviderAccount(id: string): Promise<ProviderAccountHealthCheck> {
  const response = await apiClient.post<ProviderAccountHealthCheck>(`/admin/provider-accounts/${id}/check`)
  return response.data
}

export async function getModelPricings(): Promise<ModelPricing[]> {
  const response = await apiClient.get<ModelPricing[]>('/admin/model-pricings')
  return response.data
}

export async function createModelPricing(payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.post<ModelPricing>('/admin/model-pricings', payload)
  return response.data
}

export async function updateModelPricing(id: string, payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.put<ModelPricing>(`/admin/model-pricings/${id}`, payload)
  return response.data
}

export async function getAPIKeys(): Promise<APIKeyRecord[]> {
  const response = await apiClient.get<APIKeyRecord[]>('/admin/api-keys')
  return response.data
}

export async function getAPIKeyPolicyExplanation(id: string): Promise<GatewayPolicyExplanation> {
  const response = await apiClient.get<GatewayPolicyExplanation>(`/admin/api-keys/${id}/policy-explanation`)
  return response.data
}

export async function createAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>('/admin/api-keys', payload)
  return response.data
}

export async function updateAPIKey(id: string, payload: APIKeyUpdateRequest): Promise<APIKeyRecord> {
  const response = await apiClient.put<APIKeyRecord>(`/admin/api-keys/${id}`, payload)
  return response.data
}

export async function rotateAPIKey(id: string): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>(`/admin/api-keys/${id}/rotate`)
  return response.data
}

export async function disableAPIKey(id: string): Promise<void> {
  await apiClient.post(`/admin/api-keys/${id}/disable`)
}

export async function getAuditLogs(params?: RecordListQuery): Promise<AuditLog[]> {
  const response = await apiClient.get<AuditLog[]>('/admin/audit-logs', { params })
  return response.data
}

export async function getAuditLogSummary(params?: RecordListQuery): Promise<AuditLogSummary> {
  const response = await apiClient.get<AuditLogSummary>('/admin/audit-logs/summary', { params })
  return response.data
}

export async function getAlerts(params?: RecordListQuery): Promise<AlertEvent[]> {
  const response = await apiClient.get<AlertEvent[]>('/admin/alerts', { params })
  return response.data
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
  const response = await apiClient.get<UsageReport>('/admin/usage', { params })
  return response.data
}

export async function exportUsageCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/usage/export', `usage-${Date.now()}.csv`, params)
}

export async function getCostAllocationReport(params?: RecordListQuery): Promise<CostAllocationReport> {
  const response = await apiClient.get<CostAllocationReport>('/admin/cost-allocation', { params })
  return response.data
}

export async function exportCostAllocationCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/cost-allocation/export', `cost-allocation-${Date.now()}.csv`, params)
}

export async function getGatewayTraces(params?: RecordListQuery): Promise<GatewayTrace[]> {
  const response = await apiClient.get<GatewayTrace[]>('/admin/gateway-traces', { params })
  return response.data
}

export async function getGatewayTraceSummary(params?: RecordListQuery): Promise<GatewayTraceSummary> {
  const response = await apiClient.get<GatewayTraceSummary>('/admin/gateway-traces/summary', { params })
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
  const response = await apiClient.get<ExportJob[]>('/admin/export-jobs', { params: { limit } })
  return response.data
}

export async function getExportJob(id: string): Promise<ExportJob> {
  const response = await apiClient.get<ExportJob>(`/admin/export-jobs/${id}`)
  return response.data
}

export async function downloadExportJob(job: ExportJob): Promise<void> {
  await downloadCSV(`/admin/export-jobs/${job.id}/download`, job.filename)
}

export async function getPortalWorkspace(): Promise<PortalWorkspace> {
  const response = await apiClient.get<PortalWorkspace>('/portal/workspace')
  return response.data
}

export async function createPortalAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>('/portal/api-keys', payload)
  return response.data
}

export async function rotatePortalAPIKey(id: string): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>(`/portal/api-keys/${id}/rotate`)
  return response.data
}

export async function disablePortalAPIKey(id: string): Promise<void> {
  await apiClient.post(`/portal/api-keys/${id}/disable`)
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
