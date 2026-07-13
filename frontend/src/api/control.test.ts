import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as control from './control'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('@/api/client', () => ({ apiClient: client }))

type ClientMethod = keyof typeof client

describe('control API contracts', () => {
  beforeEach(() => {
    for (const method of Object.values(client)) method.mockReset()
    client.get.mockResolvedValue({ data: [] })
    client.post.mockResolvedValue({ data: {} })
    client.put.mockResolvedValue({ data: {} })
    client.delete.mockResolvedValue({ data: {} })
    window.history.replaceState({}, '', '/admin/dashboard')
  })

  it('normalizes nullable provider and routing collections', async () => {
    client.get.mockResolvedValueOnce({ data: [{ id: 'provider-1', models: null }] })
    expect(await control.getProviders()).toEqual([{ id: 'provider-1', models: [] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-1', models: ['model', 1, null] }] })
    expect(await control.getProviderHealthChecks()).toEqual([{ id: 'check-1', models: ['model'] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'account-1', models: null, group_ids: null, temp_unschedulable_rules: null }] })
    expect(await control.getProviderAccounts()).toEqual([{ id: 'account-1', models: [], group_ids: [], temp_unschedulable_rules: [] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-2', models: null }] })
    expect(await control.getProviderAccountHealthChecks()).toEqual([{ id: 'check-2', models: [] }])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getRoutingGroups()).toEqual([])
    client.get.mockResolvedValueOnce({ data: undefined })
    expect(await control.getGatewayModels()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getModelRoutes()).toEqual([])
  })

  it('uses admin CRUD endpoint contracts', async () => {
    const payload = { synthetic: true } as never
    const cases: Array<{ run: () => Promise<unknown>; method: ClientMethod; args: unknown[] }> = [
      { run: () => control.getDashboard(), method: 'get', args: ['/admin/dashboard'] },
      { run: () => control.getProviders(), method: 'get', args: ['/admin/providers'] },
      { run: () => control.getProviderHealthChecks(), method: 'get', args: ['/admin/provider-health-checks'] },
      { run: () => control.createProvider(payload), method: 'post', args: ['/admin/providers', payload] },
      { run: () => control.updateProvider('provider-1', payload), method: 'put', args: ['/admin/providers/provider-1', payload] },
      { run: () => control.checkProvider('provider-1'), method: 'post', args: ['/admin/providers/provider-1/check'] },
      { run: () => control.getDepartments(), method: 'get', args: ['/admin/departments'] },
      { run: () => control.createDepartment(payload), method: 'post', args: ['/admin/departments', payload] },
      { run: () => control.updateDepartment('department-1', payload), method: 'put', args: ['/admin/departments/department-1', payload] },
      { run: () => control.getOrganizationGroups(), method: 'get', args: ['/admin/organization-groups'] },
      { run: () => control.createOrganizationGroup(payload), method: 'post', args: ['/admin/organization-groups', payload] },
      { run: () => control.updateOrganizationGroup('organization-1', payload), method: 'put', args: ['/admin/organization-groups/organization-1', payload] },
      { run: () => control.deleteOrganizationGroup('organization-1'), method: 'delete', args: ['/admin/organization-groups/organization-1'] },
      { run: () => control.getGovernancePolicies(), method: 'get', args: ['/admin/policies'] },
      { run: () => control.createGovernancePolicy(payload), method: 'post', args: ['/admin/policies', payload] },
      { run: () => control.updateGovernancePolicy('policy-1', payload), method: 'put', args: ['/admin/policies/policy-1', payload] },
      { run: () => control.getWorkspaceUsers(), method: 'get', args: ['/admin/users'] },
      { run: () => control.createWorkspaceUser(payload), method: 'post', args: ['/admin/users', payload] },
      { run: () => control.updateWorkspaceUser('user-1', payload), method: 'put', args: ['/admin/users/user-1', payload] },
      { run: () => control.getRoleBindings(), method: 'get', args: ['/admin/role-bindings'] },
      { run: () => control.createRoleBinding(payload), method: 'post', args: ['/admin/role-bindings', payload] },
      { run: () => control.deleteRoleBinding('binding-1'), method: 'delete', args: ['/admin/role-bindings/binding-1'] },
      { run: () => control.getRoutingGroups(), method: 'get', args: ['/admin/routing-groups'] },
      { run: () => control.createRoutingGroup(payload), method: 'post', args: ['/admin/routing-groups', payload] },
      { run: () => control.updateRoutingGroup('group-1', payload), method: 'put', args: ['/admin/routing-groups/group-1', payload] },
      { run: () => control.getProviderAccounts(), method: 'get', args: ['/admin/provider-accounts'] },
      { run: () => control.getProviderAccountHealthChecks(), method: 'get', args: ['/admin/provider-account-health-checks'] },
      { run: () => control.createProviderAccount(payload), method: 'post', args: ['/admin/provider-accounts', payload] },
      { run: () => control.updateProviderAccount('account-1', payload), method: 'put', args: ['/admin/provider-accounts/account-1', payload] },
      { run: () => control.checkProviderAccount('account-1'), method: 'post', args: ['/admin/provider-accounts/account-1/check'] },
      { run: () => control.clearProviderAccountCooldown('account-1'), method: 'post', args: ['/admin/provider-accounts/account-1/clear-cooldown'] },
      { run: () => control.getGatewayModels(), method: 'get', args: ['/admin/gateway-models'] },
      { run: () => control.createGatewayModel(payload), method: 'post', args: ['/admin/gateway-models', payload] },
      { run: () => control.updateGatewayModel('model-1', payload), method: 'put', args: ['/admin/gateway-models/model-1', payload] },
      { run: () => control.deleteGatewayModel('model-1'), method: 'delete', args: ['/admin/gateway-models/model-1'] },
      { run: () => control.getModelRoutes(), method: 'get', args: ['/admin/model-routes'] },
      { run: () => control.createModelRoute(payload), method: 'post', args: ['/admin/model-routes', payload] },
      { run: () => control.updateModelRoute('route-1', payload), method: 'put', args: ['/admin/model-routes/route-1', payload] },
      { run: () => control.deleteModelRoute('route-1'), method: 'delete', args: ['/admin/model-routes/route-1'] },
      { run: () => control.simulateGatewayRouting('model-a', 123), method: 'post', args: ['/admin/gateway-simulator', { model: 'model-a', estimated_tokens: 123 }] },
      { run: () => control.getModelPricings(), method: 'get', args: ['/admin/model-pricings'] },
      { run: () => control.createModelPricing(payload), method: 'post', args: ['/admin/model-pricings', payload] },
      { run: () => control.updateModelPricing('pricing-1', payload), method: 'put', args: ['/admin/model-pricings/pricing-1', payload] },
      { run: () => control.getAPIKeys(), method: 'get', args: ['/admin/api-keys'] },
      { run: () => control.getAPIKeyPolicyExplanation('key-1'), method: 'get', args: ['/admin/api-keys/key-1/policy-explanation'] },
      { run: () => control.createAPIKey(payload), method: 'post', args: ['/admin/api-keys', payload] },
      { run: () => control.updateAPIKey('key-1', payload), method: 'put', args: ['/admin/api-keys/key-1', payload] },
      { run: () => control.rotateAPIKey('key-1'), method: 'post', args: ['/admin/api-keys/key-1/rotate'] },
      { run: () => control.disableAPIKey('key-1'), method: 'post', args: ['/admin/api-keys/key-1/disable'] },
      { run: () => control.acknowledgeAlert('alert-1'), method: 'post', args: ['/admin/alerts/alert-1/acknowledge'] },
      { run: () => control.resolveAlert('alert-1'), method: 'post', args: ['/admin/alerts/alert-1/resolve'] }
    ]
    for (const testCase of cases) {
      await testCase.run()
      expect(client[testCase.method]).toHaveBeenLastCalledWith(...testCase.args)
    }
  })

  it('uses query, summary, and asynchronous export endpoint contracts', async () => {
    const params = { limit: 10, q: 'synthetic' }
    const cases: Array<{ run: () => Promise<unknown>; method: ClientMethod; args: unknown[] }> = [
      { run: () => control.getAuditLogs(params), method: 'get', args: ['/admin/audit-logs', { params }] },
      { run: () => control.getAuditLogSummary(params), method: 'get', args: ['/admin/audit-logs/summary', { params }] },
      { run: () => control.getAlerts(params), method: 'get', args: ['/admin/alerts', { params }] },
      { run: () => control.getAlertSummary(params), method: 'get', args: ['/admin/alerts/summary', { params }] },
      { run: () => control.getUsageReport(params), method: 'get', args: ['/admin/usage', { params }] },
      { run: () => control.getCostAllocationReport(params), method: 'get', args: ['/admin/cost-allocation', { params }] },
      { run: () => control.getGatewayTraces(params), method: 'get', args: ['/admin/gateway-traces', { params }] },
      { run: () => control.getGatewayTraceSummary(params), method: 'get', args: ['/admin/gateway-traces/summary', { params }] },
      { run: () => control.createExportJob('usage', params), method: 'post', args: ['/admin/export-jobs', null, { params: { ...params, kind: 'usage' } }] },
      { run: () => control.getExportJobs(25), method: 'get', args: ['/admin/export-jobs', { params: { limit: 25 } }] },
      { run: () => control.getExportJob('job-1'), method: 'get', args: ['/admin/export-jobs/job-1'] }
    ]
    for (const testCase of cases) {
      await testCase.run()
      expect(client[testCase.method]).toHaveBeenLastCalledWith(...testCase.args)
    }
  })

  it('selects portal or customer self-service endpoint contracts from the active path', async () => {
    const payload = { name: 'Self-service key' } as never
    for (const [browserPath, apiBase] of [['/portal/overview', '/portal'], ['/customer/overview', '/customer']]) {
      window.history.replaceState({}, '', browserPath)
      await control.getPortalWorkspace()
      expect(client.get).toHaveBeenLastCalledWith(`${apiBase}/workspace`)
      await control.createPortalAPIKey(payload)
      expect(client.post).toHaveBeenLastCalledWith(`${apiBase}/api-keys`, payload)
      await control.rotatePortalAPIKey('key-1')
      expect(client.post).toHaveBeenLastCalledWith(`${apiBase}/api-keys/key-1/rotate`)
      await control.disablePortalAPIKey('key-1')
      expect(client.post).toHaveBeenLastCalledWith(`${apiBase}/api-keys/key-1/disable`)
    }
  })

  it('downloads synchronous and asynchronous CSV exports', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(123456)
    client.get.mockResolvedValue({ data: new Blob(['id,value\n1,synthetic\n']) })
    const createObjectURL = vi.fn(() => 'blob:test-control-csv')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const params = { limit: 5 }

    await control.exportAuditLogsCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/admin/audit-logs/export', { params, responseType: 'blob' })
    await control.exportUsageCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/admin/usage/export', { params, responseType: 'blob' })
    await control.exportCostAllocationCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/admin/cost-allocation/export', { params, responseType: 'blob' })
    await control.exportGatewayTracesCSV(params)
    expect(client.get).toHaveBeenLastCalledWith('/admin/gateway-traces/export', { params, responseType: 'blob' })
    await control.downloadExportJob({ id: 'job-1', filename: 'job.csv' } as never)
    expect(client.get).toHaveBeenLastCalledWith('/admin/export-jobs/job-1/download', { params: undefined, responseType: 'blob' })
    expect(createObjectURL).toHaveBeenCalledTimes(5)
    expect(click).toHaveBeenCalledTimes(5)
    expect(revokeObjectURL).toHaveBeenCalledTimes(5)
  })
})
