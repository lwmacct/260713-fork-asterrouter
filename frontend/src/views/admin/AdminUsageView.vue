<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  BarChart3,
  Download,
  KeyRound,
  PieChart,
  RefreshCw,
  Search,
  Sigma
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { exportUsageCSV, getCostAllocationReport, getUsageReport } from '@/api/control'
import { isNotFoundError } from '@/api/client'
import type { CostAllocationReport, CostAllocationRow, RecordListQuery, UsageModelSummary, UsageRecord, UsageReport } from '@/types'
import { datetimeLocalToISOString } from '@/utils/timeRange'

type TimePreset = '24h' | '7d' | '30d' | 'custom'
type DetailTab = 'usage' | 'errors' | 'models' | 'keys'
type DistributionMetric = 'tokens' | 'actual_cost'
type UsageView = 'workbench' | 'analysis' | 'records'

interface DistributionRow {
  label: string
  scope: string
  requests: number
  errors: number
  tokens: number
  usage_cost_micros: number
  avg_latency_ms: number
}

interface DistributionSeriesItem extends DistributionRow {
  value: number
  share: number
  color: string
}

interface TrendPoint {
  bucket: string
  label: string
  requests: number
  errors: number
  input_tokens: number
  output_tokens: number
  tokens: number
  usage_cost_micros: number
}

interface SelectOption {
  value: string
  label: string
}

const { t } = useI18n()
const pageLoading = ref(false)
const analysisLoading = ref(false)
const error = ref('')
const pageReport = ref<UsageReport | null>(null)
const analysisReport = ref<UsageReport | null>(null)
const keyAllocation = ref<CostAllocationReport | null>(null)
const activeTab = ref<DetailTab>('usage')
const activeView = ref<UsageView>('workbench')
const distributionMetric = ref<DistributionMetric>('tokens')

const query = ref('')
const modelFilter = ref('')
const statusFilter = ref('')
const providerFilter = ref('')
const accountFilter = ref('')
const apiKeyFilter = ref('')
const timePreset = ref<TimePreset>('24h')
const granularity = ref<'hour' | 'day'>('hour')
const pageSize = ref(25)
const offset = ref(0)

const initialRange = presetRange('24h')
const fromTime = ref(initialRange.from)
const toTime = ref(initialRange.to)

const analysisLimit = 500
const colors = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316', '#6366f1', '#84cc16', '#06b6d4', '#a855f7']

const emptyReport: UsageReport = {
  total_requests: 0,
  error_requests: 0,
  total_tokens: 0,
  total_output_images: 0,
  total_video_milliseconds: 0,
  total_audio_milliseconds: 0,
  total_usage_cost_micros: 0,
  priced_requests: 0,
  unpriced_requests: 0,
  disputed_requests: 0,
  cost_available: false,
  avg_latency_ms: 0,
  by_model: [],
  recent: []
}

const loading = computed(() => pageLoading.value || analysisLoading.value)
const summaryReport = computed(() => analysisReport.value || pageReport.value || emptyReport)
const pageRecords = computed(() => pageReport.value?.recent || [])
const analysisRecords = computed(() => analysisReport.value?.recent || pageRecords.value)
const modelRows = computed(() => (analysisReport.value?.by_model || []).map(modelSummaryToDistributionRow))
const keyRows = computed(() => (keyAllocation.value?.rows || []).map(keyAllocationToDistributionRow))
const errorRows = computed(() => analysisRecords.value.filter(isErrorRecord).sort(sortRecordsDesc).slice(0, 100))
const pageNumber = computed(() => Math.floor(offset.value / pageSize.value) + 1)
const canPrevious = computed(() => offset.value > 0)
const canNext = computed(() => pageRecords.value.length >= pageSize.value)
const errorRate = computed(() => {
  const total = summaryReport.value.total_requests || 0
  return total ? (summaryReport.value.error_requests / total) * 100 : 0
})

const metrics = computed(() => [
  {
    label: t('usage.requests'),
    value: formatNumber(summaryReport.value.total_requests),
    sub: `${formatNumber(summaryReport.value.error_requests)} ${t('usage.errors')} · ${formatPercent(errorRate.value)}`
  },
  {
    label: t('usage.tokens'),
    value: compactNumber(summaryReport.value.total_tokens),
    sub: t('usage.totalTokens')
  },
  {
    label: t('usage.cost'),
    value: formatCost(summaryReport.value.total_usage_cost_micros),
    sub: t('usage.estimatedCost')
  },
  {
    label: t('usage.latency'),
    value: `${formatNumber(summaryReport.value.avg_latency_ms)} ms`,
    sub: t('usage.averageLatency')
  }
])

const usageViews = computed<Array<{ value: UsageView; label: string }>>(() => [
  { value: 'workbench', label: t('usage.workbench') },
  { value: 'analysis', label: t('usage.analysisView') },
  { value: 'records', label: t('usage.recordsView') }
])

const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.detailTab'), icon: Activity, count: pageRecords.value.length },
  { key: 'errors' as const, label: t('usage.errorTab'), icon: AlertTriangle, count: errorRows.value.length },
  { key: 'models' as const, label: t('usage.modelTab'), icon: Sigma, count: modelRows.value.length },
  { key: 'keys' as const, label: t('usage.keyTab'), icon: KeyRound, count: keyRows.value.length }
])

const modelOptions = computed(() => unique([...modelRows.value.map((row) => row.label), ...analysisRecords.value.map((item) => item.model)]))
const statusOptions = computed(() => unique([...analysisRecords.value.map((item) => item.status), 'accepted', 'forwarded', 'upstream_error', 'error']))
const providerOptions = computed(() => unique(analysisRecords.value.map((item) => item.provider_id)))
const accountOptions = computed(() => unique(analysisRecords.value.map((item) => item.provider_account_id)))
const apiKeyOptions = computed<SelectOption[]>(() => {
  const map = new Map<string, string>()
  for (const item of analysisRecords.value) {
    if (!item.api_key_id) continue
    map.set(item.api_key_id, item.api_fingerprint || item.api_key_id)
  }
  return Array.from(map.entries())
    .map(([value, label]) => ({ value, label }))
    .sort((a, b) => a.label.localeCompare(b.label))
})

const modelSeries = computed(() => buildSeries(modelRows.value, distributionMetric.value))
const keySeries = computed(() => buildSeries(keyRows.value, distributionMetric.value))
const statusRows = computed(() => aggregateRecordsByStatus(analysisRecords.value))
const statusSeries = computed(() => buildSeries(statusRows.value, 'requests'))
const trendPoints = computed(() => buildTrendPoints(analysisRecords.value, granularity.value))
const trendInputPath = computed(() => linePath(trendPoints.value, 'input_tokens'))
const trendOutputPath = computed(() => linePath(trendPoints.value, 'output_tokens'))
const trendAxisLabels = computed(() => trendAxis(trendPoints.value))
const recentWorkbenchRecords = computed(() => [...analysisRecords.value].sort(sortRecordsDesc).slice(0, 6))
const costPerRequest = computed(() => summaryReport.value.total_requests
  ? summaryReport.value.total_usage_cost_micros / summaryReport.value.total_requests
  : 0)
const topModel = computed(() => modelSeries.value[0]?.label || '-')
const topKey = computed(() => keySeries.value[0]?.label || '-')

function toDateTimeLocal(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function presetRange(preset: Exclude<TimePreset, 'custom'>): { from: string; to: string } {
  const to = new Date()
  const hours = preset === '24h' ? 24 : preset === '7d' ? 24 * 7 : 24 * 30
  const from = new Date(to.getTime() - hours * 60 * 60 * 1000)
  return { from: toDateTimeLocal(from), to: toDateTimeLocal(to) }
}

function handlePresetChange() {
  if (timePreset.value === 'custom') return
  const range = presetRange(timePreset.value)
  fromTime.value = range.from
  toTime.value = range.to
  granularity.value = timePreset.value === '24h' ? 'hour' : 'day'
  applyFilters()
}

function markCustomRange() {
  timePreset.value = 'custom'
  applyFilters()
}

function clean(value: string): string | undefined {
  const trimmed = value.trim()
  return trimmed || undefined
}

function listQuery(limit: number, nextOffset: number): RecordListQuery {
  return {
    limit,
    offset: nextOffset,
    q: clean(query.value),
    api_key_id: clean(apiKeyFilter.value),
    model: clean(modelFilter.value),
    provider_id: clean(providerFilter.value),
    provider_account_id: clean(accountFilter.value),
    status: clean(statusFilter.value),
    from: datetimeLocalToISOString(fromTime.value),
    to: datetimeLocalToISOString(toTime.value)
  }
}

async function loadPage() {
  pageLoading.value = true
  error.value = ''
  try {
    pageReport.value = await getUsageReport(listQuery(pageSize.value, offset.value))
  } catch (err) {
    if (isNotFoundError(err)) {
      pageReport.value = emptyReport
      return
    }
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    pageLoading.value = false
  }
}

async function loadAnalysis() {
  analysisLoading.value = true
  error.value = ''
  try {
    const [usage, allocation] = await Promise.all([
      getUsageReport(listQuery(analysisLimit, 0)),
      getCostAllocationReport({ ...listQuery(analysisLimit, 0), dimension: 'api_key' })
    ])
    analysisReport.value = usage
    keyAllocation.value = allocation
  } catch (err) {
    if (isNotFoundError(err)) {
      analysisReport.value = emptyReport
      keyAllocation.value = null
      return
    }
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    analysisLoading.value = false
  }
}

async function loadAll() {
  await Promise.all([loadPage(), loadAnalysis()])
}

function applyFilters() {
  offset.value = 0
  void loadAll()
}

function previousPage() {
  if (!canPrevious.value) return
  offset.value = Math.max(0, offset.value - pageSize.value)
  void loadPage()
}

function nextPage() {
  if (!canNext.value) return
  offset.value += pageSize.value
  void loadPage()
}

function changePageSize() {
  offset.value = 0
  void loadPage()
}

async function exportCSV() {
  error.value = ''
  try {
    await exportUsageCSV({ ...listQuery(5000, 0), limit: 5000, offset: 0 })
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

function modelSummaryToDistributionRow(item: UsageModelSummary): DistributionRow {
  return {
    label: item.model || '-',
    scope: item.model || '-',
    requests: item.requests,
    errors: item.errors,
    tokens: item.tokens,
    usage_cost_micros: item.usage_cost_micros,
    avg_latency_ms: item.avg_latency_ms
  }
}

function keyAllocationToDistributionRow(item: CostAllocationRow): DistributionRow {
  return {
    label: item.api_key_name || item.api_fingerprint || item.resource_name || item.resource_id || '-',
    scope: item.api_fingerprint || item.api_key_id || item.resource_id || '-',
    requests: item.requests,
    errors: item.error_requests,
    tokens: item.total_tokens,
    usage_cost_micros: item.total_usage_cost_micros,
    avg_latency_ms: item.avg_latency_ms
  }
}

function aggregateRecordsByStatus(records: UsageRecord[]): DistributionRow[] {
  const map = new Map<string, DistributionRow & { latency_total: number }>()
  for (const record of records) {
    const key = record.status || '-'
    const existing = map.get(key) || {
      label: key,
      scope: key,
      requests: 0,
      errors: 0,
      tokens: 0,
      usage_cost_micros: 0,
      avg_latency_ms: 0,
      latency_total: 0
    }
    existing.requests += 1
    existing.errors += isErrorRecord(record) ? 1 : 0
    existing.tokens += record.input_tokens + record.output_tokens
    existing.usage_cost_micros += record.usage_cost_micros || 0
    existing.latency_total += record.latency_ms
    existing.avg_latency_ms = Math.round(existing.latency_total / existing.requests)
    map.set(key, existing)
  }
  return Array.from(map.values())
}

function buildSeries(rows: DistributionRow[], metric: DistributionMetric | 'requests', limit = 6): DistributionSeriesItem[] {
  const sorted = [...rows].sort((a, b) => metricValue(b, metric) - metricValue(a, metric))
  const top = sorted.slice(0, limit)
  const rest = sorted.slice(limit)
  const merged = [...top]
  if (rest.length) {
    const requests = rest.reduce((sum, item) => sum + item.requests, 0)
    const errors = rest.reduce((sum, item) => sum + item.errors, 0)
    const tokens = rest.reduce((sum, item) => sum + item.tokens, 0)
    const cost = rest.reduce((sum, item) => sum + item.usage_cost_micros, 0)
    merged.push({ label: t('usage.other'), scope: '-', requests, errors, tokens, usage_cost_micros: cost, avg_latency_ms: 0 })
  }
  const total = Math.max(merged.reduce((sum, item) => sum + metricValue(item, metric), 0), 1)
  return merged.map((item, index) => ({
    ...item,
    value: metricValue(item, metric),
    share: (metricValue(item, metric) / total) * 100,
    color: colors[index % colors.length]
  }))
}

function metricValue(row: DistributionRow, metric: DistributionMetric | 'requests'): number {
  if (metric === 'actual_cost') return row.usage_cost_micros
  if (metric === 'requests') return row.requests
  return row.tokens
}

function seriesTotal(series: DistributionSeriesItem[]): number {
  return series.reduce((sum, item) => sum + item.value, 0)
}

function donutGradient(series: DistributionSeriesItem[]): string {
  if (!series.length || seriesTotal(series) <= 0) return 'conic-gradient(var(--border) 0 100%)'
  let cursor = 0
  const parts = series.map((item) => {
    const start = cursor
    const end = cursor + item.share
    cursor = end
    return `${item.color} ${start}% ${end}%`
  })
  return `conic-gradient(${parts.join(', ')})`
}

function formatSeriesValue(value: number): string {
  if (distributionMetric.value === 'actual_cost') return formatCost(value)
  return compactNumber(value)
}

function buildTrendPoints(records: UsageRecord[], unit: 'hour' | 'day'): TrendPoint[] {
  const map = new Map<string, TrendPoint>()
  for (const record of records) {
    const date = new Date(record.created_at)
    if (Number.isNaN(date.getTime())) continue
    const key = trendKey(date, unit)
    const existing = map.get(key) || {
      bucket: key,
      label: trendLabel(date, unit),
      requests: 0,
      errors: 0,
      input_tokens: 0,
      output_tokens: 0,
      tokens: 0,
      usage_cost_micros: 0
    }
    existing.requests += 1
    existing.errors += isErrorRecord(record) ? 1 : 0
    existing.input_tokens += record.input_tokens
    existing.output_tokens += record.output_tokens
    existing.tokens += record.input_tokens + record.output_tokens
    existing.usage_cost_micros += record.usage_cost_micros || 0
    map.set(key, existing)
  }
  return Array.from(map.values()).sort((a, b) => a.bucket.localeCompare(b.bucket))
}

function trendKey(date: Date, unit: 'hour' | 'day'): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  const day = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
  return unit === 'day' ? day : `${day} ${pad(date.getHours())}:00`
}

function trendLabel(date: Date, unit: 'hour' | 'day'): string {
  return unit === 'day'
    ? `${date.getMonth() + 1}/${date.getDate()}`
    : `${String(date.getHours()).padStart(2, '0')}:00`
}

function linePath(points: TrendPoint[], field: 'input_tokens' | 'output_tokens'): string {
  if (!points.length) return ''
  const max = Math.max(...points.map((item) => item[field]), 1)
  const width = 920
  const height = 142
  const left = 42
  const bottom = 174
  const step = points.length > 1 ? width / (points.length - 1) : width
  return points
    .map((item, index) => {
      const x = left + index * step
      const y = bottom - (item[field] / max) * height
      return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`
    })
    .join(' ')
}

function trendAxis(points: TrendPoint[]): Array<{ x: number; label: string }> {
  if (!points.length) return []
  const maxLabels = 6
  const step = Math.max(1, Math.ceil(points.length / maxLabels))
  return points
    .map((point, index) => ({ point, index }))
    .filter(({ index }) => index % step === 0 || index === points.length - 1)
    .map(({ point, index }) => ({
      x: points.length > 1 ? 42 + index * (920 / (points.length - 1)) : 42,
      label: point.label
    }))
}

function isErrorRecord(record: UsageRecord): boolean {
  return record.status === 'upstream_error' || record.status === 'error' || !!record.error_type
}

function sortRecordsDesc(a: UsageRecord, b: UsageRecord): number {
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values.map((item) => item.trim()).filter(Boolean))).sort()
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

function compactNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 2 }).format(value || 0)
}

function formatCost(micros?: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 6 }).format((micros || 0) / 1_000_000)
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value || 0)}%`
}

function formatTime(value: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatLatency(value: number): string {
  return `${formatNumber(Math.round(value || 0))} ms`
}

function statusClass(status: string) {
  if (status === 'accepted' || status === 'forwarded' || status === 'ok') return 'status-success'
  if (status === 'upstream_error' || status === 'error') return 'status-danger'
  return 'status-warning'
}

function recordTokens(record: UsageRecord): number {
  return record.input_tokens + record.output_tokens
}

function metricLabel(): string {
  return distributionMetric.value === 'actual_cost' ? t('usage.cost') : t('usage.tokens')
}

function showView(view: UsageView) {
  activeView.value = view
}

function selectTab(tab: DetailTab) {
  activeTab.value = tab
}

onMounted(loadAll)
</script>

<template>
  <main class="content crud-page usage-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.usage') }}</h1>
        <p>{{ t('usage.subtitle') }}</p>
      </div>
      <div class="row-actions usage-page-actions">
        <button class="button secondary" type="button" :disabled="!summaryReport.total_requests" @click="exportCSV">
          <Download :size="17" />
          {{ t('common.export') }}
        </button>
        <button class="button" type="button" :disabled="loading" @click="loadAll">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </section>

    <div v-if="error" class="notice">{{ error }}</div>

    <nav class="usage-primary-tabs" :aria-label="t('usage.viewsLabel')">
      <button
        v-for="view in usageViews"
        :key="view.value"
        class="usage-primary-tab"
        :class="{ active: activeView === view.value }"
        type="button"
        :data-view="view.value"
        :aria-current="activeView === view.value ? 'page' : undefined"
        @click="showView(view.value)"
      >
        {{ view.label }}
      </button>
    </nav>

    <section v-if="activeView === 'workbench'" class="usage-workbench" data-section="usage-workbench">
      <section class="metric-grid usage-metric-grid usage-workbench-metrics">
        <article v-for="metric in metrics" :key="metric.label" class="metric-card">
          <div>
            <span>{{ metric.label }}</span>
            <strong>{{ metric.value }}</strong>
            <small>{{ metric.sub }}</small>
          </div>
        </article>
      </section>

      <div class="usage-workbench-context">
        <div>
          <strong>{{ t('usage.analysisWindow') }}</strong>
          <span>{{ t('usage.analysisWindowHelp') }}</span>
        </div>
        <label>
          <span>{{ t('usage.timeRange') }}</span>
          <select v-model="timePreset" @change="handlePresetChange">
            <option value="24h">{{ t('usage.last24Hours') }}</option>
            <option value="7d">{{ t('usage.last7Days') }}</option>
            <option value="30d">{{ t('usage.last30Days') }}</option>
            <option value="custom">{{ t('usage.customRange') }}</option>
          </select>
        </label>
        <label>
          <span>{{ t('usage.granularity') }}</span>
          <select v-model="granularity">
            <option value="hour">{{ t('usage.hour') }}</option>
            <option value="day">{{ t('usage.day') }}</option>
          </select>
        </label>
      </div>

      <div class="usage-workbench-grid">
        <article class="panel usage-trend-panel usage-workbench-trend">
          <header class="panel-header split-header">
            <div>
              <h2>{{ t('usage.tokenTrend') }}</h2>
              <p>{{ t('usage.recentSample', { count: analysisRecords.length }) }}</p>
            </div>
            <BarChart3 :size="18" aria-hidden="true" />
          </header>
          <div class="usage-line-wrap">
            <div v-if="!trendPoints.length" class="usage-chart-empty">
              <Activity :size="22" aria-hidden="true" />
              <strong>{{ t('usage.emptyWindow') }}</strong>
              <p>{{ t('usage.emptyWindowHelp') }}</p>
            </div>
            <template v-else>
              <svg viewBox="0 0 1000 220" class="usage-line-chart" role="img" :aria-label="t('usage.tokenTrend')">
                <line x1="42" y1="174" x2="962" y2="174" class="usage-axis" />
                <line x1="42" y1="104" x2="962" y2="104" class="usage-grid-line" />
                <line x1="42" y1="34" x2="962" y2="34" class="usage-grid-line" />
                <path v-if="trendInputPath" :d="trendInputPath" class="usage-line usage-line-input" />
                <path v-if="trendOutputPath" :d="trendOutputPath" class="usage-line usage-line-output" />
                <text v-for="item in trendAxisLabels" :key="`${item.x}-${item.label}`" :x="item.x" y="206" text-anchor="middle" class="usage-axis-label">
                  {{ item.label }}
                </text>
              </svg>
              <div class="usage-line-legend">
                <span><i class="usage-line-dot input"></i>{{ t('usage.inputTokens') }}</span>
                <span><i class="usage-line-dot output"></i>{{ t('usage.outputTokens') }}</span>
              </div>
            </template>
          </div>
        </article>

        <aside class="usage-workbench-side">
          <section class="panel usage-health-panel">
            <header class="panel-header">
              <Activity :size="17" aria-hidden="true" />
              <h2>{{ t('usage.operationalHealth') }}</h2>
            </header>
            <dl class="usage-health-list">
              <div><dt>{{ t('usage.errorRate') }}</dt><dd :class="errorRate > 5 ? 'health-danger' : 'health-good'">{{ formatPercent(errorRate) }}</dd></div>
              <div><dt>{{ t('usage.latency') }}</dt><dd>{{ formatLatency(summaryReport.avg_latency_ms) }}</dd></div>
              <div><dt>{{ t('usage.costPerRequest') }}</dt><dd>{{ formatCost(costPerRequest) }}</dd></div>
              <div><dt>{{ t('usage.topModel') }}</dt><dd>{{ topModel }}</dd></div>
              <div><dt>{{ t('usage.topKey') }}</dt><dd>{{ topKey }}</dd></div>
            </dl>
          </section>

          <section class="panel usage-health-panel">
            <header class="panel-header"><h2>{{ t('usage.quickAnalysis') }}</h2></header>
            <div class="usage-quick-actions">
              <button class="button secondary" type="button" @click="showView('analysis')">
                <BarChart3 :size="16" />
                {{ t('usage.openAnalysis') }}
              </button>
              <button class="button secondary" type="button" @click="showView('records')">
                <Activity :size="16" />
                {{ t('usage.openRecords') }}
              </button>
              <button class="button secondary" type="button" :disabled="!summaryReport.total_requests" @click="exportCSV">
                <Download :size="16" />
                {{ t('common.export') }}
              </button>
            </div>
          </section>
        </aside>
      </div>

      <section class="panel usage-recent-panel">
        <header class="panel-header split-header">
          <div>
            <h2>{{ t('usage.recentRequests') }}</h2>
            <p>{{ t('usage.recentRequestsHelp') }}</p>
          </div>
          <button class="button secondary tiny-button" type="button" @click="showView('records')">
            {{ t('usage.viewAllRecords') }}
            <ArrowRight :size="15" />
          </button>
        </header>
        <div class="panel-body table-scroll usage-workbench-table">
          <table class="data-table crud-table">
            <thead><tr><th>{{ t('audit.time') }}</th><th>{{ t('usage.model') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('usage.tokens') }}</th><th>{{ t('usage.cost') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in recentWorkbenchRecords" :key="item.id">
                <td>{{ formatTime(item.created_at) }}</td>
                <td><strong>{{ item.model || '-' }}</strong><span>{{ item.provider_id || '-' }}</span></td>
                <td><span class="pill" :class="statusClass(item.status)">{{ item.status }}</span></td>
                <td>{{ formatNumber(recordTokens(item)) }}</td>
                <td>{{ formatCost(item.usage_cost_micros) }}</td>
                <td><button class="icon-button" type="button" :aria-label="t('usage.openRecords')" @click="showView('records')"><ArrowRight :size="17" /></button></td>
              </tr>
              <tr v-if="!recentWorkbenchRecords.length"><td colspan="6" class="empty-cell">{{ t('usage.emptyWindow') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </section>

    <section v-if="activeView !== 'workbench'" class="panel usage-filter-panel" data-section="usage-filters">
      <div class="usage-filter-header">
        <div>
          <h2>{{ t('usage.filters') }}</h2>
          <p>{{ t('usage.filteredWindow') }}</p>
        </div>
        <div v-if="activeView === 'analysis'" class="usage-segmented">
          <button type="button" :class="{ active: distributionMetric === 'tokens' }" @click="distributionMetric = 'tokens'">
            {{ t('usage.tokens') }}
          </button>
          <button type="button" :class="{ active: distributionMetric === 'actual_cost' }" @click="distributionMetric = 'actual_cost'">
            {{ t('usage.cost') }}
          </button>
        </div>
      </div>

      <div class="usage-filter-grid">
        <label class="field usage-filter-wide">
          <span>{{ t('common.search') }}</span>
          <span class="search-box">
            <Search :size="17" />
            <input v-model="query" :placeholder="t('usage.searchPlaceholder')" @keyup.enter="applyFilters" />
          </span>
        </label>
        <label class="field">
          <span>{{ t('usage.timeRange') }}</span>
          <select v-model="timePreset" @change="handlePresetChange">
            <option value="24h">{{ t('usage.last24Hours') }}</option>
            <option value="7d">{{ t('usage.last7Days') }}</option>
            <option value="30d">{{ t('usage.last30Days') }}</option>
            <option value="custom">{{ t('usage.customRange') }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('usage.granularity') }}</span>
          <select v-model="granularity">
            <option value="hour">{{ t('usage.hour') }}</option>
            <option value="day">{{ t('usage.day') }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('usage.model') }}</span>
          <select v-model="modelFilter" @change="applyFilters">
            <option value="">{{ t('usage.allModels') }}</option>
            <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('providers.status') }}</span>
          <select v-model="statusFilter" @change="applyFilters">
            <option value="">{{ t('providers.allStatuses') }}</option>
            <option v-for="status in statusOptions" :key="status" :value="status">{{ status }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('usage.provider') }}</span>
          <select v-model="providerFilter" @change="applyFilters">
            <option value="">{{ t('usage.allProviders') }}</option>
            <option v-for="provider in providerOptions" :key="provider" :value="provider">{{ provider }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('usage.account') }}</span>
          <select v-model="accountFilter" @change="applyFilters">
            <option value="">{{ t('usage.allAccounts') }}</option>
            <option v-for="account in accountOptions" :key="account" :value="account">{{ account }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('usage.apiKey') }}</span>
          <select v-model="apiKeyFilter" @change="applyFilters">
            <option value="">{{ t('usage.allApiKeys') }}</option>
            <option v-for="apiKey in apiKeyOptions" :key="apiKey.value" :value="apiKey.value">{{ apiKey.label }}</option>
          </select>
        </label>
        <label class="field">
          <span>{{ t('common.from') }}</span>
          <input v-model="fromTime" type="datetime-local" @change="markCustomRange" />
        </label>
        <label class="field">
          <span>{{ t('common.to') }}</span>
          <input v-model="toTime" type="datetime-local" @change="markCustomRange" />
        </label>
        <div class="usage-filter-actions">
          <button class="button secondary" type="button" @click="applyFilters">{{ t('common.apply') }}</button>
        </div>
      </div>
    </section>

    <section v-if="activeView === 'analysis'" class="usage-chart-grid" data-section="usage-analysis">
      <article class="panel usage-chart-panel">
        <header class="panel-header split-header">
          <div>
            <h2>{{ t('usage.modelDistribution') }}</h2>
            <p>{{ metricLabel() }}</p>
          </div>
          <PieChart :size="18" />
        </header>
        <div class="usage-chart-body">
          <div class="usage-donut" :style="{ background: donutGradient(modelSeries) }">
            <div class="usage-donut-center">
              <strong>{{ formatSeriesValue(seriesTotal(modelSeries)) }}</strong>
              <span>{{ t('usage.total') }}</span>
            </div>
          </div>
          <div class="usage-legend">
            <div v-for="item in modelSeries" :key="item.label" class="usage-legend-row">
              <span class="usage-legend-dot" :style="{ background: item.color }"></span>
              <strong>{{ item.label }}</strong>
              <span>{{ formatSeriesValue(item.value) }}</span>
              <small>{{ formatPercent(item.share) }}</small>
            </div>
          </div>
        </div>
      </article>

      <article class="panel usage-chart-panel">
        <header class="panel-header split-header">
          <div>
            <h2>{{ t('usage.keyDistribution') }}</h2>
            <p>{{ metricLabel() }}</p>
          </div>
          <KeyRound :size="18" />
        </header>
        <div class="usage-chart-body">
          <div class="usage-donut" :style="{ background: donutGradient(keySeries) }">
            <div class="usage-donut-center">
              <strong>{{ formatSeriesValue(seriesTotal(keySeries)) }}</strong>
              <span>{{ t('admin.apiKeys') }}</span>
            </div>
          </div>
          <div class="usage-legend">
            <div v-for="item in keySeries" :key="item.label" class="usage-legend-row">
              <span class="usage-legend-dot" :style="{ background: item.color }"></span>
              <strong>{{ item.label }}</strong>
              <span>{{ formatSeriesValue(item.value) }}</span>
              <small>{{ formatPercent(item.share) }}</small>
            </div>
          </div>
        </div>
      </article>

      <article class="panel usage-chart-panel">
        <header class="panel-header split-header">
          <div>
            <h2>{{ t('usage.statusDistribution') }}</h2>
            <p>{{ t('usage.requests') }}</p>
          </div>
          <AlertTriangle :size="18" />
        </header>
        <div class="usage-chart-body">
          <div class="usage-donut" :style="{ background: donutGradient(statusSeries) }">
            <div class="usage-donut-center">
              <strong>{{ formatNumber(seriesTotal(statusSeries)) }}</strong>
              <span>{{ t('usage.records') }}</span>
            </div>
          </div>
          <div class="usage-legend">
            <div v-for="item in statusSeries" :key="item.label" class="usage-legend-row">
              <span class="usage-legend-dot" :style="{ background: item.color }"></span>
              <strong>{{ item.label }}</strong>
              <span>{{ formatNumber(item.requests) }}</span>
              <small>{{ formatPercent(item.share) }}</small>
            </div>
          </div>
        </div>
      </article>

    </section>

    <section v-if="activeView === 'records'" class="panel usage-detail-panel" data-section="usage-records">
      <div class="usage-tabs">
        <button
          v-for="tab in detailTabs"
          :key="tab.key"
          type="button"
          :class="{ active: activeTab === tab.key }"
          @click="selectTab(tab.key)"
        >
          <component :is="tab.icon" :size="15" />
          <span>{{ tab.label }}</span>
          <small>{{ tab.count }}</small>
        </button>
      </div>

      <div v-if="activeTab === 'usage'" class="panel-body table-scroll">
        <table class="data-table crud-table usage-record-table">
          <thead>
            <tr>
              <th>{{ t('audit.time') }}</th>
              <th>{{ t('usage.apiKey') }}</th>
              <th>{{ t('usage.model') }}</th>
              <th>{{ t('usage.route') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('usage.tokens') }}</th>
              <th>{{ t('usage.cost') }}</th>
              <th>{{ t('usage.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pageRecords" :key="item.id">
              <td>{{ formatTime(item.created_at) }}</td>
              <td>
                <strong>{{ item.api_fingerprint || '-' }}</strong>
                <span>{{ item.api_key_id || '-' }}</span>
              </td>
              <td>
                <strong>{{ item.model || '-' }}</strong>
                <span>{{ item.upstream_model || item.error_type || '-' }}</span>
              </td>
              <td>
                <strong>{{ item.provider_id || '-' }}</strong>
                <span>{{ item.provider_account_id || '-' }}</span>
              </td>
              <td><span class="pill" :class="statusClass(item.status)">{{ item.status }}</span></td>
              <td>
                <strong>{{ formatNumber(recordTokens(item)) }}</strong>
                <span>{{ formatNumber(item.input_tokens) }} / {{ formatNumber(item.output_tokens) }}</span>
              </td>
              <td>{{ formatCost(item.usage_cost_micros) }}</td>
              <td>{{ formatLatency(item.latency_ms) }}</td>
            </tr>
            <tr v-if="!pageRecords.length">
              <td colspan="8" class="empty-cell"></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="activeTab === 'errors'" class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('audit.time') }}</th>
              <th>{{ t('usage.model') }}</th>
              <th>{{ t('usage.apiKey') }}</th>
              <th>{{ t('usage.route') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('usage.errorType') }}</th>
              <th>{{ t('usage.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in errorRows" :key="item.id">
              <td>{{ formatTime(item.created_at) }}</td>
              <td>{{ item.model || '-' }}</td>
              <td>
                <strong>{{ item.api_fingerprint || '-' }}</strong>
                <span>{{ item.api_key_id || '-' }}</span>
              </td>
              <td>
                <strong>{{ item.provider_id || '-' }}</strong>
                <span>{{ item.provider_account_id || '-' }}</span>
              </td>
              <td><span class="pill" :class="statusClass(item.status)">{{ item.status }}</span></td>
              <td>{{ item.error_type || '-' }}</td>
              <td>{{ formatLatency(item.latency_ms) }}</td>
            </tr>
            <tr v-if="!errorRows.length">
              <td colspan="7" class="empty-cell"></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="activeTab === 'models'" class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('usage.model') }}</th>
              <th>{{ t('usage.requests') }}</th>
              <th>{{ t('usage.errorRequests') }}</th>
              <th>{{ t('usage.tokens') }}</th>
              <th>{{ t('usage.cost') }}</th>
              <th>{{ t('usage.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in modelRows" :key="item.label">
              <td><strong>{{ item.label }}</strong></td>
              <td>{{ formatNumber(item.requests) }}</td>
              <td>{{ formatNumber(item.errors) }}</td>
              <td>{{ formatNumber(item.tokens) }}</td>
              <td>{{ formatCost(item.usage_cost_micros) }}</td>
              <td>{{ formatLatency(item.avg_latency_ms) }}</td>
            </tr>
            <tr v-if="!modelRows.length">
              <td colspan="6" class="empty-cell"></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="activeTab === 'keys'" class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('usage.apiKey') }}</th>
              <th>{{ t('costAllocation.scope') }}</th>
              <th>{{ t('usage.requests') }}</th>
              <th>{{ t('usage.errorRequests') }}</th>
              <th>{{ t('usage.tokens') }}</th>
              <th>{{ t('usage.cost') }}</th>
              <th>{{ t('costAllocation.share') }}</th>
              <th>{{ t('usage.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in keyRows" :key="`${item.label}:${item.scope}`">
              <td><strong>{{ item.label }}</strong></td>
              <td>{{ item.scope }}</td>
              <td>{{ formatNumber(item.requests) }}</td>
              <td>{{ formatNumber(item.errors) }}</td>
              <td>{{ formatNumber(item.tokens) }}</td>
              <td>{{ formatCost(item.usage_cost_micros) }}</td>
              <td>{{ formatPercent(summaryReport.total_usage_cost_micros ? (item.usage_cost_micros / summaryReport.total_usage_cost_micros) * 100 : 0) }}</td>
              <td>{{ formatLatency(item.avg_latency_ms) }}</td>
            </tr>
            <tr v-if="!keyRows.length">
              <td colspan="8" class="empty-cell"></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="activeView === 'records' && activeTab === 'usage'" class="pagination-bar">
      <button class="button secondary" type="button" :disabled="!canPrevious || pageLoading" @click="previousPage">
        {{ t('common.previous') }}
      </button>
      <span>{{ t('common.page') }} {{ pageNumber }}</span>
      <select v-model.number="pageSize" :disabled="pageLoading" @change="changePageSize">
        <option :value="25">25</option>
        <option :value="50">50</option>
        <option :value="100">100</option>
      </select>
      <button class="button secondary" type="button" :disabled="!canNext || pageLoading" @click="nextPage">
        {{ t('common.next') }}
      </button>
    </section>
  </main>
</template>

<style scoped>
.usage-page-actions {
  flex-wrap: wrap;
}

.usage-primary-tabs {
  display: flex;
  min-height: 42px;
  gap: 22px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: none;
}

.usage-primary-tabs::-webkit-scrollbar {
  display: none;
}

.usage-primary-tab {
  position: relative;
  min-height: 42px;
  flex: 0 0 auto;
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 13px;
  font-weight: 650;
}

.usage-primary-tab::after {
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  background: transparent;
  content: "";
}

.usage-primary-tab:hover,
.usage-primary-tab.active {
  color: var(--text);
}

.usage-primary-tab.active::after {
  background: var(--primary-500);
}

.usage-primary-tab:focus-visible {
  border-radius: var(--radius-sm);
  outline: 3px solid var(--focus-ring);
  outline-offset: -3px;
}

.usage-workbench {
  display: grid;
  gap: 16px;
}

.usage-workbench-metrics .metric-card {
  min-height: 106px;
  border-radius: var(--radius-sm);
}

.usage-workbench-metrics .metric-card > div {
  min-width: 0;
}

.usage-workbench-context {
  display: grid;
  grid-template-columns: minmax(240px, 1fr) minmax(150px, 190px) minmax(150px, 190px);
  gap: 16px;
  align-items: end;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-subtle);
}

.usage-workbench-context > div,
.usage-workbench-context label {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.usage-workbench-context strong {
  color: var(--text);
  font-size: 13px;
}

.usage-workbench-context span {
  color: var(--text-muted);
  font-size: 11px;
}

.usage-workbench-context label > span {
  color: var(--text-secondary);
  font-weight: 650;
}

.usage-workbench-grid {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(260px, 0.72fr);
  gap: 16px;
  align-items: stretch;
}

.usage-workbench-grid .panel,
.usage-recent-panel {
  border-radius: var(--radius-sm);
}

.usage-workbench-trend {
  min-height: 356px;
  grid-column: auto;
}

.usage-workbench-side {
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  gap: 16px;
}

.usage-health-panel {
  min-height: 0;
}

.usage-health-panel .panel-header {
  justify-content: flex-start;
}

.usage-health-list {
  display: grid;
  margin: 0;
  padding: 4px 16px 12px;
}

.usage-health-list > div {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}

.usage-health-list > div:last-child {
  border-bottom: 0;
}

.usage-health-list dt {
  color: var(--text-muted);
  font-size: 12px;
}

.usage-health-list dd {
  max-width: 58%;
  margin: 0;
  overflow: hidden;
  color: var(--text);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.usage-health-list .health-good {
  color: var(--success);
}

.usage-health-list .health-danger {
  color: var(--danger);
}

.usage-quick-actions {
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
  padding: 12px 16px 16px;
}

.usage-quick-actions .button {
  width: 100%;
  justify-content: flex-start;
}

.usage-chart-empty {
  display: grid;
  min-height: 236px;
  place-content: center;
  justify-items: center;
  padding: 24px;
  color: var(--text-muted);
  text-align: center;
}

.usage-chart-empty strong {
  margin-top: 10px;
  color: var(--text);
  font-size: 13px;
}

.usage-chart-empty p {
  max-width: 400px;
  margin: 4px 0 0;
  font-size: 12px;
}

.usage-recent-panel .panel-header {
  align-items: center;
}

.usage-workbench-table td strong,
.usage-workbench-table td span {
  display: block;
}

.usage-workbench-table td span:not(.pill) {
  margin-top: 2px;
  color: var(--text-muted);
  font-size: 11px;
}

.usage-workbench-table .icon-button {
  width: 32px;
  height: 32px;
}

@media (max-width: 1100px) {
  .usage-workbench-grid {
    grid-template-columns: minmax(0, 1fr) minmax(240px, 0.58fr);
  }
}

@media (max-width: 900px) {
  .usage-workbench-grid {
    grid-template-columns: 1fr;
  }

  .usage-workbench-side {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: auto;
  }
}

@media (max-width: 640px) {
  .usage-primary-tabs {
    gap: 18px;
  }

  .usage-page-actions .button {
    flex: 1;
  }

  .usage-workbench-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .usage-workbench-metrics .metric-card {
    min-height: 112px;
    padding: 14px;
  }

  .usage-workbench-metrics .metric-card strong {
    font-size: 22px;
  }

  .usage-workbench-context {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .usage-workbench-context > div {
    grid-column: 1 / -1;
  }

  .usage-workbench-side {
    grid-template-columns: 1fr;
  }

  .usage-workbench-trend {
    min-height: 310px;
  }

  .usage-line-chart {
    min-width: 600px;
  }

  .usage-workbench-trend .usage-line-wrap {
    overflow-x: auto;
  }

  .usage-recent-panel .panel-header {
    display: grid;
  }

  .usage-recent-panel .tiny-button {
    width: 100%;
  }

  .usage-workbench-table table {
    min-width: 0;
  }

  .usage-workbench-table th:nth-child(1),
  .usage-workbench-table td:nth-child(1),
  .usage-workbench-table th:nth-child(5),
  .usage-workbench-table td:nth-child(5) {
    display: none;
  }

  .usage-workbench-table th,
  .usage-workbench-table td {
    padding-right: 9px;
    padding-left: 9px;
  }
}
</style>
