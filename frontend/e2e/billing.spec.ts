import { expect, test, type Page } from '@playwright/test'
import { adminPost, createGatewayFixture, createPublishedPricingRule, envelope, loginDemo, loginUser, registerUsers } from './fixtures'

async function operatorPost<T>(page: Page, token: string, path: string, data: unknown): Promise<T> {
  return envelope<T>(await page.request.post(`/api/v1/operator${path}`, {
    headers: { Authorization: `Bearer ${token}` },
    data
  }))
}

async function operatorGet<T>(page: Page, token: string, path: string): Promise<T> {
  return envelope<T>(await page.request.get(`/api/v1/operator${path}`, {
    headers: { Authorization: `Bearer ${token}` }
  }))
}

async function customerGet<T>(page: Page, token: string, path: string): Promise<T> {
  return envelope<T>(await page.request.get(`/api/v1/customer${path}`, {
    headers: { Authorization: `Bearer ${token}` }
  }))
}

test('@smoke @j06 operator allocation and customer billing notifications stay atomic and isolated', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The billing workflow is viewport-independent and runs once on desktop.')

  await loginDemo(page)
  const adminToken = await page.evaluate(() => localStorage.getItem('asterrouter_admin_token') || '')
  const runID = `${testInfo.project.name}-${Date.now()}`
  const password = 'synthetic-password-123'
  const [lowBalanceUser, fundedUser] = await registerUsers(page, adminToken, [
    { email: `customer-low-${runID}@example.test`, password, displayName: 'Low Balance Customer', balanceMicros: 5_000_000 },
    { email: `customer-funded-${runID}@example.test`, password, displayName: 'Funded Customer', balanceMicros: 50_000_000 }
  ])
  const lowToken = await loginUser(page, lowBalanceUser.email, password)
  const fundedToken = await loginUser(page, fundedUser.email, password)

  const publicModel = `e2e-billing-${runID}`
  await createGatewayFixture(page, adminToken, runID, publicModel)

  const plan = await operatorPost<{ id: string }>(page, adminToken, '/plans', {
    name: `Internal allocation ${runID}`,
    monthly_fee_micros: 0,
    included_tokens: 100000,
    monthly_limit_micros: 100_000_000,
    status: 'active'
  })
  await createPublishedPricingRule(page, adminToken, 'operator', {
    name: `Synthetic pricing ${runID}`,
    purpose: 'customer_charge',
    scope_type: 'operator_plan',
    scope_id: plan.id,
    model: publicModel,
    expression: 'v1: token_line("input", uncached_input_tokens, 10000000000) + token_line("output", output_tokens, 10000000000)'
  })
  const operatorCustomer = await operatorPost<{ id: string }>(page, adminToken, '/customers', {
    name: `Internal consumer ${runID}`,
    email: `internal-${runID}@example.test`,
    plan_id: plan.id,
    status: 'active',
    credit_micros: 0
  })
  const allocation = await operatorPost<{ id: string; balance_after_micros: number }>(page, adminToken, '/balance-entries', {
    customer_id: operatorCustomer.id,
    kind: 'allocation_increase',
    amount_micros: 10_000_000,
    reference: `allocation-${runID}`,
    note: 'Synthetic initial allocation'
  })
  expect(allocation.balance_after_micros).toBe(10_000_000)
  const duplicate = await operatorPost<{ id: string; balance_after_micros: number }>(page, adminToken, '/balance-entries', {
    customer_id: operatorCustomer.id,
    kind: 'allocation_increase',
    amount_micros: 10_000_000,
    reference: `allocation-${runID}`,
    note: 'Synthetic duplicate retry'
  })
  expect(duplicate.id).toBe(allocation.id)
  expect(duplicate.balance_after_micros).toBe(10_000_000)
  expect((await operatorPost<{ balance_after_micros: number }>(page, adminToken, '/balance-entries', {
    customer_id: operatorCustomer.id,
    kind: 'allocation_decrease',
    amount_micros: -1_000_000,
    reference: `reclaim-${runID}`
  })).balance_after_micros).toBe(9_000_000)
  expect((await operatorPost<{ balance_after_micros: number }>(page, adminToken, '/balance-entries', {
    customer_id: operatorCustomer.id,
    kind: 'cost_correction',
    amount_micros: 500_000,
    reference: `correction-${runID}`
  })).balance_after_micros).toBe(9_500_000)

  const operatorKey = await operatorPost<{ key: string; record: { id: string } }>(page, adminToken, `/customers/${operatorCustomer.id}/keys`, {
    name: `Operator customer key ${runID}`,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })
  const operatorCompletion = await page.request.post('/v1/chat/completions', {
    headers: { Authorization: `Bearer ${operatorKey.key}` },
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic operator billing request' }] }
  })
  expect(operatorCompletion.status()).toBe(200)
  const operatorUsage = await operatorGet<{ total_requests: number; recent: Array<{ customer_id: string }> }>(page, adminToken, `/usage?customer_id=${operatorCustomer.id}`)
  expect(operatorUsage.total_requests).toBe(1)
  expect(operatorUsage.recent).toContainEqual(expect.objectContaining({ customer_id: operatorCustomer.id }))
  await expect.poll(async () => {
    const customers = await operatorGet<Array<{ id: string; balance_micros: number }>>(page, adminToken, '/customers')
    return customers.find((customer) => customer.id === operatorCustomer.id)?.balance_micros
  }, { message: 'customer charge ledger consumed by operator balance', timeout: 10_000 }).toBe(9_320_000)
  await expect.poll(async () => {
    const entries = await operatorGet<Array<{ customer_id: string }>>(page, adminToken, '/balance-entries')
    return entries.filter((entry) => entry.customer_id === operatorCustomer.id).length
  }, { message: 'customer charge balance entry created', timeout: 10_000 }).toBe(4)
  const entries = await operatorGet<Array<{ customer_id: string; kind: string; balance_after_micros: number }>>(page, adminToken, '/balance-entries')
  expect(entries.filter((entry) => entry.customer_id === operatorCustomer.id)).toHaveLength(4)
  expect(entries).toContainEqual(expect.objectContaining({ customer_id: operatorCustomer.id, kind: 'usage_charge', balance_after_micros: 9_320_000 }))

  const userKey = await adminPost<{ key: string; record: { id: string } }>(page, adminToken, '/api-keys', {
    name: `Low balance owned key ${runID}`,
    key_type: 'user',
    owner_user_id: lowBalanceUser.id,
    model_allowlist: [publicModel],
    qps_limit: 10,
    monthly_token_limit: 100000
  })
  const customerCompletion = await page.request.post('/v1/chat/completions', {
    headers: { Authorization: `Bearer ${userKey.key}` },
    data: { model: publicModel, messages: [{ role: 'user', content: 'synthetic customer notification request' }] }
  })
  expect(customerCompletion.status()).toBe(200)

  const lowBilling = await customerGet<{ balance_micros: number; total_micros: number }>(page, lowToken, '/billing')
  const fundedBilling = await customerGet<{ balance_micros: number; total_micros: number }>(page, fundedToken, '/billing')
  expect(lowBilling).toMatchObject({ balance_micros: 5_000_000, total_micros: 5_000_000 })
  expect(fundedBilling).toMatchObject({ balance_micros: 50_000_000, total_micros: 50_000_000 })
  const recharge = await page.request.post('/api/v1/customer/billing/recharge-orders', {
    headers: { Authorization: `Bearer ${lowToken}` },
    data: { amount_micros: 10_000_000, payment_method: 'wechat' }
  })
  expect(recharge.status()).toBe(503)
  expect(await customerGet<{ balance_micros: number }>(page, lowToken, '/billing')).toMatchObject({ balance_micros: 5_000_000 })

  await operatorPost(page, adminToken, '/notices', {
    title: `Synthetic notice ${runID}`,
    content: 'Synthetic customer broadcast',
    audience: 'all',
    status: 'published'
  })
  const lowNotifications = await customerGet<{ items: Array<{ id: string; type: string }>; unread: number }>(page, lowToken, '/notifications?limit=100&offset=0')
  const fundedNotifications = await customerGet<{ items: Array<{ id: string; type: string }>; unread: number }>(page, fundedToken, '/notifications?limit=100&offset=0')
  expect(lowNotifications.items).toContainEqual(expect.objectContaining({ type: 'balance_low' }))
  expect(fundedNotifications.items).not.toContainEqual(expect.objectContaining({ type: 'balance_low' }))
  expect(lowNotifications.items).toContainEqual(expect.objectContaining({ type: 'announcement' }))
  expect(fundedNotifications.items).toContainEqual(expect.objectContaining({ type: 'announcement' }))
  const lowOnlyNotification = lowNotifications.items.find((item) => item.type === 'balance_low')
  expect(lowOnlyNotification).toBeTruthy()
  expect((await page.request.post(`/api/v1/customer/notifications/${lowOnlyNotification!.id}/read`, {
    headers: { Authorization: `Bearer ${fundedToken}` }
  })).status()).toBe(404)
  expect((await page.request.post(`/api/v1/customer/notifications/${lowOnlyNotification!.id}/read`, {
    headers: { Authorization: `Bearer ${lowToken}` }
  })).status()).toBe(200)

  const lowExport = await page.request.get('/api/v1/customer/billing/entries/export?limit=100', {
    headers: { Authorization: `Bearer ${lowToken}` }
  })
  expect(lowExport.status()).toBe(200)
  expect(lowExport.headers()['content-type']).toContain('text/csv')
  expect(await lowExport.text()).toContain('金额')
})
