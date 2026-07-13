import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { loginDemo } from './fixtures'

test('@smoke @j09 console overview has no serious accessibility violations', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The semantic audit runs once; layout coverage runs in every Chromium viewport.')

  await loginDemo(page)
  const results = await new AxeBuilder({ page }).analyze()
  const blocking = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
})
