import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

// Candidate-package journeys use one external origin instead of the local dev
// server. Keep health assertions on that same origin.
const backendURL = process.env.ASTER_E2E_EXTERNAL_URL || `http://127.0.0.1:${process.env.ASTER_E2E_BACKEND_PORT || '18080'}`
const expectedDemoMode = process.env.ASTER_E2E_EXPECT_DEMO_MODE === undefined
  ? true
  : process.env.ASTER_E2E_EXPECT_DEMO_MODE === 'true'
const expectedProfile = process.env.ASTER_E2E_EXPECT_PROFILE || ''
const profileJourney: Record<string, { path: string; heading: string }> = {
  personal: { path: '/console/overview', heading: 'Personal Console' },
  relay_operator: { path: '/operator/overview', heading: 'Relay Operator Console' },
  enterprise: { path: '/admin/dashboard', heading: 'Overview' },
  platform: { path: '/platform/overview', heading: 'Platform overview' }
}
const activeJourney = profileJourney[expectedProfile] || profileJourney.personal

test('@smoke @surface-smoke backend health and public settings are ready', async ({ request }) => {
  const health = await request.get(`${backendURL}/health`)
  expect(health.status()).toBe(200)
  await expect(health.json()).resolves.toMatchObject({ data: { status: 'ok' } })

  const ready = await request.get(`${backendURL}/ready`)
  expect(ready.status()).toBe(200)
  await expect(ready.json()).resolves.toMatchObject({ data: { status: 'ready' } })

  const settings = await request.get(`${backendURL}/api/v1/settings/public`)
  expect(settings.status()).toBe(200)
  const settingsBody = await settings.json()
  expect(settingsBody).toMatchObject({ data: { demo_mode: expectedDemoMode, setup_completed: true } })
  if (expectedProfile) {
    expect(settingsBody.data).toMatchObject({ default_profile: expectedProfile, enabled_profiles: [expectedProfile] })
  }
})

test('@smoke @surface-smoke anonymous protected navigation redirects to login', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  const protectedPath = `${activeJourney.path}?status=active`
  await page.goto(protectedPath)

  await expect(page).toHaveURL(/\/login\?redirect=/)
  const loginURL = new URL(page.url())
  expect(loginURL.searchParams.get('redirect')).toBe(protectedPath)
  await expect(page.getByRole('heading', { level: 2, name: 'Welcome back' })).toBeVisible()
  if (expectedDemoMode) {
    await expect(page.getByText('Experience AsterRouter now')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Try the demo' })).toBeVisible()
  }
  await expect(page.getByLabel('Username')).toHaveValue('admin')
  await expect(page.locator('input#password')).toHaveAttribute('type', 'password')
  expect(errors).toEqual([])
})

test('@smoke @surface-smoke login persists and opens the enabled deployment surface', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  await page.reload()
  await page.waitForLoadState('networkidle')
  await expect(page).toHaveURL(new RegExp(`${activeJourney.path}$`))
  await expect(page.getByRole('heading', { level: 1, name: activeJourney.heading })).toBeVisible()

  const additionalSurfaces = expectedProfile
    ? []
    : ['/operator/overview', '/admin/dashboard', '/portal/overview', '/platform/overview']
  for (const path of additionalSurfaces) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page.locator('main')).toBeVisible()
  }
  expect(errors).toEqual([])
})

test('@smoke @surface-smoke settings can switch between all product modes', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  test.skip(Boolean(process.env.CI), 'Profile mutation is covered outside CI; CI keeps global deployment state stable.')
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The profile mutation contract runs once; responsive layout is covered separately.')

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const headers = { Authorization: `Bearer ${token}` }
  const allProfiles = ['enterprise', 'personal', 'relay_operator', 'platform']
  const originalProfile = expectedProfile || 'personal'
  const targetProfile = originalProfile === 'enterprise' ? 'personal' : 'enterprise'
  const targetJourney = profileJourney[targetProfile]
  const switchedProfiles = [targetProfile]
  const originalProfiles = expectedDemoMode ? allProfiles : [originalProfile]

  await page.goto('/admin/settings')
  await page.getByRole('button', { name: 'Gateway services' }).click()
  await expect(page.getByText(expectedDemoMode ? 'Switch demo experience' : 'Switch deployment profile')).toBeVisible()
  await expect(page.locator('button[data-profile]')).toHaveCount(4)
  if (process.env.CI) {
    expect(errors).toEqual([])
    return
  }

  try {
    await page.evaluate(() => {
      (window as Window & { __asterProfileSwitchPage?: string }).__asterProfileSwitchPage = 'before-switch'
    })
    await page.locator(`button[data-profile="${targetProfile}"]`).click()
    await expect(page).toHaveURL(new RegExp(`${targetJourney.path}$`))
    await expect.poll(() => page.evaluate(
      () => (window as Window & { __asterProfileSwitchPage?: string }).__asterProfileSwitchPage
    )).toBeUndefined()
    await expect(page.locator('aside a[href="/console/overview"]')).toHaveCount(targetProfile === 'personal' ? 1 : 0)
    await expect(page.locator('aside a[href="/operator/overview"]')).toHaveCount(0)
    await expect(page.locator('aside a[href="/admin/dashboard"]')).toHaveCount(targetProfile === 'enterprise' ? 1 : 0)
    await expect(page.locator('aside a[href="/platform/overview"]')).toHaveCount(targetProfile === 'platform' ? 1 : 0)
    await page.getByRole('button', { name: 'Account menu' }).click()
    await expect(page.locator('.account-dropdown button')).toHaveCount(2)
    const settings = await page.request.get('/api/v1/settings/public')
    await expect(settings.json()).resolves.toMatchObject({ data: { default_profile: targetProfile, enabled_profiles: switchedProfiles } })
  } finally {
    await page.request.put('/api/v1/system/profiles', {
      headers,
      data: { enabled_profiles: originalProfiles, default_profile: originalProfile }
    })
  }

  expect(errors).toEqual([])
})

test('@smoke @surface-smoke locale, theme, and responsive layout remain usable', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  const language = page.getByLabel('Language')
  await language.selectOption('zh-CN')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('h1')).toBeVisible()

  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  const themeButton = page.getByRole('button', { name: /深色模式|浅色模式/ })
  await themeButton.click()
  const theme = await page.locator('html').getAttribute('data-theme')
  expect(['dark', 'light']).toContain(theme)

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme || '')
  await expectNoHorizontalOverflow(page)
  expect(errors).toEqual([])
})
