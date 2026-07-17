<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Code2, KeyRound, Plus, RefreshCw, RotateCw, Server, Settings, ShieldOff, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { createAPIKey, disableAPIKey, getAPIKeys, getGatewayModels, getProviders, getUsageReport, rotateAPIKey } from '@/api/control'
import { useAppStore } from '@/stores/app'
import type { APIKeyCreateRequest, APIKeyRecord, GatewayModel, ProviderConnection, UsageReport } from '@/types'
import { apiKeyLifecycleClass, apiKeyLifecycleLabelKey, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from '@/utils/apiKeys'

const { t } = useI18n()
const route = useRoute()
const app = useAppStore()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const createdSecret = ref('')
const providers = ref<ProviderConnection[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const apiKeys = ref<APIKeyRecord[]>([])
const usage = ref<UsageReport | null>(null)
const form = reactive<APIKeyCreateRequest>({
  name: t('console.defaultKeyName'),
  policy_id: '',
  model_allowlist: [],
  qps_limit: 0,
  monthly_token_limit: 0,
  expires_at: ''
})

const gatewayBaseUrl = computed(() => {
  const settings = app.publicSettings
  const base = settings?.public_base_url || window.location.origin
  const path = settings?.gateway_base_path || '/v1'
  return `${base.replace(/\/$/, '')}${path}`
})

const activeProviders = computed(() => providers.value.filter((item) => item.status === 'active').length)
const activeKeys = computed(() => apiKeys.value.filter((item) => apiKeyLifecycleStatus(item) === 'active').length)
const activePanel = computed(() => (typeof route.meta.consolePanel === 'string' ? route.meta.consolePanel : 'overview'))
const availableModels = computed(() => gatewayModels.value
  .filter((item) => item.status === 'active')
  .map((item) => item.model_id)
  .filter((item, index, values) => Boolean(item) && values.indexOf(item) === index)
  .slice(0, 12))
const sortedKeys = computed(() =>
  [...apiKeys.value].sort((a, b) => {
    const statusOrder = { active: 0, retiring: 1, disabled: 2, retired: 3 }
    const statusDifference = statusOrder[apiKeyLifecycleStatus(a)] - statusOrder[apiKeyLifecycleStatus(b)]
    if (statusDifference !== 0) return statusDifference
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  })
)

function formatNumber(value?: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function formatCost(micros?: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format((micros || 0) / 1_000_000)
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatLimit(value: number): string {
  return value > 0 ? formatNumber(value) : t('apiKeys.unlimited')
}

function ensureFormDefaults() {
  if (!form.model_allowlist.length && availableModels.value[0]) {
    form.model_allowlist = [availableModels.value[0]]
  }
  if (!form.name.trim()) {
    form.name = t('console.defaultKeyName')
  }
}

function resetForm() {
  form.name = t('console.defaultKeyName')
  form.policy_id = ''
  form.qps_limit = 0
  form.monthly_token_limit = 0
  form.expires_at = ''
  form.model_allowlist = availableModels.value[0] ? [availableModels.value[0]] : []
  ensureFormDefaults()
}

function toggleModel(model: string) {
  if (form.model_allowlist.includes(model)) {
    form.model_allowlist = form.model_allowlist.filter((item) => item !== model)
    return
  }
  form.model_allowlist = [...form.model_allowlist, model]
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [providerList, keyList, modelList, usageReport] = await Promise.all([
      getProviders(),
      getAPIKeys(),
      getGatewayModels(),
      getUsageReport()
    ])
    providers.value = providerList
    apiKeys.value = keyList
    gatewayModels.value = modelList
    usage.value = usageReport
    ensureFormDefaults()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function createKey() {
  saving.value = true
  error.value = ''
  notice.value = ''
  createdSecret.value = ''
  try {
    ensureFormDefaults()
    const result = await createAPIKey({ ...form, model_allowlist: form.model_allowlist.filter(Boolean) })
    createdSecret.value = result.key
    notice.value = t('console.keyCreated')
    resetForm()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function rotateKey(key: APIKeyRecord) {
  saving.value = true
  error.value = ''
  notice.value = ''
  createdSecret.value = ''
  try {
    const result = await rotateAPIKey(key.id)
    createdSecret.value = result.key
    notice.value = t('console.keyRotated')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function disableKey(key: APIKeyRecord) {
  saving.value = true
  error.value = ''
  notice.value = ''
  createdSecret.value = ''
  try {
    await disableAPIKey(key.id)
    notice.value = t('console.keyDisabled')
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
  <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('console.title') }}</h1>
          <p>{{ t('console.subtitle') }}</p>
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
        <span v-if="createdSecret" class="hint">{{ t('console.secretOnce') }}</span>
      </div>

      <section v-if="activePanel === 'overview' || activePanel === 'usage'" class="metric-grid">
        <article class="metric-card">
          <span class="metric-icon"><Server :size="18" /></span>
          <div>
            <span>{{ t('console.activeProviders') }}</span>
            <strong>{{ activeProviders }}</strong>
            <small>{{ providers.length }} {{ t('admin.providers') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><KeyRound :size="18" /></span>
          <div>
            <span>{{ t('console.activeKeys') }}</span>
            <strong>{{ activeKeys }}</strong>
            <small>{{ apiKeys.length }} {{ t('admin.apiKeys') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><Activity :size="18" /></span>
          <div>
            <span>{{ t('console.requests') }}</span>
            <strong>{{ formatNumber(usage?.total_requests) }}</strong>
            <small>{{ formatNumber(usage?.total_tokens) }} {{ t('usage.tokens') }}</small>
          </div>
        </article>
        <article class="metric-card">
          <span class="metric-icon"><WalletCards :size="18" /></span>
          <div>
            <span>{{ t('console.cost') }}</span>
            <strong>{{ formatCost(usage?.total_usage_cost_micros) }}</strong>
            <small>{{ formatNumber(usage?.error_requests) }} {{ t('usage.errors') }}</small>
          </div>
        </article>
      </section>

      <section v-if="activePanel === 'overview'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('console.gateway') }}</h2>
              <p>{{ t('console.gatewayHelp') }}</p>
            </div>
            <KeyRound :size="18" />
          </div>
          <div class="panel-body">
            <input :value="gatewayBaseUrl" :aria-label="t('console.gateway')" readonly />
            <pre class="code-block" tabindex="0">curl {{ gatewayBaseUrl }}/chat/completions \
  -H "Authorization: Bearer $ASTERROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"{{ availableModels[0] || '<gateway-model-id>' }}","messages":[{"role":"user","content":"ping"}]}'</pre>
            <div class="chip-list">
              <span v-for="model in availableModels" :key="model" class="pill">{{ model }}</span>
              <span v-if="!availableModels.length" class="hint">{{ t('apiKeys.noActiveModels') }}</span>
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('console.shortcuts') }}</h2>
              <p>{{ t('console.shortcutsHelp') }}</p>
            </div>
            <Settings :size="18" />
          </div>
          <div class="panel-body">
            <div class="row-actions">
              <RouterLink class="button secondary" to="/console/providers">{{ t('admin.providers') }}</RouterLink>
              <RouterLink class="button secondary" to="/console/routing-groups">{{ t('admin.routingGroups') }}</RouterLink>
              <RouterLink class="button secondary" to="/console/resources">{{ t('admin.providerAccounts') }}</RouterLink>
              <RouterLink class="button secondary" to="/console/keys">{{ t('console.keys') }}</RouterLink>
              <RouterLink class="button secondary" to="/console/usage">{{ t('console.usage') }}</RouterLink>
              <RouterLink class="button secondary" to="/console/settings">{{ t('admin.settings') }}</RouterLink>
            </div>
          </div>
        </section>
      </section>

      <section v-if="activePanel === 'keys'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('console.createKey') }}</h2>
            <p>{{ t('console.createKeyHelp') }}</p>
          </div>
          <Plus :size="18" />
        </div>
        <form class="panel-body" @submit.prevent="createKey">
            <fieldset class="form-fieldset" :disabled="saving">
              <label class="field">
                <span>{{ t('apiKeys.name') }}</span>
                <input v-model="form.name" required :placeholder="t('console.keyNamePlaceholder')" />
              </label>
              <div class="field">
                <span>{{ t('apiKeys.modelAllowlist') }}</span>
                <div class="chip-list">
                  <button
                    v-for="model in availableModels"
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
              <button class="button" type="submit" :disabled="saving || !form.model_allowlist.length">
                <KeyRound :size="17" />
                {{ saving ? t('common.saving') : t('console.createKey') }}
              </button>
            </fieldset>
        </form>
      </section>

      <section v-if="activePanel === 'keys'" class="panel section-gap">
        <div class="panel-header split-header">
          <div>
            <h2>{{ t('console.myKeys') }}</h2>
            <p>{{ t('console.keySummary') }}</p>
          </div>
          <Code2 :size="18" />
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table crud-table">
            <thead>
              <tr>
                <th>{{ t('apiKeys.name') }}</th>
                <th>{{ t('apiKeys.models') }}</th>
                <th>{{ t('apiKeys.limits') }}</th>
                <th>{{ t('providers.status') }}</th>
                <th>{{ t('apiKeys.lastUsed') }}</th>
                <th>{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="key in sortedKeys" :key="key.id">
                <td>
                  <strong>{{ key.name }}</strong>
                  <span>{{ key.prefix }} · {{ key.fingerprint }}</span>
                </td>
                <td>
                  <span>{{ key.model_allowlist.length ? key.model_allowlist.join(', ') : t('apiKeys.unlimited') }}</span>
                </td>
                <td>
                  <strong>{{ formatLimit(key.qps_limit) }} QPS</strong>
                  <span>{{ formatLimit(key.monthly_token_limit) }} {{ t('usage.tokens') }}</span>
                </td>
                <td>
                  <span class="pill" :class="apiKeyLifecycleClass(key)">{{ t(apiKeyLifecycleLabelKey(key)) }}</span>
                  <span>{{ formatDate(key.expires_at) }}</span>
                </td>
                <td>{{ formatDate(key.last_used_at) }}</td>
                <td>
                  <div class="row-actions">
                    <button class="button secondary" type="button" :disabled="saving || !canRotateAPIKey(key)" @click="rotateKey(key)">
                      <RotateCw :size="15" />
                      {{ t('apiKeys.rotate') }}
                    </button>
                    <button class="button danger" type="button" :disabled="saving || !canDisableAPIKey(key)" @click="disableKey(key)">
                      <ShieldOff :size="15" />
                      {{ t('apiKeys.disable') }}
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="!sortedKeys.length">
                <td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('console.emptyKeys') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="activePanel === 'usage'" class="grid section-gap">
        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('usage.byModel') }}</h2>
              <p>{{ t('console.usageHelp') }}</p>
            </div>
            <Activity :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ t('usage.model') }}</th>
                  <th>{{ t('usage.requests') }}</th>
                  <th>{{ t('usage.errors') }}</th>
                  <th>{{ t('usage.tokens') }}</th>
                  <th>{{ t('usage.cost') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in usage?.by_model || []" :key="item.model">
                  <td><strong>{{ item.model }}</strong></td>
                  <td>{{ formatNumber(item.requests) }}</td>
                  <td>{{ formatNumber(item.errors) }}</td>
                  <td>{{ formatNumber(item.tokens) }}</td>
                  <td>{{ formatCost(item.usage_cost_micros) }}</td>
                </tr>
                <tr v-if="!(usage?.by_model || []).length">
                  <td colspan="5" class="empty-cell"></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header split-header">
            <div>
              <h2>{{ t('portal.recentTraces') }}</h2>
              <p>{{ t('portal.traceHelp') }}</p>
            </div>
            <Code2 :size="18" />
          </div>
          <div class="panel-body table-scroll">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ t('usage.model') }}</th>
                  <th>{{ t('providers.status') }}</th>
                  <th>{{ t('usage.tokens') }}</th>
                  <th>{{ t('usage.latency') }}</th>
                  <th>{{ t('usage.createdAt') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in usage?.recent || []" :key="item.id">
                  <td><strong>{{ item.model }}</strong></td>
                  <td><span class="pill" :class="item.status === 'success' ? 'status-success' : 'status-danger'">{{ item.status }}</span></td>
                  <td>{{ formatNumber(item.input_tokens + item.output_tokens) }}</td>
                  <td>{{ item.latency_ms }}ms</td>
                  <td>{{ formatDate(item.created_at) }}</td>
                </tr>
                <tr v-if="!(usage?.recent || []).length">
                  <td colspan="5" class="empty-cell"></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </section>
  </main>
</template>
