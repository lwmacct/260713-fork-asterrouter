import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as account from './account'
import * as auth from './auth'
import * as customer from './customer'
import * as operator from './operator'
import * as plugins from './plugins'
import * as platform from './platform'
import * as settings from './settings'
import * as system from './system'
import type { APIKeyCreateRequest, APIKeyUpdateRequest, PlatformUsageSinkRequest } from '@/types'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('@/api/client', () => ({ apiClient: client }))

describe('API module contracts', () => {
  beforeEach(() => {
    for (const method of Object.values(client)) method.mockReset()
    client.get.mockResolvedValue({ data: {} })
    client.post.mockResolvedValue({ data: {} })
    client.put.mockResolvedValue({ data: {} })
    client.delete.mockResolvedValue({ data: {} })
  })

  it('uses the authentication endpoint contracts', async () => {
    const loginResult = { access_token: 'token', token_type: 'Bearer', expires_at: '2099-01-01T00:00:00Z', user: { username: 'user', role: 'developer' } }
    client.post.mockResolvedValueOnce({ data: loginResult })
    expect(await auth.login('user', 'secret', true, 'turnstile')).toEqual(loginResult)
    expect(client.post).toHaveBeenLastCalledWith('/auth/login', {
      username: 'user', password: 'secret', agreement_accepted: true, turnstile_token: 'turnstile'
    })

    client.get.mockResolvedValueOnce({ data: loginResult.user })
    expect(await auth.getCurrentUser()).toEqual(loginResult.user)
    expect(client.get).toHaveBeenLastCalledWith('/auth/me')

    await auth.completeTOTPLogin('challenge', '123456')
    expect(client.post).toHaveBeenLastCalledWith('/auth/totp/login', { challenge: 'challenge', code: '123456' })
    await auth.register('user@example.com', 'password-123', 'User', 'invite', true)
    expect(client.post).toHaveBeenLastCalledWith('/auth/register', {
      email: 'user@example.com', password: 'password-123', display_name: 'User', invitation_code: 'invite', agreement_accepted: true
    })
    await auth.verifyEmail('verify-token')
    expect(client.post).toHaveBeenLastCalledWith('/auth/verify-email', { token: 'verify-token' })
    await auth.resendVerification('user@example.com')
    expect(client.post).toHaveBeenLastCalledWith('/auth/resend-verification', { email: 'user@example.com' })
    await auth.forgotPassword('user@example.com')
    expect(client.post).toHaveBeenLastCalledWith('/auth/forgot-password', { email: 'user@example.com' })
    await auth.resetPassword('reset-token', 'new-password-123')
    expect(client.post).toHaveBeenLastCalledWith('/auth/reset-password', { token: 'reset-token', password: 'new-password-123' })
  })

  it('uses the public and admin settings endpoint contracts', async () => {
    await settings.getPublicSettings()
    expect(client.get).toHaveBeenLastCalledWith('/settings/public')
    await settings.getLegalDocument('terms / zh')
    expect(client.get).toHaveBeenLastCalledWith('/legal/terms%20%2F%20zh')
    await settings.getAdminSettings()
    expect(client.get).toHaveBeenLastCalledWith('/admin/settings')

    const payload = { site_name: 'Test' } as never
    await settings.updateAdminSettings(payload)
    expect(client.put).toHaveBeenLastCalledWith('/admin/settings', payload)
    await settings.runRetentionCleanup()
    expect(client.post).toHaveBeenLastCalledWith('/admin/settings/retention/cleanup')
    await settings.testSMTP('recipient@example.com')
    expect(client.post).toHaveBeenLastCalledWith('/admin/settings/smtp/test', { recipient: 'recipient@example.com' })
    await settings.getDefaultEmailTemplates()
    expect(client.get).toHaveBeenLastCalledWith('/admin/settings/email-templates/defaults')
    await settings.previewEmailTemplate('Subject', '<p>Body</p>')
    expect(client.post).toHaveBeenLastCalledWith('/admin/settings/email-templates/preview', { subject: 'Subject', html: '<p>Body</p>' })
    await settings.testEmailTemplate('recipient@example.com', 'Subject', '<p>Body</p>')
    expect(client.post).toHaveBeenLastCalledWith('/admin/settings/email-templates/test', {
      recipient: 'recipient@example.com', subject: 'Subject', html: '<p>Body</p>'
    })
    await settings.applySetupProfile('platform')
    expect(client.post).toHaveBeenLastCalledWith('/setup/profiles', {
      profile: 'platform'
    })
    await settings.getLocales()
    expect(client.get).toHaveBeenLastCalledWith('/i18n/locales')
  })

  it('uses the platform control-plane dashboard endpoint', async () => {
    await platform.getPlatformDashboard()
    expect(client.get).toHaveBeenLastCalledWith('/platform/dashboard')
    const operationsQuery = { limit: 10, status: 'queued' }
    await platform.getPlatformAIJobs(operationsQuery)
    expect(client.get).toHaveBeenLastCalledWith('/platform/ai-jobs', { params: operationsQuery })
    await platform.getPlatformAIJobSummary(operationsQuery)
    expect(client.get).toHaveBeenLastCalledWith('/platform/ai-jobs/summary', { params: operationsQuery })
    await platform.getPlatformAIJobRuntime()
    expect(client.get).toHaveBeenLastCalledWith('/platform/ai-jobs/runtime')
    await platform.getPlatformAIJob('job / 1')
    expect(client.get).toHaveBeenLastCalledWith('/platform/ai-jobs/job%20%2F%201')
    await platform.cancelPlatformAIJob('job / 1')
    expect(client.post).toHaveBeenLastCalledWith('/platform/ai-jobs/job%20%2F%201/cancel')
    await platform.schedulePlatformAIJobAttemptReconciliation('job / 1', 'attempt / 1')
    expect(client.post).toHaveBeenLastCalledWith('/platform/ai-jobs/job%20%2F%201/attempts/attempt%20%2F%201/reconcile')

    const artifactQuery = { limit: 10, status: 'ready' }
    await platform.getPlatformArtifacts(artifactQuery)
    expect(client.get).toHaveBeenLastCalledWith('/platform/artifacts', { params: artifactQuery })
    await platform.getPlatformArtifactSummary(artifactQuery)
    expect(client.get).toHaveBeenLastCalledWith('/platform/artifacts/summary', { params: artifactQuery })
    await platform.getPlatformArtifact('artifact / 1')
    expect(client.get).toHaveBeenLastCalledWith('/platform/artifacts/artifact%20%2F%201')
    await platform.getPlatformArtifactRuntimes()
    expect(client.get).toHaveBeenLastCalledWith('/platform/artifact-runtimes')
    await platform.retryPlatformArtifactDelivery('artifact / 1')
    expect(client.post).toHaveBeenLastCalledWith('/platform/artifacts/artifact%20%2F%201/retry-delivery')

    await platform.getPlatformAPIKeys()
    expect(client.get).toHaveBeenLastCalledWith('/platform/api-keys')
    const createPayload: APIKeyCreateRequest = {
      name: 'Service key',
      key_type: 'service',
      policy_id: '',
      model_allowlist: ['model'],
      qps_limit: 0,
      monthly_token_limit: 0,
      expires_at: ''
    }
    await platform.createPlatformAPIKey(createPayload)
    expect(client.post).toHaveBeenLastCalledWith('/platform/api-keys', createPayload)
    const updatePayload: APIKeyUpdateRequest = { ...createPayload, status: 'active' }
    await platform.updatePlatformAPIKey('key / 1', updatePayload)
    expect(client.put).toHaveBeenLastCalledWith('/platform/api-keys/key%20%2F%201', updatePayload)
    await platform.rotatePlatformAPIKey('key / 1', 3600)
    expect(client.post).toHaveBeenLastCalledWith('/platform/api-keys/key%20%2F%201/rotate', { grace_period_seconds: 3600 })
    await platform.disablePlatformAPIKey('key / 1')
    expect(client.post).toHaveBeenLastCalledWith('/platform/api-keys/key%20%2F%201/disable')

    const sinkPayload: PlatformUsageSinkRequest = {
      tenant_id: 'tenant-1', external_auth_integration_id: 'integration-1', name: 'Billing', endpoint_url: 'https://billing.example/events', status: 'active', max_attempts: 5
    }
    await platform.getPlatformUsageSinks()
    expect(client.get).toHaveBeenLastCalledWith('/platform/usage-sinks')
    await platform.createPlatformUsageSink(sinkPayload)
    expect(client.post).toHaveBeenLastCalledWith('/platform/usage-sinks', sinkPayload)
    await platform.updatePlatformUsageSink('sink / 1', sinkPayload)
    expect(client.put).toHaveBeenLastCalledWith('/platform/usage-sinks/sink%20%2F%201', sinkPayload)
    await platform.rotatePlatformUsageSinkEndpoint('sink / 1', 'https://billing.example/rotated', 'secret')
    expect(client.post).toHaveBeenLastCalledWith('/platform/usage-sinks/sink%20%2F%201/rotate-endpoint', {
      endpoint_url: 'https://billing.example/rotated', signing_secret: 'secret'
    })
    await platform.getPlatformUsageDeliveries('sink / 1', 'dead_letter')
    expect(client.get).toHaveBeenLastCalledWith('/platform/usage-sinks/sink%20%2F%201/deliveries', { params: { status: 'dead_letter' } })
    await platform.requeuePlatformUsageDelivery('sink / 1', 'delivery / 1')
    expect(client.post).toHaveBeenLastCalledWith('/platform/usage-sinks/sink%20%2F%201/deliveries/delivery%20%2F%201/requeue')
  })

  it('normalizes nullable platform collections', async () => {
    client.get.mockResolvedValueOnce({ data: { provider_count: 0, active_provider_count: 0, api_key_count: 0, active_api_key_count: 0, models: null, recent_audit: null } })
    expect(await platform.getPlatformDashboard()).toMatchObject({ models: [], recent_audit: [] })

    for (const load of [
      platform.getPlatformAPIKeys,
      platform.getPlatformTenants,
      platform.getGatewayPrincipals,
      platform.getExternalAuthIntegrations,
      platform.getPlatformUsageSinks,
      platform.getPlatformAIJobs,
      platform.getPlatformArtifacts,
      platform.getPlatformArtifactRuntimes
    ]) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }
    client.get.mockResolvedValueOnce({ data: null })
    expect(await platform.getPlatformUsageDeliveries('sink-1')).toEqual([])
  })

  it('normalizes nullable collections across settings, plugins, operator, system, customer, and account APIs', async () => {
    for (const load of [settings.getDefaultEmailTemplates, settings.getLocales]) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }

    for (const load of [
      () => plugins.getArtifactSinkDestinations('plugin-1'),
      () => plugins.getPluginAPITokens(),
      () => plugins.getOfficialFeedStatuses(),
      () => plugins.getOfficialFeedSyncRuns(),
      () => plugins.getPluginDeliveries('plugin-1'),
      () => plugins.getPluginPackages('plugin-1'),
      () => operator.listOperatorResource('customers'),
      operator.getOperatorBalances,
      operator.getOperatorCustomerKeys,
      operator.getOperatorRiskBlocks,
      system.listSystemBackups,
      system.listS3Backups
    ]) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }

    client.get.mockResolvedValueOnce({ data: { summary: {}, plugins: null } })
    expect(await plugins.getPluginCatalog()).toMatchObject({ plugins: [] })

    client.get.mockResolvedValueOnce({ data: { recharge_options: null, payment_channels: null, vouchers: null } })
    expect(await customer.getCustomerBilling()).toMatchObject({ recharge_options: [], payment_channels: [], vouchers: [] })

    client.get.mockResolvedValueOnce({ data: { preferences: [{ event_type: 'balance_low', channels: null }] } })
    expect(await customer.getCustomerNotificationSettings()).toMatchObject({ preferences: [{ channels: [] }] })

    client.get.mockResolvedValueOnce({ data: { auth_identities: null, login_methods: null } })
    expect(await account.getAccountProfile()).toMatchObject({ auth_identities: [], login_methods: [] })
  })

  it('uses customer billing and notification endpoint contracts', async () => {
    await customer.getCustomerBilling()
    expect(client.get).toHaveBeenLastCalledWith('/customer/billing')
    const query = { kind: 'allocation', limit: 10, offset: 20 }
    await customer.getCustomerBillingEntries(query)
    expect(client.get).toHaveBeenLastCalledWith('/customer/billing/entries', { params: query })
    await customer.redeemCustomerCode('CODE-1')
    expect(client.post).toHaveBeenLastCalledWith('/customer/billing/redeem', { code: 'CODE-1' })
    await customer.createCustomerRechargeOrder({ amount_cents: 1000, payment_method: 'alipay', voucher_id: 'voucher-1' })
    expect(client.post).toHaveBeenLastCalledWith('/customer/billing/recharge-orders', {
      amount_cents: 1000, payment_method: 'alipay', voucher_id: 'voucher-1'
    })
    await customer.getCustomerNotificationSettings()
    expect(client.get).toHaveBeenLastCalledWith('/customer/notification-settings')

    const preferences: customer.CustomerNotificationPreference[] = [
      { event_type: 'balance_low', enabled: true, channels: ['inapp'], marketing: false }
    ]
    await customer.updateCustomerNotificationSettings(preferences)
    expect(client.put).toHaveBeenLastCalledWith('/customer/notification-settings', { preferences })
    await customer.getCustomerNotifications(5, 10)
    expect(client.get).toHaveBeenLastCalledWith('/customer/notifications', { params: { limit: 5, offset: 10 } })
    await customer.markCustomerNotificationRead('notification / 1')
    expect(client.post).toHaveBeenLastCalledWith('/customer/notifications/notification%20%2F%201/read')
    await customer.markAllCustomerNotificationsRead()
    expect(client.post).toHaveBeenLastCalledWith('/customer/notifications/read-all')
  })

  it('downloads customer billing CSV through an object URL', async () => {
    client.get.mockResolvedValueOnce({ data: new Blob(['id,amount\n1,100\n']) })
    const createObjectURL = vi.fn(() => 'blob:test-csv')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)

    await customer.downloadCustomerBillingCSV({ limit: 20 })

    expect(client.get).toHaveBeenLastCalledWith('/customer/billing/entries/export', { params: { limit: 20 }, responseType: 'blob' })
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:test-csv')
    expect(document.querySelector('a[download]')).toBeNull()
  })

  it('uses account security endpoint contracts', async () => {
    await account.getAccountProfile()
    expect(client.get).toHaveBeenLastCalledWith('/account/profile')
    await account.updateAccountProfile('Updated', 'data:image/png;base64,AA==')
    expect(client.put).toHaveBeenLastCalledWith('/account/profile', { display_name: 'Updated', avatar_data_url: 'data:image/png;base64,AA==' })
    await account.changeAccountPassword('current', 'new-password-123')
    expect(client.put).toHaveBeenLastCalledWith('/account/password', { current_password: 'current', new_password: 'new-password-123' })
    await account.beginTOTPSetup()
    expect(client.post).toHaveBeenLastCalledWith('/account/totp/setup')
    await account.confirmTOTP('123456')
    expect(client.post).toHaveBeenLastCalledWith('/account/totp/confirm', { code: '123456' })
    client.post.mockResolvedValueOnce({ data: { access_token: 'replacement', expires_at: '2099-01-01T00:00:00Z', codes: ['code-1'] } })
    expect((await account.generateTOTPRecoveryCodes()).codes).toEqual(['code-1'])
    expect(client.post).toHaveBeenLastCalledWith('/account/totp/recovery-codes')
    await account.disableTOTP('654321')
    expect(client.delete).toHaveBeenLastCalledWith('/account/totp', { data: { code: '654321' } })
    await account.unbindAccountIdentity('oidc / test')
    expect(client.delete).toHaveBeenLastCalledWith('/account/identities/oidc%20%2F%20test')
    client.post.mockResolvedValueOnce({ data: { authorization_url: 'https://identity.test/authorize' } })
    expect(await account.beginAccountIdentityBinding('github / enterprise', '/console/account')).toBe('https://identity.test/authorize')
    expect(client.post).toHaveBeenLastCalledWith('/account/identities/github%20%2F%20enterprise/bind', { return_path: '/console/account' })
    await account.revokeOtherAccountSessions()
    expect(client.post).toHaveBeenLastCalledWith('/account/sessions/revoke-others')
  })

  it('uses operator lifecycle endpoint contracts', async () => {
    await operator.getOperatorDashboard()
    expect(client.get).toHaveBeenLastCalledWith('/operator/dashboard')
    await operator.listOperatorResource('customers')
    expect(client.get).toHaveBeenLastCalledWith('/operator/customers')
    await operator.createOperatorResource('plans', { name: 'Plan' })
    expect(client.post).toHaveBeenLastCalledWith('/operator/plans', { name: 'Plan' })
    await operator.updateOperatorResource('notices', 'notice-1', { status: 'published' })
    expect(client.put).toHaveBeenLastCalledWith('/operator/notices/notice-1', { status: 'published' })
    await operator.deleteOperatorResource('risk-rules', 'risk-1')
    expect(client.delete).toHaveBeenLastCalledWith('/operator/risk-rules/risk-1')
    await operator.getOperatorBalances()
    expect(client.get).toHaveBeenLastCalledWith('/operator/balance-entries')
    await operator.createOperatorBalance({ customer_id: 'customer-1', amount_cents: 100 })
    expect(client.post).toHaveBeenLastCalledWith('/operator/balance-entries', { customer_id: 'customer-1', amount_cents: 100 })
    await operator.getOperatorCustomerKeys()
    expect(client.get).toHaveBeenLastCalledWith('/operator/customer-keys')
    await operator.rotateOperatorCustomerKey('key-1', 300)
    expect(client.post).toHaveBeenLastCalledWith('/operator/customer-keys/key-1/rotate', { grace_period_seconds: 300 })
    await operator.disableOperatorCustomerKey('key-1')
    expect(client.post).toHaveBeenLastCalledWith('/operator/customer-keys/key-1/disable')
    const keyPayload = { name: 'Customer Key', policy_id: '', model_allowlist: ['model'], qps_limit: 1, monthly_token_limit: 100, expires_at: '' }
    await operator.createOperatorCustomerKey('customer-1', keyPayload)
    expect(client.post).toHaveBeenLastCalledWith('/operator/customers/customer-1/keys', keyPayload)
    await operator.getOperatorUsage({ limit: 10 })
    expect(client.get).toHaveBeenLastCalledWith('/operator/usage', { params: { limit: 10 } })
    await operator.getOperatorRiskBlocks()
    expect(client.get).toHaveBeenLastCalledWith('/operator/risk-blocks')
    await operator.clearOperatorRiskBlock('key / 1')
    expect(client.delete).toHaveBeenLastCalledWith('/operator/risk-blocks/key%20%2F%201')
  })

  it('uses plugin trust and package endpoint contracts', async () => {
    const payload = { value: 'synthetic' } as never
    const cases: Array<{ run: () => Promise<unknown>; method: keyof typeof client; args: unknown[] }> = [
      { run: () => plugins.getPluginCatalog(), method: 'get', args: ['/admin/plugins'] },
      { run: () => plugins.enablePlugin('plugin / 1'), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/enable'] },
      { run: () => plugins.disablePlugin('plugin / 1'), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/disable'] },
      { run: () => plugins.getPluginConfig('plugin / 1'), method: 'get', args: ['/admin/plugins/plugin%20%2F%201/config'] },
      { run: () => plugins.updatePluginConfig('plugin / 1', payload), method: 'put', args: ['/admin/plugins/plugin%20%2F%201/config', payload] },
      { run: () => plugins.getArtifactSinkDestinations('plugin / 1'), method: 'get', args: ['/admin/plugins/plugin%20%2F%201/artifact-sinks'] },
      { run: () => plugins.upsertArtifactSinkDestination('plugin / 1', 'sink / 1', payload), method: 'put', args: ['/admin/plugins/plugin%20%2F%201/artifact-sinks/sink%20%2F%201', payload] },
      { run: () => plugins.deleteArtifactSinkDestination('plugin / 1', 'sink / 1'), method: 'delete', args: ['/admin/plugins/plugin%20%2F%201/artifact-sinks/sink%20%2F%201'] },
      { run: () => plugins.getPluginAPITokens('plugin-1'), method: 'get', args: ['/admin/plugins/api-tokens', { params: { plugin_id: 'plugin-1' } }] },
      { run: () => plugins.createPluginAPIToken(payload), method: 'post', args: ['/admin/plugins/api-tokens', payload] },
      { run: () => plugins.revokePluginAPIToken('token / 1'), method: 'delete', args: ['/admin/plugins/api-tokens/token%20%2F%201'] },
      { run: () => plugins.getOfficialFeedClientInfo(), method: 'get', args: ['/admin/plugins/feeds/client'] },
      { run: () => plugins.getOfficialFeedStatuses('service-1'), method: 'get', args: ['/admin/plugins/feeds', { params: { service_key: 'service-1' } }] },
      { run: () => plugins.importOfficialFeed(payload), method: 'post', args: ['/admin/plugins/feeds/import', payload] },
      { run: () => plugins.syncOfficialFeed('service-1'), method: 'post', args: ['/admin/plugins/feeds/sync', { service_key: 'service-1' }] },
      { run: () => plugins.getOfficialFeedSyncRuns('service-1', 5), method: 'get', args: ['/admin/plugins/feeds/sync-runs', { params: { service_key: 'service-1', limit: 5 } }] },
      { run: () => plugins.getPluginDeliveries('plugin / 1', { status: 'failed' }), method: 'get', args: ['/admin/plugins/plugin%20%2F%201/deliveries', { params: { status: 'failed' } }] },
      { run: () => plugins.getOfficialCatalogStatus(), method: 'get', args: ['/admin/plugins/catalog-sync/status'] },
      { run: () => plugins.syncOfficialCatalog(), method: 'post', args: ['/admin/plugins/catalog-sync'] },
      { run: () => plugins.getOfficialLicenseStatus(), method: 'get', args: ['/admin/plugins/license/status'] },
      { run: () => plugins.activateOfficialLicense(payload), method: 'post', args: ['/admin/plugins/license/activate', payload] },
      { run: () => plugins.redeemOfficialLicense(payload), method: 'post', args: ['/admin/plugins/license/redeem', payload] },
      { run: () => plugins.importOfficialLicense(payload), method: 'post', args: ['/admin/plugins/license/import', payload] },
      { run: () => plugins.getPluginPackages('plugin / 1'), method: 'get', args: ['/admin/plugins/plugin%20%2F%201/packages'] },
      { run: () => plugins.downloadPluginPackage('plugin / 1', 'package / 1', payload), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/packages/package%20%2F%201/download', payload] },
      { run: () => plugins.installPluginPackage('plugin / 1', 'package / 1'), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/packages/package%20%2F%201/install'] },
      { run: () => plugins.importPluginPackage('plugin / 1', 'package / 1', payload), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/packages/package%20%2F%201/import', payload] },
      { run: () => plugins.uninstallPluginPackage('plugin / 1', 'package / 1'), method: 'post', args: ['/admin/plugins/plugin%20%2F%201/packages/package%20%2F%201/uninstall'] },
      { run: () => plugins.getSidecarRuntimeStatus('plugin / 1'), method: 'get', args: ['/admin/plugins/plugin%20%2F%201/runtime/status'] }
    ]
    for (const testCase of cases) {
      await testCase.run()
      expect(client[testCase.method]).toHaveBeenLastCalledWith(...testCase.args)
    }
  })

  it('uses system lifecycle endpoint contracts and downloads archives', async () => {
    await system.checkSystemUpdates(true)
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/check-updates', { params: { force: true } })
    await system.performSystemUpdate()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/update')
    await system.rollbackSystemUpdate()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/rollback')
    await system.restartSystem()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/restart')
    await system.listSystemBackups()
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/backups')
    await system.createSystemBackup()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/backups')
    await system.testBackupS3()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/backups/s3/test')
    await system.listS3Backups()
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/backups/s3')
    await system.restoreS3Backup('backup / key')
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/backups/s3/restore', { key: 'backup / key', confirm: true })
    await system.restoreSystemBackup('backup-1')
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/backups/restore', { backup_id: 'backup-1', confirm: true })
    await system.createDiagnosticBundle()
    expect(client.post).toHaveBeenLastCalledWith('/admin/system/diagnostics')

    client.get.mockResolvedValue({ data: new Blob(['archive']) })
    const createObjectURL = vi.fn(() => 'blob:test-archive')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    await system.downloadS3Backup({ id: 's3-backup', key: 'folder / backup', size_bytes: 1, last_modified: '' })
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/backups/s3/download?key=folder%20%2F%20backup', { responseType: 'blob' })
    await system.downloadSystemBackup({ id: 'backup / 1', path: 'backup.tar.gz', size_bytes: 1, created_at: '' })
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/backups/backup%20%2F%201/download', { responseType: 'blob' })
    await system.downloadDiagnosticBundle({ id: 'diagnostic / 1', path: 'diagnostic.tar.gz', size_bytes: 1, created_at: '' })
    expect(client.get).toHaveBeenLastCalledWith('/admin/system/diagnostics/diagnostic%20%2F%201/download', { responseType: 'blob' })
    expect(createObjectURL).toHaveBeenCalledTimes(3)
    expect(click).toHaveBeenCalledTimes(3)
    expect(revokeObjectURL).toHaveBeenCalledTimes(3)
  })
})
