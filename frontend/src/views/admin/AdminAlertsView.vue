<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { BellRing, Check, CircleCheck, RefreshCw, Search } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { acknowledgeAlert, getAlertSummary, getAlerts, resolveAlert } from '@/api/control'
import { isNotFoundError } from '@/api/client'
import type { AlertEvent, AlertSummary, RecordListQuery } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

const { t } = useI18n()
const loading = ref(false)
const actionLoading = ref('')
const error = ref('')
const alerts = ref<AlertEvent[]>([])
const summary = ref<AlertSummary>({ total: 0, active: 0, acknowledged: 0, resolved: 0, warning: 0, critical: 0 })
const query = ref('')
const typeFilter = ref('')
const severityFilter = ref('')
const statusFilter = ref('active')
const resourceTypeFilter = ref('')
const fromTime = ref('')
const toTime = ref('')
const pageSize = ref(25)
const offset = ref(0)

const visibleAlerts = computed(() => alerts.value)
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => alerts.value.length >= pageSize.value)

const alertTypes = ['project_budget', 'api_key_quota', 'gateway_error_rate', 'provider_health', 'provider_account_health']
const severities = ['critical', 'warning', 'info']
const statuses = ['active', 'acknowledged', 'resolved']
const resourceTypes = ['project', 'api_key', 'provider', 'provider_account']

async function load() {
  loading.value = true
  error.value = ''
  try {
    const currentQuery = listQuery()
    const [alertData, summaryData] = await Promise.all([
      getAlerts(currentQuery),
      getAlertSummary(currentQuery)
    ])
    alerts.value = alertData
    summary.value = summaryData
  } catch (err) {
    if (isNotFoundError(err)) {
      alerts.value = []
      summary.value = { total: 0, active: 0, acknowledged: 0, resolved: 0, warning: 0, critical: 0 }
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
    type: typeFilter.value || undefined,
    severity: severityFilter.value || undefined,
    status: statusFilter.value || undefined,
    resource_type: resourceTypeFilter.value || undefined,
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
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

async function acknowledge(item: AlertEvent) {
  actionLoading.value = item.id
  error.value = ''
  try {
    await acknowledgeAlert(item.id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionLoading.value = ''
  }
}

async function resolve(item: AlertEvent) {
  actionLoading.value = item.id
  error.value = ''
  try {
    await resolveAlert(item.id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    actionLoading.value = ''
  }
}

function severityClass(severity: string): string {
  if (severity === 'critical') return 'status-danger'
  if (severity === 'warning') return 'status-warning'
  return 'status-success'
}

function statusClass(status: string): string {
  if (status === 'resolved') return 'status-success'
  if (status === 'acknowledged') return 'status-warning'
  return 'status-danger'
}

function label(scope: string, value: string): string {
  if (!value) return '-'
  return t(`alerts.${scope}.${value}`)
}

function formatTime(value?: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.alerts') }}</h1>
        <p>{{ t('alerts.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('alerts.events') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('alerts.active') }}</span>
      <span><strong>{{ summary.critical }}</strong>{{ t('alerts.critical') }}</span>
      <span><strong>{{ summary.warning }}</strong>{{ t('alerts.warning') }}</span>
      <span><strong>{{ summary.resolved }}</strong>{{ t('alerts.resolved') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('alerts.searchPlaceholder')" @keyup.enter="applyFilters" />
      </label>
      <select v-model="typeFilter" @change="applyFilters">
        <option value="">{{ t('alerts.allTypes') }}</option>
        <option v-for="type in alertTypes" :key="type" :value="type">{{ label('types', type) }}</option>
      </select>
      <select v-model="severityFilter" @change="applyFilters">
        <option value="">{{ t('alerts.allSeverities') }}</option>
        <option v-for="severity in severities" :key="severity" :value="severity">{{ label('severities', severity) }}</option>
      </select>
      <select v-model="statusFilter" @change="applyFilters">
        <option value="">{{ t('alerts.allStatuses') }}</option>
        <option v-for="status in statuses" :key="status" :value="status">{{ label('statuses', status) }}</option>
      </select>
      <select v-model="resourceTypeFilter" @change="applyFilters">
        <option value="">{{ t('alerts.allResources') }}</option>
        <option v-for="resource in resourceTypes" :key="resource" :value="resource">{{ label('resources', resource) }}</option>
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

    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('alerts.lastSeen') }}</th>
              <th>{{ t('alerts.alert') }}</th>
              <th>{{ t('alerts.severity') }}</th>
              <th>{{ t('alerts.status') }}</th>
              <th>{{ t('alerts.resource') }}</th>
              <th>{{ t('alerts.firstSeen') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="alert in visibleAlerts" :key="alert.id">
              <td>{{ formatTime(alert.last_seen_at) }}</td>
              <td>
                <strong>{{ alert.title }}</strong>
                <span>{{ alert.summary }}</span>
              </td>
              <td><span class="pill" :class="severityClass(alert.severity)">{{ label('severities', alert.severity) }}</span></td>
              <td>
                <span class="pill" :class="statusClass(alert.status)">{{ label('statuses', alert.status) }}</span>
                <span v-if="alert.acknowledged_by">{{ alert.acknowledged_by }}</span>
                <span v-else-if="alert.resolved_by">{{ alert.resolved_by }}</span>
              </td>
              <td>
                <strong>{{ label('resources', alert.resource_type) }}</strong>
                <span>{{ alert.resource_id || '-' }}</span>
                <span v-if="alert.project_id">{{ t('projects.project') }}: {{ alert.project_id }}</span>
              </td>
              <td>{{ formatTime(alert.first_seen_at) }}</td>
              <td>
                <div class="row-actions">
                  <button
                    v-if="alert.status === 'active'"
                    class="button secondary"
                    type="button"
                    :disabled="actionLoading === alert.id"
                    @click="acknowledge(alert)"
                  >
                    <Check :size="15" />
                    {{ t('alerts.acknowledge') }}
                  </button>
                  <button
                    v-if="alert.status !== 'resolved'"
                    class="button secondary"
                    type="button"
                    :disabled="actionLoading === alert.id"
                    @click="resolve(alert)"
                  >
                    <CircleCheck :size="15" />
                    {{ t('alerts.resolve') }}
                  </button>
                  <span v-if="alert.status === 'resolved'" class="pill status-success">
                    <BellRing :size="14" />
                    {{ t('alerts.closed') }}
                  </span>
                </div>
              </td>
            </tr>
            <tr v-if="!visibleAlerts.length">
              <td colspan="7" class="empty-cell">{{ loading ? t('common.loading') : t('alerts.empty') }}</td>
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
