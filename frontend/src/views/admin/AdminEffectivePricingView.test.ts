import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import * as control from '@/api/control'
import AdminEffectivePricingView from './AdminEffectivePricingView.vue'

vi.mock('@/api/control', () => ({
  actOnEffectivePricingDecision: vi.fn(),
  createProcurementPrice: vi.fn(),
  createProviderBillingLine: vi.fn(),
  evaluateEffectivePricingDecision: vi.fn(),
  getEffectivePricingDecisions: vi.fn(),
  getEffectivePricingDecisionEvaluations: vi.fn(),
  getEffectivePricingReport: vi.fn(),
  getProviderAccounts: vi.fn(),
  getProviderBillingSourceEvidence: vi.fn(),
  getProviderBillingSources: vi.fn(),
  getProviderCacheCapabilities: vi.fn(),
  getProviderCacheProbeRuns: vi.fn(),
  inspectProviderBillingSource: vi.fn(),
  runProviderCacheProbe: vi.fn(),
  syncProviderBillingSource: vi.fn(),
  updateProviderBillingSource: vi.fn(),
  updateProviderCacheCapability: vi.fn(),
  updateEffectivePricingPolicy: vi.fn()
}))

describe('AdminEffectivePricingView', () => {
  beforeEach(() => {
    vi.mocked(control.getEffectivePricingReport).mockResolvedValue({
      window_start: '2026-07-13T12:00:00Z',
      window_end: '2026-07-14T12:00:00Z',
      policy: {
        id: 'default', mode: 'observe_only', window_hours: 24, min_sample_count: 200,
        min_metrics_coverage: 0.8, min_billing_consistency: 0.95, min_cost_improvement: 0.08,
        min_cache_hit_rate_improvement: 0.1, min_affinity_improvement: 0.1, max_cache_tiebreak_cost_regression: 0.02,
        max_error_rate_regression: 0.005, max_p95_latency_regression: 0.2, canary_percent: 5,
        supplier_affinity_ttl_seconds: 86400, account_affinity_ttl_seconds: 1800,
        automatic_actions_enabled: false, evaluation_interval_minutes: 60,
        promotion_window_count: 3, degradation_window_count: 2,
        probe_enabled: true, probe_daily_token_budget: 100000, probe_daily_cost_budget_micros: 10000000,
        probe_cooldown_seconds: 3600, updated_by: '', created_at: '', updated_at: ''
      },
      rows: [{
        provider_id: 'provider-a', provider_name: 'Channel A', provider_account_id: 'account-a',
        provider_account_name: 'Procurement A', upstream_model: 'model-a', protocol: 'openai_chat_completions',
        currency: 'USD', cost_available: true,
        uncached_input_micros_per_1m_tokens: 1000000, cache_read_micros_per_1m_tokens: 100000,
        cache_write_5m_micros_per_1m_tokens: 1250000, cache_write_1h_micros_per_1m_tokens: 2000000,
        output_micros_per_1m_tokens: 3000000, request_micros: 50,
        reference_input_micros_per_1m_tokens: 1000000, reference_output_micros_per_1m_tokens: 3000000,
        recharge_multiplier: 0.9, quoted_multiplier: 0.2, billed_multiplier: 0.6, effective_multiplier: 0.5,
        effective_cost_micros_per_1m: 500000, request_count: 1000, error_rate: 0.01, p95_latency_ms: 420,
        uncached_cost_micros_per_1m: 1000000, cache_savings_micros_per_1m: 500000,
        cache_savings_rate: 0.5, cache_economics_available: true,
        metrics_coverage: 0.98, eligible_request_hit_rate: 0.7, cache_token_hit_rate: 0.65,
        cache_write_read_ratio: 0.2, billing_consistency_rate: 0.99, affinity_consistency_rate: 0.95,
        cache_support_status: 'billed_verified', pool_affinity_grade: 'verified', cost_confidence: 'exact',
        price_id: 'price-a', recommendation: 'preferred', reason_codes: []
      }],
      decisions: []
    })
    vi.mocked(control.getProviderCacheCapabilities).mockResolvedValue([])
    vi.mocked(control.getProviderCacheProbeRuns).mockResolvedValue([])
    vi.mocked(control.getEffectivePricingDecisions).mockResolvedValue([])
    vi.mocked(control.getEffectivePricingDecisionEvaluations).mockResolvedValue([])
    vi.mocked(control.getProviderAccounts).mockResolvedValue([{
      id: 'account-a', provider_id: 'provider-a', name: 'Procurement A', status: 'active', models: ['model-a']
    } as never])
    vi.mocked(control.getProviderBillingSources).mockResolvedValue([])
    vi.mocked(control.runProviderCacheProbe).mockResolvedValue({ status: 'succeeded' } as never)
    vi.mocked(control.updateProviderCacheCapability).mockResolvedValue({ support_status: 'claimed' } as never)
    vi.mocked(control.inspectProviderBillingSource).mockResolvedValue({
      provider_id: 'provider-a', provider_account_id: 'account-a', provider_name: 'Channel A', provider_account_name: 'Procurement A',
      adapter_id: 'sub2api_compatible', detection_status: 'schema_match', contract_version: 'sub2api_v1_usage',
      currency: 'USD',
      capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
      balance: { kind: 'api_key_quota_remaining', amount_micros: 7_500_000, unlimited: false, currency: 'USD', observed_at: '2026-07-15T08:00:00Z' },
      usage_aggregates: [{ scope: 'total', request_count: 10, input_tokens: 500, output_tokens: 80, cache_creation_tokens: 120, cache_read_tokens: 200, list_cost_micros: 8_000_000, actual_cost_micros: 4_250_000 }, { scope: 'model_30d', model: 'claude-sonnet', request_count: 7, input_tokens: 350, output_tokens: 60, cache_creation_tokens: 100, cache_read_tokens: 180, list_cost_micros: 6_500_000, actual_cost_micros: 3_250_000 }],
      discovered_lines: 0, evidence_hash: '0123456789abcdef0123456789abcdef',
      warnings: ['usage_cost_lines_unavailable', 'remaining_is_quota_not_wallet_balance', 'aggregate_totals_are_not_billing_lines'], observed_at: '2026-07-15T08:00:00Z'
    })
  })

  it('renders effective cost evidence and responsive tab content', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Channel A')
    expect(wrapper.text()).toContain('0.50x')
    expect(wrapper.text()).toContain('420 ms')
    expect(wrapper.text()).toContain('Net cache savings 50%')
    expect(wrapper.text()).toContain('1.25x')
    expect(wrapper.findAll('.ep-table tbody tr')).toHaveLength(1)
    expect(wrapper.get('.effective-filters option[value="gemini_generate_content"]').text()).toBe('Gemini Generate Content')

    await wrapper.find('.ep-table tbody button').trigger('click')
    expect(wrapper.find('.evidence-drawer').exists()).toBe(true)
    expect(wrapper.get('.evidence-drawer').text()).toContain('Uncached equivalent cost')
    expect(wrapper.get('.evidence-drawer').text()).toContain('5-minute cache write price')
    await wrapper.get('.evidence-drawer .icon-button').trigger('click')

    const tabs = wrapper.findAll('.effective-tabs button')
    await tabs[1].trigger('click')
    expect(wrapper.find('.cache-row').exists()).toBe(true)

    await wrapper.get('.page-header .button').trigger('click')
    expect(wrapper.get('.effective-dialog').text()).toContain('Maximum error-rate regression')
    expect(wrapper.get('.effective-dialog').text()).toContain('Maximum P95 latency regression')
    expect(wrapper.get('.effective-dialog').text()).toContain('Healthy windows before automatic promotion')
    expect(wrapper.get('.effective-dialog').text()).toContain('Enable automatic promotion and rollback')
    expect(wrapper.find('.effective-dialog option[value="fixed_route"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('renders observed zero cost as a real value instead of missing evidence', async () => {
    const report = await control.getEffectivePricingReport({})
    vi.mocked(control.getEffectivePricingReport).mockResolvedValue({
      ...report,
      rows: [{
        ...report.rows[0],
        cost_available: true,
        effective_multiplier: 0,
        effective_cost_micros_per_1m: 0
      }]
    })

    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    const cells = wrapper.get('.ep-table tbody tr').findAll('td')
    expect(cells[3].text()).toBe('0.00x')
    expect(cells[4].get('strong').text()).toBe('$0.00')
    expect(cells[3].text()).not.toBe('-')
    expect(cells[4].get('strong').text()).not.toBe('-')

    wrapper.unmount()
  })

  it('captures the complete cache-aware procurement price', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-panel .panel-header button')[1].trigger('click')
    const dialog = wrapper.get('.effective-dialog')
    expect(dialog.text()).toContain('5-minute cache write price')
    expect(dialog.text()).toContain('1-hour cache write price')
    expect(dialog.text()).toContain('Per-request fee')
    expect(dialog.text()).toContain('Recharge paid multiplier')
    expect(dialog.find('option[value="gemini_generate_content"]').exists()).toBe(true)

    const field = (label: string) => dialog.findAll('.field').find((item) => item.text().includes(label))!
    await field('5-minute cache write price').get('input').setValue(1250000)
    await field('1-hour cache write price').get('input').setValue(2000000)
    await field('Per-request fee').get('input').setValue(75)
    await field('Recharge paid multiplier').get('input').setValue(0.8)
    await dialog.trigger('submit')
    await flushPromises()

    expect(control.createProcurementPrice).toHaveBeenCalledWith(expect.objectContaining({
      provider_account_id: 'account-a', cache_write_5m_micros_per_1m_tokens: 1250000,
      cache_write_1h_micros_per_1m_tokens: 2000000, request_micros: 75, recharge_multiplier: 0.8
    }))
    wrapper.unmount()
  })

  it('configures verified third-party affinity transport from the cache view', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-tabs button')[1].trigger('click')
    await wrapper.get('.cache-row button').trigger('click')
    const dialog = wrapper.get('.effective-dialog')
    const field = (label: string) => dialog.findAll('.field').find((item) => item.text().includes(label))!
    await field('Affinity transport').get('select').setValue('header')
    await field('Affinity field').get('input').setValue('X-Session-ID')
    await field('Cache control mode').get('select').setValue('prompt_cache_key')
    await dialog.trigger('submit')
    await flushPromises()

    expect(control.updateProviderCacheCapability).toHaveBeenCalledWith({
      provider_account_id: 'account-a', upstream_model: 'model-a', protocol: 'openai_chat_completions',
      support_status: 'claimed', pool_affinity_grade: 'unknown', affinity_transport: 'header',
      affinity_field: 'X-Session-ID', cache_control_mode: 'prompt_cache_key', usage_schema: 'auto'
    })
    wrapper.unmount()
  })

  it('requires explicit cost confirmation before running a cache probe', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-tabs button')[3].trigger('click')
    await wrapper.get('.effective-panel .panel-header button').trigger('click')
    expect(wrapper.find('.effective-dialog').exists()).toBe(true)
    expect(wrapper.find('.effective-dialog option[value="gemini_generate_content"]').exists()).toBe(true)

    const submit = wrapper.get('.modal-footer button[type="submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    await wrapper.get('.probe-confirmation input').setValue(true)
    expect(submit.attributes('disabled')).toBeUndefined()
    await wrapper.get('.effective-dialog').trigger('submit')
    await flushPromises()

    expect(control.runProviderCacheProbe).toHaveBeenCalledWith({
      provider_account_id: 'account-a', upstream_model: 'model-a', protocol: 'openai_chat_completions',
      prefix_tokens: 2048, max_cost_micros: 100000
    })
    wrapper.unmount()
  })

  it('inspects a provider billing source without presenting aggregates as bill lines', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-tabs button')[4].trigger('click')
    await wrapper.get('.billing-source-controls button').trigger('click')
    await flushPromises()

    expect(control.inspectProviderBillingSource).toHaveBeenCalledWith('account-a')
    expect(wrapper.get('.billing-source-result').text()).toContain('sub2api_compatible')
    expect(wrapper.get('.source-capabilities').text()).toContain('Per-request cost lines')
    expect(wrapper.get('.source-capabilities').text()).toContain('Unavailable')
    expect(wrapper.get('.source-balance').text()).toContain('API key quota remaining')
    expect(wrapper.get('.source-aggregate-table').text()).toContain('$4.25')
    expect(wrapper.get('.source-aggregate-table').text()).toContain('claude-sonnet · Last 30 days')
    expect(wrapper.get('.source-warnings').text()).toContain('Aggregate totals are evidence only and are not billing lines.')
    wrapper.unmount()
  })

  it('saves a persisted billing source with CAS and refreshes sync evidence', async () => {
    const source = {
      id: 'source-a', provider_id: 'provider-a', provider_account_id: 'account-a', adapter_id: 'sub2api_compatible',
      status: 'observe_only' as const, automatic_sync_enabled: true, sync_interval_seconds: 3600,
      capabilities: { usage_cost_lines: false, aggregate_usage: true, balance: true, incremental_sync: false, price_feed: false },
      detection_status: 'schema_match', contract_version: 'sub2api_v1_usage', evidence_hash: 'hash-a', warnings: [],
      next_sync_at: '2026-07-15T09:00:00Z', last_sync_started_at: '2026-07-15T08:00:00Z',
      last_sync_completed_at: '2026-07-15T08:00:01Z', last_success_at: '2026-07-15T08:00:01Z',
      consecutive_failures: 0, last_error_code: '', version: 3, created_by: 'admin', updated_by: 'admin',
      created_at: '2026-07-15T07:00:00Z', updated_at: '2026-07-15T08:00:01Z',
      routing_health: { source_status: 'observe_only', status: 'observe_only', hard_blocked: false, economic_switch_eligible: false, reason_codes: ['provider_billing_source_observe_only'], evaluated_at: '2026-07-15T08:00:01Z', evidence_observed_at: '2026-07-15T08:00:01Z', evidence_stale_after_seconds: 21600 }
    }
    const evidence = {
      source,
      runs: [{
        id: 'run-a', source_id: source.id, provider_id: source.provider_id, provider_account_id: source.provider_account_id,
        trigger: 'scheduled' as const, triggered_by: 'worker', adapter_id: source.adapter_id, status: 'succeeded' as const,
        capabilities: source.capabilities, detection_status: source.detection_status, contract_version: source.contract_version,
        discovered_lines: 0, imported_lines: 0, skipped_lines: 0, evidence_hash: source.evidence_hash,
        warnings: [], error_code: '', started_at: '2026-07-15T08:00:00Z', finished_at: '2026-07-15T08:00:01Z', created_at: '2026-07-15T08:00:00Z'
      }],
      balances: [{
        id: 'balance-a', source_id: source.id, sync_run_id: 'run-a', provider_account_id: source.provider_account_id,
        kind: 'wallet_balance' as const, amount_micros: 12_500_000, unlimited: false, currency: 'USD',
        evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
      }],
      aggregates: [{
        id: 'aggregate-a', source_id: source.id, sync_run_id: 'run-a', provider_account_id: source.provider_account_id,
        scope: 'model_30d', model: 'model-a', request_count: 20, input_tokens: 1000, output_tokens: 100,
        cache_creation_tokens: 80, cache_read_tokens: 600, list_cost_micros: 3_000_000, actual_cost_micros: 1_250_000,
        currency: 'USD', evidence_hash: source.evidence_hash, observed_at: '2026-07-15T08:00:00Z', created_at: '2026-07-15T08:00:01Z'
      }]
    }
    vi.mocked(control.getProviderBillingSources).mockResolvedValue([source])
    vi.mocked(control.getProviderBillingSourceEvidence).mockResolvedValue(evidence)
    vi.mocked(control.updateProviderBillingSource).mockResolvedValue({ ...source, version: 4, automatic_sync_enabled: false })
    vi.mocked(control.syncProviderBillingSource).mockResolvedValue({
      source: { ...source, version: 6 }, run: evidence.runs[0], balance: evidence.balances[0], aggregates: evidence.aggregates
    })

    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('.effective-tabs button')[4].trigger('click')

    expect(control.getProviderBillingSourceEvidence).toHaveBeenCalledWith('source-a', 100)
    expect(wrapper.get('.source-history-table').text()).toContain('succeeded')
    expect(wrapper.get('.billing-source-evidence').text()).toContain('$12.50')
    expect(wrapper.get('.billing-source-evidence').text()).toContain('model-a · Last 30 days')
    expect(wrapper.get('.routing-health-summary').text()).toContain('Routing health')
    expect(wrapper.get('.routing-health-summary').text()).toContain('Automatic economic switch')

    await wrapper.get('.source-auto-sync input').setValue(false)
    await wrapper.findAll('.billing-source-config button')[0].trigger('click')
    await flushPromises()
    expect(control.updateProviderBillingSource).toHaveBeenCalledWith({
      provider_account_id: 'account-a', adapter_id: 'sub2api_compatible', status: 'observe_only',
      automatic_sync_enabled: false, sync_interval_seconds: 3600, version: 3
    })

    await wrapper.findAll('.billing-source-config button')[1].trigger('click')
    await flushPromises()
    expect(control.syncProviderBillingSource).toHaveBeenCalledWith('source-a')
    expect(control.getProviderBillingSourceEvidence).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('keeps gateway and upstream models separate when evaluating and displaying a switch', async () => {
    const initialReport = await control.getEffectivePricingReport({})
    const baseRow = initialReport.rows[0]
    vi.mocked(control.getEffectivePricingReport).mockResolvedValue({
      ...initialReport,
      rows: [
        { ...baseRow, provider_account_id: 'account-a', provider_account_name: 'Procurement A', upstream_model: 'upstream-a', cache_token_hit_rate: 0.11, error_rate: 0.011, p95_latency_ms: 111 },
        { ...baseRow, provider_account_id: 'account-a', provider_account_name: 'Procurement A', upstream_model: 'upstream-b', cache_token_hit_rate: 0.22, cache_savings_rate: 0.1, error_rate: 0.022, p95_latency_ms: 222 },
        { ...baseRow, provider_id: 'provider-b', provider_name: 'Channel B', provider_account_id: 'account-b', provider_account_name: 'Procurement B', upstream_model: 'upstream-b', cache_token_hit_rate: 0.77, cache_savings_rate: 0.6, error_rate: 0.033, p95_latency_ms: 333 }
      ]
    })
    vi.mocked(control.getEffectivePricingDecisions).mockResolvedValue([{
      id: 'decision-b', model: 'gateway-public', upstream_model: 'upstream-b', protocol: 'openai_chat_completions',
      current_provider_account_id: 'account-a', candidate_provider_account_id: 'account-b',
      current_cost_micros_per_1m: 800000, candidate_cost_micros_per_1m: 500000, cost_improvement: 0.375,
      status: 'recommended', reason_codes: [], canary_percent: 5, sample_count: 1000, confidence: 'exact',
      healthy_window_count: 0, degraded_window_count: 0, last_evaluation_id: '',
      last_evaluation_verdict: '', last_evaluation_reason_codes: [], last_automatic_action: '',
      created_by: 'tester', created_at: '2026-07-14T12:00:00Z', updated_at: '2026-07-14T12:00:00Z'
    }])
    vi.mocked(control.getProviderAccounts).mockResolvedValue([
      { id: 'account-a', provider_id: 'provider-a', name: 'Procurement A', status: 'active', models: ['upstream-a', 'upstream-b'] },
      { id: 'account-b', provider_id: 'provider-b', name: 'Procurement B', status: 'active', models: ['upstream-b'] }
    ] as never)

    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-tabs button')[2].trigger('click')
    const cardText = wrapper.get('.decision-card').text()
    expect(cardText).toContain('gateway-public')
    expect(cardText).toContain('upstream-b')
    expect(cardText).toContain('22%')
    expect(cardText).toContain('77%')
    expect(cardText).toContain('Net cache savings 10%')
    expect(cardText).toContain('Net cache savings 60%')
    expect(cardText).toContain('222 ms')
    expect(cardText).toContain('333 ms')
    expect(cardText).not.toContain('111 ms')

    await wrapper.findAll('.effective-tabs button')[0].trigger('click')
    const upstreamBRows = wrapper.findAll('.ep-table tbody tr')
    expect(upstreamBRows[0].findAll('button')[1].attributes('disabled')).toBeDefined()
    expect(upstreamBRows[2].findAll('button')[1].attributes('disabled')).toBeUndefined()
    await upstreamBRows[2].findAll('button')[1].trigger('click')
    const dialog = wrapper.get('.effective-dialog')
    expect(dialog.get('input').element.value).toBe('')
    expect(dialog.findAll('select')[0].element.value).toBe('upstream-b')
    expect(dialog.findAll('select')[2].element.value).toBe('account-a')
    expect(dialog.findAll('select')[3].element.value).toBe('account-b')

    await dialog.get('input').setValue('gateway-public')
    await dialog.trigger('submit')
    await flushPromises()
    expect(control.evaluateEffectivePricingDecision).toHaveBeenCalledWith({
      model: 'gateway-public', upstream_model: 'upstream-b', protocol: 'openai_chat_completions',
      current_provider_account_id: 'account-a', candidate_provider_account_id: 'account-b'
    })
    wrapper.unmount()
  })

  it('loads immutable window evidence and shows automatic actions', async () => {
    vi.mocked(control.getEffectivePricingDecisions).mockResolvedValue([{
      id: 'decision-window', model: 'gateway-public', upstream_model: 'model-a', protocol: 'openai_chat_completions',
      current_provider_account_id: 'account-a', candidate_provider_account_id: 'account-b',
      current_cost_micros_per_1m: 800000, candidate_cost_micros_per_1m: 500000, cost_improvement: 0.375,
      status: 'active', reason_codes: [], canary_percent: 5, sample_count: 1000, confidence: 'exact',
      healthy_window_count: 3, degraded_window_count: 0, last_evaluation_id: 'window-1',
      last_evaluation_verdict: 'healthy', last_evaluation_reason_codes: [],
      last_evaluated_window_end: '2026-07-15T12:00:00Z', monitoring_started_at: '2026-07-15T09:00:00Z',
      last_healthy_at: '2026-07-15T12:00:00Z', last_automatic_action: 'activate',
      created_by: 'tester', created_at: '2026-07-15T09:00:00Z', updated_at: '2026-07-15T12:00:00Z'
    }])
    vi.mocked(control.getEffectivePricingDecisionEvaluations).mockResolvedValue([{
      id: 'window-1', decision_id: 'decision-window', window_start: '2026-07-15T09:00:00Z',
      window_end: '2026-07-15T12:00:00Z', verdict: 'healthy', reason_codes: ['cache_quality_tiebreaker'],
      current_snapshot_id: 'snapshot-current', candidate_snapshot_id: 'snapshot-candidate',
      current_request_count: 1000, candidate_request_count: 500,
      current_cost_micros_per_1m: 800000, candidate_cost_micros_per_1m: 500000, cost_improvement: 0.375,
      current_cache_token_hit_rate: 0.2, candidate_cache_token_hit_rate: 0.7,
      current_cache_savings_rate: 0.1, candidate_cache_savings_rate: 0.6,
      current_affinity_consistency_rate: 0.4, candidate_affinity_consistency_rate: 0.9,
      current_error_rate: 0.01, candidate_error_rate: 0.012,
      current_p95_latency_ms: 400, candidate_p95_latency_ms: 420,
      current_metrics_coverage: 0.95, candidate_metrics_coverage: 0.98,
      current_billing_consistency_rate: 0.96, candidate_billing_consistency_rate: 0.99,
      automatic_action: 'activate', created_at: '2026-07-15T12:00:01Z'
    }])

    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.findAll('.effective-tabs button')[2].trigger('click')
    expect(wrapper.get('.decision-monitoring').text()).toContain('3 / 3')
    await wrapper.get('.decision-card footer .button.ghost').trigger('click')
    await flushPromises()

    expect(control.getEffectivePricingDecisionEvaluations).toHaveBeenCalledWith('decision-window', 100)
    expect(wrapper.get('.evaluation-history').text()).toContain('cache_quality_tiebreaker')
    expect(wrapper.get('.evaluation-history').text()).toContain('Automatic action: activate')
    wrapper.unmount()
  })
})
