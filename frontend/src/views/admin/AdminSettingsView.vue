<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Database, Download, KeyRound, Power, RefreshCw, RotateCcw, Save, ServerCog, ShieldCheck, SlidersHorizontal } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getAdminSettings, updateAdminSettings } from '@/api/settings'
import { checkSystemUpdates, performSystemUpdate, restartSystem, rollbackSystemUpdate } from '@/api/system'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'
import type { AdminSettings, SystemUpdateInfo } from '@/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')
const updateInfo = ref<SystemUpdateInfo | null>(null)
const updateAction = ref('')
const activeSettingsTab = ref<'general' | 'deployment' | 'identity' | 'governance'>('general')

const settingsTabs = [
  { id: 'general', label: 'settings.general', icon: SlidersHorizontal },
  { id: 'deployment', label: 'settings.deployment', icon: ServerCog },
  { id: 'identity', label: 'settings.identity', icon: KeyRound },
  { id: 'governance', label: 'settings.governance', icon: ShieldCheck }
] as const

const form = reactive<AdminSettings>({
  site_name: 'AsterRouter',
  site_subtitle: 'AI Gateway Control Plane',
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
  service_center_mode: 'disabled',
  version: '',
  server_timezone: '',
  server_utc_offset: '',
  storage_mode: '',
  oidc_issuer_url: '',
  oidc_client_id: '',
  data_retention_days: 30,
  prompt_logging_mode: 'metadata_only',
  update_channel: 'stable'
})

const gatewayBaseUrl = computed(() => {
  const base = form.public_base_url || window.location.origin
  return `${base.replace(/\/$/, '')}${form.gateway_base_path}`
})

function profileRoute(profile: string): string {
  if (profile === 'personal') return '/console'
  if (profile === 'relay_operator') return '/operator'
  return '/admin/settings'
}

function currentSurfaceDisabled(settings: AdminSettings): boolean {
  const profiles = settings.enabled_profiles || []
  if (route.path.startsWith('/console')) return !profiles.includes('personal')
  if (route.path.startsWith('/operator')) return !profiles.includes('relay_operator')
  if (route.path.startsWith('/portal')) return !profiles.includes('enterprise')
  if (route.path.startsWith('/admin') && route.path !== '/admin/settings') return !profiles.includes('enterprise')
  return false
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
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    assignSettings(await getAdminSettings())
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
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

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const nextSettings = await updateAdminSettings({ ...form })
    assignSettings(nextSettings)
    setPublicSettingsCache(nextSettings)
    await app.loadPublicSettings()
    message.value = t('common.saved')
    if (currentSurfaceDisabled(nextSettings)) {
      await router.replace(profileRoute(nextSettings.default_profile || nextSettings.enabled_profiles[0] || 'enterprise'))
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
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

function toggleProfile(profile: string) {
  const set = new Set(form.enabled_profiles)
  if (set.has(profile)) {
    if (set.size === 1) return
    set.delete(profile)
  } else {
    set.add(profile)
  }
  form.enabled_profiles = Array.from(set)
  if (!form.default_profile || !set.has(form.default_profile)) {
    form.default_profile = form.enabled_profiles[0] || ''
  }
}

onMounted(async () => {
  await load()
  await refreshUpdates(false)
})
</script>

<template>
  <main class="content">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.title') }}</h1>
        <p>{{ t('admin.subtitle') }}</p>
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
        <button class="button" :disabled="saving" @click="save">
          <Save :size="17" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <nav class="settings-tabs" :aria-label="t('admin.settings')">
      <button
        v-for="tab in settingsTabs"
        :key="tab.id"
        type="button"
        :class="{ active: activeSettingsTab === tab.id }"
        @click="activeSettingsTab = tab.id"
      >
        <component :is="tab.icon" :size="17" />
        {{ t(tab.label) }}
      </button>
    </nav>

    <section class="grid section-gap">
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
          <div class="field">
            <label>{{ t('settings.siteSubtitle') }}</label>
            <input v-model="form.site_subtitle" />
          </div>
          <div class="field">
            <label>{{ t('settings.publicBaseUrl') }}</label>
            <input v-model="form.public_base_url" placeholder="https://ai.company.internal" />
          </div>
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

      <div v-if="activeSettingsTab === 'deployment'" class="panel">
        <div class="panel-header">
          <ServerCog :size="18" />
          <h2>{{ t('settings.deployment') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.enabledProfiles') }}</label>
            <div class="status-line">
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('personal') }"
                @click="toggleProfile('personal')"
              >
                personal
              </button>
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('relay_operator') }"
                @click="toggleProfile('relay_operator')"
              >
                relay_operator
              </button>
              <button
                class="pill"
                type="button"
                :class="{ 'status-success': form.enabled_profiles.includes('enterprise') }"
                @click="toggleProfile('enterprise')"
              >
                enterprise
              </button>
            </div>
            <span class="hint">{{ form.enabled_profiles.join(', ') || '-' }}</span>
          </div>
          <div class="field">
            <label>{{ t('settings.defaultProfile') }}</label>
            <select v-model="form.default_profile">
              <option v-for="profile in form.enabled_profiles" :key="profile" :value="profile">{{ profile }}</option>
            </select>
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

      <div v-if="activeSettingsTab === 'deployment'" class="panel">
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

      <div v-if="activeSettingsTab === 'identity'" class="panel">
        <div class="panel-header">
          <KeyRound :size="18" />
          <h2>{{ t('settings.identity') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.oidcEnabled') }}</label>
            <select v-model="form.oidc_enabled">
              <option :value="false">false</option>
              <option :value="true">true</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('settings.oidcProviderName') }}</label>
            <input v-model="form.oidc_provider_name" />
          </div>
          <div class="field">
            <label>{{ t('settings.oidcIssuerUrl') }}</label>
            <input v-model="form.oidc_issuer_url" placeholder="https://idp.example.com" />
          </div>
          <div class="field">
            <label>{{ t('settings.oidcClientId') }}</label>
            <input v-model="form.oidc_client_id" />
          </div>
        </div>
      </div>

      <div v-if="activeSettingsTab === 'governance'" class="panel">
        <div class="panel-header">
          <ShieldCheck :size="18" />
          <h2>{{ t('settings.governance') }}</h2>
        </div>
        <div class="panel-body">
          <div class="field">
            <label>{{ t('settings.retentionDays') }}</label>
            <input v-model.number="form.data_retention_days" type="number" min="1" max="3650" />
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
  </main>
</template>
