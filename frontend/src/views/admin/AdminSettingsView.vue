<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { AlertTriangle, Building2, Check, Database, Download, FileText, KeyRound, Laptop, Mail, PanelsTopLeft, Power, RadioTower, RefreshCw, RotateCcw, Save, ServerCog, ShieldCheck, SlidersHorizontal, ToggleLeft, UserRound } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getAdminSettings, getDefaultEmailTemplates, previewEmailTemplate, runRetentionCleanup, testEmailTemplate, testSMTP, updateAdminSettings } from '@/api/settings'
import {
  checkSystemUpdates,
  createDiagnosticBundle,
  createSystemBackup,
	downloadDiagnosticBundle,
	downloadS3Backup,
  downloadSystemBackup,
  listSystemBackups,
	listS3Backups,
  performSystemUpdate,
  restartSystem,
  restoreSystemBackup,
	restoreS3Backup,
	testBackupS3,
  rollbackSystemUpdate,
  updateSystemProfiles
} from '@/api/system'
import { useAppStore } from '@/stores/app'
import type { AdminSettings, S3BackupObject, SystemArchiveInfo, SystemUpdateInfo } from '@/types'

const { t } = useI18n()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')
const restartReasons = ref<string[]>([])
const updateInfo = ref<SystemUpdateInfo | null>(null)
const updateAction = ref('')
const archiveAction = ref('')
const backups = ref<SystemArchiveInfo[]>([])
const remoteBackups = ref<S3BackupObject[]>([])
type SettingsTab = 'general' | 'terms' | 'features' | 'security' | 'defaults' | 'gateway' | 'email' | 'backup'

const activeSettingsTab = ref<SettingsTab>('general')
const smtpTestRecipient = ref('')
const smtpTesting = ref(false)
const s3Testing = ref(false)
const retentionCleaning = ref(false)
const selectedEmailTemplate = ref(0)
const emailPreview = ref({ subject: '', html: '' })
const emailTemplateRecipient = ref('')
const profileSwitching = ref('')

const deploymentProfiles = [
  { id: 'enterprise', title: 'setup.enterprise', desc: 'setup.enterpriseDesc', owner: 'setup.enterpriseOwner', route: '/admin/dashboard', icon: Building2 },
  { id: 'personal', title: 'setup.personal', desc: 'setup.personalDesc', owner: 'setup.personalOwner', route: '/console/overview', icon: Laptop },
  { id: 'relay_operator', title: 'setup.relay', desc: 'setup.relayDesc', owner: 'setup.relayOwner', route: '/operator/overview', icon: RadioTower },
  { id: 'platform', title: 'setup.platform', desc: 'setup.platformDesc', owner: 'setup.platformOwner', route: '/platform/overview', icon: PanelsTopLeft }
] as const

const settingsTabs = [
  { id: 'general', label: 'settings.general', icon: SlidersHorizontal },
  { id: 'terms', label: 'settings.loginTerms', icon: FileText },
  { id: 'features', label: 'settings.featureFlags', icon: ToggleLeft },
  { id: 'security', label: 'settings.securityAndAuth', icon: ShieldCheck },
  { id: 'defaults', label: 'settings.userDefaults', icon: UserRound },
  { id: 'gateway', label: 'settings.gatewayServices', icon: ServerCog },
  { id: 'email', label: 'settings.emailSettings', icon: Mail },
  { id: 'backup', label: 'settings.dataBackup', icon: Database }
] as const

const settingsTabKeyboardActions = {
  ArrowLeft: -1,
  ArrowUp: -1,
  ArrowRight: 1,
  ArrowDown: 1,
  Home: 'first',
  End: 'last'
} as const

const activeSettingsTabLabel = computed(() => {
  const tab = settingsTabs.find((item) => item.id === activeSettingsTab.value)
  return tab ? t(tab.label) : ''
})

function selectSettingsTab(tab: SettingsTab) {
  activeSettingsTab.value = tab
}

function focusSettingsTab(tab: SettingsTab) {
  window.requestAnimationFrame(() => document.getElementById(`settings-tab-${tab}`)?.focus())
}

function handleSettingsTabKeydown(event: KeyboardEvent, tab: SettingsTab) {
  const action = settingsTabKeyboardActions[event.key as keyof typeof settingsTabKeyboardActions]
  if (action === undefined) return
  event.preventDefault()
  const currentIndex = settingsTabs.findIndex((item) => item.id === tab)
  let nextIndex = currentIndex < 0 ? 0 : currentIndex
  if (action === 'first') nextIndex = 0
  else if (action === 'last') nextIndex = settingsTabs.length - 1
  else nextIndex = (nextIndex + action + settingsTabs.length) % settingsTabs.length
  const nextTab = settingsTabs[nextIndex]?.id
  if (!nextTab) return
  selectSettingsTab(nextTab)
  focusSettingsTab(nextTab)
}

const socialOAuthProviders = [
  { key: 'github', name: 'GitHub', enabled: 'github_oauth_enabled', client: 'github_oauth_client_id', secret: 'github_oauth_client_secret', configured: 'github_oauth_configured' },
  { key: 'google', name: 'Google', enabled: 'google_oauth_enabled', client: 'google_oauth_client_id', secret: 'google_oauth_client_secret', configured: 'google_oauth_configured' }
] as const

const form = reactive<AdminSettings>({
	runtime_restart_required: false,
	runtime_restart_reasons: [],
  site_name: 'AsterRouter',
  site_subtitle: 'AI Gateway Control Plane',
	site_logo: '',
  public_base_url: '',
  api_base_url: '/api/v1',
  gateway_base_path: '/v1',
  default_profile: '',
  enabled_profiles: [],
  setup_completed: false,
  default_locale: 'en-US',
  enabled_locales: ['en-US', 'zh-CN'],
  oidc_enabled: false,
  oidc_provider_name: 'OIDC',
	oidc_require_verified_email: true,
	feishu_enabled: false,
	feishu_region: 'cn',
	github_oauth_enabled: false,
	google_oauth_enabled: false,
	dingtalk_enabled: false,
	registration_enabled: false,
	email_verify_enabled: false,
	totp_enabled: false,
	turnstile_enabled: false,
  service_center_mode: 'disabled',
  version: '',
  server_timezone: '',
  server_utc_offset: '',
  storage_mode: '',
  demo_mode: false,
  oidc_issuer_url: '',
  oidc_client_id: '',
	feishu_app_id: '',
	feishu_app_secret: '',
	feishu_configured: false,
	github_oauth_client_id: '',
	github_oauth_client_secret: '',
	github_oauth_configured: false,
	google_oauth_client_id: '',
	google_oauth_client_secret: '',
	google_oauth_configured: false,
	dingtalk_client_id: '',
	dingtalk_client_secret: '',
	dingtalk_configured: false,
	allowed_email_domains: [],
	invitation_required: false,
  login_agreement_enabled: false,
	login_agreement_mode: 'modal',
	login_agreement_updated_at: '',
	legal_documents: [],
	backend_mode: false,
	support_contact: '',
	documentation_url: '',
	custom_endpoints: [],
	custom_menu_items: [],
	channel_monitor_enabled: true,
	available_channels_enabled: true,
	risk_control_enabled: true,
	cyber_session_block_enabled: true,
	backup_s3_enabled: false,
	invitation_codes: [],
	trusted_proxy_headers: false,
	turnstile_site_key: '',
	turnstile_secret_key: '',
	turnstile_configured: false,
	default_balance_micros: 0,
	default_concurrency: 5,
	default_rpm: 0,
	auth_source_defaults: Object.fromEntries(['local','oidc','feishu','dingtalk','github','google'].map(source => [source, { enabled: false, balance_micros: 0, concurrency: 5, rpm: 0 }])),
	smtp_host: '',
	smtp_port: 587,
	smtp_username: '',
	smtp_password: '',
	smtp_from: '',
	smtp_configured: false,
	email_templates: [],
	login_agreement_title: 'Terms of Service',
	login_agreement_content: '',
	default_page_size: 20,
	page_size_options: [10, 20, 50],
	home_content: '',
	hide_import_button: false,
	channel_monitor_interval_seconds: 300,
	cyber_session_block_ttl_seconds: 3600,
	backup_s3_endpoint: '',
	backup_s3_region: 'auto',
	backup_s3_bucket: '',
	backup_s3_prefix: 'asterrouter',
	backup_s3_access_key: '',
	backup_s3_secret_key: '',
	backup_s3_configured: false,
	backup_s3_path_style: false,
	backup_retention_days: 30,
	backup_max_retained: 10,
	backup_schedule_enabled: false,
	backup_interval_hours: 24,
  data_retention_days: 30,
  prompt_logging_mode: 'metadata_only',
  update_channel: 'stable'
})

const gatewayBaseUrl = computed(() => {
  const base = form.public_base_url || window.location.origin
  return `${base.replace(/\/$/, '')}${form.gateway_base_path}`
})
const feishuCallbackUrl = computed(() => `${(form.public_base_url || window.location.origin).replace(/\/$/, '')}/api/v1/auth/feishu/callback`)
function socialCallbackUrl(provider: string): string { return `${(form.public_base_url || window.location.origin).replace(/\/$/, '')}/api/v1/auth/oauth/${provider}/callback` }

function profileLabel(profile: string): string {
  if (profile === 'personal') return t('setup.personal')
  if (profile === 'relay_operator') return t('setup.relay')
  if (profile === 'enterprise') return t('setup.enterprise')
  if (profile === 'platform') return t('setup.platform')
  return profile
}

async function switchProfile(profile: string) {
  if (profileSwitching.value || form.default_profile === profile) return
  const target = deploymentProfiles.find((item) => item.id === profile)
  if (!target) return

  profileSwitching.value = profile
  error.value = ''
  message.value = ''
  try {
    const next = await updateSystemProfiles([profile], profile)
    form.enabled_profiles = [...next.enabled_profiles]
    form.default_profile = next.default_profile
    await app.loadPublicSettings()
    message.value = t(form.demo_mode ? 'settings.demoProfileSwitched' : 'settings.profileSwitched', { profile: profileLabel(profile) })
    window.location.assign(target.route)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    profileSwitching.value = ''
  }
}

const updateState = computed(() => {
  if (!updateInfo.value) return t('settings.updateUnknown')
  if (updateInfo.value.has_update) return t('settings.updateAvailable')
  return t('settings.upToDate')
})

const updateSourceLabel = computed(() => {
  const source = updateInfo.value?.source
  if (!source || source === 'none') return ''
  if (source === 'official_catalog') return t('settings.signedCatalog')
  if (source === 'manifest') return t('settings.updateManifest')
  return source
})

function assignSettings(data: AdminSettings) {
	Object.assign(form, data)
	for (const source of ['local','oidc','feishu','dingtalk','github','google']) form.auth_source_defaults[source] ||= { enabled: false, balance_micros: 0, concurrency: 5, rpm: 0 }
}

function addLegalDocument() {
  const sequence = form.legal_documents.length + 1
  form.legal_documents.push({ id: crypto.randomUUID(), name: `文档 ${sequence}`, slug: `document-${sequence}`, content: '' })
}

function removeLegalDocument(index: number) {
  form.legal_documents.splice(index, 1)
}

function addCustomEndpoint() { form.custom_endpoints.push({ name: '', endpoint: '/v1', description: '' }) }
function addCustomMenuItem() { form.custom_menu_items.push({ id: crypto.randomUUID(), label: '', url: '/', open_in_new_tab: false }) }
function uploadSiteLogo(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 1024 * 1024) { error.value = 'Logo 必须是 PNG、JPEG 或 WebP，且不超过 1 MiB'; return }
  const reader = new FileReader(); reader.onload = () => { form.site_logo = String(reader.result || '') }; reader.readAsDataURL(file)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    assignSettings(await getAdminSettings())
	await ensureEmailTemplates()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function runSMTPTest() {
  smtpTesting.value = true
  error.value = ''
  message.value = ''
  try {
    await testSMTP(smtpTestRecipient.value)
    message.value = 'SMTP 测试邮件已发送'
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    smtpTesting.value = false
  }
}

async function runS3Test() {
  s3Testing.value = true
  error.value = ''
  try {
    await testBackupS3()
    message.value = 'S3 / R2 连接成功'
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    s3Testing.value = false
  }
}

async function ensureEmailTemplates() {
  if (!form.email_templates.length) form.email_templates = await getDefaultEmailTemplates()
}

async function runEmailPreview() {
  const template = form.email_templates[selectedEmailTemplate.value]
  if (template) emailPreview.value = await previewEmailTemplate(template.subject, template.html)
}

async function restoreEmailTemplate() {
  const current = form.email_templates[selectedEmailTemplate.value]
  if (!current) return
  const defaults = await getDefaultEmailTemplates()
  const official = defaults.find(item => item.event === current.event && item.locale === current.locale)
  if (official) form.email_templates[selectedEmailTemplate.value] = { ...official }
  await runEmailPreview()
}

async function runEmailTemplateTest() {
  const template = form.email_templates[selectedEmailTemplate.value]
  if (template) await testEmailTemplate(emailTemplateRecipient.value, template.subject, template.html)
  message.value = '模板测试邮件已发送'
}

async function refreshUpdates(force = false) {
  updateAction.value = 'check'
  error.value = ''
  try {
    updateInfo.value = await checkSystemUpdates(force)
    if (force) {
      message.value = t('settings.updateChecked')
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runUpdate() {
  updateAction.value = 'update'
  error.value = ''
  message.value = ''
  try {
    const result = await performSystemUpdate()
    message.value = result.message
    await refreshUpdates(false)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runRollback() {
  updateAction.value = 'rollback'
  error.value = ''
  message.value = ''
  try {
    const result = await rollbackSystemUpdate()
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function runRestart() {
  updateAction.value = 'restart'
  error.value = ''
  message.value = ''
  try {
    const result = await restartSystem()
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    updateAction.value = ''
  }
}

async function loadBackups() {
  try {
    backups.value = await listSystemBackups()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function loadRemoteBackups() {
  if (!form.backup_s3_enabled || !form.backup_s3_configured) { remoteBackups.value = []; return }
  try { remoteBackups.value = await listS3Backups() } catch { remoteBackups.value = [] }
}

async function runRemoteRestore(backup: S3BackupObject) {
  if (!window.confirm(`确认从远端恢复 ${backup.id}？当前数据将被覆盖。`)) return
  archiveAction.value = backup.key
  try { const result = await restoreS3Backup(backup.key); message.value = result.message; await loadBackups() }
  catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
  finally { archiveAction.value = '' }
}

async function runRemoteDownload(backup: S3BackupObject) {
	archiveAction.value = backup.key
	try { await downloadS3Backup(backup) }
	catch (err) { error.value = err instanceof Error ? err.message : t('common.failed') }
	finally { archiveAction.value = '' }
}

async function runBackup() {
  archiveAction.value = 'backup'
  error.value = ''
  message.value = ''
  try {
    await createSystemBackup()
    message.value = t('settings.backupCreated')
    await loadBackups()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

async function runRestore(backup: SystemArchiveInfo) {
  if (!window.confirm(t('settings.restoreConfirm', { id: backup.id }))) return
  archiveAction.value = backup.id
  error.value = ''
  message.value = ''
  try {
    const result = await restoreSystemBackup(backup.id)
    message.value = result.message
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

async function runDiagnostic() {
  archiveAction.value = 'diagnostic'
  error.value = ''
  message.value = ''
  try {
    const bundle = await createDiagnosticBundle()
    await downloadDiagnosticBundle(bundle)
    message.value = t('settings.diagnosticCreated')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    archiveAction.value = ''
  }
}

function formatArchiveSize(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const nextSettings = await updateAdminSettings({ ...form })
		restartReasons.value = nextSettings.runtime_restart_required ? nextSettings.runtime_restart_reasons : []
    assignSettings(nextSettings)
    await app.loadPublicSettings()
    message.value = t('common.saved')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function cleanRetainedData() {
	if (!window.confirm(t('settings.retentionCleanupConfirm', { days: form.data_retention_days }))) return
	retentionCleaning.value = true
	error.value = ''
	message.value = ''
	try {
		const result = await runRetentionCleanup()
		message.value = t('settings.retentionCleanupDone', { count: result.usage_records + result.gateway_traces + result.alert_events + result.audit_logs })
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		retentionCleaning.value = false
	}
}

function toggleLocale(locale: string) {
  const set = new Set(form.enabled_locales)
  if (set.has(locale)) {
    set.delete(locale)
  } else {
    set.add(locale)
  }
  form.enabled_locales = Array.from(set)
}

onMounted(async () => {
  await load()
  await Promise.all([refreshUpdates(false), loadBackups(), loadRemoteBackups()])
})
</script>

<template>
  <main class="content settings-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.settings') }}</h1>
        <p>{{ t('settings.pageSubtitle') }}</p>
        <div class="status-line">
          <span class="pill">{{ t('common.version') }} {{ form.version || '-' }}</span>
          <span class="pill">{{ t('common.storage') }} {{ form.storage_mode || '-' }}</span>
          <span class="pill">{{ gatewayBaseUrl }}</span>
        </div>
      </div>
      <div class="row-actions">
        <button class="button secondary" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
		<div v-if="restartReasons.length" class="notice profile-danger-notice"><strong>{{ t('settings.restartRequiredTitle') }}</strong><p>{{ t('settings.restartRequiredHelp') }}</p><ul><li v-for="reason in restartReasons" :key="reason">{{ t(`settings.restartReasons.${reason}`) }}</li></ul></div>

    <div class="settings-tabs-shell">
      <nav class="settings-tabs-scroll" role="tablist" :aria-label="t('settings.tabsLabel')">
        <div class="settings-tabs">
          <button
            v-for="tab in settingsTabs"
            :id="`settings-tab-${tab.id}`"
            :key="tab.id"
            type="button"
            role="tab"
            aria-controls="settings-panel"
            :aria-selected="activeSettingsTab === tab.id"
            :tabindex="activeSettingsTab === tab.id ? 0 : -1"
            :class="{ active: activeSettingsTab === tab.id }"
            @click="selectSettingsTab(tab.id)"
            @keydown="handleSettingsTabKeydown($event, tab.id)"
          >
            <span class="settings-tab-icon"><component :is="tab.icon" :size="17" /></span>
            <span class="settings-tab-label">{{ t(tab.label) }}</span>
          </button>
        </div>
      </nav>
    </div>

    <section id="settings-panel" class="grid section-gap settings-content-grid" role="tabpanel" :aria-labelledby="`settings-tab-${activeSettingsTab}`">
      <div v-if="activeSettingsTab === 'general'" class="panel">
        <div class="panel-header">
          <SlidersHorizontal :size="18" />
          <h2>{{ t('settings.general') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.siteName') }}</label>
            <input v-model="form.site_name" />
          </div>
          <div class="auth-provider-header"><div><strong>Backend Mode</strong><p>仅提供 API 与管理控制面，不展示门户首页。</p></div><label class="switch"><input v-model="form.backend_mode" type="checkbox"/><span></span></label></div>
          <div class="field">
            <label>{{ t('settings.siteSubtitle') }}</label>
            <input v-model="form.site_subtitle" />
          </div>
		  <div class="field"><label>站点 Logo</label><div class="status-line"><img v-if="form.site_logo" :src="form.site_logo" class="site-logo-preview" alt="Logo preview"/><input type="file" accept="image/png,image/jpeg,image/webp" @change="uploadSiteLogo"/><button v-if="form.site_logo" class="icon-button danger" type="button" title="移除 Logo" @click="form.site_logo=''">×</button></div></div>
          <div class="field">
            <label>{{ t('settings.publicBaseUrl') }}</label>
            <input v-model="form.public_base_url" placeholder="https://ai.company.internal" />
          </div>
          <div class="auth-credential-grid">
            <div class="field"><label>默认分页数量</label><input v-model.number="form.default_page_size" type="number" min="5" max="1000"/></div>
            <div class="field"><label>可选分页数量</label><input :value="form.page_size_options.join(', ')" @change="form.page_size_options = ($event.target as HTMLInputElement).value.split(',').map(Number).filter(Number.isFinite)"/></div>
            <div class="field"><label>客服联系方式</label><input v-model="form.support_contact"/></div>
            <div class="field"><label>文档链接</label><input v-model="form.documentation_url" placeholder="https://docs.example.com"/></div>
          </div>
          <div class="field"><label>首页 Markdown / HTML</label><textarea v-model="form.home_content" rows="10"/></div>
          <div class="auth-provider-header"><div><strong>隐藏导入按钮</strong><p>在用户界面隐藏配置导入入口。</p></div><label class="switch"><input v-model="form.hide_import_button" type="checkbox"/><span></span></label></div>
		  <section class="auth-provider-card"><div class="auth-provider-header"><div><strong>自定义 API 端点</strong><p>向企业用户展示可快速复制的稳定 API 入口。</p></div><button class="button secondary" type="button" @click="addCustomEndpoint">添加端点</button></div><div class="auth-provider-config"><div v-for="(endpoint,index) in form.custom_endpoints" :key="index" class="auth-credential-grid"><div class="field"><label>名称</label><input v-model="endpoint.name"/></div><div class="field"><label>端点</label><input v-model="endpoint.endpoint" placeholder="/v1/responses"/></div><div class="field"><label>说明</label><input v-model="endpoint.description"/></div><button class="icon-button danger" type="button" title="删除" @click="form.custom_endpoints.splice(index,1)">×</button></div></div></section>
		  <section class="auth-provider-card"><div class="auth-provider-header"><div><strong>自定义菜单页面</strong><p>向企业用户展示内部文档或服务入口。</p></div><button class="button secondary" type="button" @click="addCustomMenuItem">添加菜单</button></div><div class="auth-provider-config"><div v-for="(item,index) in form.custom_menu_items" :key="item.id" class="auth-credential-grid"><div class="field"><label>名称</label><input v-model="item.label"/></div><div class="field"><label>URL</label><input v-model="item.url"/></div><label class="agreement-check"><input v-model="item.open_in_new_tab" type="checkbox"/><span>新窗口打开</span></label><button class="icon-button danger" type="button" title="删除" @click="form.custom_menu_items.splice(index,1)">×</button></div></div></section>
          <div class="field">
            <label>{{ t('settings.defaultLocale') }}</label>
            <select v-model="form.default_locale">
              <option value="en-US">English</option>
              <option value="zh-CN">简体中文</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.enabledLocales') }}</label>
            <div class="status-line">
              <button class="pill" type="button" @click="toggleLocale('en-US')">en-US</button>
              <button class="pill" type="button" @click="toggleLocale('zh-CN')">zh-CN</button>
            </div>
            <span class="hint">{{ form.enabled_locales.join(', ') }}</span>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'terms'" class="panel"><div class="panel-header"><FileText :size="18"/><h2>{{ t('settings.loginTerms') }}</h2></div><div class="panel-body auth-provider-list"><div class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ t('settings.enableLoginTerms') }}</strong><p>{{ t('settings.loginTermsHelp') }}</p></div><label class="switch"><input v-model="form.login_agreement_enabled" type="checkbox"/><span></span></label></div><div v-if="form.login_agreement_enabled" class="auth-provider-config"><div class="auth-credential-grid"><div class="field"><label>展示模式</label><div class="segmented-control"><button type="button" :class="{active:form.login_agreement_mode==='modal'}" @click="form.login_agreement_mode='modal'">Modal</button><button type="button" :class="{active:form.login_agreement_mode==='checkbox'}" @click="form.login_agreement_mode='checkbox'">Checkbox</button></div></div><div class="field"><label>更新日期</label><input v-model="form.login_agreement_updated_at" type="date"/></div></div></div></div><section v-for="(document,index) in form.legal_documents" :key="document.id" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ document.name || `文档 ${index+1}` }}</strong><p>/legal/{{ document.slug }}</p></div><button class="button danger" type="button" @click="removeLegalDocument(index)">删除</button></div><div class="auth-provider-config"><div class="auth-credential-grid"><div class="field"><label>文档名称</label><input v-model="document.name"/></div><div class="field"><label>URL Slug</label><input v-model="document.slug" pattern="[a-z0-9-]+"/></div></div><div class="field"><label>Markdown 内容</label><textarea v-model="document.content" rows="12"/></div></div></section><button class="button secondary" type="button" @click="addLegalDocument">添加文档</button></div></div>

      <div v-if="activeSettingsTab === 'features'" class="panel"><div class="panel-header"><ToggleLeft :size="18"/><h2>{{ t('settings.featureFlags') }}</h2></div><div class="panel-body auth-provider-list"><section v-for="item in [{label:'settings.registrationEnabled',help:'settings.registrationHelp',value:form.registration_enabled,set:(v:boolean)=>form.registration_enabled=v},{label:'settings.emailVerifyEnabled',help:'settings.emailVerifyHelp',value:form.email_verify_enabled,set:(v:boolean)=>form.email_verify_enabled=v},{label:'settings.invitationRequired',help:'settings.invitationHelp',value:form.invitation_required,set:(v:boolean)=>form.invitation_required=v},{label:'settings.totpEnabled',help:'settings.totpHelp',value:form.totp_enabled,set:(v:boolean)=>form.totp_enabled=v}]" :key="item.label" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ t(item.label) }}</strong><p>{{ t(item.help) }}</p></div><label class="switch"><input :checked="item.value" type="checkbox" @change="item.set(($event.target as HTMLInputElement).checked)"/><span></span></label></div></section></div></div>

      <div v-if="activeSettingsTab === 'features'" class="panel"><div class="panel-header"><ShieldCheck :size="18"/><h2>网关运行能力</h2></div><div class="panel-body auth-provider-list"><section class="auth-provider-card"><div class="auth-provider-header"><div><strong>渠道监控</strong><p>定期探测稳定 API 上游的连通性与响应状态。</p></div><label class="switch"><input v-model="form.channel_monitor_enabled" type="checkbox"/><span></span></label></div><div v-if="form.channel_monitor_enabled" class="auth-provider-config"><div class="field"><label>检测间隔（秒）</label><input v-model.number="form.channel_monitor_interval_seconds" type="number" min="30" max="86400"/></div></div></section><section class="auth-provider-card"><div class="auth-provider-header"><div><strong>可用渠道</strong><p>向授权用户展示当前可用的模型与 API 渠道。</p></div><label class="switch"><input v-model="form.available_channels_enabled" type="checkbox"/><span></span></label></div></section><section class="auth-provider-card"><div class="auth-provider-header"><div><strong>风险控制中心</strong><p>启用网关异常行为识别与处置策略。</p></div><label class="switch"><input v-model="form.risk_control_enabled" type="checkbox"/><span></span></label></div><div v-if="form.risk_control_enabled" class="auth-provider-config"><div class="auth-provider-header"><div><strong>异常会话自动屏蔽</strong><p>临时屏蔽触发风险规则的会话。</p></div><label class="switch"><input v-model="form.cyber_session_block_enabled" type="checkbox"/><span></span></label></div><div v-if="form.cyber_session_block_enabled" class="field"><label>屏蔽时长（秒）</label><input v-model.number="form.cyber_session_block_ttl_seconds" type="number" min="60" max="2592000"/></div></div></section></div></div>

      <div v-if="activeSettingsTab === 'defaults'" class="panel"><div class="panel-header"><UserRound :size="18"/><h2>{{ t('settings.userDefaults') }}</h2></div><div class="panel-body"><div class="auth-credential-grid"><div class="field"><label>{{ t('settings.defaultBalance') }}</label><input v-model.number="form.default_balance_micros" type="number" min="0"/></div><div class="field"><label>{{ t('settings.defaultConcurrency') }}</label><input v-model.number="form.default_concurrency" type="number" min="0"/></div><div class="field"><label>{{ t('settings.defaultRpm') }}</label><input v-model.number="form.default_rpm" type="number" min="0"/></div></div></div></div>

      <div v-if="activeSettingsTab === 'defaults'" class="panel"><div class="panel-header"><ShieldCheck :size="18"/><h2>按认证来源默认值</h2></div><div class="panel-body auth-provider-list"><section v-for="source in ['local','oidc','feishu','dingtalk','github','google']" :key="source" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ source }}</strong><p>覆盖该身份源首次创建用户时的全局默认值。</p></div><label class="switch"><input v-model="form.auth_source_defaults[source].enabled" type="checkbox"/><span></span></label></div><div v-if="form.auth_source_defaults[source].enabled" class="auth-provider-config auth-credential-grid"><div class="field"><label>余额（微美元）</label><input v-model.number="form.auth_source_defaults[source].balance_micros" type="number" min="0"/></div><div class="field"><label>并发</label><input v-model.number="form.auth_source_defaults[source].concurrency" type="number" min="0"/></div><div class="field"><label>RPM</label><input v-model.number="form.auth_source_defaults[source].rpm" type="number" min="0"/></div></div></section></div></div>

      <div v-if="activeSettingsTab === 'email'" class="panel"><div class="panel-header"><Mail :size="18"/><h2>{{ t('settings.emailSettings') }}</h2></div><div class="panel-body"><div class="auth-credential-grid"><div class="field"><label>SMTP Host</label><input v-model="form.smtp_host"/></div><div class="field"><label>SMTP Port</label><input v-model.number="form.smtp_port" type="number" min="1" max="65535"/></div><div class="field"><label>{{ t('auth.username') }}</label><input v-model="form.smtp_username"/></div><div class="field"><label>{{ t('auth.password') }}</label><input v-model="form.smtp_password" type="password" :placeholder="form.smtp_configured?t('plugins.keepSecret'):''"/></div><div class="field auth-config-span"><label>{{ t('settings.smtpFrom') }}</label><input v-model="form.smtp_from" type="email"/></div></div><div class="auth-provider-config"><div class="field"><label>测试收件邮箱</label><input v-model="smtpTestRecipient" type="email" placeholder="admin@example.com"/></div><button class="button secondary" type="button" :disabled="smtpTesting || !smtpTestRecipient" @click="runSMTPTest">{{ smtpTesting ? '正在测试' : '发送测试邮件' }}</button></div></div></div>

      <div v-if="activeSettingsTab === 'email' && form.email_templates.length" class="panel"><div class="panel-header"><FileText :size="18"/><h2>邮件通知模板</h2></div><div class="panel-body"><div class="auth-credential-grid"><div class="field"><label>事件与语言</label><select v-model.number="selectedEmailTemplate" @change="runEmailPreview"><option v-for="(item,index) in form.email_templates" :key="item.event+item.locale" :value="index">{{ item.event }} / {{ item.locale }}</option></select></div><div class="field"><label>测试收件邮箱</label><input v-model="emailTemplateRecipient" type="email"/></div></div><div v-if="form.email_templates[selectedEmailTemplate]" class="email-template-grid"><div><div class="field"><label>主题</label><input v-model="form.email_templates[selectedEmailTemplate].subject"/></div><div class="field"><label>HTML 模板</label><textarea v-model="form.email_templates[selectedEmailTemplate].html" rows="18" class="code-input"/></div><div class="hint">可用变量：<code v-pre>{{.SiteName}}</code> <code v-pre>{{.UserName}}</code> <code v-pre>{{.ActionURL}}</code> <code v-pre>{{.Amount}}</code> <code v-pre>{{.Limit}}</code> <code v-pre>{{.Period}}</code> <code v-pre>{{.Message}}</code></div><div class="status-line"><button class="button secondary" type="button" @click="runEmailPreview">预览</button><button class="button secondary" type="button" @click="restoreEmailTemplate">恢复官方模板</button><button class="button" type="button" :disabled="!emailTemplateRecipient" @click="runEmailTemplateTest">测试发送</button></div></div><div class="email-preview"><strong>{{ emailPreview.subject || '邮件预览' }}</strong><iframe sandbox="" :srcdoc="emailPreview.html" title="邮件预览"></iframe></div></div></div></div>

      <div v-if="activeSettingsTab === 'gateway'" class="panel">
        <div class="panel-header">
          <ServerCog :size="18" />
          <h2>{{ t('settings.deployment') }}</h2>
        </div>
        <div class="panel-body">
          <div class="notice" :class="{ success: form.demo_mode }">
            <strong>{{ t(form.demo_mode ? 'settings.demoProfileSwitchTitle' : 'settings.profileSwitchTitle') }}</strong>
            <span>{{ t(form.demo_mode ? 'settings.demoProfileSwitchHelp' : 'settings.profileSwitchHelp') }}</span>
          </div>
          <div class="setup-grid">
            <button
              v-for="profile in deploymentProfiles"
              :key="profile.id"
              class="profile-card"
              :class="{ active: form.default_profile === profile.id, primary: form.default_profile === profile.id }"
              type="button"
              :data-profile="profile.id"
              :aria-pressed="form.default_profile === profile.id"
              :disabled="Boolean(profileSwitching)"
              @click="switchProfile(profile.id)"
            >
              <span class="profile-card-topline">
                <component :is="profile.icon" :size="28" aria-hidden="true" />
                <span class="profile-check" :class="{ active: form.default_profile === profile.id }">
                  <Check v-if="form.default_profile === profile.id" :size="15" aria-hidden="true" />
                </span>
              </span>
              <h2>{{ t(profile.title) }}</h2>
              <p>{{ t(profile.desc) }}</p>
              <span class="profile-owner">{{ t(profile.owner) }}</span>
              <span class="profile-route">{{ profile.route }}</span>
            </button>
          </div>
          <div class="field">
            <label>{{ t('settings.gatewayBasePath') }}</label>
            <input v-model="form.gateway_base_path" />
          </div>
          <div class="field">
            <label>{{ t('settings.updateChannel') }}</label>
            <select v-model="form.update_channel">
              <option value="stable">stable</option>
              <option value="beta">beta</option>
              <option value="manual">manual</option>
            </select>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel"><div class="panel-header"><Database :size="18"/><h2>S3 / R2 对象存储</h2></div><div class="panel-body"><div class="auth-provider-header"><div><strong>{{ t('settings.backupSchedule') }}</strong><p>{{ t('settings.backupScheduleHelp') }}</p></div><label class="switch"><input v-model="form.backup_schedule_enabled" type="checkbox"/><span></span></label></div><div v-if="form.backup_schedule_enabled" class="field"><label>{{ t('settings.backupIntervalHours') }}</label><input v-model.number="form.backup_interval_hours" type="number" min="1" max="720"/></div><div class="auth-provider-header"><div><strong>远端备份</strong><p>备份创建后同步上传到 S3 兼容对象存储，并执行保留策略。</p></div><label class="switch"><input v-model="form.backup_s3_enabled" type="checkbox"/><span></span></label></div><div v-if="form.backup_s3_enabled" class="auth-provider-config"><div class="auth-credential-grid"><div class="field"><label>Endpoint</label><input v-model="form.backup_s3_endpoint" placeholder="https://account.r2.cloudflarestorage.com"/></div><div class="field"><label>Region</label><input v-model="form.backup_s3_region"/></div><div class="field"><label>Bucket</label><input v-model="form.backup_s3_bucket"/></div><div class="field"><label>Prefix</label><input v-model="form.backup_s3_prefix"/></div><div class="field"><label>Access Key</label><input v-model="form.backup_s3_access_key" autocomplete="off"/></div><div class="field"><label>Secret Key</label><input v-model="form.backup_s3_secret_key" type="password" autocomplete="new-password" :placeholder="form.backup_s3_configured?t('plugins.keepSecret'):''"/></div><div class="field"><label>过期天数</label><input v-model.number="form.backup_retention_days" type="number" min="1" max="3650"/></div><div class="field"><label>最大保留数</label><input v-model.number="form.backup_max_retained" type="number" min="1" max="1000"/></div></div><div class="auth-provider-header"><div><strong>Path Style</strong><p>为 MinIO 等兼容服务启用路径式寻址。</p></div><label class="switch"><input v-model="form.backup_s3_path_style" type="checkbox"/><span></span></label></div><button class="button secondary" type="button" :disabled="s3Testing" @click="runS3Test">{{ s3Testing?'正在测试':'测试连接' }}</button></div></div></div>

      <div v-if="activeSettingsTab === 'backup' && form.backup_s3_enabled" class="panel"><div class="panel-header"><Database :size="18"/><h2>远端备份记录</h2></div><div class="panel-body table-scroll"><table class="data-table crud-table"><thead><tr><th>备份</th><th>更新时间</th><th>大小</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="backup in remoteBackups" :key="backup.key"><td><strong>{{ backup.id }}</strong><span>{{ backup.key }}</span></td><td>{{ new Date(backup.last_modified).toLocaleString() }}</td><td>{{ formatArchiveSize(backup.size_bytes) }}</td><td><div class="status-line"><button class="button secondary" type="button" :disabled="!!archiveAction" title="下载远端备份" @click="runRemoteDownload(backup)"><Download :size="16"/>下载</button><button class="button danger" type="button" :disabled="!!archiveAction" @click="runRemoteRestore(backup)">恢复</button></div></td></tr><tr v-if="!remoteBackups.length"><td colspan="4" class="empty-cell">暂无远端备份</td></tr></tbody></table></div></div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <Download :size="18" />
          <h2>{{ t('settings.systemUpdate') }}</h2>
        </div>
        <div class="panel-body">
          <div class="status-line">
            <span class="pill">{{ updateState }}</span>
            <span class="pill">{{ updateInfo?.build_type || '-' }}</span>
            <span class="pill">{{ updateInfo?.platform || '-' }}</span>
            <span v-if="updateSourceLabel" class="pill">{{ updateSourceLabel }}</span>
            <span v-if="updateInfo?.signed_metadata" class="pill">{{ t('settings.signedMetadata') }}</span>
          </div>
          <div class="field">
            <label>{{ t('settings.latestVersion') }}</label>
            <input :value="updateInfo?.latest_version || form.version || '-'" readonly />
            <span v-if="updateInfo?.warning" class="hint">{{ updateInfo.warning }}</span>
          </div>
          <div class="status-line">
            <button class="button secondary" type="button" :disabled="!!updateAction" @click="refreshUpdates(true)">
              <RefreshCw :size="16" />
              {{ updateAction === 'check' ? t('common.loading') : t('settings.checkUpdates') }}
            </button>
            <button class="button" type="button" :disabled="!!updateAction || !updateInfo?.has_update" @click="runUpdate">
              <Download :size="16" />
              {{ updateAction === 'update' ? t('common.loading') : t('settings.oneClickUpdate') }}
            </button>
          </div>
          <div class="status-line">
            <button class="button secondary" type="button" :disabled="!!updateAction" @click="runRollback">
              <RotateCcw :size="16" />
              {{ t('settings.rollback') }}
            </button>
            <button class="button secondary" type="button" :disabled="!!updateAction || !updateInfo?.restart_supported" @click="runRestart">
              <Power :size="16" />
              {{ t('settings.restart') }}
            </button>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <Database :size="18" />
          <h2>{{ t('settings.backupAndDiagnostics') }}</h2>
        </div>
        <div class="panel-body">
          <div class="notice profile-danger-notice">
            <strong><AlertTriangle :size="15" />{{ t('settings.restoreDangerTitle') }}</strong>
            <span>{{ t('settings.restoreDangerHelp') }}</span>
          </div>
          <div class="status-line">
            <button class="button" type="button" :disabled="!!archiveAction" @click="runBackup">
              <Download :size="16" />
              {{ archiveAction === 'backup' ? t('common.loading') : t('settings.createBackup') }}
            </button>
            <button class="button secondary" type="button" :disabled="!!archiveAction" @click="runDiagnostic">
              <ShieldCheck :size="16" />
              {{ archiveAction === 'diagnostic' ? t('common.loading') : t('settings.createDiagnostic') }}
            </button>
          </div>
          <div class="table-scroll">
            <table class="data-table crud-table">
              <thead>
                <tr>
                  <th>{{ t('settings.backup') }}</th>
                  <th>{{ t('audit.time') }}</th>
                  <th>{{ t('settings.archiveSize') }}</th>
                  <th>{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="backup in backups" :key="backup.id">
                  <td><strong>{{ backup.id }}</strong><span>{{ backup.path }}</span></td>
                  <td>{{ new Date(backup.created_at).toLocaleString() }}</td>
                  <td>{{ formatArchiveSize(backup.size_bytes) }}</td>
                  <td class="table-actions">
                    <button class="icon-button" type="button" :title="t('common.download')" @click="downloadSystemBackup(backup)">
                      <Download :size="16" />
                    </button>
                    <button class="icon-button danger" type="button" :disabled="!!archiveAction" :title="t('settings.restore')" @click="runRestore(backup)">
                      <RotateCcw :size="16" />
                    </button>
                  </td>
                </tr>
                <tr v-if="!backups.length"><td colspan="4" class="empty-cell"></td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'security'" class="panel">
        <div class="panel-header">
          <KeyRound :size="18" />
          <h2>{{ t('settings.identity') }}</h2>
        </div>
        <div class="panel-body auth-provider-list">
          <section class="auth-provider-card">
            <div class="auth-provider-header">
              <div><strong>{{ t('settings.feishuProviderTitle') }}</strong><p>{{ t('settings.feishuProviderHelp') }}</p></div>
              <label class="switch"><input v-model="form.feishu_enabled" type="checkbox" /><span></span></label>
            </div>
            <div v-if="form.feishu_enabled" class="auth-provider-config">
              <div class="field"><label>{{ t('settings.feishuRegion') }}</label><div class="segmented-control"><button type="button" :class="{ active: form.feishu_region === 'cn' }" @click="form.feishu_region = 'cn'">{{ t('settings.feishuChina') }}</button><button type="button" :class="{ active: form.feishu_region === 'global' }" @click="form.feishu_region = 'global'">{{ t('settings.larkGlobal') }}</button></div></div>
              <div class="auth-credential-grid"><div class="field"><label>{{ t('settings.feishuAppId') }}</label><input v-model="form.feishu_app_id" autocomplete="off" /></div><div class="field"><label>{{ t('settings.feishuAppSecret') }}</label><input v-model="form.feishu_app_secret" type="password" autocomplete="new-password" :placeholder="form.feishu_configured ? t('plugins.keepSecret') : ''" /></div></div>
              <span class="hint">{{ t('settings.feishuCallbackHelp') }} {{ feishuCallbackUrl }}</span>
            </div>
          </section>

          <section class="auth-provider-card">
            <div class="auth-provider-header">
              <div><strong>{{ t('settings.oidcProviderTitle') }}</strong><p>{{ t('settings.oidcProviderHelp') }}</p></div>
              <label class="switch"><input v-model="form.oidc_enabled" type="checkbox" /><span></span></label>
            </div>
            <div v-if="form.oidc_enabled" class="auth-provider-config auth-credential-grid">
              <div class="field"><label>{{ t('settings.oidcProviderName') }}</label><input v-model="form.oidc_provider_name" /></div>
              <div class="field"><label>{{ t('settings.oidcClientId') }}</label><input v-model="form.oidc_client_id" /></div>
              <div class="field auth-config-span"><label>{{ t('settings.oidcIssuerUrl') }}</label><input v-model="form.oidc_issuer_url" placeholder="https://idp.example.com" /></div>
				<div class="auth-provider-header auth-config-span"><div><strong>要求已验证邮箱</strong><p>仅接受 <code>email_verified=true</code> 的身份声明。内部 IdP 不提供该声明时才应关闭。</p></div><label class="switch"><input v-model="form.oidc_require_verified_email" type="checkbox"/><span></span></label></div>
            </div>
          </section>

          <section v-for="provider in socialOAuthProviders" :key="provider.key" class="auth-provider-card"><div class="auth-provider-header"><div><strong>{{ provider.name }} OAuth</strong><p>{{ t('settings.socialOAuthHelp') }}</p></div><label class="switch"><input v-model="form[provider.enabled]" type="checkbox"/><span></span></label></div><div v-if="form[provider.enabled]" class="auth-provider-config auth-credential-grid"><div class="field"><label>Client ID</label><input v-model="form[provider.client]"/></div><div class="field"><label>Client Secret</label><input v-model="form[provider.secret]" type="password" :placeholder="form[provider.configured]?t('plugins.keepSecret'):''"/></div><span class="hint auth-config-span">Callback: {{ socialCallbackUrl(provider.key) }}</span></div></section>

          <section class="auth-provider-card auth-provider-static">
            <div class="auth-provider-header"><div><strong>{{ t('settings.localLoginTitle') }}</strong><p>{{ t('settings.localLoginHelp') }}</p></div><span class="status-badge success">{{ t('settings.alwaysEnabled') }}</span></div>
          </section>

          <section class="auth-provider-card"><div class="auth-provider-header"><div><strong>钉钉企业登录</strong><p>同步企业邮箱、姓名和首个部门，并按企业策略自动入职。</p></div><label class="switch"><input v-model="form.dingtalk_enabled" type="checkbox"/><span></span></label></div><div v-if="form.dingtalk_enabled" class="auth-provider-config auth-credential-grid"><div class="field"><label>Client ID / AppKey</label><input v-model="form.dingtalk_client_id"/></div><div class="field"><label>Client Secret / AppSecret</label><input v-model="form.dingtalk_client_secret" type="password" :placeholder="form.dingtalk_configured?t('plugins.keepSecret'):''"/></div><span class="hint auth-config-span">Callback: {{ (form.public_base_url || '').replace(/\/$/,'') }}/api/v1/auth/dingtalk/callback</span></div></section>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'backup'" class="panel">
        <div class="panel-header">
          <ShieldCheck :size="18" />
          <h2>{{ t('settings.governance') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.retentionDays') }}</label>
            <input v-model.number="form.data_retention_days" type="number" min="1" max="3650" />
						<button class="button danger" type="button" :disabled="retentionCleaning" @click="cleanRetainedData">{{ retentionCleaning ? t('common.loading') : t('settings.runRetentionCleanup') }}</button>
          </div>
          <div class="field">
            <label>{{ t('settings.promptLoggingMode') }}</label>
            <select v-model="form.prompt_logging_mode">
              <option value="metadata_only">{{ t('settings.metadataOnly') }}</option>
              <option value="disabled">{{ t('settings.disabled') }}</option>
              <option value="full">{{ t('settings.full') }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.serviceCenterMode') }}</label>
            <select v-model="form.service_center_mode">
              <option value="disabled">disabled</option>
              <option value="online">online</option>
              <option value="private_mirror">private_mirror</option>
              <option value="offline">offline</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.serviceCenter') }}</label>
            <div class="status-line">
              <span class="pill"><Database :size="14" />{{ form.service_center_mode }}</span>
              <span class="pill"><ShieldCheck :size="14" />{{ form.prompt_logging_mode }}</span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <footer class="settings-save-bar" data-section="settings-save-bar">
      <div><strong>{{ activeSettingsTabLabel }}</strong><span>{{ t('settings.saveCurrentSectionHelp') }}</span></div>
      <button class="button" type="button" :disabled="saving || loading" @click="save()">
        <Save :size="17" />
        {{ saving ? t('common.saving') : t('settings.saveSettings') }}
      </button>
    </footer>

  </main>
</template>
