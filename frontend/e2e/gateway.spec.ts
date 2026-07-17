import { expect, test, type Page } from '@playwright/test'
import { adminPost, createGatewayFixture, createPublishedPricingRule, envelope, loginDemo } from './fixtures'

type PolicyAlert = {
  id: string
  type: string
  severity: string
  status: string
  resource_id: string
  metadata: Record<string, string>
}

async function invokeWithSyntheticUsage(page: Page, key: string, model: string, tokens: number) {
  return page.request.post('/v1/chat/completions', {
    data: {
      model,
      max_cost_micros: 1,
      messages: [{ role: 'user', content: `synthetic ${tokens}-token policy request` }],
      synthetic_usage: { prompt_tokens: tokens, completion_tokens: 0 }
    },
    headers: { Authorization: `Bearer ${key}` }
  })
}

async function policyAlert(page: Page, adminToken: string, keyID: string, type: string): Promise<PolicyAlert> {
  const alerts = await envelope<PolicyAlert[]>(await page.request.get(`/api/v1/admin/alerts?type=${type}&resource_type=api_key&limit=100`, {
    headers: { Authorization: `Bearer ${adminToken}` }
  }))
  const alert = alerts.find((item) => item.resource_id === keyID)
  expect(alert, `missing ${type} alert for ${keyID}`).toBeTruthy()
  return alert!
}

async function expectUsageCost(page: Page, adminToken: string, keyID: string, expectedCostMicros: number) {
  await expect.poll(async () => {
    const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get('/api/v1/admin/usage?limit=100', {
      headers: { Authorization: `Bearer ${adminToken}` }
    }))
    return usage.recent
      .filter((item) => item.api_key_id === keyID && item.status !== 'error')
      .reduce((sum, item) => sum + Number(item.usage_cost_micros || 0), 0)
  }, { message: `usage cost for ${keyID}` }).toBe(expectedCostMicros)
}

test('@smoke @j01 provider-to-gateway request records evidence', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The API workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  expect(token).not.toBe('')

  const runID = `${testInfo.project.name}-${Date.now()}`
  const publicModel = `e2e-public-${runID}`
  const account = await createGatewayFixture(page, token, runID, publicModel)
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })

  const completion = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic e2e request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(completion.status()).toBe(200)
  await expect(completion.json()).resolves.toMatchObject({
    id: 'e2e-completion',
    choices: [{ message: { content: 'e2e-ok' } }],
    usage: { prompt_tokens: 7, completion_tokens: 11 }
  })

  const streaming = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, stream: true, messages: [{ role: 'user', content: 'synthetic streaming e2e request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(streaming.status()).toBe(200)
  expect(streaming.headers()['content-type']).toContain('text/event-stream')
  const streamingBody = await streaming.text()
  expect(streamingBody).toContain('"id":"e2e-stream"')
  expect(streamingBody).toContain('data: [DONE]')

  const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get('/api/v1/admin/usage?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(usage.recent).toContainEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    provider_account_id: account.id,
    status: 'forwarded',
    input_tokens: 7,
    output_tokens: 11
  }))

  const traces = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/gateway-traces?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  const forwardedTraces = traces.filter((trace) =>
    trace.api_key_id === workspaceKey.record.id &&
    trace.model === publicModel &&
    trace.provider_account_id === account.id &&
    trace.status === 'forwarded' &&
    trace.http_status === 200
  )
  expect(forwardedTraces).toHaveLength(2)
  expect(forwardedTraces).toContainEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    provider_account_id: account.id,
    upstream_model: 'upstream-model',
    status: 'forwarded',
    http_status: 200
  }))

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/audit-logs?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'invoke', resource_type: 'gateway_call' }))
})

test('@smoke @j05 quota and budget warn, deduplicate, escalate, and reject with evidence', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The API workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  expect(token).not.toBe('')

  const runID = `${testInfo.project.name}-${Date.now()}`
  const quotaModel = `e2e-quota-${runID}`
  await createGatewayFixture(page, token, `${runID}-quota`, quotaModel)
  const quotaKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Quota Key ${runID}`,
    model_allowlist: [quotaModel],
    qps_limit: 10,
    monthly_token_limit: 100
  })

  expect((await invokeWithSyntheticUsage(page, quotaKey.key, quotaModel, 40)).status()).toBe(200)
  expect((await invokeWithSyntheticUsage(page, quotaKey.key, quotaModel, 40)).status()).toBe(200)
  const quotaWarning = await policyAlert(page, token, quotaKey.record.id, 'api_key_quota')
  expect(quotaWarning).toMatchObject({ severity: 'warning', status: 'active' })
  expect(quotaWarning.metadata).toMatchObject({ current_month_tokens: '80', quota_used_percent: '80' })

  expect((await invokeWithSyntheticUsage(page, quotaKey.key, quotaModel, 20)).status()).toBe(200)
  const quotaCritical = await policyAlert(page, token, quotaKey.record.id, 'api_key_quota')
  expect(quotaCritical).toMatchObject({ id: quotaWarning.id, severity: 'critical', status: 'active' })
  expect(quotaCritical.metadata).toMatchObject({ current_month_tokens: '100', quota_used_percent: '100' })

  const quotaRejected = await invokeWithSyntheticUsage(page, quotaKey.key, quotaModel, 1)
  expect(quotaRejected.status()).toBe(429)
  await expect(quotaRejected.json()).resolves.toMatchObject({ error: { type: 'insufficient_quota' } })

  const budgetModel = `e2e-budget-${runID}`
  await createGatewayFixture(page, token, `${runID}-budget`, budgetModel)
  await createPublishedPricingRule(page, token, 'admin', {
    name: `E2E usage cost ${runID}`,
    purpose: 'usage_cost',
    scope_type: 'global',
    scope_id: '',
    model: budgetModel,
    expression: 'v1: token_line("input", uncached_input_tokens, 10000000000)'
  })
  const budgetKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Budget Key ${runID}`,
    model_allowlist: [budgetModel],
    qps_limit: 10,
    monthly_token_limit: 0
  })
  await adminPost(page, token, '/policies', {
    name: `E2E Budget Policy ${runID}`,
    scope_type: 'api_key',
    scope_id: budgetKey.record.id,
    model_allowlist: [],
    model_denylist: [],
    qps_limit: 0,
    monthly_token_limit: 0,
    monthly_budget_micros: 4_000_000,
    overage_action: 'block',
    prompt_logging_mode: 'metadata_only',
    retention_days: 30,
    status: 'active'
  })

  expect((await invokeWithSyntheticUsage(page, budgetKey.key, budgetModel, 160)).status()).toBe(200)
  expect((await invokeWithSyntheticUsage(page, budgetKey.key, budgetModel, 160)).status()).toBe(200)
  const budgetWarning = await policyAlert(page, token, budgetKey.record.id, 'api_key_budget')
  expect(budgetWarning).toMatchObject({ severity: 'warning', status: 'active' })
  expect(budgetWarning.metadata).toMatchObject({ current_month_usage_cost_micros: '3200000', budget_used_percent: '80' })
  await expectUsageCost(page, token, budgetKey.record.id, 3_200_000)

  expect((await invokeWithSyntheticUsage(page, budgetKey.key, budgetModel, 80)).status()).toBe(200)
  const budgetCritical = await policyAlert(page, token, budgetKey.record.id, 'api_key_budget')
  expect(budgetCritical).toMatchObject({ id: budgetWarning.id, severity: 'critical', status: 'active' })
  expect(budgetCritical.metadata).toMatchObject({ current_month_usage_cost_micros: '4000000', budget_used_percent: '100' })

  const budgetRejected = await invokeWithSyntheticUsage(page, budgetKey.key, budgetModel, 1)
  expect(budgetRejected.status()).toBe(402)
  await expect(budgetRejected.json()).resolves.toMatchObject({
    error: { type: 'budget_hold_failed' }
  })

  const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get('/api/v1/admin/usage?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(usage.recent).toContainEqual(expect.objectContaining({ api_key_id: quotaKey.record.id, model: quotaModel, status: 'error', error_type: 'quota_exceeded' }))
  expect(usage.recent).toContainEqual(expect.objectContaining({ api_key_id: budgetKey.record.id, model: budgetModel, status: 'error', error_type: 'budget_hold_failed' }))
  expect(usage.recent.filter((item) => item.api_key_id === budgetKey.record.id).reduce((sum, item) => sum + Number(item.usage_cost_micros || 0), 0)).toBe(4_000_000)

  const traces = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/gateway-traces?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(traces).toContainEqual(expect.objectContaining({ api_key_id: quotaKey.record.id, model: quotaModel, status: 'error', http_status: 429, error_type: 'quota_exceeded' }))
  expect(traces).toContainEqual(expect.objectContaining({ api_key_id: budgetKey.record.id, model: budgetModel, status: 'error', http_status: 402, error_type: 'budget_hold_failed' }))

  const audit = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/audit-logs?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(audit).toContainEqual(expect.objectContaining({ action: 'invoke', resource_type: 'gateway_call', summary: expect.stringContaining('status=policy_rejected') }))
})

test('@smoke @j04 failed primary route falls back and records attempts', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The API workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  expect(token).not.toBe('')

  const runID = `${testInfo.project.name}-${Date.now()}`
  const publicModel = `e2e-failover-${runID}`
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `E2E Failover Provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    models: ['fail-model', 'upstream-model'],
    priority: 10,
    api_key: 'synthetic-provider-secret'
  })
  const createAccount = (name: string, model: string, priority: number) => adminPost<{ id: string }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `${name} ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority,
    concurrency: 2,
    rate_multiplier: 1,
    models: [model],
    group_ids: [],
    secret: `synthetic-${name.toLowerCase()}-secret`
  })
  const primary = await createAccount('Primary', 'fail-model', 10)
  const fallback = await createAccount('Fallback', 'upstream-model', 20)
  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel,
    name: `E2E Failover Model ${runID}`,
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  for (const [account, upstreamModel, priority] of [[primary, 'fail-model', 10], [fallback, 'upstream-model', 20]] as const) {
    await adminPost(page, token, '/model-routes', {
      gateway_model_id: model.id,
      route_group: 'default',
      provider_account_id: account.id,
      upstream_model: upstreamModel,
      priority,
      weight: 100,
      status: 'active'
    })
  }
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Failover Key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })

  const completion = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic failover request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(completion.status()).toBe(200)
  await expect(completion.json()).resolves.toMatchObject({ id: 'e2e-completion' })

  const traces = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/gateway-traces?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  const trace = traces.find((item) => item.api_key_id === workspaceKey.record.id && item.model === publicModel)
  expect(trace).toEqual(expect.objectContaining({
    provider_account_id: fallback.id,
    upstream_model: 'upstream-model',
    status: 'forwarded',
    http_status: 200
  }))
  expect(String(trace?.route_attempts)).toContain('"account_id":"' + primary.id + '"')
  expect(String(trace?.route_attempts)).toContain('"outcome":"failed"')
  expect(String(trace?.route_attempts)).toContain('"account_id":"' + fallback.id + '"')
  expect(String(trace?.route_attempts)).toContain('"outcome":"selected"')
})
