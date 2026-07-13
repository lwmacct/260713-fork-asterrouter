import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import {
  getCustomerNotifications,
  markAllCustomerNotificationsRead,
  markCustomerNotificationRead
} from '@/api/customer'
import CustomerNotificationBell from './CustomerNotificationBell.vue'

const pushMock = vi.fn()

vi.mock('vue-router', () => ({ useRouter: () => ({ push: pushMock }) }))
vi.mock('@/api/customer', () => ({
  getCustomerNotifications: vi.fn(),
  markAllCustomerNotificationsRead: vi.fn(),
  markCustomerNotificationRead: vi.fn()
}))

const getNotificationsMock = vi.mocked(getCustomerNotifications)
const markAllMock = vi.mocked(markAllCustomerNotificationsRead)
const markReadMock = vi.mocked(markCustomerNotificationRead)

describe('CustomerNotificationBell', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    getNotificationsMock.mockResolvedValue({
      items: [{
        id: 'notification-1',
        type: 'account_security',
        title: 'Password changed',
        content: 'Your password was updated.',
        link: '/customer/account',
        is_read: false,
        created_at: '2026-07-13T10:00:00Z'
      }],
      total: 1,
      unread: 120,
      limit: 20,
      offset: 0
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('loads unread state, marks an item read, and follows its internal link', async () => {
    const wrapper = mount(CustomerNotificationBell, { global: { plugins: [i18n] } })
    await flushPromises()

    expect(wrapper.get('.notification-count').text()).toBe('99+')
    await wrapper.get('.notification-bell-trigger').trigger('click')
    await flushPromises()
    await wrapper.get('.notification-item').trigger('click')
    await flushPromises()

    expect(markReadMock).toHaveBeenCalledWith('notification-1')
    expect(pushMock).toHaveBeenCalledWith('/customer/account')
    expect(wrapper.find('.notification-dropdown').exists()).toBe(false)

    wrapper.unmount()
  })

  it('marks all visible notifications read', async () => {
    const wrapper = mount(CustomerNotificationBell, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.get('.notification-bell-trigger').trigger('click')
    await flushPromises()

    const buttons = wrapper.findAll('.notification-dropdown header button')
    await buttons[0].trigger('click')
    await flushPromises()

    expect(markAllMock).toHaveBeenCalledOnce()
    expect(wrapper.find('.notification-count').exists()).toBe(false)
    expect(wrapper.get('.notification-item').classes()).not.toContain('unread')

    wrapper.unmount()
  })
})
