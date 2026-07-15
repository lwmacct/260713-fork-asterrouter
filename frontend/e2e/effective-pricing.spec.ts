import { expect, test } from '@playwright/test'
import { captureBrowserErrors, expectNoHorizontalOverflow, loginDemo } from './fixtures'

test('@effective-pricing automatic window policy remains usable across viewports', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  await page.goto('/admin/effective-pricing')
  await expect(page.getByRole('heading', { level: 1, name: 'Effective Pricing & Cache Routing' })).toBeVisible()

  await page.getByRole('button', { name: 'Policy' }).click()
  const dialog = page.getByRole('dialog', { name: 'Effective pricing policy' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByLabel('Evaluation interval (minutes)')).toHaveValue('60')
  await expect(dialog.getByLabel('Healthy windows before automatic promotion')).toHaveValue('3')
  await expect(dialog.getByLabel('Degraded windows before automatic rollback')).toHaveValue('2')
  const automaticActions = dialog.getByLabel(/Enable automatic promotion and rollback/)
  await automaticActions.scrollIntoViewIfNeeded()
  await expect(automaticActions).toBeVisible()
  await expect(automaticActions).not.toBeChecked()

  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('effective-pricing-policy.png'), fullPage: true })
  expect(errors).toEqual([])
})

test('@effective-pricing billing source inspection keeps aggregate evidence distinct from bill lines', async ({ page }, testInfo) => {
  const errors = captureBrowserErrors(page)
  const source = {
    id: 'source-e2e', provider_id: 'provider-source-e2e', provider_account_id: 'account-source-e2e', adapter_id: 'sub2api_compatible',
    status: 'observe_only', automatic_sync_enabled: true, sync_interval_seconds: 3600,
    capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
    detection_status: 'schema_match', contract_version: 'sub2api_v1_usage', evidence_hash: '0123456789abcdef0123456789abcdef', warnings: [],
    next_sync_at: '2026-07-15T09:00:00Z', last_sync_started_at: '2026-07-15T08:00:00Z',
    last_sync_completed_at: '2026-07-15T08:00:01Z', last_success_at: '2026-07-15T08:00:01Z',
    consecutive_failures: 0, last_error_code: '', version: 3, created_by: 'admin', updated_by: 'admin',
    created_at: '2026-07-15T07:00:00Z', updated_at: '2026-07-15T08:00:01Z',
    routing_health: { source_status: 'observe_only', status: 'observe_only', hard_blocked: false, economic_switch_eligible: false, reason_codes: ['provider_billing_source_observe_only'], evaluated_at: '2026-07-15T08:00:01Z', evidence_observed_at: '2026-07-15T08:00:01Z', evidence_stale_after_seconds: 21600 }
  }
  const run = {
    id: 'run-e2e', source_id: source.id, provider_id: source.provider_id, provider_account_id: source.provider_account_id,
    trigger: 'scheduled', triggered_by: 'worker', adapter_id: source.adapter_id, status: 'succeeded',
    capabilities: source.capabilities, detection_status: source.detection_status, contract_version: source.contract_version,
    discovered_lines: 0, imported_lines: 0, skipped_lines: 0, evidence_hash: source.evidence_hash,
    warnings: [], error_code: '', started_at: '2026-07-15T08:00:00Z', finished_at: '2026-07-15T08:00:01Z', created_at: '2026-07-15T08:00:00Z'
  }
  const balance = {
    id: 'balance-e2e', source_id: source.id, sync_run_id: run.id, provider_account_id: source.provider_account_id,
    kind: 'api_key_quota_remaining', amount_micros: 7_500_000, unlimited: false, currency: 'USD',
    evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
  }
  const aggregate = {
    id: 'aggregate-e2e', source_id: source.id, sync_run_id: run.id, provider_account_id: source.provider_account_id,
    scope: 'model_30d', model: 'claude-sonnet', request_count: 7, input_tokens: 350, output_tokens: 60,
    cache_creation_tokens: 100, cache_read_tokens: 180, list_cost_micros: 6_500_000, actual_cost_micros: 3_250_000,
    currency: 'USD', evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
  }
  let savedPayload: Record<string, unknown> | undefined
  let syncRequests = 0
  await loginDemo(page)
  await page.route('**/api/v1/admin/provider-accounts', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        message: 'success',
        data: [{ id: 'account-source-e2e', provider_id: 'provider-source-e2e', name: 'Synthetic procurement', status: 'active', models: ['synthetic-model'] }]
      })
    })
  })
  await page.route('**/api/v1/admin/provider-billing-sources/inspect', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        message: 'success',
        data: {
          provider_id: 'provider-source-e2e', provider_account_id: 'account-source-e2e',
          provider_name: 'Synthetic channel', provider_account_name: 'Synthetic procurement',
          adapter_id: 'sub2api_compatible', detection_status: 'schema_match', contract_version: 'sub2api_v1_usage',
          currency: 'USD',
          capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
          balance: { kind: 'api_key_quota_remaining', amount_micros: 7_500_000, unlimited: false, currency: 'USD', observed_at: '2026-07-15T08:00:00Z' },
          usage_aggregates: [{ scope: 'total', request_count: 10, input_tokens: 500, output_tokens: 80, cache_creation_tokens: 120, cache_read_tokens: 200, list_cost_micros: 8_000_000, actual_cost_micros: 4_250_000 }, { scope: 'model_30d', model: 'claude-sonnet', request_count: 7, input_tokens: 350, output_tokens: 60, cache_creation_tokens: 100, cache_read_tokens: 180, list_cost_micros: 6_500_000, actual_cost_micros: 3_250_000 }],
          discovered_lines: 0, evidence_hash: '0123456789abcdef0123456789abcdef',
          warnings: ['usage_cost_lines_unavailable', 'remaining_is_quota_not_wallet_balance', 'aggregate_totals_are_not_billing_lines'],
          observed_at: '2026-07-15T08:00:00Z'
        }
      })
    })
  })
  await page.route('**/api/v1/admin/provider-billing-sources', async (route) => {
    if (route.request().method() === 'PUT') {
      savedPayload = route.request().postDataJSON()
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { ...source, ...savedPayload, version: 4 } }) })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: [source] }) })
  })
  await page.route('**/api/v1/admin/provider-billing-sources/source-e2e/evidence**', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { source, runs: [run], balances: [balance], aggregates: [aggregate] } }) })
  })
  await page.route('**/api/v1/admin/provider-billing-sources/source-e2e/sync', async (route) => {
    syncRequests++
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, message: 'success', data: { source: { ...source, version: 5 }, run, balance, aggregates: [aggregate] } }) })
  })

  await page.goto('/admin/effective-pricing')
  await page.getByRole('button', { name: 'Billing source' }).click()
  await expect(page.getByRole('heading', { name: 'Third-party billing source inspection' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Sync run history' })).toBeVisible()
  await expect(page.locator('.routing-health-summary')).toContainText('Routing health')
  await expect(page.locator('.routing-health-summary')).toContainText('Automatic economic switch')
  await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
  await expect(page.getByText('$7.50', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Auto-detect' }).click()
  const inspectionResult = page.locator('.billing-source-result')
  await expect(inspectionResult.locator('.source-result-head p').filter({ hasText: 'sub2api_compatible' })).toBeVisible()
  await expect(inspectionResult.getByText('API key quota remaining', { exact: true })).toBeVisible()
  await expect(inspectionResult.getByText('Aggregate totals are evidence only and are not billing lines. They never create a pseudo-precise unit price.')).toBeVisible()
  await expect(inspectionResult.getByRole('cell', { name: '$4.25' })).toBeVisible()
  await expect(inspectionResult.getByText('claude-sonnet · Last 30 days')).toBeVisible()
  await page.getByLabel('Enable automatic sync').uncheck()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect.poll(() => savedPayload).toMatchObject({ provider_account_id: 'account-source-e2e', automatic_sync_enabled: false, sync_interval_seconds: 3600, version: 3 })
  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect.poll(() => syncRequests).toBe(1)
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('effective-pricing-billing-source.png'), fullPage: true })
  expect(errors).toEqual([])
})
