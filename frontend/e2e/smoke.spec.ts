import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

const backendURL = `http://127.0.0.1:${process.env.ASTER_E2E_BACKEND_PORT || '18080'}`

test('@smoke backend health and public settings are ready', async ({ request }) => {
  const health = await request.get(`${backendURL}/health`)
  expect(health.status()).toBe(200)
  await expect(health.json()).resolves.toMatchObject({ data: { status: 'ok' } })

  const ready = await request.get(`${backendURL}/ready`)
  expect(ready.status()).toBe(200)
  await expect(ready.json()).resolves.toMatchObject({ data: { status: 'ready' } })

  const settings = await request.get(`${backendURL}/api/v1/settings/public`)
  expect(settings.status()).toBe(200)
  await expect(settings.json()).resolves.toMatchObject({ data: { demo_mode: true, setup_completed: true } })
})

test('@smoke anonymous protected navigation redirects to login', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await page.goto('/admin/providers?status=active')

  await expect(page).toHaveURL(/\/login\?redirect=/)
  const loginURL = new URL(page.url())
  expect(loginURL.searchParams.get('redirect')).toBe('/admin/providers?status=active')
  await expect(page.getByRole('heading', { level: 2, name: 'Welcome back' })).toBeVisible()
  await expect(page.getByLabel('Username')).toHaveValue('admin')
  await expect(page.locator('input#password')).toHaveAttribute('type', 'password')
  expect(errors).toEqual([])
})

test('@smoke demo login persists and opens enabled surfaces', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  await page.reload()
  await page.waitForLoadState('networkidle')
  await expect(page).toHaveURL(/\/console\/overview$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Personal Console' })).toBeVisible()

  for (const path of ['/operator/overview', '/admin/dashboard', '/portal/overview']) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page.locator('main')).toBeVisible()
  }
  expect(errors).toEqual([])
})

test('@smoke locale, theme, and responsive layout remain usable', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)

  const language = page.getByLabel('Language')
  await language.selectOption('zh-CN')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.getByRole('heading', { level: 1, name: '个人控制台' })).toBeVisible()

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
