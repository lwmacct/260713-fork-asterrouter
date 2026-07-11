<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Clock3, Coins, Download, RefreshCw, Search, Sigma } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { exportUsageCSV, getUsageReport } from '@/api/control'
import { isNotFoundError } from '@/api/client'
import type { RecordListQuery, UsageReport } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const report = ref<UsageReport | null>(null)
const query = ref('')
const modelFilter = ref('')
const statusFilter = ref('')
const fromTime = ref('')
const toTime = ref('')
const pageSize = ref(25)
const offset = ref(0)

const filteredRecent = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return (report.value?.recent || []).filter((record) => {
    if (modelFilter.value && record.model !== modelFilter.value) return false
    if (statusFilter.value && record.status !== statusFilter.value) return false
    if (!keyword) return true
    return [
      record.model,
      record.status,
      record.error_type,
      record.provider_id,
      record.provider_account_id,
      record.api_fingerprint,
      record.project_id,
      record.application_id
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
})

const filteredModelSummary = computed(() => {
  return report.value?.by_model || []
})

const modelOptions = computed(() => Array.from(new Set((report.value?.recent || []).map((item) => item.model))).filter(Boolean).sort())
const statusOptions = computed(() => Array.from(new Set((report.value?.recent || []).map((item) => item.status))).filter(Boolean).sort())
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => (report.value?.recent.length || 0) >= pageSize.value)

const emptyReport: UsageReport = {
  total_requests: 0,
  error_requests: 0,
  total_tokens: 0,
  total_cost_cents: 0,
  avg_latency_ms: 0,
  by_model: [],
  recent: []
}

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
    sub: t('usage.estimatedCost'),
    icon: Coins
  },
  {
    label: t('usage.latency'),
    value: `${formatNumber(report.value?.avg_latency_ms || 0)} ms`,
    sub: t('usage.averageLatency'),
    icon: Clock3
  }
])

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

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function statusClass(status: string) {
  if (status === 'accepted' || status === 'ok') return 'status-success'
  if (status === 'upstream_error' || status === 'error') return 'status-danger'
  return 'status-warning'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    report.value = await getUsageReport(listQuery())
  } catch (err) {
    if (isNotFoundError(err)) {
      report.value = emptyReport
      return
    }
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function listQuery(): RecordListQuery {
  return {
    limit: pageSize.value,
    offset: offset.value,
    q: query.value.trim() || undefined,
    model: modelFilter.value || undefined,
    status: statusFilter.value || undefined,
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
  }
}

async function exportCSV() {
  error.value = ''
  try {
    await exportUsageCSV({ ...listQuery(), limit: 5000, offset: 0 })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function applyFilters() {
  offset.value = 0
  void load()
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
        <h1>{{ t('admin.usage') }}</h1>
        <p>{{ t('usage.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="!filteredRecent.length" @click="exportCSV">
          <Download :size="17" />
          {{ t('common.export') }}
        </button>
        <button class="button secondary" :disabled="loading" @click="load">
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

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('usage.searchPlaceholder')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="modelFilter" @change="applyFilters">
        <option value="">{{ t('usage.allModels') }}</option>
        <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
      </select>
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

    <section class="grid section-gap">
      <div class="panel">
        <div class="panel-header">
          <Sigma :size="18" />
          <h2>{{ t('usage.byModel') }}</h2>
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ t('usage.model') }}</th>
                <th>{{ t('usage.requests') }}</th>
                <th>{{ t('usage.errors') }}</th>
                <th>{{ t('usage.tokens') }}</th>
                <th>{{ t('usage.cost') }}</th>
                <th>{{ t('usage.latency') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in filteredModelSummary" :key="item.model">
                <td><strong>{{ item.model }}</strong></td>
                <td>{{ formatNumber(item.requests) }}</td>
                <td>{{ formatNumber(item.errors) }}</td>
                <td>{{ formatNumber(item.tokens) }}</td>
                <td>{{ formatCost(item.cost_cents) }}</td>
                <td>{{ formatNumber(item.avg_latency_ms) }} ms</td>
              </tr>
              <tr v-if="!filteredModelSummary.length">
                <td colspan="6" class="empty-cell">{{ t('usage.noData') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="panel">
        <div class="panel-header">
          <Clock3 :size="18" />
          <h2>{{ t('usage.recentRequests') }}</h2>
        </div>
        <div class="panel-body table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ t('audit.time') }}</th>
                <th>{{ t('usage.model') }}</th>
                <th>{{ t('usage.route') }}</th>
                <th>{{ t('providers.status') }}</th>
                <th>{{ t('usage.tokens') }}</th>
                <th>{{ t('usage.cost') }}</th>
                <th>{{ t('usage.latency') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in filteredRecent" :key="item.id">
                <td>{{ formatTime(item.created_at) }}</td>
                <td>
                  <strong>{{ item.model || '-' }}</strong>
                  <span>{{ item.api_fingerprint }}</span>
                </td>
                <td>
                  <strong>{{ item.provider_id || '-' }}</strong>
                  <span>{{ item.provider_account_id || '-' }}</span>
                </td>
                <td><span class="pill" :class="statusClass(item.status)">{{ item.status }}</span></td>
                <td>{{ formatNumber(item.input_tokens + item.output_tokens) }}</td>
                <td>{{ formatCost(item.cost_cents) }}</td>
                <td>{{ formatNumber(item.latency_ms) }} ms</td>
              </tr>
              <tr v-if="!filteredRecent.length">
                <td colspan="7" class="empty-cell">{{ t('usage.noData') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
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
