<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Building2, Edit3, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createDepartment, getDepartments, updateDepartment } from '@/api/control'
import { isNotFoundError } from '@/api/client'
import type { Department, DepartmentRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const departments = ref<Department[]>([])
const modalOpen = ref(false)
const editingDepartment = ref<Department | null>(null)

const departmentForm = reactive<DepartmentRequest>({
  name: '',
  code: '',
  parent_id: '',
  cost_center: '',
  monthly_budget_micros: 0,
  status: 'active'
})

const parentOptions = computed(() => {
  return departments.value.filter((department) => department.id !== editingDepartment.value?.id && department.status === 'active')
})

const filteredDepartments = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return departments.value.filter((department) => {
    if (statusFilter.value && department.status !== statusFilter.value) return false
    if (!keyword) return true
    return [department.name, department.code, department.cost_center, department.status].some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: departments.value.length,
  active: departments.value.filter((item) => item.status === 'active').length,
  archived: departments.value.filter((item) => item.status === 'archived').length,
  costCenters: new Set(departments.value.map((item) => item.cost_center).filter(Boolean)).size
}))

function resetForm() {
  Object.assign(departmentForm, {
    name: '',
    code: '',
    parent_id: '',
    cost_center: '',
    monthly_budget_micros: 0,
    status: 'active'
  })
}

function openCreateModal() {
  editingDepartment.value = null
  resetForm()
  modalOpen.value = true
}

function openEditModal(department: Department) {
  editingDepartment.value = department
  Object.assign(departmentForm, {
    name: department.name,
    code: department.code,
    parent_id: department.parent_id,
    cost_center: department.cost_center,
    monthly_budget_micros: department.monthly_budget_micros,
    status: department.status
  })
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editingDepartment.value = null
}

function parentLabel(parentID: string): string {
  if (!parentID) return t('departments.root')
  const parent = departments.value.find((department) => department.id === parentID)
  return parent ? `${parent.name} · ${parent.code}` : parentID
}

function formatCost(micros: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2
  }).format(micros / 1_000_000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    departments.value = await getDepartments()
  } catch (err) {
    if (isNotFoundError(err)) {
      departments.value = []
      return
    }
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function saveDepartment() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editingDepartment.value) {
      await updateDepartment(editingDepartment.value.id, { ...departmentForm })
      message.value = t('departments.updated')
    } else {
      await createDepartment({ ...departmentForm })
      message.value = t('departments.created')
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
        <h1>{{ t('admin.departments') }}</h1>
        <p>{{ t('departments.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreateModal">
        <Plus :size="17" />
        {{ t('departments.newDepartment') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('departments.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('departments.active') }}</span>
      <span><strong>{{ summary.archived }}</strong>{{ t('departments.archived') }}</span>
      <span><strong>{{ summary.costCenters }}</strong>{{ t('departments.costCenters') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('departments.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="archived">archived</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <header class="panel-header">
        <div>
          <h2>{{ t('departments.departmentList') }}</h2>
          <p>{{ t('departments.listSubtitle') }}</p>
        </div>
      </header>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('departments.department') }}</th>
              <th>{{ t('departments.parent') }}</th>
              <th>{{ t('departments.costCenter') }}</th>
              <th>{{ t('departments.monthlyBudget') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="department in filteredDepartments" :key="department.id">
              <td>
                <strong>{{ department.name }}</strong>
                <span>{{ department.code }} · {{ department.id }}</span>
              </td>
              <td>{{ parentLabel(department.parent_id) }}</td>
              <td>{{ department.cost_center || '-' }}</td>
              <td>{{ department.monthly_budget_micros ? formatCost(department.monthly_budget_micros) : t('apiKeys.unlimited') }}</td>
              <td><span class="pill" :class="department.status === 'active' ? 'status-success' : 'status-warning'">{{ department.status }}</span></td>
              <td>
                <button class="button secondary" type="button" @click="openEditModal(department)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredDepartments.length">
              <td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('departments.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <form class="modal-card" @submit.prevent="saveDepartment">
        <header class="modal-header">
          <div>
            <h2>{{ editingDepartment ? t('departments.editDepartment') : t('departments.newDepartment') }}</h2>
            <p>{{ t('departments.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label>{{ t('departments.name') }}</label>
            <input v-model="departmentForm.name" required autocomplete="off" />
          </div>
          <div class="field">
            <label>{{ t('departments.code') }}</label>
            <input v-model="departmentForm.code" required autocomplete="off" />
          </div>
          <div class="field">
            <label>{{ t('departments.parent') }}</label>
            <select v-model="departmentForm.parent_id">
              <option value="">{{ t('departments.root') }}</option>
              <option v-for="department in parentOptions" :key="department.id" :value="department.id">{{ department.name }} · {{ department.code }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('departments.costCenter') }}</label>
            <input v-model="departmentForm.cost_center" autocomplete="off" />
          </div>
          <div class="field">
            <label>{{ t('departments.monthlyBudget') }}</label>
            <input v-model.number="departmentForm.monthly_budget_micros" type="number" min="0" step="100" />
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="departmentForm.status">
              <option value="active">active</option>
              <option value="archived">archived</option>
            </select>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>
