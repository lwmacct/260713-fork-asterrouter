import { beforeEach, describe, expect, it } from 'vitest'
import { getLocale, initI18n, setLocale } from './index'

describe('i18n locale state', () => {
  beforeEach(() => {
    setLocale('en-US')
  })

  it('persists the selected locale and updates the document language', () => {
    setLocale('zh-CN')

    expect(getLocale()).toBe('zh-CN')
    expect(localStorage.getItem('asterrouter_locale')).toBe('zh-CN')
    expect(document.documentElement.lang).toBe('zh-CN')
  })

  it('initializes the document language from current state', async () => {
    document.documentElement.removeAttribute('lang')

    await initI18n()

    expect(document.documentElement.lang).toBe('en-US')
  })
})
