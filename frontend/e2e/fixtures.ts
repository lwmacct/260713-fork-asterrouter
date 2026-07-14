import { expect, type APIResponse, type Page } from '@playwright/test'

export type Envelope<T> = { code: number; message: string; data: T }

const controlSurfaceForProfile: Record<string, string> = {
  enterprise: 'admin',
  personal: 'console',
  relay_operator: 'operator',
  platform: 'platform'
}

const demoEntryForProfile: Record<string, { path: string; heading: string }> = {
  enterprise: { path: '/admin/dashboard', heading: 'Overview' },
  personal: { path: '/console/overview', heading: 'Personal Console' },
  relay_operator: { path: '/operator/overview', heading: 'Relay Operator Console' },
  platform: { path: '/platform/overview', heading: 'Platform overview' }
}

export function controlAPI(path = ''): string {
  const surface = process.env.ASTER_E2E_CONTROL_SURFACE || controlSurfaceForProfile[process.env.ASTER_E2E_EXPECT_PROFILE || ''] || 'admin'
  return `/api/v1/${surface}${path}`
}

export async function envelope<T>(response: APIResponse, expectedStatus = 200): Promise<T> {
  const body = await response.json() as Envelope<T>
  expect(response.status(), JSON.stringify(body)).toBe(expectedStatus)
  expect(body.code, JSON.stringify(body)).toBe(0)
  return body.data
}

export async function adminPost<T>(page: Page, token: string, path: string, data: unknown): Promise<T> {
  return envelope<T>(await page.request.post(controlAPI(path), {
    data,
    headers: { Authorization: `Bearer ${token}` }
  }))
}

export async function loginUser(page: Page, email: string, password: string): Promise<string> {
  const result = await envelope<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  return result.access_token
}

export async function registerUsers(
  page: Page,
  adminToken: string,
  users: Array<{ email: string; password: string; displayName: string; balanceCents?: number }>
): Promise<Array<{ id: string; email: string }>> {
  const headers = { Authorization: `Bearer ${adminToken}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get(controlAPI('/settings'), { headers }))
  try {
    const registered: Array<{ id: string; email: string }> = []
    for (const user of users) {
      await envelope(await page.request.put(controlAPI('/settings'), {
        headers,
        data: {
          ...settings,
          registration_enabled: true,
          email_verify_enabled: false,
          default_balance_cents: user.balanceCents ?? settings.default_balance_cents
        }
      }))
      const result = await envelope<{ user_id: string }>(await page.request.post('/api/v1/auth/register', {
        data: {
          email: user.email,
          password: user.password,
          display_name: user.displayName,
          agreement_accepted: true
        }
      }))
      registered.push({ id: result.user_id, email: user.email })
    }
    return registered
  } finally {
    await envelope(await page.request.put(controlAPI('/settings'), { headers, data: settings }))
  }
}

export async function createGatewayFixture(page: Page, token: string, runID: string, publicModel: string) {
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

export function captureBrowserErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  page.on('requestfailed', (request) => {
    const failure = request.failure()
    errors.push(`requestfailed: ${request.method()} ${request.url()} ${failure?.errorText || ''}`.trim())
  })
  return errors
}

export async function loginDemo(page: Page): Promise<void> {
  await page.goto('/login')
  const username = process.env.ASTER_E2E_USERNAME
  const password = process.env.ASTER_E2E_PASSWORD
  if (username && password) {
    await page.getByLabel('Username').fill(username)
    await page.locator('input#password').fill(password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).not.toHaveURL(/\/login/)
    return
  }
  const demoButton = page.getByRole('button', { name: 'Enter demo mode' })
  await expect(demoButton).toBeVisible()
  await demoButton.click()
  const entry = demoEntryForProfile[process.env.ASTER_E2E_EXPECT_PROFILE || ''] || demoEntryForProfile.personal
  await expect(page).toHaveURL(new RegExp(`${entry.path}$`))
  await expect(page.getByRole('heading', { level: 1, name: entry.heading })).toBeVisible()
}

export async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => ({
    body: document.body.scrollWidth,
    document: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth
  }))
  expect(Math.max(dimensions.body, dimensions.document)).toBeLessThanOrEqual(dimensions.viewport + 1)
}
