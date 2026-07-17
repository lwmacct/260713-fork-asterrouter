<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Edit3, Image, Plus, RefreshCw, Save, Search, ShieldCheck, Video, X, Zap } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createRoutingGroup, getRoutingGroups, updateRoutingGroup } from '@/api/control'
import type { RoutingGroup, RoutingGroupRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const groups = ref<RoutingGroup[]>([])
const query = ref('')
const statusFilter = ref('')
const platformFilter = ref('')
const typeFilter = ref('')
const visibilityFilter = ref('')
const modalOpen = ref(false)
const editing = ref<RoutingGroup | null>(null)

const groupTypes = ['standard', 'subscription', 'exclusive', 'image_generation', 'video_generation']

const form = reactive<RoutingGroupRequest>(defaultForm())

const platforms = computed(() => Array.from(new Set(groups.value.map((item) => item.platform))).filter(Boolean).sort())

const filteredGroups = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return groups.value.filter((group) => {
    if (statusFilter.value && group.status !== statusFilter.value) return false
    if (platformFilter.value && group.platform !== platformFilter.value) return false
    if (typeFilter.value && normalizedGroupType(group) !== typeFilter.value) return false
    if (visibilityFilter.value === 'exclusive' && !group.is_exclusive) return false
    if (visibilityFilter.value === 'public' && group.is_exclusive) return false
    if (!keyword) return true
    return [group.name, group.description, group.platform, normalizedGroupType(group)]
      .some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: groups.value.length,
  active: groups.value.filter((item) => item.status === 'active').length,
  exclusive: groups.value.filter((item) => item.is_exclusive).length,
  media: groups.value.filter((item) => item.image_enabled || item.video_enabled).length,
  accounts: groups.value.reduce((total, item) => total + item.account_count, 0),
  schedulable: groups.value.reduce((total, item) => total + item.active_account_count, 0)
}))

function defaultForm(): RoutingGroupRequest {
  return {
    name: '',
    description: '',
    platform: 'openai_compatible',
    group_type: 'standard',
    rate_multiplier: 1,
    rpm_limit: 0,
    is_exclusive: false,
    daily_budget_micros: 0,
    weekly_budget_micros: 0,
    monthly_budget_micros: 0,
    image_enabled: false,
    batch_image_enabled: false,
    image_rate_multiplier: 1,
    batch_image_discount_multiplier: 1,
    image_price_1k_cents: 0,
    image_price_2k_cents: 0,
    image_price_4k_cents: 0,
    video_enabled: false,
    video_rate_multiplier: 1,
    video_price_480p_cents: 0,
    video_price_720p_cents: 0,
    video_price_1080p_cents: 0,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    status: 'active',
    sort_order: 100
  }
}

function resetForm() {
  Object.assign(form, defaultForm())
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(group: RoutingGroup) {
  editing.value = group
  Object.assign(form, {
    name: group.name,
    description: group.description,
    platform: group.platform,
    group_type: normalizedGroupType(group),
    rate_multiplier: group.rate_multiplier,
    rpm_limit: group.rpm_limit,
    is_exclusive: group.is_exclusive,
    daily_budget_micros: group.daily_budget_micros,
    weekly_budget_micros: group.weekly_budget_micros,
    monthly_budget_micros: group.monthly_budget_micros,
    image_enabled: group.image_enabled,
    batch_image_enabled: group.batch_image_enabled,
    image_rate_multiplier: group.image_rate_multiplier || 1,
    batch_image_discount_multiplier: group.batch_image_discount_multiplier || 1,
    image_price_1k_cents: group.image_price_1k_cents,
    image_price_2k_cents: group.image_price_2k_cents,
    image_price_4k_cents: group.image_price_4k_cents,
    video_enabled: group.video_enabled,
    video_rate_multiplier: group.video_rate_multiplier || 1,
    video_price_480p_cents: group.video_price_480p_cents,
    video_price_720p_cents: group.video_price_720p_cents,
    video_price_1080p_cents: group.video_price_1080p_cents,
    peak_rate_enabled: group.peak_rate_enabled,
    peak_start: group.peak_start,
    peak_end: group.peak_end,
    peak_rate_multiplier: group.peak_rate_multiplier || 1,
    status: group.status,
    sort_order: group.sort_order
  })
  applyTypeDefaults(form.group_type)
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    groups.value = await getRoutingGroups()
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
    const payload = normalizedPayload()
    if (editing.value) {
      await updateRoutingGroup(editing.value.id, payload)
      message.value = t('routingGroups.updated')
    } else {
      await createRoutingGroup(payload)
      message.value = t('routingGroups.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

function normalizedPayload(): RoutingGroupRequest {
  const payload: RoutingGroupRequest = { ...form }
  payload.rate_multiplier = numberOrDefault(payload.rate_multiplier, 1)
  payload.rpm_limit = numberOrDefault(payload.rpm_limit, 0)
  payload.image_rate_multiplier = numberOrDefault(payload.image_rate_multiplier, 1)
  payload.video_rate_multiplier = numberOrDefault(payload.video_rate_multiplier, 1)
  payload.batch_image_discount_multiplier = numberOrDefault(payload.batch_image_discount_multiplier, 1)
  payload.peak_rate_multiplier = numberOrDefault(payload.peak_rate_multiplier, 1)
  if (payload.group_type !== 'subscription') {
    payload.daily_budget_micros = 0
    payload.weekly_budget_micros = 0
    payload.monthly_budget_micros = 0
    payload.peak_rate_enabled = false
    payload.peak_start = ''
    payload.peak_end = ''
    payload.peak_rate_multiplier = 1
  }
  if (payload.group_type !== 'image_generation') {
    payload.image_enabled = false
    payload.batch_image_enabled = false
    payload.image_price_1k_cents = 0
    payload.image_price_2k_cents = 0
    payload.image_price_4k_cents = 0
    payload.image_rate_multiplier = 1
    payload.batch_image_discount_multiplier = 1
  }
  if (payload.group_type !== 'video_generation') {
    payload.video_enabled = false
    payload.video_price_480p_cents = 0
    payload.video_price_720p_cents = 0
    payload.video_price_1080p_cents = 0
    payload.video_rate_multiplier = 1
  }
  if (payload.group_type === 'exclusive') payload.is_exclusive = true
  if (payload.group_type === 'image_generation') {
    payload.image_enabled = true
    payload.video_enabled = false
  }
  if (payload.group_type === 'video_generation') {
    payload.video_enabled = true
    payload.image_enabled = false
    payload.batch_image_enabled = false
  }
  return payload
}

function numberOrDefault(value: number, fallback: number): number {
  return Number.isFinite(value) ? value : fallback
}

function applyTypeDefaults(type: string) {
  if (type === 'exclusive') {
    form.is_exclusive = true
  }
  if (type === 'image_generation') {
    form.image_enabled = true
    form.video_enabled = false
  }
  if (type === 'video_generation') {
    form.video_enabled = true
    form.image_enabled = false
    form.batch_image_enabled = false
  }
  if (type === 'standard' || type === 'subscription') {
    form.image_enabled = false
    form.batch_image_enabled = false
    form.video_enabled = false
  }
  if (type !== 'subscription') {
    form.peak_rate_enabled = false
  }
}

function normalizedGroupType(group: Pick<RoutingGroup, 'group_type'>): string {
  return group.group_type || 'standard'
}

function groupTypeLabel(type: string): string {
  return t(`routingGroups.types.${type || 'standard'}`)
}

function statusClass(status: string) {
  return status === 'active' ? 'status-success' : 'status-danger'
}

function typeClass(type: string) {
  return `routing-group-type-${type || 'standard'}`
}

function formatCost(micros: number): string {
  if (!micros) return t('apiKeys.unlimited')
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatLimit(value: number): string {
  return value > 0 ? String(value) : t('apiKeys.unlimited')
}

function budgetSummary(group: RoutingGroup): string {
  if (normalizedGroupType(group) !== 'subscription') return '-'
  const parts = [
    group.daily_budget_micros ? `${t('routingGroups.dailyBudgetShort')} ${formatCost(group.daily_budget_micros)}` : '',
    group.weekly_budget_micros ? `${t('routingGroups.weeklyBudgetShort')} ${formatCost(group.weekly_budget_micros)}` : '',
    group.monthly_budget_micros ? `${t('routingGroups.monthlyBudgetShort')} ${formatCost(group.monthly_budget_micros)}` : ''
  ].filter(Boolean)
  return parts.length ? parts.join(' / ') : t('apiKeys.unlimited')
}

function mediaSummary(group: RoutingGroup): string {
  if (group.image_enabled) {
    return `${t('routingGroups.imagePricing')} ${group.image_rate_multiplier || 1}x`
  }
  if (group.video_enabled) {
    return `${t('routingGroups.videoPricing')} ${group.video_rate_multiplier || 1}x`
  }
  if (group.peak_rate_enabled) {
    return `${t('routingGroups.peakRate')} ${group.peak_rate_multiplier || 1}x`
  }
  return '-'
}

watch(() => form.group_type, (type) => applyTypeDefaults(type))

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.routingGroups') }}</h1>
        <p>{{ t('routingGroups.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('routingGroups.newGroup') }}
      </button>
    </section>

    <div class="notice">{{ t('routingGroups.advancedNotice') }}</div>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('routingGroups.groups') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.exclusive }}</strong>{{ t('routingGroups.exclusive') }}</span>
      <span><strong>{{ summary.media }}</strong>{{ t('routingGroups.mediaGroups') }}</span>
      <span><strong>{{ summary.schedulable }} / {{ summary.accounts }}</strong>{{ t('providerAccounts.schedulable') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('routingGroups.searchPlaceholder')" />
      </label>
      <select v-model="platformFilter">
        <option value="">{{ t('routingGroups.allPlatforms') }}</option>
        <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
      </select>
      <select v-model="typeFilter">
        <option value="">{{ t('routingGroups.allTypes') }}</option>
        <option v-for="type in groupTypes" :key="type" :value="type">{{ groupTypeLabel(type) }}</option>
      </select>
      <select v-model="visibilityFilter">
        <option value="">{{ t('routingGroups.allVisibility') }}</option>
        <option value="public">{{ t('routingGroups.public') }}</option>
        <option value="exclusive">{{ t('routingGroups.exclusive') }}</option>
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
              <th>{{ t('routingGroups.name') }}</th>
              <th>{{ t('routingGroups.platform') }}</th>
              <th>{{ t('routingGroups.type') }}</th>
              <th>{{ t('routingGroups.billingAndCapacity') }}</th>
              <th>{{ t('routingGroups.visibility') }}</th>
              <th>{{ t('routingGroups.accounts') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="group in filteredGroups" :key="group.id">
              <td>
                <strong>{{ group.name }}</strong>
                <span>{{ group.description || '-' }}</span>
              </td>
              <td>{{ group.platform }}</td>
              <td>
                <span class="pill routing-group-type" :class="typeClass(normalizedGroupType(group))">
                  {{ groupTypeLabel(normalizedGroupType(group)) }}
                </span>
                <span>{{ mediaSummary(group) }}</span>
              </td>
              <td>
                <strong>{{ group.rate_multiplier }}x</strong>
                <span>{{ t('routingGroups.rpmLimit') }} {{ formatLimit(group.rpm_limit) }}</span>
                <span>{{ budgetSummary(group) }}</span>
              </td>
              <td>
                <span class="pill" :class="group.is_exclusive ? 'status-warning' : 'status-muted'">
                  {{ group.is_exclusive ? t('routingGroups.exclusive') : t('routingGroups.public') }}
                </span>
              </td>
              <td>
                <strong>{{ group.active_account_count }} / {{ group.account_count }}</strong>
                <span>{{ t('providerAccounts.schedulable') }}</span>
              </td>
              <td><span class="pill" :class="statusClass(group.status)">{{ group.status }}</span></td>
              <td>
                <button class="button secondary icon-text-button" type="button" @click="openEdit(group)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredGroups.length">
              <td colspan="8" class="empty-cell">
                {{ loading ? t('common.loading') : t('routingGroups.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card modal-card-wide">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('routingGroups.editGroup') : t('routingGroups.newGroup') }}</h2>
            <p>{{ t('routingGroups.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="19" />
          </button>
        </header>

        <div class="modal-body routing-group-form">
          <section class="form-section">
            <div class="form-section-header">
              <div>
                <h3>{{ t('routingGroups.baseConfig') }}</h3>
                <p>{{ t('routingGroups.baseConfigHelp') }}</p>
              </div>
              <Zap :size="18" />
            </div>
            <div class="form-grid">
              <div class="field">
                <label>{{ t('routingGroups.name') }}</label>
                <input v-model="form.name" placeholder="Default OpenAI Pool" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.platform') }}</label>
                <input v-model="form.platform" placeholder="openai_compatible" />
              </div>
              <div class="field form-span-2">
                <label>{{ t('policies.description') }}</label>
                <input v-model="form.description" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.type') }}</label>
                <select v-model="form.group_type">
                  <option v-for="type in groupTypes" :key="type" :value="type">{{ groupTypeLabel(type) }}</option>
                </select>
              </div>
              <div class="field">
                <label>{{ t('providers.status') }}</label>
                <select v-model="form.status">
                  <option value="active">active</option>
                  <option value="disabled">disabled</option>
                </select>
              </div>
              <div class="field">
                <label>{{ t('routingGroups.rateMultiplier') }}</label>
                <input v-model.number="form.rate_multiplier" type="number" min="0" step="0.01" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.rpmLimit') }}</label>
                <input v-model.number="form.rpm_limit" type="number" min="0" step="1" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.sortOrder') }}</label>
                <input v-model.number="form.sort_order" type="number" min="0" />
              </div>
              <label class="field checkbox-line routing-toggle-line">
                <input v-model="form.is_exclusive" type="checkbox" :disabled="form.group_type === 'exclusive'" />
                <span>
                  <strong>{{ t('routingGroups.exclusive') }}</strong>
                  <small>{{ t('routingGroups.exclusiveHelp') }}</small>
                </span>
              </label>
            </div>
          </section>

          <section v-if="form.group_type === 'subscription'" class="form-section">
            <div class="form-section-header">
              <div>
                <h3>{{ t('routingGroups.subscriptionConfig') }}</h3>
                <p>{{ t('routingGroups.subscriptionConfigHelp') }}</p>
              </div>
              <ShieldCheck :size="18" />
            </div>
            <div class="form-grid">
              <div class="field">
                <label>{{ t('routingGroups.dailyBudget') }}</label>
                <input v-model.number="form.daily_budget_micros" type="number" min="0" step="100" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.weeklyBudget') }}</label>
                <input v-model.number="form.weekly_budget_micros" type="number" min="0" step="100" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.monthlyBudget') }}</label>
                <input v-model.number="form.monthly_budget_micros" type="number" min="0" step="100" />
              </div>
              <label class="field checkbox-line routing-toggle-line">
                <input v-model="form.peak_rate_enabled" type="checkbox" />
                <span>
                  <strong>{{ t('routingGroups.peakRate') }}</strong>
                  <small>{{ t('routingGroups.peakRateHelp') }}</small>
                </span>
              </label>
              <template v-if="form.peak_rate_enabled">
                <div class="field">
                  <label>{{ t('routingGroups.peakStart') }}</label>
                  <input v-model="form.peak_start" type="time" />
                </div>
                <div class="field">
                  <label>{{ t('routingGroups.peakEnd') }}</label>
                  <input v-model="form.peak_end" type="time" />
                </div>
                <div class="field">
                  <label>{{ t('routingGroups.peakMultiplier') }}</label>
                  <input v-model.number="form.peak_rate_multiplier" type="number" min="0" step="0.01" />
                </div>
              </template>
            </div>
          </section>

          <section v-if="form.group_type === 'exclusive'" class="form-section">
            <div class="form-section-header">
              <div>
                <h3>{{ t('routingGroups.exclusiveConfig') }}</h3>
                <p>{{ t('routingGroups.exclusiveConfigHelp') }}</p>
              </div>
              <ShieldCheck :size="18" />
            </div>
            <div class="config-hint-grid">
              <div>
                <strong>{{ t('routingGroups.exclusiveRouting') }}</strong>
                <span>{{ t('routingGroups.exclusiveRoutingHelp') }}</span>
              </div>
              <div>
                <strong>{{ t('routingGroups.exclusiveCapacity') }}</strong>
                <span>{{ t('routingGroups.exclusiveCapacityHelp') }}</span>
              </div>
            </div>
          </section>

          <section v-if="form.group_type === 'image_generation'" class="form-section">
            <div class="form-section-header">
              <div>
                <h3>{{ t('routingGroups.imageConfig') }}</h3>
                <p>{{ t('routingGroups.imageConfigHelp') }}</p>
              </div>
              <Image :size="18" />
            </div>
            <div class="form-grid">
              <div class="field">
                <label>{{ t('routingGroups.imageMultiplier') }}</label>
                <input v-model.number="form.image_rate_multiplier" type="number" min="0" step="0.01" />
              </div>
              <label class="field checkbox-line routing-toggle-line">
                <input v-model="form.batch_image_enabled" type="checkbox" />
                <span>
                  <strong>{{ t('routingGroups.batchImage') }}</strong>
                  <small>{{ t('routingGroups.batchImageHelp') }}</small>
                </span>
              </label>
              <div class="field">
                <label>{{ t('routingGroups.imagePrice1K') }}</label>
                <input v-model.number="form.image_price_1k_cents" type="number" min="0" step="1" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.imagePrice2K') }}</label>
                <input v-model.number="form.image_price_2k_cents" type="number" min="0" step="1" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.imagePrice4K') }}</label>
                <input v-model.number="form.image_price_4k_cents" type="number" min="0" step="1" />
              </div>
              <div v-if="form.batch_image_enabled" class="field">
                <label>{{ t('routingGroups.batchDiscountMultiplier') }}</label>
                <input v-model.number="form.batch_image_discount_multiplier" type="number" min="0" step="0.01" />
              </div>
            </div>
          </section>

          <section v-if="form.group_type === 'video_generation'" class="form-section">
            <div class="form-section-header">
              <div>
                <h3>{{ t('routingGroups.videoConfig') }}</h3>
                <p>{{ t('routingGroups.videoConfigHelp') }}</p>
              </div>
              <Video :size="18" />
            </div>
            <div class="form-grid">
              <div class="field">
                <label>{{ t('routingGroups.videoMultiplier') }}</label>
                <input v-model.number="form.video_rate_multiplier" type="number" min="0" step="0.01" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.videoPrice480P') }}</label>
                <input v-model.number="form.video_price_480p_cents" type="number" min="0" step="1" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.videoPrice720P') }}</label>
                <input v-model.number="form.video_price_720p_cents" type="number" min="0" step="1" />
              </div>
              <div class="field">
                <label>{{ t('routingGroups.videoPrice1080P') }}</label>
                <input v-model.number="form.video_price_1080p_cents" type="number" min="0" step="1" />
              </div>
            </div>
          </section>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="saving" @click="save">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
