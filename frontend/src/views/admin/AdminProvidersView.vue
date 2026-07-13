<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Activity,
  Bot,
  Check,
  Cloud,
  Edit3,
  KeyRound,
  Plus,
  RefreshCw,
  Save,
  Search,
  Sparkles,
  Trash2,
  X,
  Zap
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { checkProvider, createProvider, getProviderHealthChecks, getProviders, updateProvider } from '@/api/control'
import type { ProviderConnection, ProviderHealthCheck, ProviderRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const checkingID = ref('')
const error = ref('')
const message = ref('')
const providers = ref<ProviderConnection[]>([])
const query = ref('')
const statusFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ProviderConnection | null>(null)
const platform = ref<ProviderPlatform>('openai')
const customModel = ref('')
const healthChecks = ref<Record<string, ProviderHealthCheck>>({})

type ProviderPlatform = 'anthropic' | 'openai' | 'gemini' | 'antigravity' | 'grok'

const PLATFORM_CONFIG = {
  anthropic: {
    label: 'Anthropic',
    icon: Sparkles,
    type: 'anthropic',
    baseUrl: 'https://api.anthropic.com',
    apiKeyPlaceholder: 'sk-ant-api03-...',
    models: ['claude-sonnet-4-5', 'claude-opus-4-1', 'claude-haiku-4-5']
  },
  openai: {
    label: 'OpenAI',
    icon: Zap,
    type: 'openai_compatible',
    baseUrl: 'https://api.openai.com/v1',
    apiKeyPlaceholder: 'sk-proj-...',
    models: ['gpt-5.2', 'gpt-5.1', 'gpt-4o', 'o3']
  },
  gemini: {
    label: 'Gemini',
    icon: Sparkles,
    type: 'gemini',
    baseUrl: 'https://generativelanguage.googleapis.com',
    apiKeyPlaceholder: 'AIza...',
    models: ['gemini-2.5-pro', 'gemini-2.5-flash', 'gemini-2.0-flash']
  },
  antigravity: {
    label: 'Antigravity',
    icon: Cloud,
    type: 'openai_compatible',
    baseUrl: 'https://cloudcode-pa.googleapis.com',
    apiKeyPlaceholder: 'sk-...',
    models: ['gemini-2.5-pro', 'gemini-2.5-flash', 'claude-sonnet-4-5']
  },
  grok: {
    label: 'Grok',
    icon: Bot,
    type: 'openai_compatible',
    baseUrl: 'https://api.x.ai/v1',
    apiKeyPlaceholder: 'xai-...',
    models: ['grok-4', 'grok-4-fast-reasoning', 'grok-3']
  }
} as const

const platformEntries = Object.entries(PLATFORM_CONFIG) as Array<
  [ProviderPlatform, (typeof PLATFORM_CONFIG)[ProviderPlatform]]
>
const currentPlatform = computed(() => PLATFORM_CONFIG[platform.value])

const form = reactive<ProviderRequest>({
  name: '',
  type: 'openai_compatible',
  base_url: '',
  status: 'active',
  models: [],
  priority: 100,
  api_key: ''
})

const filteredProviders = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return providers.value.filter((provider) => {
    if (statusFilter.value && provider.status !== statusFilter.value) return false
    if (!keyword) return true
    return [provider.name, provider.type, provider.base_url, provider.models.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: providers.value.length,
  active: providers.value.filter((item) => item.status === 'active').length,
  warning: providers.value.filter((item) => item.status === 'needs_secret').length,
  disabled: providers.value.filter((item) => item.status === 'disabled').length
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    type: 'openai_compatible',
    base_url: PLATFORM_CONFIG.openai.baseUrl,
    status: 'active',
    models: [],
    priority: 100,
    api_key: ''
  })
  platform.value = 'openai'
  customModel.value = ''
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(provider: ProviderConnection) {
  editing.value = provider
  platform.value = inferPlatform(provider)
  Object.assign(form, {
    name: provider.name,
    type: provider.type,
    base_url: provider.base_url,
    status: provider.status,
    models: [...provider.models],
    priority: provider.priority,
    api_key: ''
  })
  customModel.value = ''
  modalOpen.value = true
}

function inferPlatform(provider: ProviderConnection): ProviderPlatform {
  const baseURL = provider.base_url.toLowerCase()
  if (baseURL.includes('api.x.ai') || baseURL.includes('grok')) return 'grok'
  if (baseURL.includes('cloudcode-pa') || baseURL.includes('antigravity')) return 'antigravity'
  if (provider.type === 'anthropic' || baseURL.includes('anthropic')) return 'anthropic'
  if (provider.type === 'gemini' || baseURL.includes('generativelanguage')) return 'gemini'
  return 'openai'
}

function selectPlatform(nextPlatform: ProviderPlatform) {
  platform.value = nextPlatform
  const config = PLATFORM_CONFIG[nextPlatform]
  form.type = config.type
  form.base_url = config.baseUrl
}

function toggleModel(model: string) {
  form.models = form.models.includes(model)
    ? form.models.filter((item) => item !== model)
    : [...form.models, model]
}

function fillRecommendedModels() {
  form.models = Array.from(new Set([...form.models, ...currentPlatform.value.models]))
}

function addCustomModel() {
  const model = customModel.value.trim()
  if (!model || form.models.includes(model)) return
  form.models = [...form.models, model]
  customModel.value = ''
}

function updateEnabled(event: Event) {
  form.status = (event.target as HTMLInputElement).checked ? 'active' : 'disabled'
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [providerData, healthData] = await Promise.all([getProviders(), getProviderHealthChecks()])
    providers.value = providerData
    healthChecks.value = Object.fromEntries(healthData.map((item) => [item.provider_id, item]))
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const payload = { ...form, models: [...form.models] }
    if (editing.value) {
      await updateProvider(editing.value.id, payload)
      message.value = t('providers.updated')
    } else {
      await createProvider(payload)
      message.value = t('providers.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function runCheck(provider: ProviderConnection) {
  checkingID.value = provider.id
  error.value = ''
  message.value = ''
  try {
    const result = await checkProvider(provider.id)
    healthChecks.value = { ...healthChecks.value, [provider.id]: result }
    message.value = result.message
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    checkingID.value = ''
  }
}

function statusClass(status: string) {
  if (status === 'active' || status === 'ok') return 'status-success'
  if (status === 'disabled' || status === 'error') return 'status-danger'
  return 'status-warning'
}

function formatHealth(check: ProviderHealthCheck): string {
  const time = new Date(check.checked_at).toLocaleString()
  return `${check.status} / ${check.latency_ms}ms / ${time}`
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.providers') }}</h1>
        <p>{{ t('providers.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('providers.newProvider') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('providers.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('providers.active') }}</span>
      <span><strong>{{ summary.warning }}</strong>{{ t('providers.warning') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('providers.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="needs_secret">needs_secret</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('providers.name') }}</th>
              <th>{{ t('providers.type') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('providers.models') }}</th>
              <th>{{ t('providers.priority') }}</th>
              <th>{{ t('providers.health') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="provider in filteredProviders" :key="provider.id">
              <td>
                <strong>{{ provider.name }}</strong>
                <span>{{ provider.base_url }}</span>
              </td>
              <td>{{ provider.type }}</td>
              <td><span class="pill" :class="statusClass(provider.status)">{{ provider.status }}</span></td>
              <td>
                <div class="chip-list">
                  <span v-for="model in provider.models.slice(0, 3)" :key="model" class="pill">{{ model }}</span>
                  <span v-if="provider.models.length > 3" class="pill">+{{ provider.models.length - 3 }}</span>
                </div>
              </td>
              <td>{{ provider.priority }}</td>
              <td>
                <template v-if="healthChecks[provider.id]">
                  <span class="pill" :class="statusClass(healthChecks[provider.id].status)">
                    {{ formatHealth(healthChecks[provider.id]) }}
                  </span>
                  <span>{{ healthChecks[provider.id].message }}</span>
                </template>
                <span v-else class="hint">{{ t('providers.notChecked') }}</span>
              </td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" :disabled="checkingID === provider.id" @click="runCheck(provider)">
                    <Activity :size="15" />
                    {{ checkingID === provider.id ? t('common.loading') : t('providers.check') }}
                  </button>
                  <button class="button secondary" type="button" @click="openEdit(provider)">
                    <Edit3 :size="15" />
                    {{ t('common.edit') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredProviders.length">
              <td colspan="7" class="empty-cell">
                {{ loading ? t('common.loading') : t('providers.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section
        class="modal-card modal-card-wide provider-api-modal"
        :data-platform="platform"
        role="dialog"
        aria-modal="true"
        :aria-label="editing ? t('providers.editAccount') : t('providers.addAccount')"
      >
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('providers.editAccount') : t('providers.addAccount') }}</h2>
            <p>{{ t('providers.accountModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeModal">
            <X :size="19" />
          </button>
        </header>
        <form class="provider-modal-form" @submit.prevent="save">
          <div class="modal-body provider-modal-body">
            <div class="field">
              <label for="provider-account-name">{{ t('providers.accountName') }}</label>
              <input
                id="provider-account-name"
                v-model="form.name"
                required
                :placeholder="t('providers.accountNamePlaceholder')"
              />
            </div>

            <div class="field provider-platform-field">
              <label>{{ t('providers.platform') }}</label>
              <div class="provider-platform-tabs" role="tablist" :aria-label="t('providers.platform')">
                <button
                  v-for="[platformID, config] in platformEntries"
                  :key="platformID"
                  class="provider-platform-tab"
                  :class="{ active: platform === platformID }"
                  type="button"
                  role="tab"
                  :aria-selected="platform === platformID"
                  @click="selectPlatform(platformID)"
                >
                  <component :is="config.icon" :size="17" />
                  {{ config.label }}
                </button>
              </div>
            </div>

            <div class="field">
              <label>{{ t('providers.accountType') }}</label>
              <div class="provider-account-type-card" aria-current="true">
                <span class="provider-account-type-icon"><KeyRound :size="18" /></span>
                <span>
                  <strong>API Key</strong>
                  <small>{{ t('providers.apiOnlyDescription', { platform: currentPlatform.label }) }}</small>
                </span>
                <Check class="provider-account-type-check" :size="18" />
              </div>
            </div>

            <section class="provider-form-section">
              <div class="field">
                <label for="provider-base-url">{{ t('providers.baseUrl') }}</label>
                <input
                  id="provider-base-url"
                  v-model="form.base_url"
                  required
                  class="provider-mono-input"
                  :placeholder="currentPlatform.baseUrl"
                />
                <span class="hint">{{ t('providers.baseUrlHint', { platform: currentPlatform.label }) }}</span>
              </div>
              <div class="field">
                <label for="provider-api-key">{{ t('providers.apiKeyRequired') }}</label>
                <input
                  id="provider-api-key"
                  v-model="form.api_key"
                  type="password"
                  :required="!editing"
                  autocomplete="new-password"
                  class="provider-mono-input"
                  :placeholder="editing ? t('providers.keepSecret') : currentPlatform.apiKeyPlaceholder"
                />
                <span class="hint">{{ t('providers.apiKeyHint', { platform: currentPlatform.label }) }}</span>
              </div>
            </section>

            <section class="provider-form-section provider-model-section">
              <div class="provider-section-heading">
                <div>
                  <h3>{{ t('providers.modelRestrictions') }}</h3>
                  <p>{{ t('providers.modelRestrictionHint') }}</p>
                </div>
                <span class="provider-mode-badge"><Check :size="14" />{{ t('providers.modelWhitelist') }}</span>
              </div>

              <div class="provider-model-picker">
                <div v-if="form.models.length" class="provider-model-grid">
                  <span v-for="model in form.models" :key="model" class="provider-model-chip">
                    <Bot :size="15" />
                    <span>{{ model }}</span>
                    <button type="button" :aria-label="t('providers.removeModel', { model })" @click="toggleModel(model)">
                      <X :size="14" />
                    </button>
                  </span>
                </div>
                <p v-else class="provider-model-empty">{{ t('providers.supportsAllModels') }}</p>
                <div class="provider-model-count">
                  <span>{{ t('providers.modelCount', { count: form.models.length }) }}</span>
                  <span>{{ t('providers.modelWhitelist') }}</span>
                </div>
              </div>

              <div class="provider-model-actions">
                <button type="button" class="provider-action-button recommended" @click="fillRecommendedModels">
                  <Sparkles :size="15" />
                  {{ t('providers.fillRelatedModels') }}
                </button>
                <button type="button" class="provider-action-button danger" :disabled="!form.models.length" @click="form.models = []">
                  <Trash2 :size="15" />
                  {{ t('providers.clearAllModels') }}
                </button>
              </div>

              <div class="provider-recommended-models">
                <span>{{ t('providers.recommendedModels') }}</span>
                <div>
                  <button
                    v-for="model in currentPlatform.models"
                    :key="model"
                    type="button"
                    :class="{ selected: form.models.includes(model) }"
                    @click="toggleModel(model)"
                  >
                    <Check v-if="form.models.includes(model)" :size="13" />
                    <Plus v-else :size="13" />
                    {{ model }}
                  </button>
                </div>
              </div>

              <div class="field provider-custom-model">
                <label for="provider-custom-model">{{ t('providers.customModelName') }}</label>
                <div>
                  <input
                    id="provider-custom-model"
                    v-model="customModel"
                    :placeholder="t('providers.customModelPlaceholder')"
                    @keydown.enter.prevent="addCustomModel"
                  />
                  <button class="button secondary" type="button" :disabled="!customModel.trim()" @click="addCustomModel">
                    <Plus :size="16" />
                    {{ t('providers.addModel') }}
                  </button>
                </div>
              </div>
            </section>

            <section class="provider-form-section provider-common-section">
              <div class="provider-section-heading">
                <div>
                  <h3>{{ t('providers.commonConfiguration') }}</h3>
                  <p>{{ t('providers.commonConfigurationHint') }}</p>
                </div>
              </div>
              <div class="provider-config-grid">
                <div class="provider-toggle-row">
                  <div>
                    <strong>{{ t('providers.enabledStatus') }}</strong>
                    <small>{{ t('providers.enabledStatusHint') }}</small>
                  </div>
                  <label class="switch">
                    <input type="checkbox" :checked="form.status === 'active'" @change="updateEnabled" />
                    <span />
                  </label>
                </div>
                <div class="field">
                  <label for="provider-priority">{{ t('providers.priority') }}</label>
                  <input id="provider-priority" v-model.number="form.priority" type="number" min="1" required />
                  <span class="hint">{{ t('providers.priorityHint') }}</span>
                </div>
              </div>
            </section>
          </div>
          <footer class="modal-footer">
            <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
            <button class="button" type="submit" :disabled="saving">
              <Save :size="17" />
              {{ saving ? t('common.saving') : editing ? t('providers.updateAccount') : t('providers.createAccount') }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </main>
</template>
