<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Code2, KeyRound, Plus, RefreshCw, RotateCw, ShieldAlert, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import {
  createPortalAPIKey,
  disablePortalAPIKey,
  getPortalWorkspace,
  rotatePortalAPIKey
} from '@/api/control'
import type { APIKeyCreateRequest, APIKeyRecord, PortalWorkspace } from '@/types'
import { apiKeyLifecycleClass, apiKeyLifecycleLabelKey, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from '@/utils/apiKeys'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const workspace = ref<PortalWorkspace | null>(null)
const createdSecret = ref('')
const form = reactive<APIKeyCreateRequest>({
  name: '',
  policy_id: '',
  model_allowlist: [],
  qps_limit: 0,
  monthly_token_limit: 0,
  expires_at: ''
})

const apiKeys = computed(() => workspace.value?.api_keys || [])
const usage = computed(() => workspace.value?.usage)
const recentTraces = computed(() => workspace.value?.recent_traces || [])
const alerts = computed(() => workspace.value?.alerts || [])
const canManageKeys = computed(() => Boolean(workspace.value?.can_manage_keys))
const activeKeys = computed(() => apiKeys.value.filter((key) => apiKeyLifecycleStatus(key) === 'active').length)
const modelOptions = computed(() => workspace.value?.models || [])
const activePanel = computed(() => (typeof route.meta.portalPanel === 'string' ? route.meta.portalPanel : 'overview'))

async function load() {
  loading.value = true
  error.value = ''
  try {
    workspace.value = await getPortalWorkspace()
    ensureFormDefaults()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function ensureFormDefaults() {
  if (!form.model_allowlist.length && modelOptions.value[0]) {
    form.model_allowlist = [modelOptions.value[0]]
  }
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function formatCost(micros: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format((micros || 0) / 1_000_000)
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function toggleModel(model: string) {
  if (form.model_allowlist.includes(model)) {
    form.model_allowlist = form.model_allowlist.filter((item) => item !== model)
    return
  }
  form.model_allowlist = [...form.model_allowlist, model]
}

function resetForm() {
  form.name = ''
  form.policy_id = ''
  form.qps_limit = 0
  form.monthly_token_limit = 0
  form.expires_at = ''
  form.model_allowlist = modelOptions.value[0] ? [modelOptions.value[0]] : []
  ensureFormDefaults()
}

async function createKey() {
  if (!canManageKeys.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await createPortalAPIKey({ ...form, model_allowlist: form.model_allowlist.filter(Boolean) })
    createdSecret.value = result.key
    notice.value = t('portal.keyCreated')
    resetForm()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function rotateKey(key: APIKeyRecord) {
  if (!canManageKeys.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await rotatePortalAPIKey(key.id)
    createdSecret.value = result.key
    notice.value = t('portal.keyRotated')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function disableKey(key: APIKeyRecord) {
  if (!canManageKeys.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    await disablePortalAPIKey(key.id)
    notice.value = t('portal.keyDisabled')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
      <section class="page-header">
        <div>
          <h1>{{ t('portal.title') }}</h1>
          <p>{{ t('portal.subtitle') }}</p>
        </div>
        <div class="row-actions">
          <button class="button secondary" type="button" :disabled="loading" @click="load">
            <RefreshCw :size="17" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>
      <div v-if="notice" class="notice success">
        <strong>{{ notice }}</strong>
        <input v-if="createdSecret" :value="createdSecret" readonly />
        <span v-if="createdSecret" class="hint">{{ t('portal.secretOnce') }}</span>
      </div>

      <section v-if="activePanel === 'overview' || activePanel === 'usage' || activePanel === 'alerts'" class="metric-grid">
        <article class="metric-card">
          <span class="metric-icon"><KeyRound :size="18" /></span>
          <div>
            <span>{{ t('portal.activeKeys') }}</span>
            <strong>{{ activeKeys }}</strong>
            <small>{{ apiKeys.length }} {{ t('admin.apiKeys') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><Activity :size="18" /></span>
          <div>
            <span>{{ t('portal.requests') }}</span>
            <strong>{{ formatNumber(usage?.total_requests || 0) }}</strong>
            <small>{{ formatNumber(usage?.total_tokens || 0) }} {{ t('usage.tokens') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><WalletCards :size="18" /></span>
          <div>
            <span>{{ t('portal.cost') }}</span>
            <strong>{{ formatCost(usage?.total_usage_cost_micros || 0) }}</strong>
            <small>{{ formatNumber(usage?.error_requests || 0) }} {{ t('usage.errors') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><ShieldAlert :size="18" /></span>
          <div>
            <span>{{ t('portal.activeAlerts') }}</span>
            <strong>{{ alerts.length }}</strong>
            <small>{{ workspace?.principal || '-' }}</small>
          </div>
        </article>
      </section>

      <section v-if="activePanel === 'overview'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('portal.next') }}</h2>
              <p>{{ t('portal.nextHelp') }}</p>
            </div>
            <KeyRound :size="18" />
          </div>
          <div class="panel-body">
            <div class="row-actions">
              <RouterLink class="button secondary" to="/portal/keys">{{ t('portal.myKeys') }}</RouterLink>
              <RouterLink class="button secondary" to="/portal/usage">{{ t('portal.usage') }}</RouterLink>
              <RouterLink class="button secondary" to="/portal/alerts">{{ t('portal.alerts') }}</RouterLink>
            </div>
          </div>
        </section>
      </section>

      <section v-if="activePanel === 'keys'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('portal.createKey') }}</h2>
              <p>{{ t('portal.createKeyHelp') }}</p>
            </div>
            <Plus :size="18" />
          </div>
          <form class="panel-body" @submit.prevent="createKey">
            <fieldset :disabled="!canManageKeys || saving" class="form-fieldset">
              <label class="field">
                <span>{{ t('apiKeys.name') }}</span>
                <input v-model="form.name" required :placeholder="t('portal.keyNamePlaceholder')" />
              </label>
              <div class="field">
                <span>{{ t('apiKeys.modelAllowlist') }}</span>
                <div class="chip-list">
                  <button
                    v-for="model in modelOptions"
                    :key="model"
                    class="pill"
                    type="button"
                    :class="{ 'status-success': form.model_allowlist.includes(model) }"
                    @click="toggleModel(model)"
                  >
                    {{ model }}
                  </button>
                </div>
              </div>
              <div class="form-grid">
                <label class="field">
                  <span>{{ t('apiKeys.qps') }}</span>
                  <input v-model.number="form.qps_limit" type="number" min="0" />
                </label>
                <label class="field">
                  <span>{{ t('apiKeys.monthlyTokens') }}</span>
                  <input v-model.number="form.monthly_token_limit" type="number" min="0" />
                </label>
              </div>
              <button class="button primary" type="submit" :disabled="!form.model_allowlist.length || saving">
                <KeyRound :size="17" />
                {{ t('portal.createKey') }}
              </button>
              <span v-if="!canManageKeys" class="hint">{{ t('portal.readOnly') }}</span>
            </fieldset>
          </form>
        </section>
      </section>

      <section v-if="activePanel === 'keys'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('portal.myKeys') }}</h2>
            <p>{{ t('portal.keySummary') }}</p>
          </div>
          <KeyRound :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table crud-table">
            <thead>
              <tr>
                <th>{{ t('apiKeys.name') }}</th>
                <th>{{ t('apiKeys.models') }}</th>
                <th>{{ t('apiKeys.policy') }}</th>
                <th>{{ t('apiKeys.limits') }}</th>
                <th>{{ t('providers.status') }}</th>
                <th>{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in apiKeys" :key="key.id">
                <td>
                  <strong>{{ key.name }}</strong>
                  <span>{{ key.prefix }} · {{ key.fingerprint }}</span>
                </td>
                <td>
                  <span>{{ key.model_allowlist.join(', ') }}</span>
                </td>
                <td>{{ key.policy_id || t('policies.inherit') }}</td>
                <td>
                  <strong>{{ key.qps_limit || t('apiKeys.unlimited') }} QPS</strong>
                  <span>{{ key.monthly_token_limit || t('apiKeys.unlimited') }} {{ t('usage.tokens') }}</span>
                </td>
                <td>
                  <span class="pill" :class="apiKeyLifecycleClass(key)">{{ t(apiKeyLifecycleLabelKey(key)) }}</span>
                  <span>{{ formatDate(key.last_used_at) }}</span>
                </td>
                <td>
                  <div class="row-actions">
                    <button class="button secondary" type="button" :disabled="!canManageKeys || saving || !canRotateAPIKey(key)" @click="rotateKey(key)">
                      <RotateCw :size="15" />
                      {{ t('apiKeys.rotate') }}
                    </button>
                    <button class="button secondary" type="button" :disabled="!canManageKeys || saving || !canDisableAPIKey(key)" @click="disableKey(key)">
                      {{ t('apiKeys.disable') }}
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="!apiKeys.length">
                <td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('portal.emptyKeys') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activePanel === 'usage'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('portal.usage') }}</h2>
            <p>{{ t('portal.usageHelp') }}</p>
          </div>
          <Activity :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ t('usage.model') }}</th>
                <th>{{ t('usage.requests') }}</th>
                <th>{{ t('usage.tokens') }}</th>
                <th>{{ t('usage.cost') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in usage?.by_model || []" :key="item.model">
                <td>{{ item.model }}</td>
                <td>{{ formatNumber(item.requests) }}</td>
                <td>{{ formatNumber(item.tokens) }}</td>
                <td>{{ formatCost(item.usage_cost_micros) }}</td>
              </tr>
              <tr v-if="!(usage?.by_model || []).length">
                <td colspan="4" class="empty-cell">{{ loading ? t('common.loading') : t('portal.emptyUsage') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activePanel === 'alerts'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('portal.alerts') }}</h2>
            <p>{{ t('portal.alertsHelp') }}</p>
          </div>
          <ShieldAlert :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ t('alerts.alert') }}</th>
                <th>{{ t('alerts.severity') }}</th>
                <th>{{ t('alerts.lastSeen') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="alert in alerts" :key="alert.id">
                <td>
                  <strong>{{ alert.title }}</strong>
                  <span>{{ alert.summary }}</span>
                </td>
                <td><span class="pill" :class="alert.severity === 'critical' ? 'status-danger' : 'status-warning'">{{ alert.severity }}</span></td>
                <td>{{ formatDate(alert.last_seen_at) }}</td>
              </tr>
              <tr v-if="!alerts.length">
                <td colspan="3" class="empty-cell">{{ loading ? t('common.loading') : t('portal.emptyAlerts') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activePanel === 'traces'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('portal.recentTraces') }}</h2>
            <p>{{ t('portal.traceHelp') }}</p>
          </div>
          <Code2 :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table crud-table">
            <thead>
              <tr>
                <th>{{ t('audit.time') }}</th>
                <th>{{ t('usage.model') }}</th>
                <th>{{ t('providers.status') }}</th>
                <th>{{ t('traces.policy') }}</th>
                <th>{{ t('traces.summary') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="trace in recentTraces" :key="trace.id">
                <td>{{ formatDate(trace.created_at) }}</td>
                <td>{{ trace.model || '-' }}</td>
                <td>
                  <span class="pill" :class="trace.status === 'forwarded' || trace.status === 'accepted' ? 'status-success' : 'status-warning'">{{ trace.status }}</span>
                  <span>{{ trace.error_type || '-' }}</span>
                </td>
                <td>
                  <strong>{{ trace.policy_name || trace.policy_id || '-' }}</strong>
                  <span>{{ trace.policy_source || '-' }}</span>
                </td>
                <td>
                  <strong>{{ trace.response_summary || '-' }}</strong>
                  <span>{{ trace.route_reason || trace.request_summary || '-' }}</span>
                </td>
              </tr>
              <tr v-if="!recentTraces.length">
                <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('portal.emptyTraces') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
  </main>
</template>
