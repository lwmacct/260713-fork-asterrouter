import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { captureBrowserErrors, envelope, loginDemo, registerUsers } from './fixtures'

async function loginThroughPage(page: Page, email: string, password: string, redirect: string): Promise<void> {
  await page.goto(`/login?redirect=${encodeURIComponent(redirect)}`)
  await page.getByLabel('Username').fill(email)
  await page.locator('input#password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(new RegExp(`${redirect}$`))
}

async function focusWithTab(page: Page, target: ReturnType<Page['getByRole']>): Promise<void> {
  for (let index = 0; index < 80; index++) {
    await page.keyboard.press('Tab')
    if (await target.evaluate((element) => document.activeElement === element)) return
  }
  await expect(target).toBeFocused()
}

test('@smoke @j09 console overview has no serious accessibility violations', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The semantic audit runs once; layout coverage runs in every Chromium viewport.')

  await loginDemo(page)
  const results = await new AxeBuilder({ page }).analyze()
  const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
})

test('@smoke @j09 customer and account sessions are isolated, surface-safe, and keyboard-operable', async ({ browser, page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The cross-session workflow is viewport-independent and runs once on desktop.')

  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const runID = `${testInfo.project.name}-${Date.now()}`
  const password = 'synthetic-password-123'
  const [customerA, customerB] = await registerUsers(page, adminToken, [
    { email: `surface-a-${runID}@example.test`, password, displayName: 'Surface Customer A', balanceMicros: 5_000_000 },
    { email: `surface-b-${runID}@example.test`, password, displayName: 'Surface Customer B', balanceMicros: 50_000_000 }
  ])

  await page.context().clearCookies()
  await page.evaluate(() => localStorage.clear())
  await loginThroughPage(page, customerA.email, password, '/customer/overview')
  await expect(page.getByRole('heading', { level: 1, name: 'Account overview' })).toBeVisible()
  const customerAToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const customerABilling = await envelope<{ balance_micros: number }>(await page.request.get('/api/v1/customer/billing', {
    headers: { Authorization: `Bearer ${customerAToken}` }
  }))
  expect(customerABilling.balance_micros).toBe(5_000_000)

  const origin = new URL(page.url()).origin
  const otherContext = await browser.newContext()
  const otherPage = await otherContext.newPage()
  try {
    await loginThroughPage(otherPage, customerB.email, password, `${origin}/customer/overview`)
    await expect(otherPage.getByRole('heading', { level: 1, name: 'Account overview' })).toBeVisible()
    const customerBToken = await otherPage.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
    const customerBBilling = await envelope<{ balance_micros: number }>(await otherPage.request.get(`${origin}/api/v1/customer/billing`, {
      headers: { Authorization: `Bearer ${customerBToken}` }
    }))
    expect(customerBBilling.balance_micros).toBe(50_000_000)
    expect(customerBBilling.balance_micros).not.toBe(customerABilling.balance_micros)

    await otherPage.goto(`${origin}/customer/account`)
    await expect(otherPage.getByLabel('Email')).toHaveValue(customerB.email)
  } finally {
    await otherContext.close()
  }

  await page.goto('/customer/account')
  await expect(page.getByLabel('Email')).toHaveValue(customerA.email)
  expect([403, 404]).toContain((await page.request.get('/api/v1/admin/dashboard', { headers: { Authorization: `Bearer ${customerAToken}` } })).status())
  expect((await page.request.get('/api/v1/operator/dashboard', { headers: { Authorization: `Bearer ${customerAToken}` } })).status()).toBe(403)

  await page.goto('/admin/dashboard')
  await expect(page).toHaveURL(/\/customer\/overview$/)
  await page.goto('/operator/overview')
  await expect(page).toHaveURL(/\/customer\/overview$/)

  const themeButton = page.getByRole('button', { name: 'Dark mode' })
  await focusWithTab(page, themeButton)
  await page.keyboard.press('Enter')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  const billingLink = page.getByRole('link', { name: 'Billing & recharge', exact: true })
  await focusWithTab(page, billingLink)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/customer\/billing$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Billing & recharge' })).toBeVisible()
  expect(errors).toEqual([])
})
