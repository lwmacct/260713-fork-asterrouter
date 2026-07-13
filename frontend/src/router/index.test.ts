import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPublicSettings } from '@/api/settings'
import { makeAuthUser, makePublicSettings } from '@/test/fixtures'
import router, { clearPublicSettingsCache } from './index'

vi.mock('@/api/settings', () => ({ getPublicSettings: vi.fn() }))

const getPublicSettingsMock = vi.mocked(getPublicSettings)

describe('router guards', () => {
  beforeEach(async () => {
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue(makePublicSettings())
    clearPublicSettingsCache()
    await router.replace('/legal/test-fixture')
    clearPublicSettingsCache()
    getPublicSettingsMock.mockReset()
  })

  it('sends an incomplete deployment to setup', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ setup_completed: false }))

    await router.push('/')

    expect(router.currentRoute.value.fullPath).toBe('/setup')
  })

  it('uses the enabled default profile as the authenticated entry', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'personal', enabled_profiles: ['personal'] }))

    await router.push('/')

    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
  })

  it('routes relay customers and operators to different entries', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'relay_operator', enabled_profiles: ['relay_operator'] }))
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'developer' })))

    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/customer/overview')

    clearPublicSettingsCache()
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser({ role: 'super_admin' })))
    await router.replace('/login')
    await router.push('/')
    expect(router.currentRoute.value.fullPath).toBe('/operator/overview')
  })

  it('redirects anonymous protected navigation and preserves the target', async () => {
    getPublicSettingsMock.mockResolvedValue(makePublicSettings())

    await router.push('/admin/providers?status=active')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/admin/providers?status=active')
  })

  it('redirects a disabled surface to the configured entry', async () => {
    localStorage.setItem('asterrouter_admin_token', 'token')
    getPublicSettingsMock.mockResolvedValue(makePublicSettings({ default_profile: 'personal', enabled_profiles: ['personal'] }))

    await router.push('/admin/dashboard')

    expect(router.currentRoute.value.fullPath).toBe('/console/overview')
  })
})
