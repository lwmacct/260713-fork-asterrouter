import { expect, test } from '@playwright/test'
import { adminPost, captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

test('@pricing expression pricing lifecycle remains usable across viewports', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const token = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const suffix = `${testInfo.project.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}-${Date.now().toString(36)}`
  const model = `pricing-e2e-${suffix}`
  await adminPost(page, token, '/gateway-models', {
    model_id: model,
    name: `Pricing E2E ${suffix}`,
    description: 'Synthetic pricing browser model',
    modality: 'chat',
    default_route_group: 'default',
    sticky_enabled: false,
    sticky_ttl_seconds: 0,
    status: 'active'
  })

  await page.goto('/admin/pricing')
  await expect(page.getByRole('heading', { level: 1, name: 'Expression Pricing' })).toBeVisible()
  await page.getByRole('button', { name: 'New rule' }).click()
  const dialog = page.getByRole('dialog', { name: 'New rule' })
  await dialog.getByLabel('Rule name').fill(`Browser cost ${suffix}`)
  await dialog.getByLabel('Model').fill(model)
  await dialog.getByLabel('v1 expression').fill('v1: fixed_line("request", "request", 125)')
  await dialog.getByRole('button', { name: 'Create rule' }).click()

  await expect(page.getByText('Rule created')).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: `Browser cost ${suffix}` })).toBeVisible()
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByText('Validation passed').first()).toBeVisible()

  await page.getByRole('button', { name: 'Simulation' }).click()
  await page.getByRole('button', { name: 'Run simulation' }).click()
  await expect(page.getByText('$0.000125').first()).toBeVisible()

  await page.getByRole('button', { name: 'Rule editor' }).click()
  await page.getByRole('button', { name: 'Publish' }).click()
  await expect(page.getByText('Version published and activated')).toBeVisible()
  await page.getByRole('button', { name: 'Version history' }).click()
  await expect(page.getByText('Active version')).toBeVisible()

  await expectNoHorizontalOverflow(page)
  await page.getByLabel('Language').selectOption('zh-CN')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  const themeButton = page.getByRole('button', { name: /深色模式|浅色模式/ })
  await themeButton.click()
  if ((page.viewportSize()?.width || 0) <= 640) {
    await page.getByRole('button', { name: '关闭导航' }).first().click()
    await expect(page.locator('.admin-sidebar')).not.toHaveClass(/mobile-open/)
    await expect(page.locator('.sidebar-overlay')).toHaveCount(0)
    await page.waitForTimeout(350)
  }
  await expectNoHorizontalOverflow(page)
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.waitForTimeout(100)
  await page.screenshot({ path: testInfo.outputPath(`pricing-${suffix}.png`), fullPage: true })
  expect(errors).toEqual([])
})
