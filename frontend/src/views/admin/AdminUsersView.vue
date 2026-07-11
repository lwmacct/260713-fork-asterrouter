<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, Trash2, UserRound, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  createRoleBinding,
  createWorkspaceUser,
  deleteRoleBinding,
  getRoleBindings,
  getWorkspaceUsers,
  updateWorkspaceUser
} from '@/api/control'
import type { RoleBinding, RoleBindingRequest, WorkspaceUser, WorkspaceUserRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const users = ref<WorkspaceUser[]>([])
const roleBindings = ref<RoleBinding[]>([])
const userModalOpen = ref(false)
const bindingModalOpen = ref(false)
const editingUser = ref<WorkspaceUser | null>(null)

const roleOptions = ['super_admin', 'platform_admin', 'project_admin', 'read_only_auditor', 'developer']

const userForm = reactive<WorkspaceUserRequest>({
  email: '',
  display_name: '',
  status: 'active',
  role: 'developer'
})

const bindingForm = reactive<RoleBindingRequest>({
  user_id: '',
  role: 'developer',
  scope_type: 'global',
  scope_id: ''
})

const filteredUsers = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return users.value.filter((user) => {
    if (statusFilter.value && user.status !== statusFilter.value) return false
    if (!keyword) return true
    return [user.email, user.display_name, user.role, user.status].some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: users.value.length,
  active: users.value.filter((item) => item.status === 'active').length,
  disabled: users.value.filter((item) => item.status === 'disabled').length,
  bindings: roleBindings.value.length
}))

function resetUserForm() {
  Object.assign(userForm, {
    email: '',
    display_name: '',
    status: 'active',
    role: 'developer'
  })
}

function openCreateUser() {
  editingUser.value = null
  resetUserForm()
  userModalOpen.value = true
}

function openEditUser(user: WorkspaceUser) {
  editingUser.value = user
  Object.assign(userForm, {
    email: user.email,
    display_name: user.display_name,
    status: user.status,
    role: user.role
  })
  userModalOpen.value = true
}

function closeUserModal() {
  userModalOpen.value = false
  editingUser.value = null
}

function openCreateBinding(user?: WorkspaceUser) {
  Object.assign(bindingForm, {
    user_id: user?.id || users.value[0]?.id || '',
    role: user?.role || 'developer',
    scope_type: 'global',
    scope_id: ''
  })
  bindingModalOpen.value = true
}

function closeBindingModal() {
  bindingModalOpen.value = false
}

function userLabel(userID: string): string {
  const user = users.value.find((item) => item.id === userID)
  return user ? `${user.display_name || user.email} · ${user.email}` : userID
}

function scopeLabel(binding: RoleBinding): string {
  if (binding.scope_type === 'global') return t('users.globalScope')
  return binding.scope_id || binding.scope_type
}

function statusClass(status: string): string {
  if (status === 'active') return 'status-success'
  return 'status-danger'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [userData, bindingData] = await Promise.all([getWorkspaceUsers(), getRoleBindings()])
    users.value = userData
    roleBindings.value = bindingData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function saveUser() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editingUser.value) {
      await updateWorkspaceUser(editingUser.value.id, userForm)
      message.value = t('users.updated')
    } else {
      await createWorkspaceUser(userForm)
      message.value = t('users.created')
    }
    closeUserModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function saveBinding() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await createRoleBinding({
      ...bindingForm,
      scope_id: bindingForm.scope_type === 'global' ? '' : bindingForm.scope_id
    })
    message.value = t('users.bindingCreated')
    closeBindingModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function revokeBinding(binding: RoleBinding) {
  if (!window.confirm(t('users.revokeConfirm'))) return
  error.value = ''
  message.value = ''
  try {
    await deleteRoleBinding(binding.id)
    message.value = t('users.bindingRevoked')
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
      <div>
        <h1>{{ t('admin.users') }}</h1>
        <p>{{ t('users.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreateUser">
        <Plus :size="17" />
        {{ t('users.newUser') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('users.total') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('users.active') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('users.disabled') }}</span>
      <span><strong>{{ summary.bindings }}</strong>{{ t('users.bindings') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('users.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
      <button class="button secondary" type="button" :disabled="!users.length" @click="openCreateBinding()">
        <ShieldCheck :size="17" />
        {{ t('users.grantRole') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <header class="panel-header">
        <div>
          <h2>{{ t('users.workspaceUsers') }}</h2>
          <p>{{ t('users.workspaceUsersSubtitle') }}</p>
        </div>
      </header>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('users.user') }}</th>
              <th>{{ t('users.role') }}</th>
              <th>{{ t('users.projects') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in filteredUsers" :key="user.id">
              <td>
                <strong>{{ user.display_name || user.email }}</strong>
                <span>{{ user.email }} · {{ user.id }}</span>
              </td>
              <td><span class="pill">{{ user.role }}</span></td>
              <td><span class="pill">{{ user.project_count }}</span></td>
              <td><span class="pill" :class="statusClass(user.status)">{{ user.status }}</span></td>
              <td>
                <div class="row-actions">
                  <button class="button secondary" type="button" @click="openEditUser(user)">
                    <Edit3 :size="15" />
                    {{ t('common.edit') }}
                  </button>
                  <button class="button secondary" type="button" @click="openCreateBinding(user)">
                    <ShieldCheck :size="15" />
                    {{ t('users.grantRole') }}
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredUsers.length">
              <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('users.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="panel table-panel section-gap">
      <header class="panel-header">
        <div>
          <h2>{{ t('users.roleBindings') }}</h2>
          <p>{{ t('users.roleBindingsSubtitle') }}</p>
        </div>
      </header>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('users.user') }}</th>
              <th>{{ t('users.role') }}</th>
              <th>{{ t('users.scope') }}</th>
              <th>{{ t('users.createdAt') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="binding in roleBindings" :key="binding.id">
              <td>
                <strong>{{ userLabel(binding.user_id) }}</strong>
                <span>{{ binding.user_id }}</span>
              </td>
              <td><span class="pill">{{ binding.role }}</span></td>
              <td>
                <strong>{{ binding.scope_type }}</strong>
                <span>{{ scopeLabel(binding) }}</span>
              </td>
              <td>{{ new Date(binding.created_at).toLocaleString() }}</td>
              <td>
                <button class="button danger" type="button" @click="revokeBinding(binding)">
                  <Trash2 :size="15" />
                  {{ t('users.revoke') }}
                </button>
              </td>
            </tr>
            <tr v-if="!roleBindings.length">
              <td colspan="5" class="empty-cell">{{ loading ? t('common.loading') : t('users.noBindings') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="userModalOpen" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="saveUser">
        <header class="modal-header">
          <div>
            <h2>{{ editingUser ? t('users.editUser') : t('users.newUser') }}</h2>
            <p>{{ t('users.userModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeUserModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label>{{ t('users.email') }}</label>
            <input v-model="userForm.email" type="email" required autocomplete="off" />
          </div>
          <div class="field">
            <label>{{ t('users.displayName') }}</label>
            <input v-model="userForm.display_name" autocomplete="off" />
          </div>
          <div class="field">
            <label>{{ t('users.defaultRole') }}</label>
            <select v-model="userForm.role">
              <option v-for="role in roleOptions" :key="role" :value="role">{{ role }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="userForm.status">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeUserModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>

    <div v-if="bindingModalOpen" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="saveBinding">
        <header class="modal-header">
          <div>
            <h2>{{ t('users.grantRole') }}</h2>
            <p>{{ t('users.bindingModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeBindingModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field form-span-2">
            <label>{{ t('users.user') }}</label>
            <select v-model="bindingForm.user_id" required>
              <option v-for="user in users" :key="user.id" :value="user.id">{{ user.display_name || user.email }} · {{ user.email }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('users.role') }}</label>
            <select v-model="bindingForm.role">
              <option v-for="role in roleOptions" :key="role" :value="role">{{ role }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('users.scope') }}</label>
            <input :value="t('users.globalScope')" disabled />
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeBindingModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <UserRound :size="17" />
            {{ saving ? t('common.saving') : t('users.grantRole') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>
