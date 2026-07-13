import { expect, test, type APIResponse } from '@playwright/test'
import { loginDemo } from './fixtures'

type Envelope<T> = { code: number; message: string; data: T }

async function data<T>(response: APIResponse): Promise<T> {
  const body = await response.json() as Envelope<T>
  expect(response.status(), JSON.stringify(body)).toBe(200)
  expect(body.code, JSON.stringify(body)).toBe(0)
  return body.data
}

test('@smoke @j02 logout immediately revokes a dedicated user session', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The session contract is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const headers = { Authorization: `Bearer ${adminToken}` }
  const settings = await data<Record<string, unknown>>(await page.request.get('/api/v1/admin/settings', { headers }))
  const originalRegistration = Boolean(settings.registration_enabled)
  const email = `e2e-session-${Date.now()}@example.test`
  const password = 'synthetic-password-123'

  try {
    await data(await page.request.put('/api/v1/admin/settings', {
      headers,
      data: { ...settings, registration_enabled: true, email_verify_enabled: false }
    }))
    await data(await page.request.post('/api/v1/auth/register', {
      data: { email, password, display_name: 'E2E Session User', agreement_accepted: true }
    }))
  } finally {
    await data(await page.request.put('/api/v1/admin/settings', {
      headers,
      data: { ...settings, registration_enabled: originalRegistration }
    }))
  }

  const login = await data<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  const userHeaders = { Authorization: `Bearer ${login.access_token}` }
  expect((await page.request.get('/api/v1/account/profile', { headers: userHeaders })).status()).toBe(200)
  expect((await page.request.post('/api/v1/auth/logout', { headers: userHeaders })).status()).toBe(200)
  expect((await page.request.get('/api/v1/account/profile', { headers: userHeaders })).status()).toBe(401)

  const relogin = await data<{ access_token: string }>(await page.request.post('/api/v1/auth/login', {
    data: { username: email, password, agreement_accepted: true }
  }))
  expect(relogin.access_token).not.toBe(login.access_token)
  expect((await page.request.get('/api/v1/account/profile', {
    headers: { Authorization: `Bearer ${relogin.access_token}` }
  })).status()).toBe(200)
})
