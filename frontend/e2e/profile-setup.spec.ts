import { expect, test } from '@playwright/test'
import { captureBrowserErrors } from './fixtures'

test('@setup setup requires one deployment role and shows platform confirmation', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The persistent setup workflow runs once against an isolated empty runtime.')
  const errors = captureBrowserErrors(page)
  await page.addInitScript(() => {
    if (sessionStorage.getItem('stale-auth-seeded')) return
    sessionStorage.setItem('stale-auth-seeded', 'true')
    localStorage.setItem('asterrouter_admin_token', 'stale-token-from-another-deployment-role')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify({
      username: 'old-admin',
      role: 'super_admin',
      display_name: 'Old admin',
      email: 'old-admin@example.com',
      allowed_surfaces: ['personal']
    }))
  })
  await page.goto('/setup')

  await expect(page.getByRole('heading', { name: 'Choose one deployment role' })).toBeVisible()
  const cards = page.locator('.profile-card')
  await expect(cards).toHaveCount(4)
  await expect(page.getByRole('button', { name: /Enterprise/ })).toHaveAttribute('aria-pressed', 'false')
  await expect(page.getByRole('button', { name: /AI Platform/ })).toHaveAttribute('aria-pressed', 'false')

  await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled()

  await page.getByRole('button', { name: /AI Platform/ }).click()
  await expect(page.getByRole('button', { name: /AI Platform/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: /Enterprise/ })).toHaveAttribute('aria-pressed', 'false')

  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByText('AI Platform', { exact: true })).toBeVisible()
  await expect(page.locator('.setup-review-grid strong').filter({ hasText: '/platform/overview' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Included with this installation' })).toBeVisible()
  await expect(page.getByText('Business and identity boundary')).toBeVisible()
  await expect(page.getByText('you manage platform tenants, callers, and integrations; the product keeps end users.', { exact: false })).toBeVisible()
  await expect(page.getByText('Developer API and product-integration operating boundary')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Not included in this deployment role' })).toBeVisible()
  await expect(page.getByText('External end-user accounts, sessions, and subscriptions')).toBeVisible()

  await page.getByRole('button', { name: 'Complete installation' }).click()
  await expect(page).toHaveURL(/\/login(?:\?|$)/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/platform/overview')
  await expect.poll(() => page.evaluate(() => ({
    token: localStorage.getItem('asterrouter_admin_token'),
    user: localStorage.getItem('asterrouter_admin_user')
  }))).toEqual({ token: null, user: null })
  const status = await page.request.get('/api/v1/setup/status')
  await expect(status).toBeOK()
  await expect(status.json()).resolves.toMatchObject({
    data: { default_profile: 'platform', enabled_profiles: ['platform'], setup_completed: true }
  })

  await page.locator('input#password').fill('setup-browser-test-password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/platform\/overview$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Platform overview' })).toBeVisible()
  await page.reload()
  await expect(page).toHaveURL(/\/platform\/overview$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Platform overview' })).toBeVisible()
  expect(errors).toEqual([])
})
