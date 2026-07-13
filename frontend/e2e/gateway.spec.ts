import { expect, test, type APIResponse, type Page } from '@playwright/test'
import { loginDemo } from './fixtures'

type Envelope<T> = { code: number; message: string; data: T }

async function envelope<T>(response: APIResponse): Promise<T> {
  const body = await response.json() as Envelope<T>
  expect(response.status(), JSON.stringify(body)).toBe(200)
  expect(body.code, JSON.stringify(body)).toBe(0)
  return body.data
}

async function adminPost<T>(page: Page, token: string, path: string, data: unknown): Promise<T> {
  return envelope<T>(await page.request.post(`/api/v1/admin${path}`, {
    data,
    headers: { Authorization: `Bearer ${token}` }
  }))
}

async function createGatewayFixture(page: Page, token: string, runID: string, publicModel: string) {
  const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
  const provider = await adminPost<{ id: string }>(page, token, '/providers', {
    name: `E2E Provider ${runID}`,
    type: 'openai_compatible',
    base_url: `http://127.0.0.1:${upstreamPort}/v1`,
    status: 'active',
    models: ['upstream-model'],
    priority: 10,
    api_key: 'synthetic-provider-secret'
  })
  const account = await adminPost<{ id: string; secret_configured: boolean }>(page, token, '/provider-accounts', {
    provider_id: provider.id,
    name: `E2E Account ${runID}`,
    platform: 'openai_compatible',
    auth_type: 'api_key',
    status: 'active',
    schedulable: true,
    priority: 10,
    concurrency: 2,
    rate_multiplier: 1,
    models: ['upstream-model'],
    group_ids: [],
    secret: 'synthetic-account-secret'
  })
  expect(account.secret_configured).toBe(true)

  const model = await adminPost<{ id: string }>(page, token, '/gateway-models', {
    model_id: publicModel,
    name: `E2E Model ${runID}`,
    description: 'Synthetic Playwright gateway contract',
    modality: 'chat',
    default_route_group: 'default',
    status: 'active'
  })
  await adminPost(page, token, '/model-routes', {
    gateway_model_id: model.id,
    route_group: 'default',
    provider_account_id: account.id,
    upstream_model: 'upstream-model',
    priority: 10,
    weight: 100,
    status: 'active'
  })
  return account
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

test('@smoke @j05 token quota rejects a gateway request and records evidence', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The API workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  expect(token).not.toBe('')

  const runID = `${testInfo.project.name}-${Date.now()}`
  const publicModel = `e2e-quota-${runID}`
  await createGatewayFixture(page, token, runID, publicModel)
  const workspaceKey = await adminPost<{ key: string; record: { id: string } }>(page, token, '/api-keys', {
    name: `E2E Quota Key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 1
  })

  const accepted = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic request before quota rejection' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(accepted.status()).toBe(200)

  const rejected = await page.request.post('/v1/chat/completions', {
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic quota rejection request' }] },
    headers: { Authorization: `Bearer ${workspaceKey.key}` }
  })
  expect(rejected.status()).toBe(429)
  await expect(rejected.json()).resolves.toMatchObject({
    error: { type: 'insufficient_quota' }
  })

  const usage = await envelope<{ recent: Array<Record<string, unknown>> }>(await page.request.get('/api/v1/admin/usage?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(usage.recent).toContainEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    status: 'error',
    error_type: 'quota_exceeded'
  }))

  const traces = await envelope<Array<Record<string, unknown>>>(await page.request.get('/api/v1/admin/gateway-traces?limit=100', {
    headers: { Authorization: `Bearer ${token}` }
  }))
  expect(traces).toContainEqual(expect.objectContaining({
    api_key_id: workspaceKey.record.id,
    model: publicModel,
    status: 'error',
    http_status: 429,
    error_type: 'quota_exceeded'
  }))
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
