import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import ConsoleHomeView from './ConsoleHomeView.vue'

vi.mock('vue-router', () => ({
  useRoute: () => ({ meta: { consolePanel: 'overview' } })
}))

vi.mock('@/api/control', () => ({
  createAPIKey: vi.fn(),
  disableAPIKey: vi.fn(),
  getAPIKeys: vi.fn(),
  getGatewayModels: vi.fn(),
  getProviders: vi.fn(),
  getUsageReport: vi.fn(),
  rotateAPIKey: vi.fn()
}))

describe('ConsoleHomeView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    setActivePinia(createPinia())
    vi.mocked(control.getProviders).mockResolvedValue([
      { id: 'provider-1', name: 'Provider', status: 'active', models: ['upstream-only'] }
    ] as never)
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'model-current', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'model-disabled', model_id: 'gateway-disabled', name: 'Disabled', status: 'disabled' }
    ] as never)
    vi.mocked(control.getAPIKeys).mockResolvedValue([])
    vi.mocked(control.getUsageReport).mockResolvedValue({ total_requests: 0, total_tokens: 0, total_usage_cost_micros: 0, error_requests: 0, by_model: [], recent: [] } as never)
  })

  it('builds the gateway example from active gateway models instead of provider snapshots', async () => {
    const wrapper = mount(ConsoleHomeView, {
      global: {
        plugins: [i18n],
        stubs: { RouterLink: { template: '<a><slot /></a>' } }
      }
    })
    await flushPromises()

    expect(control.getGatewayModels).toHaveBeenCalledOnce()
    expect(wrapper.get('.code-block').text()).toContain('gateway-current')
    expect(wrapper.get('.code-block').text()).not.toContain('upstream-only')
    expect(wrapper.text()).not.toContain('gateway-disabled')
    wrapper.unmount()
  })
})
