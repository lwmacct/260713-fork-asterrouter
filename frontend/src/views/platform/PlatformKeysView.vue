<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, KeyRound, Plus, RefreshCw, RotateCw, ShieldOff, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import APIKeyRotationDialog from '@/components/APIKeyRotationDialog.vue'
import { apiKeyLifecycleClass, apiKeyLifecycleLabelKey, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from '@/utils/apiKeys'
import { getGovernancePolicies } from '@/api/control'
import { createPlatformAPIKey, disablePlatformAPIKey, getGatewayPrincipals, getPlatformAPIKeys, getPlatformTenants, rotatePlatformAPIKey, updatePlatformAPIKey } from '@/api/platform'
import type { APIKeyCreateRequest, APIKeyRecord, GatewayPrincipal, GovernancePolicy, PlatformTenant } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const modalOpen = ref(false)
const editing = ref<APIKeyRecord | null>(null)
const keys = ref<APIKeyRecord[]>([])
const policies = ref<GovernancePolicy[]>([])
const tenants = ref<PlatformTenant[]>([])
const principals = ref<GatewayPrincipal[]>([])
const oneTimeKey = ref('')
const rotationTarget = ref<APIKeyRecord | null>(null)
const rotationSaving = ref(false)
const modelsText = ref('')
const status = ref('active')
const form = reactive<APIKeyCreateRequest>({
  name: '',
  policy_id: '',
  model_allowlist: [],
  qps_limit: 10,
  monthly_token_limit: 1_000_000,
  expires_at: '',
  key_type: 'workspace',
  platform_tenant_id: '',
  gateway_principal_id: ''
})

const activePolicies = computed(() => policies.value.filter((policy) => policy.status === 'active'))
const activeTenants = computed(() => tenants.value.filter((tenant) => tenant.status === 'active'))
const availablePrincipals = computed(() => principals.value.filter((principal) => principal.status === 'active' && principal.tenant_id === form.platform_tenant_id))
const summary = computed(() => ({
  total: keys.value.length,
  active: keys.value.filter((key) => apiKeyLifecycleStatus(key) === 'active').length,
  retiring: keys.value.filter((key) => apiKeyLifecycleStatus(key) === 'retiring').length,
  disabled: keys.value.filter((key) => ['disabled', 'retired'].includes(apiKeyLifecycleStatus(key))).length
}))

function splitModels(value: string): string[] {
  return value.split(/[\n,]/).map((model) => model.trim()).filter(Boolean)
}

function resetForm() {
	const tenantID = activeTenants.value[0]?.id || ''
  Object.assign(form, {
    name: '', policy_id: '', model_allowlist: [], qps_limit: 10, monthly_token_limit: 1_000_000, expires_at: '', key_type: 'workspace',
    platform_tenant_id: tenantID, gateway_principal_id: principals.value.find((principal) => principal.status === 'active' && principal.tenant_id === tenantID)?.id || ''
  })
  modelsText.value = ''
  status.value = 'active'
}

async function openCreate() {
  if (loading.value || activeTenants.value.length === 0 || principals.value.length === 0) {
    await load()
  }
  editing.value = null
  resetForm()
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
    expires_at: key.expires_at?.slice(0, 10) || '',
    key_type: key.key_type === 'service' ? 'service' : 'workspace',
    platform_tenant_id: key.platform_tenant_id,
    gateway_principal_id: key.gateway_principal_id
  })
  modelsText.value = key.model_allowlist.join('\n')
  status.value = key.status
  modalOpen.value = true
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [keyResult, policyResult, tenantResult, principalResult] = await Promise.allSettled([getPlatformAPIKeys(), getGovernancePolicies(), getPlatformTenants(), getGatewayPrincipals()])
    if (keyResult.status === 'rejected') throw keyResult.reason
    keys.value = keyResult.value
    policies.value = policyResult.status === 'fulfilled' ? policyResult.value : []
    if (tenantResult.status === 'rejected') throw tenantResult.reason
    if (principalResult.status === 'rejected') throw principalResult.reason
    tenants.value = tenantResult.value
    principals.value = principalResult.value
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
    const payload = { ...form, model_allowlist: splitModels(modelsText.value), key_type: form.key_type === 'service' ? 'service' : 'workspace' }
    if (!payload.platform_tenant_id || !payload.gateway_principal_id) {
      error.value = t('platform.keyOwnershipRequired')
      return
    }
    if (editing.value) {
      await updatePlatformAPIKey(editing.value.id, { ...payload, status: status.value })
      message.value = t('apiKeys.updated')
    } else {
      const created = await createPlatformAPIKey(payload)
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
    oneTimeKey.value = (await rotatePlatformAPIKey(rotationTarget.value.id, gracePeriodSeconds)).key
    message.value = t('apiKeys.rotated')
    rotationTarget.value = null
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    rotationSaving.value = false
  }
}

async function disable(key: APIKeyRecord) {
  error.value = ''
  try {
    await disablePlatformAPIKey(key.id)
    message.value = t('apiKeys.disabled')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function changeTenant() {
  if (!availablePrincipals.value.some((principal) => principal.id === form.gateway_principal_id)) {
    form.gateway_principal_id = availablePrincipals.value[0]?.id || ''
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.apiKeys') }}</h1>
        <p>{{ t('platform.keySubtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('apiKeys.newKey') }}</button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('apiKeys.keys') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.retiring }}</strong>{{ t('apiKeys.retiring') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
    </div>
    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="oneTimeKey" class="notice success"><strong>{{ t('apiKeys.oneTime') }}</strong><input :value="oneTimeKey" readonly /></div>

    <section class="panel table-panel content-fit">
      <div class="panel-header">
        <KeyRound :size="18" />
        <h2>{{ t('platform.apiCredentials') }}</h2>
        <button class="icon-button" type="button" :title="t('common.refresh')" :disabled="loading" @click="load"><RefreshCw :size="17" /></button>
      </div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('apiKeys.name') }}</th><th>{{ t('apiKeys.keyType') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('apiKeys.models') }}</th><th>{{ t('apiKeys.lastUsed') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="key in keys" :key="key.id">
              <td><strong>{{ key.name }}</strong><span>{{ key.fingerprint }}</span><span>{{ key.platform_tenant_id }}</span></td>
              <td>{{ key.key_type }}</td>
              <td><span class="pill" :class="apiKeyLifecycleClass(key)">{{ t(apiKeyLifecycleLabelKey(key)) }}</span></td>
              <td><div class="chip-list"><span v-for="model in key.model_allowlist.slice(0, 3)" :key="model" class="pill">{{ model }}</span><span v-if="key.model_allowlist.length > 3" class="pill">+{{ key.model_allowlist.length - 3 }}</span></div></td>
              <td>{{ formatDate(key.last_used_at) }}</td>
              <td><div class="row-actions"><button class="button secondary" type="button" :disabled="!canRotateAPIKey(key)" @click="openEdit(key)"><Edit3 :size="15" />{{ t('common.edit') }}</button><button class="button secondary" type="button" :disabled="!canRotateAPIKey(key)" @click="openRotation(key)"><RotateCw :size="15" />{{ t('apiKeys.rotate') }}</button><button v-if="canDisableAPIKey(key)" class="button danger" type="button" @click="disable(key)"><ShieldOff :size="15" />{{ t('apiKeys.disable') }}</button></div></td>
            </tr>
            <tr v-if="!keys.length"><td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('apiKeys.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="modalOpen = false">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-label="t('apiKeys.newKey')">
        <header class="modal-header"><div><h2>{{ editing ? t('apiKeys.editKey') : t('apiKeys.newKey') }}</h2><p>{{ t('platform.keyModalSubtitle') }}</p></div><button class="icon-button" type="button" :title="t('common.close')" @click="modalOpen = false"><X :size="19" /></button></header>
        <div class="modal-body form-grid">
          <div class="field form-span-2"><label for="platform-key-name">{{ t('apiKeys.name') }}</label><input id="platform-key-name" v-model="form.name" /></div>
          <div class="field"><label for="platform-key-type">{{ t('apiKeys.keyType') }}</label><select id="platform-key-type" v-model="form.key_type"><option value="workspace">workspace</option><option value="service">service</option></select></div>
          <div class="field"><label for="platform-key-status">{{ t('providers.status') }}</label><select id="platform-key-status" v-model="status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field"><label for="platform-key-tenant">{{ t('platform.tenant') }}</label><select id="platform-key-tenant" v-model="form.platform_tenant_id" @change="changeTenant"><option value="" disabled>{{ t('platform.tenant') }}</option><option v-for="tenant in activeTenants" :key="tenant.id" :value="tenant.id">{{ tenant.name }}</option></select></div>
          <div class="field"><label for="platform-key-principal">{{ t('platform.principal') }}</label><select id="platform-key-principal" v-model="form.gateway_principal_id"><option value="" disabled>{{ t('platform.principal') }}</option><option v-for="principal in availablePrincipals" :key="principal.id" :value="principal.id">{{ principal.name }} ({{ principal.principal_type }})</option></select></div>
          <div class="field form-span-2"><label for="platform-key-policy">{{ t('policies.policy') }}</label><select id="platform-key-policy" v-model="form.policy_id"><option value="">{{ t('policies.inherit') }}</option><option v-for="policy in activePolicies" :key="policy.id" :value="policy.id">{{ policy.name }}</option></select></div>
          <div class="field form-span-2"><label for="platform-key-models">{{ t('apiKeys.models') }}</label><textarea id="platform-key-models" v-model="modelsText" rows="3" /></div>
          <div class="field"><label for="platform-key-qps">{{ t('apiKeys.qps') }}</label><input id="platform-key-qps" v-model.number="form.qps_limit" type="number" min="0" /></div>
          <div class="field"><label for="platform-key-monthly-tokens">{{ t('apiKeys.monthlyTokens') }}</label><input id="platform-key-monthly-tokens" v-model.number="form.monthly_token_limit" type="number" min="0" /></div>
          <div class="field"><label for="platform-key-expires-at">{{ t('apiKeys.expiresAt') }}</label><input id="platform-key-expires-at" v-model="form.expires_at" type="date" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="modalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="button" :disabled="saving" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button></footer>
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
