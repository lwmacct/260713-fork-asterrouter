<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Clock3, Coins, Download, PieChart, RefreshCw, Search, Sigma } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { exportCostAllocationCSV, getCostAllocationReport } from '@/api/control'
import type { CostAllocationDimension, CostAllocationReport, CostAllocationRow, RecordListQuery } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const report = ref<CostAllocationReport | null>(null)
const dimension = ref<CostAllocationDimension>('api_key')
const query = ref('')
const modelFilter = ref('')
const statusFilter = ref('')
const fromTime = ref('')
const toTime = ref('')
const pageSize = ref(50)
const offset = ref(0)

const dimensions: CostAllocationDimension[] = ['api_key', 'user', 'department', 'group', 'model']
const statusOptions = ['forwarded', 'accepted', 'upstream_error', 'error']

const rows = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  const data = report.value?.rows || []
  if (!keyword) return data
  return data.filter((row) =>
    [
      row.resource_name,
      row.resource_id,
      row.api_key_name,
      row.api_fingerprint,
      row.model
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  )
})

const metrics = computed(() => [
  {
    label: t('usage.requests'),
    value: formatNumber(report.value?.total_requests || 0),
    sub: `${formatNumber(report.value?.error_requests || 0)} ${t('usage.errors')}`,
    icon: Activity
  },
  {
    label: t('usage.tokens'),
    value: formatNumber(report.value?.total_tokens || 0),
    sub: t('usage.totalTokens'),
    icon: Sigma
  },
  {
    label: t('usage.cost'),
    value: formatCost(report.value?.total_cost_cents || 0),
    sub: t('costAllocation.allocatedCost'),
    icon: Coins
  },
  {
    label: t('usage.latency'),
    value: `${formatNumber(report.value?.avg_latency_ms || 0)} ms`,
    sub: t('usage.averageLatency'),
    icon: Clock3
  }
])

const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => (report.value?.rows.length || 0) >= pageSize.value)

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

function formatCost(cents: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2
  }).format(cents / 100)
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`
}

function dimensionLabel(value: CostAllocationDimension): string {
  return t(`costAllocation.dimensions.${value}`)
}

function rowScope(row: CostAllocationRow): string {
  if (dimension.value === 'api_key') return row.api_fingerprint || row.api_key_id || '-'
  return row.resource_id || '-'
}

function listQuery(): RecordListQuery {
  return {
    dimension: dimension.value,
    limit: pageSize.value,
    offset: offset.value,
    model: modelFilter.value.trim() || undefined,
    status: statusFilter.value || undefined,
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    report.value = await getCostAllocationReport(listQuery())
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function exportCSV() {
  error.value = ''
  try {
    await exportCostAllocationCSV({ ...listQuery(), limit: 5000, offset: 0 })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function applyFilters() {
  offset.value = 0
  void load()
}

function selectDimension(value: CostAllocationDimension) {
  if (dimension.value === value) return
  dimension.value = value
  applyFilters()
}

function previousPage() {
  if (!canPrevious.value) return
  offset.value = Math.max(0, offset.value - pageSize.value)
  void load()
}

function nextPage() {
  if (!canNext.value) return
  offset.value += pageSize.value
  void load()
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.costAllocation') }}</h1>
        <p>{{ t('costAllocation.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="!rows.length" @click="exportCSV">
          <Download :size="17" />
          {{ t('common.export') }}
        </button>
        <button class="button secondary" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <section class="metric-grid">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card">
        <span class="metric-icon"><component :is="metric.icon" :size="20" /></span>
        <div>
          <span>{{ metric.label }}</span>
          <strong>{{ metric.value }}</strong>
          <small>{{ metric.sub }}</small>
        </div>
      </article>
    </section>

    <section class="settings-tabs" :aria-label="t('costAllocation.dimension')">
      <button
        v-for="item in dimensions"
        :key="item"
        type="button"
        :class="{ active: dimension === item }"
        @click="selectDimension(item)"
      >
        <PieChart :size="16" />
        {{ dimensionLabel(item) }}
      </button>
    </section>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('costAllocation.searchPlaceholder')" />
      </label>
      <label class="time-filter">
        <span>{{ t('usage.model') }}</span>
        <input v-model="modelFilter" :placeholder="t('usage.allModels')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="statusFilter" @change="applyFilters">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option v-for="status in statusOptions" :key="status" :value="status">{{ status }}</option>
      </select>
      <label class="time-filter">
        <span>{{ t('common.from') }}</span>
        <input v-model="fromTime" type="datetime-local" @change="applyFilters" />
      </label>
      <label class="time-filter">
        <span>{{ t('common.to') }}</span>
        <input v-model="toTime" type="datetime-local" @change="applyFilters" />
      </label>
      <button class="button secondary" type="button" @click="applyFilters">{{ t('common.apply') }}</button>
    </section>

    <section class="panel table-panel">
      <div class="panel-header">
        <PieChart :size="18" />
        <h2>{{ t('costAllocation.breakdown') }}</h2>
      </div>
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('costAllocation.resource') }}</th>
              <th>{{ t('costAllocation.scope') }}</th>
              <th>{{ t('admin.apiKeys') }}</th>
              <th>{{ t('usage.model') }}</th>
              <th>{{ t('usage.requests') }}</th>
              <th>{{ t('usage.tokens') }}</th>
              <th>{{ t('usage.cost') }}</th>
              <th>{{ t('costAllocation.share') }}</th>
              <th>{{ t('usage.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="`${row.dimension}:${row.resource_id}`">
              <td>
                <strong>{{ row.resource_name || '-' }}</strong>
                <span>{{ row.resource_id || '-' }}</span>
              </td>
              <td>{{ rowScope(row) }}</td>
              <td>
                <strong>{{ row.api_key_name || '-' }}</strong>
                <span>{{ row.api_fingerprint || row.api_key_id || '-' }}</span>
              </td>
              <td>{{ row.model || '-' }}</td>
              <td>
                <strong>{{ formatNumber(row.requests) }}</strong>
                <span>{{ formatNumber(row.error_requests) }} {{ t('usage.errors') }}</span>
              </td>
              <td>{{ formatNumber(row.total_tokens) }}</td>
              <td>{{ formatCost(row.total_cost_cents) }}</td>
              <td>{{ formatPercent(row.cost_share_percent) }}</td>
              <td>{{ formatNumber(row.avg_latency_ms) }} ms</td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="9" class="empty-cell">{{ t('costAllocation.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="pagination-bar">
      <button class="button secondary" type="button" :disabled="!canPrevious || loading" @click="previousPage">
        {{ t('common.previous') }}
      </button>
      <span>{{ t('common.page') }} {{ pageNumber }}</span>
      <button class="button secondary" type="button" :disabled="!canNext || loading" @click="nextPage">
        {{ t('common.next') }}
      </button>
    </section>
  </main>
</template>
