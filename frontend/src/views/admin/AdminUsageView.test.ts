import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { CostAllocationReport, UsageReport } from '@/types'
import AdminUsageView from './AdminUsageView.vue'

vi.mock('@/api/control', () => ({
  exportUsageCSV: vi.fn(),
  getCostAllocationReport: vi.fn(),
  getUsageReport: vi.fn()
}))

const emptyUsageReport: UsageReport = {
  total_requests: 0,
  error_requests: 0,
  total_tokens: 0,
  total_output_images: 0,
  total_video_milliseconds: 0,
  total_audio_milliseconds: 0,
  total_usage_cost_micros: 0,
  priced_requests: 0,
  unpriced_requests: 0,
  disputed_requests: 0,
  cost_available: false,
  avg_latency_ms: 0,
  by_model: [],
  recent: []
}

const emptyAllocationReport: CostAllocationReport = {
  dimension: 'api_key',
  total_requests: 0,
  error_requests: 0,
  total_tokens: 0,
  total_usage_cost_micros: 0,
  avg_latency_ms: 0,
  rows: []
}

describe('AdminUsageView workbench', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getUsageReport).mockResolvedValue(emptyUsageReport)
    vi.mocked(control.getCostAllocationReport).mockResolvedValue(emptyAllocationReport)
  })

  it('separates the workbench, analysis, and records into dedicated views', async () => {
    const wrapper = mount(AdminUsageView, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.findAll('.usage-primary-tab')).toHaveLength(3)
    expect(wrapper.get('[data-section="usage-workbench"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-section="usage-analysis"]').exists()).toBe(false)
    expect(wrapper.find('[data-section="usage-records"]').exists()).toBe(false)

    await wrapper.get('[data-view="analysis"]').trigger('click')
    expect(wrapper.get('[data-section="usage-analysis"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-section="usage-filters"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-section="usage-workbench"]').exists()).toBe(false)

    await wrapper.get('[data-view="records"]').trigger('click')
    expect(wrapper.get('[data-section="usage-records"]').isVisible()).toBe(true)
    expect(wrapper.find('[data-section="usage-analysis"]').exists()).toBe(false)
    expect(wrapper.find('.pagination-bar').exists()).toBe(true)

    wrapper.unmount()
  })

  it('shows an actionable empty state when the selected window has no usage', async () => {
    const wrapper = mount(AdminUsageView, { global: { plugins: [i18n] } })
    await flushPromises()

    const emptyState = wrapper.get('.usage-chart-empty')
    expect(emptyState.text()).toContain('No usage in this time range')
    expect(emptyState.text()).toContain('Adjust the time range or filters')
    expect(wrapper.get('.usage-recent-panel').text()).toContain('No usage in this time range')

    wrapper.unmount()
  })
})
