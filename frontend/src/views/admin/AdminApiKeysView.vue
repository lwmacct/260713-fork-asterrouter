<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Edit3, Plus, RefreshCw, RotateCw, Save, Search, ShieldOff, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import APIKeyRotationDialog from '@/components/APIKeyRotationDialog.vue'
import GatewayModelPicker from '@/components/model/GatewayModelPicker.vue'
import { apiKeyLifecycleClass, apiKeyLifecycleLabelKey, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from '@/utils/apiKeys'
import {
  createAPIKey,
  disableAPIKey,
  getAPIKeys,
  getAPIKeyPolicyExplanation,
  getGatewayTraces,
  getGatewayModels,
  getGovernancePolicies,
  getUsageReport,
	getWorkspaceUsers,
  rotateAPIKey,
  updateAPIKey
} from '@/api/control'
import type { APIKeyCreateRequest, APIKeyRecord, GatewayModel, GatewayPolicyExplanation, GatewayTrace, GovernancePolicy, UsageReport, WorkspaceUser } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const modalOpen = ref(false)
const editing = ref<APIKeyRecord | null>(null)
const selectedKey = ref<APIKeyRecord | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const detailUsage = ref<UsageReport | null>(null)
const detailTraces = ref<GatewayTrace[]>([])
const detailPolicyExplanation = ref<GatewayPolicyExplanation | null>(null)
const oneTimeKey = ref('')
const rotationTarget = ref<APIKeyRecord | null>(null)
const rotationSaving = ref(false)
const apiKeys = ref<APIKeyRecord[]>([])
const policies = ref<GovernancePolicy[]>([])
const users = ref<WorkspaceUser[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const query = ref('')
const statusFilter = ref('')
const keyStatus = ref('active')
const form = reactive<APIKeyCreateRequest>({
  name: '',
  policy_id: '',
  model_allowlist: [],
  qps_limit: 10,
  monthly_token_limit: 1000000,
  expires_at: '',
  key_type: 'workspace',
  customer_id: '',
  owner_user_id: ''
})

const policyByID = computed(() => new Map(policies.value.map((item) => [item.id, item])))
const activePolicies = computed(() => policies.value.filter((item) => item.status === 'active'))
const defaultGatewayModel = computed(() => gatewayModels.value.find((item) => item.status === 'active')?.model_id || '')

const filteredKeys = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return apiKeys.value.filter((key) => {
    if (statusFilter.value && apiKeyLifecycleStatus(key) !== statusFilter.value) return false
    if (!keyword) return true
    const policy = key.policy_id ? policyByID.value.get(key.policy_id)?.name || key.policy_id : ''
    return [key.name, key.fingerprint, key.prefix, key.key_type, key.owner_user_id, key.customer_id, policy, key.model_allowlist.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: apiKeys.value.length,
  active: apiKeys.value.filter((item) => apiKeyLifecycleStatus(item) === 'active').length,
  retiring: apiKeys.value.filter((item) => apiKeyLifecycleStatus(item) === 'retiring').length,
  disabled: apiKeys.value.filter((item) => ['disabled', 'retired'].includes(apiKeyLifecycleStatus(item))).length,
  policies: new Set(apiKeys.value.map((item) => item.policy_id).filter(Boolean)).size
}))

function dateInputValue(value?: string): string {
  return value ? value.slice(0, 10) : ''
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    name: '',
    policy_id: '',
    model_allowlist: defaultGatewayModel.value ? [defaultGatewayModel.value] : [],
    qps_limit: 10,
    monthly_token_limit: 1000000,
    expires_at: '',
    key_type: 'workspace',
    customer_id: '',
    owner_user_id: ''
  })
  keyStatus.value = 'active'
  modalOpen.value = true
}

function openEdit(key: APIKeyRecord) {
  editing.value = key
  Object.assign(form, {
    name: key.name,
    policy_id: key.policy_id || '',
    model_allowlist: [...key.model_allowlist],
    qps_limit: key.qps_limit,
    monthly_token_limit: key.monthly_token_limit,
    expires_at: dateInputValue(key.expires_at),
    key_type: key.key_type,
    customer_id: key.customer_id,
    owner_user_id: key.owner_user_id
  })
  keyStatus.value = key.status
  modalOpen.value = true
}

async function openDetails(key: APIKeyRecord) {
  selectedKey.value = key
  detailLoading.value = true
  detailError.value = ''
  detailUsage.value = null
  detailTraces.value = []
  detailPolicyExplanation.value = null
  try {
    const params = { api_key_id: key.id, limit: 5 }
    const [usage, traces, explanation] = await Promise.all([getUsageReport(params), getGatewayTraces(params), getAPIKeyPolicyExplanation(key.id)])
    detailUsage.value = usage
    detailTraces.value = traces
    detailPolicyExplanation.value = explanation
  } catch (err) {
    detailError.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    detailLoading.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [keyResult, policyResult, userResult, modelResult] = await Promise.allSettled([
      getAPIKeys(),
      getGovernancePolicies(),
      getWorkspaceUsers(),
      getGatewayModels()
    ])
    if (keyResult.status === 'rejected') throw keyResult.reason
    if (modelResult.status === 'rejected') throw modelResult.reason
    apiKeys.value = keyResult.value
    policies.value = policyResult.status === 'fulfilled' ? policyResult.value : []
    users.value = userResult.status === 'fulfilled' ? userResult.value : []
    gatewayModels.value = modelResult.value
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  oneTimeKey.value = ''
  try {
    if (editing.value) {
      await updateAPIKey(editing.value.id, {
        name: form.name,
        policy_id: form.policy_id,
        model_allowlist: [...form.model_allowlist],
        qps_limit: form.qps_limit,
        monthly_token_limit: form.monthly_token_limit,
        expires_at: form.expires_at,
        status: keyStatus.value,
        key_type: form.key_type,
        customer_id: form.customer_id,
        owner_user_id: form.owner_user_id
      })
      message.value = t('apiKeys.updated')
    } else {
      const created = await createAPIKey({ ...form, model_allowlist: [...form.model_allowlist] })
      oneTimeKey.value = created.key
      message.value = t('apiKeys.created')
    }
    modalOpen.value = false
    editing.value = null
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

function openRotation(key: APIKeyRecord) {
  rotationTarget.value = key
}

async function confirmRotation(gracePeriodSeconds: number) {
  if (!rotationTarget.value) return
  rotationSaving.value = true
  error.value = ''
  message.value = ''
  oneTimeKey.value = ''
  try {
    const rotated = await rotateAPIKey(rotationTarget.value.id, gracePeriodSeconds)
    oneTimeKey.value = rotated.key
    message.value = t('apiKeys.rotated')
    rotationTarget.value = null
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    rotationSaving.value = false
  }
}

async function disable(id: string) {
  error.value = ''
  message.value = ''
  try {
    await disableAPIKey(id)
    message.value = t('apiKeys.disabled')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat().format(value)
}

function formatUsageCostMicros(value: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2
  }).format(value / 1_000_000)
}

function statusClass(status: string): string {
  if (status === 'forwarded' || status === 'accepted') return 'status-success'
  if (status === 'error' || status === 'upstream_error') return 'status-danger'
  return 'status-warning'
}

function formatPolicySource(source: string): string {
  return source ? source.replace(/_/g, ' ') : '-'
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.apiKeys') }}</h1>
        <p>{{ t('apiKeys.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('apiKeys.newKey') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('apiKeys.keys') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.retiring }}</strong>{{ t('apiKeys.retiring') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
      <span><strong>{{ summary.policies }}</strong>{{ t('apiKeys.boundPolicies') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('apiKeys.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">{{ t('apiKeys.lifecycle.active') }}</option>
        <option value="retiring">{{ t('apiKeys.lifecycle.retiring') }}</option>
        <option value="retired">{{ t('apiKeys.lifecycle.retired') }}</option>
        <option value="disabled">{{ t('apiKeys.lifecycle.disabled') }}</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="oneTimeKey" class="notice success">
      <strong>{{ t('apiKeys.oneTime') }}</strong>
      <input :value="oneTimeKey" readonly />
    </div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('apiKeys.name') }}</th>
              <th>{{ t('apiKeys.fingerprint') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('policies.policy') }}</th>
              <th>{{ t('apiKeys.models') }}</th>
              <th>{{ t('apiKeys.monthlyTokens') }}</th>
              <th>{{ t('apiKeys.lastUsed') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="key in filteredKeys" :key="key.id">
              <td>
                <strong>{{ key.name }}</strong>
                <span>{{ key.key_type }} · {{ key.owner_user_id || key.customer_id || key.prefix }}</span>
              </td>
              <td>{{ key.fingerprint }}</td>
              <td><span class="pill" :class="apiKeyLifecycleClass(key)">{{ t(apiKeyLifecycleLabelKey(key)) }}</span></td>
              <td>
                <strong>{{ key.policy_id ? policyByID.get(key.policy_id)?.name || key.policy_id : t('policies.inherit') }}</strong>
                <span>{{ key.policy_id ? t('policies.explicitBinding') : t('policies.scopeFallback') }}</span>
              </td>
              <td>
                <div class="chip-list">
                  <span v-for="model in key.model_allowlist.slice(0, 3)" :key="model" class="pill">{{ model }}</span>
                  <span v-if="key.model_allowlist.length > 3" class="pill">+{{ key.model_allowlist.length - 3 }}</span>
                </div>
              </td>
              <td>{{ formatTokens(key.monthly_token_limit) }}</td>
              <td>{{ formatDate(key.last_used_at) }}</td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" :disabled="!canRotateAPIKey(key)" @click="openEdit(key)">
                    <Edit3 :size="15" />
                    {{ t('common.edit') }}
                  </button>
                  <button class="button secondary" type="button" @click="openDetails(key)">
                    <Activity :size="15" />
                    {{ t('common.details') }}
                  </button>
                  <button class="button secondary" type="button" :disabled="!canRotateAPIKey(key)" @click="openRotation(key)">
                    <RotateCw :size="15" />
                    {{ t('apiKeys.rotate') }}
                  </button>
                  <button v-if="canDisableAPIKey(key)" class="button danger" type="button" @click="disable(key.id)">
                    <ShieldOff :size="15" />
                    {{ t('apiKeys.disable') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredKeys.length">
              <td colspan="8" class="empty-cell">{{ loading ? t('common.loading') : t('apiKeys.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="modalOpen = false">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('apiKeys.editKey') : t('apiKeys.newKey') }}</h2>
            <p>{{ t('apiKeys.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="modalOpen = false; editing = null"><X :size="19" /></button>
        </header>

        <div class="modal-body form-grid">
          <div class="field form-span-2">
            <label>{{ t('apiKeys.name') }}</label>
            <input v-model="form.name" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('policies.policy') }}</label>
            <select v-model="form.policy_id">
              <option value="">{{ t('policies.inherit') }}</option>
              <option v-for="policy in activePolicies" :key="policy.id" :value="policy.id">{{ policy.name }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('apiKeys.keyType') }}</label>
            <select v-model="form.key_type">
              <option value="workspace">workspace</option>
              <option value="user">user</option>
              <option value="customer">customer</option>
              <option value="service">service</option>
            </select>
          </div>
          <div v-if="form.key_type === 'user'" class="field">
            <label>{{ t('apiKeys.owner') }}</label>
            <select v-model="form.owner_user_id" required>
              <option value="" disabled>{{ t('apiKeys.selectOwner') }}</option>
              <option v-for="user in users.filter((item) => item.status === 'active')" :key="user.id" :value="user.id">{{ user.display_name || user.email }} · {{ user.email }}</option>
            </select>
          </div>
          <div v-else-if="form.key_type === 'customer'" class="field">
            <label>{{ t('apiKeys.customerId') }}</label>
            <input v-model="form.customer_id" required />
          </div>
          <div class="field form-span-2">
            <label>{{ t('apiKeys.models') }}</label>
            <GatewayModelPicker v-model="form.model_allowlist" :models="gatewayModels" :disabled="saving" />
          </div>
          <div class="field">
            <label>{{ t('apiKeys.qps') }}</label>
            <input v-model.number="form.qps_limit" type="number" min="0" />
          </div>
          <div class="field">
            <label>{{ t('apiKeys.monthlyTokens') }}</label>
            <input v-model.number="form.monthly_token_limit" type="number" min="0" />
          </div>
          <div class="field form-span-2">
            <label>{{ t('apiKeys.expiresAt') }}</label>
            <input v-model="form.expires_at" type="date" />
          </div>
          <div v-if="editing" class="field form-span-2">
            <label>{{ t('providers.status') }}</label>
            <select v-model="keyStatus">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="modalOpen = false; editing = null">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="saving || !form.model_allowlist.length" @click="save">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="selectedKey" class="modal-backdrop" @click.self="selectedKey = null">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ selectedKey.name }}</h2>
            <p>{{ selectedKey.fingerprint }} · {{ t('apiKeys.defaultScope') }}</p>
          </div>
          <button class="icon-button" type="button" @click="selectedKey = null"><X :size="19" /></button>
        </header>

        <div class="modal-body api-key-detail">
          <div v-if="detailError" class="notice">{{ detailError }}</div>
          <div class="detail-grid">
            <div>
              <label>{{ t('apiKeys.scope') }}</label>
              <p>{{ selectedKey.key_type }} · {{ selectedKey.owner_user_id || selectedKey.customer_id || t('apiKeys.defaultScope') }}</p>
            </div>
            <div>
              <label>{{ t('providers.status') }}</label>
              <p>{{ selectedKey.status }}</p>
            </div>
            <div>
              <label>{{ t('apiKeys.qps') }}</label>
              <p>{{ selectedKey.qps_limit || t('apiKeys.unlimited') }}</p>
            </div>
            <div>
              <label>{{ t('apiKeys.monthlyTokens') }}</label>
              <p>{{ selectedKey.monthly_token_limit ? formatTokens(selectedKey.monthly_token_limit) : t('apiKeys.unlimited') }}</p>
            </div>
            <div>
              <label>{{ t('policies.policy') }}</label>
              <p>{{ selectedKey.policy_id ? policyByID.get(selectedKey.policy_id)?.name || selectedKey.policy_id : t('policies.inherit') }}</p>
            </div>
            <div>
              <label>{{ t('apiKeys.lastUsed') }}</label>
              <p>{{ formatDate(selectedKey.last_used_at) }}</p>
            </div>
            <div>
              <label>{{ t('apiKeys.expiresAt') }}</label>
              <p>{{ formatDate(selectedKey.expires_at) }}</p>
            </div>
            <div class="form-span-2">
              <label>{{ t('apiKeys.models') }}</label>
              <p>{{ selectedKey.model_allowlist.join(', ') }}</p>
            </div>
          </div>

          <section class="panel table-panel">
            <header class="panel-header">
              <div>
                <h2>{{ t('apiKeys.policyExplanation') }}</h2>
                <p>{{ detailPolicyExplanation?.selected_policy_name || t('policies.inherit') }} · {{ formatPolicySource(detailPolicyExplanation?.selected_source || '') }}</p>
              </div>
            </header>
            <div class="panel-body table-scroll">
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{{ t('policies.policy') }}</th>
                    <th>{{ t('traces.policySource') }}</th>
                    <th>{{ t('common.version') }}</th>
                    <th>{{ t('providers.status') }}</th>
                    <th>{{ t('apiKeys.policyReason') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="candidate in detailPolicyExplanation?.candidates || []" :key="`${candidate.source}-${candidate.policy_id}`">
                    <td>
                      <strong>{{ candidate.policy_name || candidate.policy_id || '-' }}</strong>
                      <span>{{ candidate.scope_type || '-' }} · {{ candidate.scope_id || '-' }}</span>
                    </td>
                    <td>{{ formatPolicySource(candidate.source) }}</td>
                    <td>v{{ candidate.policy_version || 0 }}</td>
                    <td>
                      <span class="pill" :class="candidate.selected ? 'status-success' : candidate.status === 'disabled' ? 'status-danger' : 'status-warning'">
                        {{ candidate.selected ? t('apiKeys.selectedPolicy') : candidate.status || '-' }}
                      </span>
                    </td>
                    <td>{{ candidate.reason || '-' }}</td>
                  </tr>
                  <tr v-if="!detailLoading && !(detailPolicyExplanation?.candidates || []).length">
                    <td colspan="5" class="empty-cell">{{ t('apiKeys.noPolicyExplanation') }}</td>
                  </tr>
                  <tr v-if="detailLoading">
                    <td colspan="5" class="empty-cell">{{ t('common.loading') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="metric-grid">
            <article class="metric-card">
              <div>
                <span>{{ t('usage.requests') }}</span>
                <strong>{{ detailLoading ? '-' : formatTokens(detailUsage?.total_requests || 0) }}</strong>
                <small>{{ t('apiKeys.filteredByKey') }}</small>
              </div>
            </article>
            <article class="metric-card">
              <div>
                <span>{{ t('usage.errors') }}</span>
                <strong>{{ detailLoading ? '-' : formatTokens(detailUsage?.error_requests || 0) }}</strong>
                <small>{{ t('traces.summary') }}</small>
              </div>
            </article>
            <article class="metric-card">
              <div>
                <span>{{ t('usage.tokens') }}</span>
                <strong>{{ detailLoading ? '-' : formatTokens(detailUsage?.total_tokens || 0) }}</strong>
                <small>{{ t('usage.totalTokens') }}</small>
              </div>
            </article>
            <article class="metric-card">
              <div>
                <span>{{ t('usage.cost') }}</span>
                <strong>{{ detailLoading ? '-' : formatUsageCostMicros(detailUsage?.total_usage_cost_micros || 0) }}</strong>
                <small>{{ t('usage.estimatedCost') }}</small>
              </div>
            </article>
          </section>

          <section class="panel table-panel">
            <header class="panel-header">
              <div>
                <h2>{{ t('apiKeys.recentUsage') }}</h2>
                <p>{{ t('apiKeys.recentUsageSubtitle') }}</p>
              </div>
            </header>
            <div class="panel-body table-scroll">
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{{ t('audit.time') }}</th>
                    <th>{{ t('usage.model') }}</th>
                    <th>{{ t('providers.status') }}</th>
                    <th>{{ t('usage.route') }}</th>
                    <th>{{ t('usage.tokens') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="record in detailUsage?.recent || []" :key="record.id">
                    <td>{{ formatDate(record.created_at) }}</td>
                    <td>{{ record.model }}</td>
                    <td><span class="pill" :class="statusClass(record.status)">{{ record.status }}</span></td>
                    <td>{{ record.provider_account_id || record.provider_id || '-' }}</td>
                    <td>{{ formatTokens(record.input_tokens + record.output_tokens) }}</td>
                  </tr>
                  <tr v-if="!detailLoading && !(detailUsage?.recent || []).length">
                    <td colspan="5" class="empty-cell">{{ t('usage.noData') }}</td>
                  </tr>
                  <tr v-if="detailLoading">
                    <td colspan="5" class="empty-cell">{{ t('common.loading') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section class="panel table-panel">
            <header class="panel-header">
              <div>
                <h2>{{ t('apiKeys.recentTraces') }}</h2>
                <p>{{ t('apiKeys.recentTracesSubtitle') }}</p>
              </div>
            </header>
            <div class="panel-body table-scroll">
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{{ t('audit.time') }}</th>
                    <th>{{ t('usage.model') }}</th>
                    <th>{{ t('providers.status') }}</th>
                    <th>{{ t('traces.http') }}</th>
                    <th>{{ t('traces.summary') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="trace in detailTraces" :key="trace.id">
                    <td>{{ formatDate(trace.created_at) }}</td>
                    <td>{{ trace.model }}</td>
                    <td><span class="pill" :class="statusClass(trace.status)">{{ trace.status }}</span></td>
                    <td>{{ trace.http_status || '-' }}</td>
                    <td>{{ trace.response_summary || trace.request_summary || '-' }}</td>
                  </tr>
                  <tr v-if="!detailLoading && !detailTraces.length">
                    <td colspan="5" class="empty-cell">{{ t('traces.empty') }}</td>
                  </tr>
                  <tr v-if="detailLoading">
                    <td colspan="5" class="empty-cell">{{ t('common.loading') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>
      </section>
    </div>
    <APIKeyRotationDialog
      :open="rotationTarget !== null"
      :key-name="rotationTarget?.name || ''"
      :saving="rotationSaving"
      @cancel="rotationTarget = null"
      @confirm="confirmRotation"
    />
  </main>
</template>
