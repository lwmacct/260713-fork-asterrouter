<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, KeyRound, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import GatewayModelPicker from '@/components/model/GatewayModelPicker.vue'
import { getGatewayModels } from '@/api/control'
import { createOperatorCustomerKey, createOperatorResource, listOperatorResource, updateOperatorResource } from '@/api/operator'
import type { GatewayModel, OperatorCustomer, OperatorCustomerGroup, OperatorPlan } from '@/types'

const { t } = useI18n()
const items = ref<OperatorCustomer[]>([])
const groups = ref<OperatorCustomerGroup[]>([])
const plans = ref<OperatorPlan[]>([])
const gatewayModels = ref<GatewayModel[]>([])
const query = ref('')
const error = ref('')
const message = ref('')
const modal = ref(false)
const keyModal = ref(false)
const editing = ref<OperatorCustomer | null>(null)
const keyCustomer = ref<OperatorCustomer | null>(null)
const createdKey = ref('')

const form = reactive({ name: '', email: '', group_id: '', plan_id: '', status: 'active', credit_micros: 0, notes: '' })
const keyForm = reactive({
  name: '',
  policy_id: '',
  model_allowlist: [] as string[],
  qps_limit: 0,
  monthly_token_limit: 0,
  expires_at: '',
  key_type: 'customer',
  customer_id: ''
})

const filtered = computed(() => items.value.filter((item) => !query.value || `${item.name} ${item.email}`.toLowerCase().includes(query.value.toLowerCase())))
const defaultGatewayModel = computed(() => gatewayModels.value.find((item) => item.status === 'active')?.model_id || '')
const groupName = (id: string) => groups.value.find((item) => item.id === id)?.name || '-'
const planName = (id: string) => plans.value.find((item) => item.id === id)?.name || '-'

async function load() {
  [items.value, groups.value, plans.value, gatewayModels.value] = await Promise.all([
    listOperatorResource('customers') as Promise<OperatorCustomer[]>,
    listOperatorResource('customer-groups') as Promise<OperatorCustomerGroup[]>,
    listOperatorResource('plans') as Promise<OperatorPlan[]>,
    getGatewayModels()
  ])
}

function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', email: '', group_id: '', plan_id: '', status: 'active', credit_micros: 0, notes: '' })
  modal.value = true
}

function openEdit(item: OperatorCustomer) {
  editing.value = item
  Object.assign(form, item)
  modal.value = true
}

async function save() {
  try {
    if (editing.value) await updateOperatorResource('customers', editing.value.id, { ...form })
    else await createOperatorResource('customers', { ...form })
    modal.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function openKey(item: OperatorCustomer) {
  keyCustomer.value = item
  createdKey.value = ''
  keyForm.name = `${item.name} key`
  keyForm.customer_id = item.id
  keyForm.model_allowlist = defaultGatewayModel.value ? [defaultGatewayModel.value] : []
  keyModal.value = true
}

async function createKey() {
  if (!keyCustomer.value || !keyForm.model_allowlist.length) return
  try {
    const result = await createOperatorCustomerKey(keyCustomer.value.id, { ...keyForm })
    createdKey.value = result.key
    message.value = t('operatorDomain.keyCreated')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>
<template>
  <main class="content crud-page">
    <section class="page-header">
      <div><h1>{{ t('operatorDomain.customers') }}</h1><p>{{ t('operatorDomain.customersHelp') }}</p></div>
      <button class="button" @click="openCreate"><Plus :size="17" />{{ t('operatorDomain.newCustomer') }}</button>
    </section>
    <section class="table-toolbar">
      <label class="search-box"><Search :size="17" /><input v-model="query" :placeholder="t('operatorCrud.search')" /></label>
      <button class="button secondary" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>
    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead><tr><th>{{ t('operatorDomain.customer') }}</th><th>{{ t('operatorDomain.group') }}</th><th>{{ t('operatorDomain.plan') }}</th><th>{{ t('operatorDomain.balance') }}</th><th>{{ t('operatorDomain.credit') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="item in filtered" :key="item.id">
              <td><strong>{{ item.name }}</strong><span>{{ item.email || item.id }}</span></td>
              <td>{{ groupName(item.group_id) }}</td><td>{{ planName(item.plan_id) }}</td>
              <td>{{ (item.balance_micros / 1_000_000).toFixed(6) }}</td><td>{{ (item.credit_micros / 1_000_000).toFixed(6) }}</td>
              <td><span class="pill" :class="item.status === 'active' ? 'status-success' : 'status-warning'">{{ item.status }}</span></td>
              <td class="table-actions"><button class="icon-button" :title="t('common.edit')" @click="openEdit(item)"><Edit3 :size="16" /></button><button class="icon-button" :title="t('operatorDomain.createKey')" @click="openKey(item)"><KeyRound :size="16" /></button></td>
            </tr>
            <tr v-if="!filtered.length"><td colspan="7" class="empty-cell"></td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modal" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-header"><h2>{{ editing ? t('common.edit') : t('operatorDomain.newCustomer') }}</h2><button class="icon-button" type="button" @click="modal = false"><X :size="18" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label>{{ t('operatorDomain.name') }}</label><input v-model="form.name" required /></div>
          <div class="field"><label>Email</label><input v-model="form.email" type="email" /></div>
          <div class="field"><label>{{ t('operatorDomain.group') }}</label><select v-model="form.group_id"><option value="">-</option><option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option></select></div>
          <div class="field"><label>{{ t('operatorDomain.plan') }}</label><select v-model="form.plan_id"><option value="">-</option><option v-for="item in plans" :key="item.id" :value="item.id">{{ item.name }}</option></select></div>
          <div class="field"><label>{{ t('operatorDomain.credit') }}</label><input v-model.number="form.credit_micros" type="number" min="0" /></div>
          <div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div>
          <div class="field field-wide"><label>{{ t('operatorDomain.notes') }}</label><textarea v-model="form.notes" rows="3" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="modal = false">{{ t('common.cancel') }}</button><button class="button" type="submit"><Save :size="17" />{{ t('common.save') }}</button></footer>
      </form>
    </div>

    <div v-if="keyModal" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="createKey">
        <header class="modal-header"><h2>{{ t('operatorDomain.createKey') }}</h2><button class="icon-button" type="button" @click="keyModal = false"><X :size="18" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label>{{ t('apiKeys.name') }}</label><input v-model="keyForm.name" required /></div>
          <div class="field field-wide"><label>{{ t('apiKeys.models') }}</label><GatewayModelPicker v-model="keyForm.model_allowlist" :models="gatewayModels" /></div>
          <div class="field"><label>QPS</label><input v-model.number="keyForm.qps_limit" type="number" min="0" /></div>
          <div v-if="createdKey" class="field field-wide"><label>{{ t('operatorDomain.createdKey') }}</label><code>{{ createdKey }}</code></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="keyModal = false">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="!keyForm.model_allowlist.length"><KeyRound :size="17" />{{ t('operatorDomain.createKey') }}</button></footer>
      </form>
    </div>
  </main>
</template>
