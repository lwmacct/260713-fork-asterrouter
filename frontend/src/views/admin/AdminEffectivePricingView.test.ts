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
  getEffectivePricingReport: vi.fn(),
  getProviderAccounts: vi.fn(),
  getProviderCacheCapabilities: vi.fn(),
  getProviderCacheProbeRuns: vi.fn(),
  runProviderCacheProbe: vi.fn(),
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
        probe_enabled: true, probe_daily_token_budget: 100000, probe_daily_cost_budget_micros: 10000000,
        probe_cooldown_seconds: 3600, updated_by: '', created_at: '', updated_at: ''
      },
      rows: [{
        provider_id: 'provider-a', provider_name: 'Channel A', provider_account_id: 'account-a',
        provider_account_name: 'Procurement A', upstream_model: 'model-a', protocol: 'openai_chat_completions',
        currency: 'USD', quoted_multiplier: 0.2, billed_multiplier: 0.6, effective_multiplier: 0.5,
        effective_cost_micros_per_1m: 500000, request_count: 1000, error_rate: 0.01,
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
    vi.mocked(control.getProviderAccounts).mockResolvedValue([{
      id: 'account-a', provider_id: 'provider-a', name: 'Procurement A', status: 'active', models: ['model-a']
    } as never])
    vi.mocked(control.runProviderCacheProbe).mockResolvedValue({ status: 'succeeded' } as never)
  })

  it('renders effective cost evidence and responsive tab content', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Channel A')
    expect(wrapper.text()).toContain('0.50x')
    expect(wrapper.findAll('.ep-table tbody tr')).toHaveLength(1)

    await wrapper.find('.ep-table tbody button').trigger('click')
    expect(wrapper.find('.evidence-drawer').exists()).toBe(true)
    await wrapper.get('.evidence-drawer .icon-button').trigger('click')

    const tabs = wrapper.findAll('.effective-tabs button')
    await tabs[1].trigger('click')
    expect(wrapper.find('.cache-row').exists()).toBe(true)

    wrapper.unmount()
  })

  it('requires explicit cost confirmation before running a cache probe', async () => {
    const wrapper = mount(AdminEffectivePricingView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.findAll('.effective-tabs button')[3].trigger('click')
    await wrapper.get('.effective-panel .panel-header button').trigger('click')
    expect(wrapper.find('.effective-dialog').exists()).toBe(true)

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
})
