import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as settings from '@/api/settings'
import * as system from '@/api/system'
import AdminSettingsView from './AdminSettingsView.vue'

const loadPublicSettingsMock = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ loadPublicSettings: loadPublicSettingsMock })
}))

vi.mock('@/api/settings', () => ({
  getAdminSettings: vi.fn(),
  getDefaultEmailTemplates: vi.fn(),
  previewEmailTemplate: vi.fn(),
  runRetentionCleanup: vi.fn(),
  testEmailTemplate: vi.fn(),
  testSMTP: vi.fn(),
  updateAdminSettings: vi.fn()
}))

vi.mock('@/api/system', () => ({
  checkSystemUpdates: vi.fn(),
  createDiagnosticBundle: vi.fn(),
  createSystemBackup: vi.fn(),
  downloadDiagnosticBundle: vi.fn(),
  downloadS3Backup: vi.fn(),
  downloadSystemBackup: vi.fn(),
  listS3Backups: vi.fn(),
  listSystemBackups: vi.fn(),
  performSystemUpdate: vi.fn(),
  restartSystem: vi.fn(),
  restoreS3Backup: vi.fn(),
  restoreSystemBackup: vi.fn(),
  rollbackSystemUpdate: vi.fn(),
  testBackupS3: vi.fn(),
  updateSystemProfiles: vi.fn()
}))

const loadedSettings = {
  version: '0.9.0-test',
  storage_mode: 'memory',
  public_base_url: 'https://router.example.test',
  gateway_base_path: '/v1',
  default_profile: 'enterprise',
  enabled_profiles: ['enterprise'],
  demo_mode: false,
  email_templates: [],
  runtime_restart_required: false,
  runtime_restart_reasons: [],
  auth_source_defaults: {}
}

describe('AdminSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(settings.getAdminSettings).mockResolvedValue(structuredClone(loadedSettings) as never)
    vi.mocked(settings.getDefaultEmailTemplates).mockResolvedValue([])
    vi.mocked(settings.updateAdminSettings).mockResolvedValue(structuredClone(loadedSettings) as never)
    vi.mocked(system.checkSystemUpdates).mockResolvedValue({ has_update: false, source: 'none' } as never)
    vi.mocked(system.listSystemBackups).mockResolvedValue([])
    vi.mocked(system.listS3Backups).mockResolvedValue([])
    window.history.replaceState({}, '', '/admin/settings')
  })

  it('opens on general settings and supports keyboard tab navigation', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(8)
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('h1').text()).toBe('System Settings')

    await tabs[0]?.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(wrapper.get('#settings-tab-terms').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe('settings-tab-terms')

    wrapper.unmount()
  })

  it('keeps all four deployment modes and a save action available from the current section', async () => {
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.get('#settings-tab-gateway').trigger('click')
    expect(wrapper.findAll('.profile-card')).toHaveLength(4)
    const saveBar = wrapper.get('[data-section="settings-save-bar"]')
    expect(saveBar.text()).toContain('Deployment & gateway')
    expect(saveBar.text()).toContain('Save settings')

    await saveBar.get('button').trigger('click')
    await flushPromises()
    expect(settings.updateAdminSettings).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('switches an installed non-demo instance to one profile and opens its workspace', async () => {
    vi.mocked(system.updateSystemProfiles).mockResolvedValue({
      enabled_profiles: ['relay_operator'],
      default_profile: 'relay_operator'
    })
    const wrapper = mount(AdminSettingsView, { global: { plugins: [i18n] } })
    await flushPromises()

    await wrapper.get('#settings-tab-gateway').trigger('click')
    await wrapper.get('[data-profile="relay_operator"]').trigger('click')
    await flushPromises()

    expect(system.updateSystemProfiles).toHaveBeenCalledWith(['relay_operator'], 'relay_operator')
    expect(loadPublicSettingsMock).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-profile="relay_operator"]').attributes('aria-pressed')).toBe('true')
    expect(window.location.pathname).toBe('/operator/overview')

    wrapper.unmount()
  })
})
