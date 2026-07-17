<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import GatewayModelPicker from '@/components/model/GatewayModelPicker.vue'
import { createGovernancePolicy, getGatewayModels, getGovernancePolicies, updateGovernancePolicy } from '@/api/control'
import type { GatewayModel, GovernancePolicy, GovernancePolicyRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const scopeFilter = ref('')
const modalOpen = ref(false)
const editing = ref<GovernancePolicy | null>(null)
const policies = ref<GovernancePolicy[]>([])
const gatewayModels = ref<GatewayModel[]>([])

const form = reactive<GovernancePolicyRequest>({
  name: '',
  description: '',
  scope_type: 'global',
  scope_id: '',
  model_allowlist: [],
  model_denylist: [],
  qps_limit: 0,
  monthly_token_limit: 0,
  monthly_budget_micros: 0,
  overage_action: 'block',
  prompt_logging_mode: 'metadata_only',
  retention_days: 30,
  tool_call_allowed: true,
  image_input_allowed: true,
  web_access_allowed: false,
  status: 'active'
})

const filteredPolicies = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return policies.value.filter((policy) => {
    if (statusFilter.value && policy.status !== statusFilter.value) return false
    if (scopeFilter.value && policy.scope_type !== scopeFilter.value) return false
    if (!keyword) return true
    return [policy.name, policy.description, policy.scope_type, policy.scope_id, policy.model_allowlist.join(' '), policy.model_denylist.join(' ')].some((value) =>
      value.toLowerCase().includes(keyword)
    )
  })
})

const summary = computed(() => ({
  total: policies.value.length,
  active: policies.value.filter((item) => item.status === 'active').length,
  disabled: policies.value.filter((item) => item.status === 'disabled').length,
  scoped: policies.value.filter((item) => item.scope_type !== 'global').length
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    description: '',
    scope_type: 'global',
    scope_id: '',
    model_allowlist: [],
    model_denylist: [],
    qps_limit: 0,
    monthly_token_limit: 0,
    monthly_budget_micros: 0,
    overage_action: 'block',
    prompt_logging_mode: 'metadata_only',
    retention_days: 30,
    tool_call_allowed: true,
    image_input_allowed: true,
    web_access_allowed: false,
    status: 'active'
  })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(policy: GovernancePolicy) {
  editing.value = policy
  Object.assign(form, {
    name: policy.name,
    description: policy.description,
    scope_type: policy.scope_type,
    scope_id: policy.scope_id,
    model_allowlist: [...policy.model_allowlist],
    model_denylist: [...policy.model_denylist],
    qps_limit: policy.qps_limit,
    monthly_token_limit: policy.monthly_token_limit,
    monthly_budget_micros: policy.monthly_budget_micros,
    overage_action: policy.overage_action,
    prompt_logging_mode: policy.prompt_logging_mode,
    retention_days: policy.retention_days,
    tool_call_allowed: policy.tool_call_allowed,
    image_input_allowed: policy.image_input_allowed,
    web_access_allowed: policy.web_access_allowed,
    status: policy.status
  })
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

function formatBudget(micros: number): string {
  return micros
    ? new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(micros / 1_000_000)
    : t('apiKeys.unlimited')
}

function statusClass(status: string): string {
  return status === 'active' ? 'status-success' : 'status-danger'
}

function formatDate(value: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [policyList, modelList] = await Promise.all([getGovernancePolicies(), getGatewayModels()])
    policies.value = policyList
    gatewayModels.value = modelList
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
    const payload: GovernancePolicyRequest = {
      ...form,
      scope_id: form.scope_type === 'global' ? '' : form.scope_id.trim(),
      model_allowlist: [...form.model_allowlist],
      model_denylist: [...form.model_denylist]
    }
    if (editing.value) {
      await updateGovernancePolicy(editing.value.id, payload)
      message.value = t('policies.updated')
    } else {
      await createGovernancePolicy(payload)
      message.value = t('policies.created')
    }
    closeModal()
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
        <h1>{{ t('admin.policies') }}</h1>
        <p>{{ t('policies.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('policies.newPolicy') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('policies.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
      <span><strong>{{ summary.scoped }}</strong>{{ t('policies.scoped') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('policies.searchPlaceholder')" />
      </label>
      <select v-model="scopeFilter">
        <option value="">{{ t('policies.allScopes') }}</option>
        <option value="global">global</option>
        <option value="api_key">api_key</option>
      </select>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('policies.policy') }}</th>
              <th>{{ t('policies.scope') }}</th>
              <th>{{ t('policies.limits') }}</th>
              <th>{{ t('policies.modelRules') }}</th>
              <th>{{ t('common.version') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="policy in filteredPolicies" :key="policy.id">
              <td>
                <strong>{{ policy.name }}</strong>
                <span>{{ policy.description || policy.id }}</span>
              </td>
              <td>
                <strong>{{ policy.scope_type }}</strong>
                <span>{{ policy.scope_id || '-' }}</span>
              </td>
              <td>
                <strong>{{ formatBudget(policy.monthly_budget_micros) }}</strong>
                <span>{{ policy.qps_limit || '-' }} QPS · {{ policy.monthly_token_limit || '-' }} tokens</span>
              </td>
              <td>
                <strong>{{ policy.overage_action }} · {{ policy.prompt_logging_mode }}</strong>
                <span>{{ policy.model_allowlist.length }} allow · {{ policy.model_denylist.length }} deny</span>
              </td>
              <td>
                <strong>v{{ policy.version || 1 }}</strong>
                <span>{{ policy.last_updated_by || '-' }} · {{ formatDate(policy.updated_at) }}</span>
              </td>
              <td><span class="pill" :class="statusClass(policy.status)">{{ policy.status }}</span></td>
              <td>
                <button class="button secondary" type="button" @click="openEdit(policy)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredPolicies.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('policies.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('policies.editPolicy') : t('policies.newPolicy') }}</h2>
            <p>{{ t('policies.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="18" />
          </button>
        </header>

        <div class="modal-body form-grid">
          <label>
            <span>{{ t('policies.name') }}</span>
            <input v-model="form.name" required />
          </label>
          <label>
            <span>{{ t('providers.status') }}</span>
            <select v-model="form.status">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </label>
          <label class="form-span-2">
            <span>{{ t('policies.description') }}</span>
            <input v-model="form.description" />
          </label>
          <label>
            <span>{{ t('policies.scopeType') }}</span>
            <select v-model="form.scope_type">
              <option value="global">global</option>
              <option value="api_key">api_key</option>
            </select>
          </label>
          <label>
            <span>{{ t('policies.scopeId') }}</span>
            <input v-model="form.scope_id" :disabled="form.scope_type === 'global'" />
          </label>
          <label>
            <span>QPS</span>
            <input v-model.number="form.qps_limit" type="number" min="0" />
          </label>
          <label>
            <span>{{ t('policies.monthlyTokens') }}</span>
            <input v-model.number="form.monthly_token_limit" type="number" min="0" />
          </label>
          <label>
            <span>{{ t('policies.monthlyBudget') }}</span>
            <input v-model.number="form.monthly_budget_micros" type="number" min="0" />
          </label>
          <label>
            <span>{{ t('policies.retentionDays') }}</span>
            <input v-model.number="form.retention_days" type="number" min="0" />
          </label>
          <label>
            <span>{{ t('policies.overageAction') }}</span>
            <select v-model="form.overage_action">
              <option value="block">block</option>
              <option value="warn">warn</option>
              <option value="fallback">fallback</option>
            </select>
          </label>
          <label>
            <span>{{ t('policies.promptLoggingMode') }}</span>
            <select v-model="form.prompt_logging_mode">
              <option value="disabled">disabled</option>
              <option value="metadata_only">metadata_only</option>
              <option value="redacted">redacted</option>
            </select>
          </label>
          <div class="field form-span-2">
            <span>{{ t('policies.allowlist') }}</span>
            <GatewayModelPicker v-model="form.model_allowlist" :models="gatewayModels" :disabled="saving" :aria-label="t('policies.allowlist')" />
          </div>
          <div class="field form-span-2">
            <span>{{ t('policies.denylist') }}</span>
            <GatewayModelPicker v-model="form.model_denylist" :models="gatewayModels" :disabled="saving" :aria-label="t('policies.denylist')" />
          </div>
          <label class="checkbox-label">
            <input v-model="form.tool_call_allowed" type="checkbox" />
            <span>{{ t('policies.toolCallAllowed') }}</span>
          </label>
          <label class="checkbox-label">
            <input v-model="form.image_input_allowed" type="checkbox" />
            <span>{{ t('policies.imageInputAllowed') }}</span>
          </label>
          <label class="checkbox-label">
            <input v-model="form.web_access_allowed" type="checkbox" />
            <span>{{ t('policies.webAccessAllowed') }}</span>
          </label>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="16" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>
