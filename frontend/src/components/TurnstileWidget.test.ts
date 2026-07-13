import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TurnstileWidget from './TurnstileWidget.vue'

describe('TurnstileWidget', () => {
  afterEach(() => {
    delete window.turnstile
  })

  it('renders, emits token lifecycle events, resets, and removes the widget', async () => {
    let options: Record<string, unknown> = {}
    const reset = vi.fn()
    const remove = vi.fn()
    window.turnstile = {
      render: vi.fn((_element, value) => {
        options = value
        return 'widget-1'
      }),
      reset,
      remove
    }

    const wrapper = mount(TurnstileWidget, { props: { siteKey: 'site-key', resetKey: 0 } })
    ;(options.callback as (token: string) => void)('verified-token')
    ;(options['expired-callback'] as () => void)()

    expect(window.turnstile.render).toHaveBeenCalledOnce()
    expect(options.sitekey).toBe('site-key')
    expect(wrapper.emitted('token')).toEqual([['verified-token'], ['']])

    await wrapper.setProps({ resetKey: 1 })
    expect(reset).toHaveBeenCalledWith('widget-1')

    wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('widget-1')
  })
})
