<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Trash2, UsersRound, X } from '@lucide/vue'
import { createOrganizationGroup, deleteOrganizationGroup, getOrganizationGroups, getWorkspaceUsers, updateOrganizationGroup } from '@/api/control'
import type { OrganizationGroup, OrganizationGroupRequest, WorkspaceUser } from '@/types'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const groups = ref<OrganizationGroup[]>([])
const users = ref<WorkspaceUser[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const modalOpen = ref(false)
const editing = ref<OrganizationGroup | null>(null)
const form = reactive<OrganizationGroupRequest>({ name: '', description: '', status: 'active', member_ids: [] })
const memberNames = computed(() => Object.fromEntries(users.value.map(user => [user.id, user.display_name || user.email])))

async function load() {
	loading.value = true
	error.value = ''
	try {
		;[groups.value, users.value] = await Promise.all([getOrganizationGroups(), getWorkspaceUsers()])
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		loading.value = false
	}
}

function openCreate() {
	editing.value = null
	Object.assign(form, { name: '', description: '', status: 'active', member_ids: [] })
	modalOpen.value = true
}

function openEdit(group: OrganizationGroup) {
	editing.value = group
	Object.assign(form, { name: group.name, description: group.description, status: group.status, member_ids: [...group.member_ids] })
	modalOpen.value = true
}

function toggleMember(userID: string) {
	form.member_ids = form.member_ids.includes(userID) ? form.member_ids.filter(id => id !== userID) : [...form.member_ids, userID]
}

async function save() {
	saving.value = true
	error.value = ''
	try {
		if (editing.value) await updateOrganizationGroup(editing.value.id, { ...form })
		else await createOrganizationGroup({ ...form })
		modalOpen.value = false
		await load()
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		saving.value = false
	}
}

async function remove(group: OrganizationGroup) {
	if (!window.confirm(t('organizationGroups.deleteConfirm', { name: group.name }))) return
	try {
		await deleteOrganizationGroup(group.id)
		await load()
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	}
}

onMounted(load)
</script>

<template>
	<main class="content crud-page">
		<section class="page-header"><div><h1>{{ t('organizationGroups.title') }}</h1><p>{{ t('organizationGroups.subtitle') }}</p></div><button class="button" @click="openCreate"><Plus :size="17" />{{ t('organizationGroups.create') }}</button></section>
		<section class="table-toolbar"><button class="button secondary" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button></section>
		<div v-if="error" class="notice">{{ error }}</div>
		<section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table"><thead><tr><th>{{ t('organizationGroups.name') }}</th><th>{{ t('organizationGroups.members') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="group in groups" :key="group.id"><td><strong>{{ group.name }}</strong><span>{{ group.description || group.id }}</span></td><td><strong>{{ group.member_ids.length }}</strong><span>{{ group.member_ids.slice(0, 3).map(id => memberNames[id] || id).join(', ') }}</span></td><td><span class="pill" :class="group.status === 'active' ? 'status-success' : 'status-warning'">{{ group.status }}</span></td><td class="table-actions"><button class="icon-button" :title="t('common.edit')" @click="openEdit(group)"><Edit3 :size="16" /></button><button class="icon-button danger" :title="t('operatorCrud.delete')" @click="remove(group)"><Trash2 :size="16" /></button></td></tr><tr v-if="!groups.length"><td colspan="4" class="empty-cell"></td></tr></tbody></table></div></section>
		<div v-if="modalOpen" class="modal-backdrop"><form class="modal-card" @submit.prevent="save"><header class="modal-header"><h2>{{ editing ? t('common.edit') : t('organizationGroups.create') }}</h2><button class="icon-button" type="button" @click="modalOpen = false"><X :size="18" /></button></header><div class="modal-body form-grid"><div class="field"><label>{{ t('organizationGroups.name') }}</label><input v-model="form.name" required /></div><div class="field"><label>{{ t('providers.status') }}</label><select v-model="form.status"><option value="active">active</option><option value="disabled">disabled</option></select></div><div class="field field-wide"><label>{{ t('operatorDomain.description') }}</label><textarea v-model="form.description" rows="3" /></div><div class="field field-wide"><label>{{ t('organizationGroups.members') }}</label><div class="group-member-list"><label v-for="user in users" :key="user.id"><input type="checkbox" :checked="form.member_ids.includes(user.id)" @change="toggleMember(user.id)" /><span><strong>{{ user.display_name || user.email }}</strong><small>{{ user.email }}</small></span></label></div></div></div><footer class="modal-footer"><button class="button secondary" type="button" @click="modalOpen = false">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer></form></div>
	</main>
</template>

<style scoped>
.group-member-list { display: grid; max-height: 280px; overflow: auto; border: 1px solid var(--border); border-radius: 8px; }
.group-member-list label { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 10px; padding: 10px 12px; border-top: 1px solid var(--border); cursor: pointer; }
.group-member-list label:first-child { border-top: 0; }
.group-member-list span { display: grid; gap: 2px; min-width: 0; }
.group-member-list small { color: var(--text-muted); overflow-wrap: anywhere; }
</style>
