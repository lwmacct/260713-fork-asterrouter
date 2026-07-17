import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import * as operator from '@/api/operator'
import OperatorCustomersView from './OperatorCustomersView.vue'

vi.mock('@/api/control', () => ({ getGatewayModels: vi.fn() }))
vi.mock('@/api/operator', () => ({
  createOperatorCustomerKey: vi.fn(),
  createOperatorResource: vi.fn(),
  listOperatorResource: vi.fn(),
  updateOperatorResource: vi.fn()
}))

describe('OperatorCustomersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(control.getGatewayModels).mockResolvedValue([
      { id: 'active', model_id: 'gateway-current', name: 'Current', status: 'active' },
      { id: 'disabled', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
    ] as never)
    vi.mocked(operator.listOperatorResource).mockImplementation(async (resource) => {
      if (resource === 'customers') return [{ id: 'customer-1', name: 'Consumer One', email: '', group_id: '', plan_id: '', balance_micros: 0, credit_micros: 0, status: 'active' }] as never
      return [] as never
    })
    vi.mocked(operator.createOperatorCustomerKey).mockResolvedValue({ key: 'ar_operator', record: { id: 'key-1' } } as never)
  })

  it('issues a customer key with the active gateway model default', async () => {
    const wrapper = mount(OperatorCustomersView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('button[title="Create consumer key"]').trigger('click')

    const picker = wrapper.get('[role="group"][aria-label="Model allowlist"]')
    expect(picker.text()).toContain('gateway-current')
    expect(picker.text()).not.toContain('gateway-retired')
    expect(picker.get('[data-model-state="active"]').attributes('aria-pressed')).toBe('true')
    const forms = wrapper.findAll('form')
    await forms[forms.length - 1]!.trigger('submit')
    await flushPromises()

    expect(operator.createOperatorCustomerKey).toHaveBeenCalledWith('customer-1', expect.objectContaining({
      model_allowlist: ['gateway-current']
    }))
    wrapper.unmount()
  })
})
