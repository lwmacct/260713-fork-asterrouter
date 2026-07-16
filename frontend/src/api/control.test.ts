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
    client.get.mockResolvedValueOnce({ data: { provider_count: 0, active_provider_count: 0, api_key_count: 0, active_api_key_count: 0, models: null, recent_audit: null } })
    expect(await control.getDashboard()).toMatchObject({ models: [], recent_audit: [] })
    client.get.mockResolvedValueOnce({ data: [{ id: 'provider-1', models: null }] })
    expect(await control.getProviders()).toEqual([{ id: 'provider-1', models: [] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-1', models: ['model', 1, null] }] })
    expect(await control.getProviderHealthChecks()).toEqual([{ id: 'check-1', models: ['model'] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'account-1', models: null, group_ids: null, temp_unschedulable_rules: null }] })
    expect(await control.getProviderAccounts()).toEqual([{ id: 'account-1', models: [], auto_enable_new_models: false, group_ids: [], temp_unschedulable_rules: [] }])
    client.get.mockResolvedValueOnce({ data: [{ id: 'check-2', models: null }] })
    expect(await control.getProviderAccountHealthChecks()).toEqual([{ id: 'check-2', models: [] }])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getRoutingGroups()).toEqual([])
    client.get.mockResolvedValueOnce({ data: undefined })
    expect(await control.getGatewayModels()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getModelRoutes()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getAPIKeys()).toEqual([])
    client.get.mockResolvedValueOnce({ data: null })
    expect(await control.getGovernancePolicies()).toEqual([])
  })

  it('normalizes nullable collections used by every admin list page', async () => {
    const loads: Array<() => Promise<unknown[]>> = [
      control.getDepartments,
      control.getOrganizationGroups,
      control.getWorkspaceUsers,
      control.getRoleBindings,
      control.getModelPricings,
      control.getAuditLogs,
      control.getAlerts,
      control.getGatewayTraces,
      control.getExportJobs
    ]

    for (const load of loads) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }
  })

  it('normalizes nested collections consumed directly by admin and portal views', async () => {
    client.get.mockResolvedValueOnce({ data: [{ id: 'group-1', member_ids: null }] })
    expect(await control.getOrganizationGroups()).toEqual([{ id: 'group-1', member_ids: [] }])

    client.get.mockResolvedValueOnce({ data: [{ id: 'policy-1', model_allowlist: null, model_denylist: null }] })
    expect(await control.getGovernancePolicies()).toEqual([{ id: 'policy-1', model_allowlist: [], model_denylist: [] }])

    client.get.mockResolvedValueOnce({ data: [{ id: 'key-1', scopes: null, model_allowlist: null, allowed_modalities: null, allowed_operations: null, allowed_cidrs: null }] })
    expect(await control.getAPIKeys()).toEqual([{
      id: 'key-1', scopes: [], model_allowlist: [], allowed_modalities: [], allowed_operations: [], allowed_cidrs: []
    }])

    client.get.mockResolvedValueOnce({
      data: {
        api_keys: [{ id: 'key-1', scopes: null, model_allowlist: null, allowed_modalities: null, allowed_operations: null, allowed_cidrs: null }],
        usage: { by_model: null, recent: null },
        recent_traces: null,
        alerts: null,
        models: null
      }
    })
    expect(await control.getPortalWorkspace()).toMatchObject({
      api_keys: [{ model_allowlist: [] }],
      usage: { by_model: [], recent: [] },
      recent_traces: [],
      alerts: [],
      models: []
    })

    client.get.mockResolvedValueOnce({ data: { rows: [{ reason_codes: null, provider_billing_routing_health: { reason_codes: null } }], decisions: [{ reason_codes: null, last_evaluation_reason_codes: null }] } })
    expect(await control.getEffectivePricingReport()).toMatchObject({
      rows: [{ reason_codes: [], provider_billing_routing_health: { reason_codes: [] } }],
      decisions: [{ reason_codes: [], last_evaluation_reason_codes: [] }]
    })

    client.post.mockResolvedValueOnce({ data: { usage_aggregates: null, warnings: null } })
    expect(await control.inspectProviderBillingSource('account-1')).toMatchObject({ usage_aggregates: [], warnings: [] })

    client.get.mockResolvedValueOnce({ data: [{ id: 'source-1', warnings: null, routing_health: { reason_codes: null } }] })
    expect(await control.getProviderBillingSources()).toEqual([{ id: 'source-1', warnings: [], routing_health: { reason_codes: [] } }])

    client.get.mockResolvedValueOnce({ data: { candidates: null } })
    expect(await control.getAPIKeyPolicyExplanation('key-1')).toMatchObject({ candidates: [] })
  })

  it('normalizes nullable provider mutation responses', async () => {
    const provider = { id: 'provider-1', models: null }
    client.post.mockResolvedValueOnce({ data: provider })
    expect(await control.createProvider({} as never)).toEqual({ id: 'provider-1', models: [] })
    client.put.mockResolvedValueOnce({ data: provider })
    expect(await control.updateProvider('provider-1', {} as never)).toEqual({ id: 'provider-1', models: [] })
    client.post.mockResolvedValueOnce({ data: { id: 'check-1', models: null } })
    expect(await control.checkProvider('provider-1')).toEqual({ id: 'check-1', models: [] })

    const account = { id: 'account-1', models: null, group_ids: null, temp_unschedulable_rules: null }
    client.post.mockResolvedValueOnce({ data: account })
    expect(await control.createProviderAccount({} as never)).toMatchObject({ id: 'account-1', models: [], group_ids: [], temp_unschedulable_rules: [] })
    client.put.mockResolvedValueOnce({ data: account })
    expect(await control.updateProviderAccount('account-1', {} as never)).toMatchObject({ id: 'account-1', models: [], group_ids: [], temp_unschedulable_rules: [] })
    client.post.mockResolvedValueOnce({ data: { id: 'check-2', models: null } })
    expect(await control.checkProviderAccount('account-1')).toEqual({ id: 'check-2', models: [] })

    client.post.mockResolvedValueOnce({
      data: {
        account,
        inventory: { account_id: 'account-1', models: null },
        discovery: { account_id: 'account-1', models: null, added_models: null, missing_models: null, unchanged_models: null, affected_route_ids: null }
      }
    })
    expect(await control.syncProviderAccountModels('account-1', { enabled_models: [], auto_enable_new_models: false })).toMatchObject({
      account: { models: [], group_ids: [], temp_unschedulable_rules: [] },
      inventory: { models: [] },
      discovery: { models: [], added_models: [], missing_models: [], unchanged_models: [], affected_route_ids: [] }
    })
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
      { run: () => control.getProviderAccountModelInventory('account-1'), method: 'get', args: ['/admin/provider-accounts/account-1/models'] },
      { run: () => control.discoverProviderAccountModels('account-1'), method: 'post', args: ['/admin/provider-accounts/account-1/models/discover'] },
      {
        run: () => {
          client.post.mockResolvedValueOnce({ data: { account: {}, inventory: {}, discovery: {} } })
          return control.syncProviderAccountModels('account-1', { enabled_models: ['model-a'], auto_enable_new_models: false })
        },
        method: 'post',
        args: ['/admin/provider-accounts/account-1/models/sync', { enabled_models: ['model-a'], auto_enable_new_models: false }]
      },
      { run: () => control.clearProviderAccountCooldown('account-1'), method: 'post', args: ['/admin/provider-accounts/account-1/clear-cooldown'] },
      { run: () => control.getGatewayModels(), method: 'get', args: ['/admin/gateway-models'] },
      { run: () => control.createGatewayModel(payload), method: 'post', args: ['/admin/gateway-models', payload] },
      { run: () => control.updateGatewayModel('model-1', payload), method: 'put', args: ['/admin/gateway-models/model-1', payload] },
      { run: () => control.deleteGatewayModel('model-1'), method: 'delete', args: ['/admin/gateway-models/model-1'] },
      { run: () => control.getModelRoutes(), method: 'get', args: ['/admin/model-routes'] },
      { run: () => control.createModelRoute(payload), method: 'post', args: ['/admin/model-routes', payload] },
      { run: () => control.bulkCreateModelRoutes({ routes: [payload] }), method: 'post', args: ['/admin/model-routes/bulk', { routes: [payload] }] },
      { run: () => control.updateModelRoute('route-1', payload), method: 'put', args: ['/admin/model-routes/route-1', payload] },
      { run: () => control.deleteModelRoute('route-1'), method: 'delete', args: ['/admin/model-routes/route-1'] },
      { run: () => control.simulateGatewayRouting('model-a', 123), method: 'post', args: ['/admin/gateway-simulator', { model: 'model-a', estimated_tokens: 123 }] },
      { run: () => control.getModelPricings(), method: 'get', args: ['/admin/model-pricings'] },
      { run: () => control.createModelPricing(payload), method: 'post', args: ['/admin/model-pricings', payload] },
      { run: () => control.updateModelPricing('pricing-1', payload), method: 'put', args: ['/admin/model-pricings/pricing-1', payload] },
      { run: () => control.getEffectivePricingPolicy(), method: 'get', args: ['/admin/effective-pricing/policy'] },
      { run: () => control.updateEffectivePricingPolicy(payload), method: 'put', args: ['/admin/effective-pricing/policy', payload] },
      { run: () => control.getProcurementPrices(), method: 'get', args: ['/admin/procurement-prices'] },
      { run: () => control.createProcurementPrice(payload), method: 'post', args: ['/admin/procurement-prices', payload] },
      { run: () => control.updateProcurementPrice('price-1', payload), method: 'put', args: ['/admin/procurement-prices/price-1', payload] },
      { run: () => control.getProviderBillingLines(), method: 'get', args: ['/admin/provider-billing-lines'] },
      { run: () => control.createProviderBillingLine(payload), method: 'post', args: ['/admin/provider-billing-lines', payload] },
      { run: () => control.inspectProviderBillingSource('account-a'), method: 'post', args: ['/admin/provider-billing-sources/inspect', { provider_account_id: 'account-a', adapter_id: 'auto' }] },
      { run: () => control.getProviderBillingSources(), method: 'get', args: ['/admin/provider-billing-sources'] },
      { run: () => control.updateProviderBillingSource(payload), method: 'put', args: ['/admin/provider-billing-sources', payload] },
      { run: () => control.syncProviderBillingSource('source-a'), method: 'post', args: ['/admin/provider-billing-sources/source-a/sync'] },
      { run: () => control.getProviderBillingSourceEvidence('source-a', 25), method: 'get', args: ['/admin/provider-billing-sources/source-a/evidence', { params: { limit: 25 } }] },
      { run: () => control.getProviderCacheCapabilities(), method: 'get', args: ['/admin/provider-cache-capabilities'] },
      { run: () => control.updateProviderCacheCapability(payload), method: 'put', args: ['/admin/provider-cache-capabilities', payload] },
      { run: () => control.getProviderCacheProbeRuns(25), method: 'get', args: ['/admin/provider-cache-probes', { params: { limit: 25 } }] },
      { run: () => control.runProviderCacheProbe({ provider_account_id: 'account-1', upstream_model: 'model-a', protocol: 'openai_chat_completions', prefix_tokens: 2048, max_cost_micros: 100000 }), method: 'post', args: ['/admin/provider-cache-probes', { provider_account_id: 'account-1', upstream_model: 'model-a', protocol: 'openai_chat_completions', prefix_tokens: 2048, max_cost_micros: 100000 }] },
      { run: () => control.getEffectivePricingDecisions(), method: 'get', args: ['/admin/effective-pricing/decisions'] },
      { run: () => control.getEffectivePricingDecisionEvaluations('decision-1', 25), method: 'get', args: ['/admin/effective-pricing/decisions/decision-1/evaluations', { params: { limit: 25 } }] },
      { run: () => control.evaluateEffectivePricingDecision(payload), method: 'post', args: ['/admin/effective-pricing/decisions/evaluate', payload] },
      { run: () => control.actOnEffectivePricingDecision('decision-1', 'approve_canary', 5), method: 'post', args: ['/admin/effective-pricing/decisions/decision-1/action', { action: 'approve_canary', canary_percent: 5 }] },
      { run: () => control.getAPIKeys(), method: 'get', args: ['/admin/api-keys'] },
      { run: () => control.getAPIKeyPolicyExplanation('key-1'), method: 'get', args: ['/admin/api-keys/key-1/policy-explanation'] },
      { run: () => control.createAPIKey(payload), method: 'post', args: ['/admin/api-keys', payload] },
      { run: () => control.updateAPIKey('key-1', payload), method: 'put', args: ['/admin/api-keys/key-1', payload] },
      { run: () => control.rotateAPIKey('key-1', 3600), method: 'post', args: ['/admin/api-keys/key-1/rotate', { grace_period_seconds: 3600 }] },
      { run: () => control.disableAPIKey('key-1'), method: 'post', args: ['/admin/api-keys/key-1/disable'] },
      { run: () => control.getArtifact('artifact-1'), method: 'get', args: ['/admin/artifacts/artifact-1'] },
      { run: () => control.getArtifactRuntimes(), method: 'get', args: ['/admin/artifact-runtimes'] },
      { run: () => control.retryArtifactDelivery('artifact-1'), method: 'post', args: ['/admin/artifacts/artifact-1/retry-delivery'] },
      { run: () => control.getAIJob('job-1'), method: 'get', args: ['/admin/ai-jobs/job-1'] },
      { run: () => control.getAIJobRuntime(), method: 'get', args: ['/admin/ai-jobs/runtime'] },
      { run: () => control.cancelAIJob('job-1'), method: 'post', args: ['/admin/ai-jobs/job-1/cancel'] },
      { run: () => control.scheduleAIJobAttemptReconciliation('job-1', 'attempt-1'), method: 'post', args: ['/admin/ai-jobs/job-1/attempts/attempt-1/reconcile'] },
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
      { run: () => control.getEffectivePricingReport({ model: 'model-a', protocol: 'openai_chat_completions', window_hours: 24 }), method: 'get', args: ['/admin/effective-pricing/report', { params: { model: 'model-a', protocol: 'openai_chat_completions', window_hours: 24 } }] },
      { run: () => control.getCostAllocationReport(params), method: 'get', args: ['/admin/cost-allocation', { params }] },
      { run: () => control.getGatewayTraces(params), method: 'get', args: ['/admin/gateway-traces', { params }] },
      { run: () => control.getGatewayTraceSummary(params), method: 'get', args: ['/admin/gateway-traces/summary', { params }] },
      { run: () => control.getArtifacts(params), method: 'get', args: ['/admin/artifacts', { params }] },
      { run: () => control.getArtifactSummary(params), method: 'get', args: ['/admin/artifacts/summary', { params }] },
      { run: () => control.getAIJobs(params), method: 'get', args: ['/admin/ai-jobs', { params }] },
      { run: () => control.getAIJobSummary(params), method: 'get', args: ['/admin/ai-jobs/summary', { params }] },
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
      await control.rotatePortalAPIKey('key-1', 300)
      expect(client.post).toHaveBeenLastCalledWith(`${apiBase}/api-keys/key-1/rotate`, { grace_period_seconds: 300 })
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
