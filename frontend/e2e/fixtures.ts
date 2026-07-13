import { expect, type Page } from '@playwright/test'

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
  const demoButton = page.getByRole('button', { name: 'Enter demo mode' })
  await expect(demoButton).toBeVisible()
  await demoButton.click()
  await expect(page).toHaveURL(/\/console\/overview$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Personal Console' })).toBeVisible()
}

export async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => ({
    body: document.body.scrollWidth,
    document: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth
  }))
  expect(Math.max(dimensions.body, dimensions.document)).toBeLessThanOrEqual(dimensions.viewport + 1)
}
