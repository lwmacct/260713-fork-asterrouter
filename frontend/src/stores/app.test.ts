import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPublicSettings } from '@/api/settings'
import { makePublicSettings } from '@/test/fixtures'
import { useAppStore } from './app'

vi.mock('@/api/settings', () => ({ getPublicSettings: vi.fn() }))

const getPublicSettingsMock = vi.mocked(getPublicSettings)

describe('app store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('loads public settings and derives display values', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ site_name: 'Private Router', site_subtitle: 'Internal AI', setup_completed: true }))
    const store = useAppStore()

    await store.loadPublicSettings()

    expect(store.siteName).toBe('Private Router')
    expect(store.siteSubtitle).toBe('Internal AI')
    expect(store.setupCompleted).toBe(true)
    expect(store.loading).toBe(false)
    expect(store.error).toBe('')
  })

  it('keeps defaults and reports a load error', async () => {
    getPublicSettingsMock.mockRejectedValue(new Error('network unavailable'))
    const store = useAppStore()

    await store.loadPublicSettings()

    expect(store.siteName).toBe('AsterRouter')
    expect(store.siteSubtitle).toBe('AI Gateway Control Plane')
    expect(store.setupCompleted).toBe(false)
    expect(store.loading).toBe(false)
    expect(store.error).toBe('network unavailable')
  })
})
