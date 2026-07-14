<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Activity,
  BadgeDollarSign,
  FileCheck2,
  FlaskConical,
  Plus,
  RefreshCw,
  Route,
  Save,
  Settings2,
  ShieldCheck,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  actOnEffectivePricingDecision,
  createProcurementPrice,
  createProviderBillingLine,
  evaluateEffectivePricingDecision,
  getEffectivePricingDecisions,
  getEffectivePricingReport,
  getProviderAccounts,
  getProviderCacheCapabilities,
  getProviderCacheProbeRuns,
  runProviderCacheProbe,
  updateEffectivePricingPolicy
} from '@/api/control'
import type {
  CacheProbeRequest,
  EffectivePricingDecision,
  EffectivePricingPolicyRequest,
  EffectivePricingReport,
  EffectivePricingReportRow,
  ProcurementPriceRequest,
  ProviderAccount,
  ProviderBillingLineRequest,
  ProviderCacheCapability,
  ProviderCacheProbeRun
} from '@/types'

type PricingTab = 'pricing' | 'cache' | 'switches' | 'probes'
type DialogKind = 'price' | 'billing' | 'policy' | 'decision' | 'probe' | null

const { t } = useI18n()
const activeTab = ref<PricingTab>('pricing')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const report = ref<EffectivePricingReport | null>(null)
const capabilities = ref<ProviderCacheCapability[]>([])
const probes = ref<ProviderCacheProbeRun[]>([])
const decisions = ref<EffectivePricingDecision[]>([])
const accounts = ref<ProviderAccount[]>([])
const selectedRow = ref<EffectivePricingReportRow | null>(null)
const dialog = ref<DialogKind>(null)
const probeBudgetConfirmed = ref(false)
const modelFilter = ref('')
const protocolFilter = ref('openai_chat_completions')
const windowHours = ref(24)

const tabs: Array<{ id: PricingTab; icon: typeof BadgeDollarSign }> = [
  { id: 'pricing', icon: BadgeDollarSign },
  { id: 'cache', icon: Activity },
  { id: 'switches', icon: Route },
  { id: 'probes', icon: FlaskConical }
]

const priceForm = reactive<ProcurementPriceRequest>({
  provider_id: '', provider_account_id: '', upstream_model: '', protocol: 'openai_chat_completions', currency: 'USD',
  uncached_input_micros_per_1m_tokens: 0, cache_read_micros_per_1m_tokens: 0,
  cache_write_5m_micros_per_1m_tokens: 0, cache_write_1h_micros_per_1m_tokens: 0,
  output_micros_per_1m_tokens: 0, request_micros: 0,
  reference_input_micros_per_1m_tokens: 0, reference_output_micros_per_1m_tokens: 0,
  quoted_multiplier: 0, recharge_multiplier: 1, source_kind: 'manual', source_reference: '',
  evidence_hash: '', confidence: 'estimated', status: 'active', effective_from: '', expires_at: ''
})
const billingForm = reactive<ProviderBillingLineRequest>({
  provider_id: '', provider_account_id: '', external_line_id: '', external_request_id: '', usage_record_id: '',
  upstream_model: '', currency: 'USD', amount_micros: 0, source_kind: 'manual', confidence: 'unknown',
  raw_payload_hash: '', usage_started_at: '', usage_ended_at: ''
})
const policyForm = reactive<EffectivePricingPolicyRequest>({
  mode: 'observe_only', window_hours: 24, min_sample_count: 200, min_metrics_coverage: 0.8,
  min_billing_consistency: 0.95, min_cost_improvement: 0.08, max_error_rate_regression: 0.005,
  min_cache_hit_rate_improvement: 0.1, min_affinity_improvement: 0.1, max_cache_tiebreak_cost_regression: 0.02,
  max_p95_latency_regression: 0.2, canary_percent: 5, supplier_affinity_ttl_seconds: 86400,
  account_affinity_ttl_seconds: 1800, probe_enabled: false, probe_daily_token_budget: 100000,
  probe_daily_cost_budget_micros: 10000000, probe_cooldown_seconds: 3600
})
const decisionForm = reactive({
  model: '', upstream_model: '', protocol: 'openai_chat_completions',
  current_provider_account_id: '', candidate_provider_account_id: ''
})
const probeForm = reactive<CacheProbeRequest>({
  provider_account_id: '', upstream_model: '', protocol: 'openai_chat_completions',
  prefix_tokens: 2048, max_cost_micros: 100000
})

const rows = computed(() => report.value?.rows || [])
const metrics = computed(() => {
  const comparable = rows.value.filter((row) => row.effective_cost_micros_per_1m > 0)
  const best = comparable[0]
  const requests = rows.value.reduce((sum, row) => sum + row.request_count, 0)
  const weightedCoverage = requests > 0 ? rows.value.reduce((sum, row) => sum + row.metrics_coverage * row.request_count, 0) / requests : 0
  return [
    { label: t('effectivePricing.metrics.bestCost'), value: best ? formatMoneyMicros(best.effective_cost_micros_per_1m, best.currency) : '-', sub: best?.provider_account_name || '-', icon: BadgeDollarSign },
    { label: t('effectivePricing.metrics.cacheCoverage'), value: formatPercent(weightedCoverage), sub: `${formatNumber(requests)} ${t('usage.requests')}`, icon: Activity },
    { label: t('effectivePricing.metrics.comparable'), value: String(comparable.length), sub: `${rows.value.length} ${t('effectivePricing.suppliers')}`, icon: ShieldCheck },
    { label: t('effectivePricing.metrics.decisions'), value: String(decisions.value.length), sub: report.value?.policy.mode || 'observe_only', icon: Route }
  ]
})
const capabilityRows = computed(() => rows.value.map((row) => ({ row, capability: capabilityFor(row) })))
const accountOptions = computed(() => accounts.value.filter((account) => account.status === 'active'))
const decisionUpstreamModelOptions = computed(() => [...new Set(rows.value.map((row) => row.upstream_model))])
const decisionProtocolOptions = computed(() => [...new Set(rows.value
  .filter((row) => row.upstream_model === decisionForm.upstream_model)
  .map((row) => row.protocol))])
const decisionAccountRows = computed(() => comparableDecisionRows(decisionForm.upstream_model, decisionForm.protocol))
const decisionCandidateRows = computed(() => decisionAccountRows.value.filter((row) => row.provider_account_id !== decisionForm.current_provider_account_id))
const hasComparableDecisionGroup = computed(() => firstComparableDecisionRows().length >= 2)
const decisionFormValid = computed(() => decisionForm.model.trim() !== '' && decisionAccountRows.value.length >= 2 &&
  decisionForm.current_provider_account_id !== '' && decisionForm.candidate_provider_account_id !== '' &&
  decisionForm.current_provider_account_id !== decisionForm.candidate_provider_account_id)

function capabilityFor(row: EffectivePricingReportRow): ProviderCacheCapability | undefined {
  return capabilities.value.find((item) => item.provider_account_id === row.provider_account_id && item.upstream_model === row.upstream_model && item.protocol === row.protocol)
}

function formatNumber(value: number): string { return new Intl.NumberFormat().format(value) }
function formatPercent(value: number): string { return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value * 100)}%` }
function formatOptionalPercent(value?: number): string { return value === undefined ? '-' : formatPercent(value) }
function formatCacheSavings(row?: EffectivePricingReportRow): string { return row?.cache_economics_available ? formatPercent(row.cache_savings_rate) : '-' }
function formatMultiplier(value: number): string { return value > 0 ? `${value.toFixed(2)}x` : '-' }
function formatMoneyMicros(value: number, currency = 'USD'): string {
  if (!value) return '-'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'USD', maximumFractionDigits: 4 }).format(value / 1_000_000)
}
function formatDate(value?: string): string { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) : '-' }
function statusClass(value: string): string {
  if (['exact', 'derived', 'matched', 'preferred', 'active', 'billed_verified', 'succeeded', 'verified'].includes(value)) return 'status-success'
  if (['degraded', 'fragmented', 'rolled_back', 'failed', 'reduce_weight'].includes(value)) return 'status-danger'
  if (['estimated', 'pending', 'canary', 'recommended', 'observe', 'ambiguous'].includes(value)) return 'status-warning'
  return ''
}
function accountName(id: string): string { return accounts.value.find((account) => account.id === id)?.name || id || '-' }
function reportRowForDecision(decision: EffectivePricingDecision, accountID: string): EffectivePricingReportRow | undefined {
  return rows.value.find((row) => row.provider_account_id === accountID && row.upstream_model === decision.upstream_model && row.protocol === decision.protocol)
}
function formatLatency(value?: number): string { return value && value > 0 ? `${formatNumber(value)} ms` : '-' }

function comparableDecisionRows(upstreamModel: string, protocol: string): EffectivePricingReportRow[] {
  const seen = new Set<string>()
  return rows.value.filter((row) => {
    if (row.upstream_model !== upstreamModel || row.protocol !== protocol || seen.has(row.provider_account_id)) return false
    seen.add(row.provider_account_id)
    return true
  })
}

function firstComparableDecisionRows(): EffectivePricingReportRow[] {
  for (const row of rows.value) {
    const comparable = comparableDecisionRows(row.upstream_model, row.protocol)
    if (comparable.length >= 2) return comparable
  }
  return []
}

function hasComparableAccount(row: EffectivePricingReportRow): boolean {
  return comparableDecisionRows(row.upstream_model, row.protocol).length >= 2
}

function resetDecisionAccounts(preferredCandidateID = '') {
  const comparable = decisionAccountRows.value
  const candidate = comparable.find((row) => row.provider_account_id === preferredCandidateID) || comparable[0]
  const current = [...comparable].reverse().find((row) => row.provider_account_id !== candidate?.provider_account_id)
  decisionForm.current_provider_account_id = current?.provider_account_id || ''
  decisionForm.candidate_provider_account_id = candidate?.provider_account_id || ''
}

function decisionUpstreamModelChanged() {
  if (!decisionProtocolOptions.value.includes(decisionForm.protocol)) {
    decisionForm.protocol = decisionProtocolOptions.value[0] || ''
  }
  resetDecisionAccounts()
}

function decisionCurrentAccountChanged() {
  if (!decisionCandidateRows.value.some((row) => row.provider_account_id === decisionForm.candidate_provider_account_id)) {
    decisionForm.candidate_provider_account_id = decisionCandidateRows.value[0]?.provider_account_id || ''
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [nextReport, nextCapabilities, nextProbes, nextDecisions, nextAccounts] = await Promise.all([
      getEffectivePricingReport({ model: modelFilter.value.trim() || undefined, protocol: protocolFilter.value || undefined, window_hours: windowHours.value }),
      getProviderCacheCapabilities(), getProviderCacheProbeRuns(100), getEffectivePricingDecisions(), getProviderAccounts()
    ])
    report.value = nextReport
    capabilities.value = nextCapabilities
    probes.value = nextProbes
    decisions.value = nextDecisions
    accounts.value = nextAccounts
    Object.assign(policyForm, nextReport.policy)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

function openPriceDialog(row?: EffectivePricingReportRow) {
  const account = accounts.value.find((item) => item.id === row?.provider_account_id) || accountOptions.value[0]
  Object.assign(priceForm, {
    provider_id: row?.provider_id || account?.provider_id || '', provider_account_id: row?.provider_account_id || account?.id || '',
    upstream_model: row?.upstream_model || '', protocol: row?.protocol || protocolFilter.value, currency: row?.currency || 'USD',
    uncached_input_micros_per_1m_tokens: 0, cache_read_micros_per_1m_tokens: 0,
    cache_write_5m_micros_per_1m_tokens: 0, cache_write_1h_micros_per_1m_tokens: 0,
    output_micros_per_1m_tokens: 0, request_micros: 0, reference_input_micros_per_1m_tokens: 0,
    reference_output_micros_per_1m_tokens: 0, quoted_multiplier: row?.quoted_multiplier || 0,
    recharge_multiplier: 1, source_kind: 'manual', source_reference: '', evidence_hash: '', confidence: 'estimated', status: 'active', effective_from: '', expires_at: ''
  })
  dialog.value = 'price'
}
function openBillingDialog(row?: EffectivePricingReportRow) {
  const account = accounts.value.find((item) => item.id === row?.provider_account_id) || accountOptions.value[0]
  Object.assign(billingForm, {
    provider_id: row?.provider_id || account?.provider_id || '', provider_account_id: row?.provider_account_id || account?.id || '',
    external_line_id: '', external_request_id: '', usage_record_id: '', upstream_model: row?.upstream_model || '',
    currency: row?.currency || 'USD', amount_micros: 0, source_kind: 'manual', confidence: 'unknown', raw_payload_hash: '', usage_started_at: '', usage_ended_at: ''
  })
  dialog.value = 'billing'
}
function openDecisionDialog(row?: EffectivePricingReportRow) {
  const comparable = row ? comparableDecisionRows(row.upstream_model, row.protocol) : firstComparableDecisionRows()
  const source = row || comparable[0]
  Object.assign(decisionForm, {
    model: '', upstream_model: source?.upstream_model || '', protocol: source?.protocol || '',
    current_provider_account_id: '', candidate_provider_account_id: ''
  })
  resetDecisionAccounts(row?.provider_account_id)
  dialog.value = 'decision'
}
function openProbeDialog(row?: EffectivePricingReportRow) {
  const source = row || rows.value[0]
  const account = accounts.value.find((item) => item.id === source?.provider_account_id) || accountOptions.value[0]
  Object.assign(probeForm, {
		provider_account_id: source?.provider_account_id || account?.id || '',
		upstream_model: source?.upstream_model || modelFilter.value || account?.models?.[0] || '',
    protocol: source?.protocol || protocolFilter.value,
    prefix_tokens: 2048,
    max_cost_micros: 100000
  })
  probeBudgetConfirmed.value = false
  dialog.value = 'probe'
}
function accountChanged(target: ProcurementPriceRequest | ProviderBillingLineRequest) {
  target.provider_id = accounts.value.find((account) => account.id === target.provider_account_id)?.provider_id || ''
}

async function saveDialog() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (dialog.value === 'price') await createProcurementPrice(priceForm)
    if (dialog.value === 'billing') await createProviderBillingLine(billingForm)
    if (dialog.value === 'policy') await updateEffectivePricingPolicy(policyForm)
    if (dialog.value === 'decision') await evaluateEffectivePricingDecision(decisionForm)
    if (dialog.value === 'probe') await runProviderCacheProbe(probeForm)
    dialog.value = null
    message.value = t('common.saved')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function decisionAction(decision: EffectivePricingDecision, action: string) {
  error.value = ''
  try {
    await actOnEffectivePricingDecision(decision.id, action, report.value?.policy.canary_percent || 5)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>

<template>
  <main class="content effective-pricing-page">
    <section class="page-header">
      <div><h1>{{ t('effectivePricing.title') }}</h1><p>{{ t('effectivePricing.subtitle') }}</p></div>
      <div class="row-actions">
        <button class="button secondary" type="button" @click="dialog = 'policy'"><Settings2 :size="17" />{{ t('effectivePricing.policy') }}</button>
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
      </div>
    </section>
    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="effective-tabs" role="tablist" :aria-label="t('effectivePricing.title')">
      <button v-for="tab in tabs" :key="tab.id" type="button" :class="{ active: activeTab === tab.id }" @click="activeTab = tab.id">
        <component :is="tab.icon" :size="16" />{{ t(`effectivePricing.tabs.${tab.id}`) }}
      </button>
    </section>
    <section class="effective-filters">
      <label><span>{{ t('usage.model') }}</span><input v-model="modelFilter" placeholder="claude-3-5-sonnet" @keyup.enter="load" /></label>
      <label><span>{{ t('effectivePricing.protocol') }}</span><select v-model="protocolFilter" @change="load"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option></select></label>
      <label><span>{{ t('effectivePricing.window') }}</span><select v-model.number="windowHours" @change="load"><option :value="24">24h</option><option :value="168">7d</option><option :value="720">30d</option></select></label>
      <span class="pill" :class="statusClass(report?.policy.mode || '')">{{ report?.policy.mode || 'observe_only' }}</span>
    </section>

    <section class="metric-grid effective-metrics">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card"><span class="metric-icon"><component :is="metric.icon" :size="20" /></span><div><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.sub }}</small></div></article>
    </section>

    <template v-if="activeTab === 'pricing'">
      <section class="panel effective-panel">
        <header class="panel-header split-header"><div><h2>{{ t('effectivePricing.priceComparison') }}</h2><p>{{ t('effectivePricing.priceComparisonHelp') }}</p></div><div class="row-actions"><button class="button secondary" type="button" @click="openBillingDialog()"><FileCheck2 :size="16" />{{ t('effectivePricing.importBill') }}</button><button class="button" type="button" @click="openPriceDialog()"><Plus :size="16" />{{ t('effectivePricing.addPrice') }}</button></div></header>
        <div class="panel-body table-scroll ep-table"><table class="data-table"><thead><tr><th>{{ t('effectivePricing.supplierAccount') }}</th><th>{{ t('effectivePricing.quoted') }}</th><th>{{ t('effectivePricing.billed') }}</th><th>{{ t('effectivePricing.effective') }}</th><th>{{ t('effectivePricing.effectiveCost') }}</th><th>{{ t('effectivePricing.cacheHit') }}</th><th>{{ t('effectivePricing.quality') }}</th><th>{{ t('effectivePricing.coverage') }}</th><th>{{ t('effectivePricing.confidence') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody>
          <tr v-for="row in rows" :key="`${row.provider_account_id}:${row.upstream_model}:${row.protocol}`"><td :data-label="t('effectivePricing.supplierAccount')"><strong>{{ row.provider_name || row.provider_id }}</strong><span>{{ row.provider_account_name || row.provider_account_id }} · {{ row.upstream_model }}</span></td><td :data-label="t('effectivePricing.quoted')"><strong>{{ formatMultiplier(row.quoted_multiplier) }}</strong></td><td :data-label="t('effectivePricing.billed')"><strong>{{ formatMultiplier(row.billed_multiplier) }}</strong><span>{{ formatPercent(row.billing_consistency_rate) }}</span></td><td :data-label="t('effectivePricing.effective')"><strong>{{ formatMultiplier(row.effective_multiplier) }}</strong></td><td :data-label="t('effectivePricing.effectiveCost')"><strong>{{ formatMoneyMicros(row.effective_cost_micros_per_1m, row.currency) }}</strong><span>{{ t('effectivePricing.uncachedEquivalent') }} {{ formatMoneyMicros(row.uncached_cost_micros_per_1m, row.currency) }}</span><span>{{ t('effectivePricing.cacheSavings') }} {{ formatCacheSavings(row) }}</span></td><td :data-label="t('effectivePricing.cacheHit')"><strong>{{ formatPercent(row.cache_token_hit_rate) }}</strong><span>{{ formatNumber(row.request_count) }} {{ t('usage.requests') }}</span></td><td :data-label="t('effectivePricing.quality')"><strong>{{ formatPercent(row.error_rate) }} {{ t('effectivePricing.errorRate') }}</strong><span>P95 {{ formatLatency(row.p95_latency_ms) }}</span></td><td :data-label="t('effectivePricing.coverage')">{{ formatPercent(row.metrics_coverage) }}</td><td :data-label="t('effectivePricing.confidence')"><span class="pill" :class="statusClass(row.cost_confidence)">{{ row.cost_confidence }}</span></td><td :data-label="t('common.actions')"><div class="row-actions"><button class="button ghost" type="button" @click="selectedRow = row">{{ t('effectivePricing.evidence') }}</button><button class="button ghost" type="button" :disabled="!hasComparableAccount(row)" @click="openDecisionDialog(row)">{{ t('effectivePricing.evaluate') }}</button></div></td></tr>
          <tr v-if="!rows.length"><td colspan="10" class="empty-cell">{{ loading ? t('common.loading') : t('effectivePricing.empty') }}</td></tr>
        </tbody></table></div>
      </section>
    </template>

    <template v-else-if="activeTab === 'cache'">
      <section class="panel effective-panel"><header class="panel-header split-header"><div><h2>{{ t('effectivePricing.cacheQuality') }}</h2><p>{{ t('effectivePricing.cacheQualityHelp') }}</p></div></header><div class="panel-body cache-grid">
        <article v-for="item in capabilityRows" :key="item.row.provider_account_id" class="cache-row"><div><strong>{{ item.row.provider_name || item.row.provider_id }}</strong><span>{{ item.row.provider_account_name || item.row.provider_account_id }} · {{ item.row.upstream_model }}</span></div><div><small>{{ t('effectivePricing.cacheHit') }}</small><strong>{{ formatPercent(item.row.cache_token_hit_rate) }}</strong></div><div><small>{{ t('effectivePricing.coverage') }}</small><strong>{{ formatPercent(item.row.metrics_coverage) }}</strong></div><div><small>{{ t('effectivePricing.affinity') }}</small><strong>{{ formatPercent(item.row.affinity_consistency_rate) }}</strong></div><span class="pill" :class="statusClass(item.row.cache_support_status)">{{ item.row.cache_support_status }}</span><span class="pill" :class="statusClass(item.row.pool_affinity_grade)">{{ item.row.pool_affinity_grade }}</span></article>
        <div v-if="!capabilityRows.length" class="empty-cell">{{ t('effectivePricing.empty') }}</div>
      </div></section>
    </template>

    <template v-else-if="activeTab === 'switches'">
      <div class="switch-head">
        <div><h2>{{ t('effectivePricing.switchCenter') }}</h2><p>{{ t('effectivePricing.switchHelp') }}</p></div>
        <button class="button" type="button" :disabled="!hasComparableDecisionGroup" @click="openDecisionDialog()"><Plus :size="16" />{{ t('effectivePricing.newEvaluation') }}</button>
      </div>
      <section class="decision-grid">
        <article v-for="decision in decisions" :key="decision.id" class="panel decision-card">
          <header><div><span class="pill" :class="statusClass(decision.status)">{{ decision.status }}</span><h3>{{ decision.model }}</h3><p>{{ decision.upstream_model || '-' }} · {{ decision.protocol }} · {{ decision.id }}</p></div><strong>{{ formatPercent(decision.cost_improvement) }}</strong></header>
          <div class="decision-compare">
            <div><small>{{ t('effectivePricing.current') }}</small><strong>{{ accountName(decision.current_provider_account_id) }}</strong><span>{{ formatMoneyMicros(decision.current_cost_micros_per_1m) }}</span><span>{{ t('effectivePricing.cacheHit') }} {{ formatOptionalPercent(reportRowForDecision(decision, decision.current_provider_account_id)?.cache_token_hit_rate) }}</span><span>{{ t('effectivePricing.cacheSavings') }} {{ formatCacheSavings(reportRowForDecision(decision, decision.current_provider_account_id)) }}</span><span>{{ t('effectivePricing.errorRate') }} {{ formatOptionalPercent(reportRowForDecision(decision, decision.current_provider_account_id)?.error_rate) }}</span><span>P95 {{ formatLatency(reportRowForDecision(decision, decision.current_provider_account_id)?.p95_latency_ms) }}</span></div>
            <div><small>{{ t('effectivePricing.candidate') }}</small><strong>{{ accountName(decision.candidate_provider_account_id) }}</strong><span>{{ formatMoneyMicros(decision.candidate_cost_micros_per_1m) }}</span><span>{{ t('effectivePricing.cacheHit') }} {{ formatOptionalPercent(reportRowForDecision(decision, decision.candidate_provider_account_id)?.cache_token_hit_rate) }}</span><span>{{ t('effectivePricing.cacheSavings') }} {{ formatCacheSavings(reportRowForDecision(decision, decision.candidate_provider_account_id)) }}</span><span>{{ t('effectivePricing.errorRate') }} {{ formatOptionalPercent(reportRowForDecision(decision, decision.candidate_provider_account_id)?.error_rate) }}</span><span>P95 {{ formatLatency(reportRowForDecision(decision, decision.candidate_provider_account_id)?.p95_latency_ms) }}</span></div>
          </div>
          <p v-if="decision.reason_codes.length" class="decision-reasons">{{ decision.reason_codes.join(' · ') }}</p>
          <footer class="row-actions"><button v-if="decision.status === 'recommended'" class="button" type="button" @click="decisionAction(decision, 'approve_canary')">{{ t('effectivePricing.startCanary') }}</button><button v-if="decision.status === 'canary'" class="button" type="button" @click="decisionAction(decision, 'activate')">{{ t('effectivePricing.activate') }}</button><button v-if="['canary','active','degraded'].includes(decision.status)" class="button secondary" type="button" @click="decisionAction(decision, 'rollback')">{{ t('effectivePricing.rollback') }}</button></footer>
        </article>
        <div v-if="!decisions.length" class="panel empty-cell">{{ t('effectivePricing.noDecisions') }}</div>
      </section>
    </template>

    <template v-else>
      <section class="panel effective-panel"><header class="panel-header split-header"><div><h2>{{ t('effectivePricing.probeRecords') }}</h2><p>{{ t('effectivePricing.probeHelp') }}</p></div><button class="button secondary" type="button" :disabled="!report?.policy.probe_enabled || !accountOptions.length" :title="report?.policy.probe_enabled ? '' : t('effectivePricing.probeBudgetRequired')" @click="openProbeDialog()"><FlaskConical :size="16" />{{ t('effectivePricing.runProbe') }}</button></header><div class="panel-body probe-list"><article v-for="probe in probes" :key="probe.id" class="probe-row"><div><strong>{{ accountName(probe.provider_account_id) }}</strong><span>{{ probe.upstream_model }} · {{ probe.probe_series_id }}</span><span v-if="probe.failure_reason">{{ probe.failure_reason }}</span></div><div><small>{{ t('effectivePricing.reuseRead') }}</small><strong>{{ formatNumber(probe.reuse_cache_read_tokens) }}</strong></div><div><small>TTFT</small><strong>{{ probe.reuse_ttft_ms }} ms</strong></div><div><small>{{ t('usage.cost') }}</small><strong>{{ formatMoneyMicros(probe.estimated_cost_micros) }}</strong></div><span class="pill" :class="statusClass(probe.status)">{{ probe.status }}</span><time>{{ formatDate(probe.started_at) }}</time></article><div v-if="!probes.length" class="empty-cell">{{ t('effectivePricing.noProbes') }}</div></div></section>
    </template>

    <div v-if="selectedRow" class="drawer-backdrop" @click.self="selectedRow = null">
      <aside class="evidence-drawer" role="dialog" aria-modal="true">
        <header><div><h2>{{ selectedRow.provider_name || selectedRow.provider_id }}</h2><p>{{ selectedRow.provider_account_name || selectedRow.provider_account_id }} · {{ selectedRow.upstream_model }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="selectedRow = null"><X :size="18" /></button></header>
        <div class="evidence-body">
          <div class="evidence-grid"><div><small>{{ t('effectivePricing.quoted') }}</small><strong>{{ formatMultiplier(selectedRow.quoted_multiplier) }}</strong></div><div><small>{{ t('effectivePricing.effective') }}</small><strong>{{ formatMultiplier(selectedRow.effective_multiplier) }}</strong></div><div><small>{{ t('effectivePricing.effectiveCost') }}</small><strong>{{ formatMoneyMicros(selectedRow.effective_cost_micros_per_1m, selectedRow.currency) }}</strong></div><div><small>{{ t('effectivePricing.uncachedEquivalent') }}</small><strong>{{ formatMoneyMicros(selectedRow.uncached_cost_micros_per_1m, selectedRow.currency) }}</strong></div><div><small>{{ t('effectivePricing.cacheSavings') }}</small><strong>{{ formatCacheSavings(selectedRow) }}</strong></div><div><small>{{ t('effectivePricing.cacheSavingsAmount') }}</small><strong>{{ selectedRow.cache_economics_available ? formatMoneyMicros(selectedRow.cache_savings_micros_per_1m, selectedRow.currency) : '-' }}</strong></div><div><small>{{ t('effectivePricing.confidence') }}</small><strong>{{ selectedRow.cost_confidence }}</strong></div><div><small>{{ t('effectivePricing.cacheHit') }}</small><strong>{{ formatPercent(selectedRow.cache_token_hit_rate) }}</strong></div><div><small>{{ t('effectivePricing.affinity') }}</small><strong>{{ formatPercent(selectedRow.affinity_consistency_rate) }}</strong></div><div><small>{{ t('effectivePricing.errorRate') }}</small><strong>{{ formatPercent(selectedRow.error_rate) }}</strong></div><div><small>{{ t('effectivePricing.p95Latency') }}</small><strong>{{ formatLatency(selectedRow.p95_latency_ms) }}</strong></div></div>
          <div class="evidence-section"><h3>{{ t('effectivePricing.evidence') }}</h3><p>Price: {{ selectedRow.price_id || '-' }}</p><p>Window: {{ formatDate(report?.window_start) }} → {{ formatDate(report?.window_end) }}</p><p>Reasons: {{ selectedRow.reason_codes.join(' · ') || '-' }}</p></div>
        </div>
        <footer><button class="button secondary" type="button" @click="openBillingDialog(selectedRow); selectedRow = null">{{ t('effectivePricing.importBill') }}</button><button class="button" type="button" @click="openPriceDialog(selectedRow); selectedRow = null">{{ t('effectivePricing.addPrice') }}</button></footer>
      </aside>
    </div>

    <div v-if="dialog" class="modal-backdrop"><form class="modal-card effective-dialog" role="dialog" aria-modal="true" aria-labelledby="effective-dialog-title" aria-describedby="effective-dialog-description" @submit.prevent="saveDialog"><header class="modal-header"><div><h2 id="effective-dialog-title">{{ t(`effectivePricing.dialogs.${dialog}`) }}</h2><p id="effective-dialog-description">{{ t(`effectivePricing.dialogHelp.${dialog}`) }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="dialog = null"><X :size="18" /></button></header><div class="modal-body form-grid">
      <template v-if="dialog === 'price'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="priceForm.provider_account_id" required @change="accountChanged(priceForm)"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><input v-model="priceForm.upstream_model" required /></div><div class="field"><label>{{ t('effectivePricing.uncachedPrice') }}</label><input v-model.number="priceForm.uncached_input_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.cacheReadPrice') }}</label><input v-model.number="priceForm.cache_read_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.outputPrice') }}</label><input v-model.number="priceForm.output_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.quoted') }}</label><input v-model.number="priceForm.quoted_multiplier" type="number" min="0" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.referenceInput') }}</label><input v-model.number="priceForm.reference_input_micros_per_1m_tokens" type="number" min="0" /></div><div class="field"><label>{{ t('effectivePricing.referenceOutput') }}</label><input v-model.number="priceForm.reference_output_micros_per_1m_tokens" type="number" min="0" /></div></template>
      <template v-else-if="dialog === 'billing'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="billingForm.provider_account_id" required @change="accountChanged(billingForm)"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><input v-model="billingForm.upstream_model" /></div><div class="field"><label>{{ t('effectivePricing.externalLine') }}</label><input v-model="billingForm.external_line_id" required /></div><div class="field"><label>{{ t('effectivePricing.upstreamRequest') }}</label><input v-model="billingForm.external_request_id" /></div><div class="field"><label>{{ t('effectivePricing.amountMicros') }}</label><input v-model.number="billingForm.amount_micros" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.confidence') }}</label><select v-model="billingForm.confidence"><option value="exact">exact</option><option value="derived">derived</option><option value="unallocated">unallocated</option><option value="unknown">unknown</option></select></div></template>
      <template v-else-if="dialog === 'policy'"><div class="field"><label>{{ t('effectivePricing.mode') }}</label><select v-model="policyForm.mode"><option value="observe_only">observe_only</option><option value="recommend">recommend</option><option value="canary">canary</option><option value="balanced">balanced</option><option value="cost_first">cost_first</option><option value="fixed_route">fixed_route</option></select></div><div class="field"><label>{{ t('effectivePricing.minSamples') }}</label><input v-model.number="policyForm.min_sample_count" type="number" min="1" /></div><div class="field"><label>{{ t('effectivePricing.minMetricsCoverage') }}</label><input v-model.number="policyForm.min_metrics_coverage" type="number" min="0" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.minBillingConsistency') }}</label><input v-model.number="policyForm.min_billing_consistency" type="number" min="0" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.minCostImprovement') }}</label><input v-model.number="policyForm.min_cost_improvement" type="number" min="0" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.minCacheImprovement') }}</label><input v-model.number="policyForm.min_cache_hit_rate_improvement" type="number" min="0.01" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.minAffinityImprovement') }}</label><input v-model.number="policyForm.min_affinity_improvement" type="number" min="0.01" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.maxCacheCostRegression') }}</label><input v-model.number="policyForm.max_cache_tiebreak_cost_regression" type="number" min="0" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.maxErrorRegression') }}</label><input v-model.number="policyForm.max_error_rate_regression" type="number" min="0" max="1" step="0.001" /></div><div class="field"><label>{{ t('effectivePricing.maxP95Regression') }}</label><input v-model.number="policyForm.max_p95_latency_regression" type="number" min="0" max="1" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.supplierTTL') }}</label><input v-model.number="policyForm.supplier_affinity_ttl_seconds" type="number" min="1" /></div><div class="field"><label>{{ t('effectivePricing.accountTTL') }}</label><input v-model.number="policyForm.account_affinity_ttl_seconds" type="number" min="1" /></div><div class="field"><label>{{ t('effectivePricing.canaryPercent') }}</label><input v-model.number="policyForm.canary_percent" type="number" min="1" max="100" /></div><div class="field"><label>{{ t('effectivePricing.probeDailyTokens') }}</label><input v-model.number="policyForm.probe_daily_token_budget" type="number" min="0" /></div><div class="field"><label>{{ t('effectivePricing.probeDailyCost') }}</label><input v-model.number="policyForm.probe_daily_cost_budget_micros" type="number" min="0" /></div><div class="field"><label>{{ t('effectivePricing.probeCooldown') }}</label><input v-model.number="policyForm.probe_cooldown_seconds" type="number" min="0" /></div><label class="checkbox-row"><input v-model="policyForm.probe_enabled" type="checkbox" />{{ t('effectivePricing.enableProbes') }}</label></template>
      <template v-else-if="dialog === 'probe'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="probeForm.provider_account_id" required><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><input v-model="probeForm.upstream_model" required /></div><div class="field"><label>{{ t('effectivePricing.protocol') }}</label><select v-model="probeForm.protocol"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option></select></div><div class="field"><label>{{ t('effectivePricing.probePrefixTokens') }}</label><input v-model.number="probeForm.prefix_tokens" type="number" min="256" max="32768" required /></div><div class="field"><label>{{ t('effectivePricing.probeMaxCost') }}</label><input v-model.number="probeForm.max_cost_micros" type="number" min="1" required /></div><label class="checkbox-row probe-confirmation"><input v-model="probeBudgetConfirmed" type="checkbox" />{{ t('effectivePricing.probeConfirm') }}</label></template>
      <template v-else>
        <div class="field"><label>{{ t('effectivePricing.routeModel') }}</label><input v-model="decisionForm.model" required /></div>
        <div class="field"><label>{{ t('effectivePricing.upstreamModel') }}</label><select v-model="decisionForm.upstream_model" required @change="decisionUpstreamModelChanged"><option v-for="model in decisionUpstreamModelOptions" :key="model" :value="model">{{ model }}</option></select></div>
        <div class="field"><label>{{ t('effectivePricing.protocol') }}</label><select v-model="decisionForm.protocol" required @change="resetDecisionAccounts()"><option v-for="protocol in decisionProtocolOptions" :key="protocol" :value="protocol">{{ protocol }}</option></select></div>
        <div class="field"><label>{{ t('effectivePricing.current') }}</label><select v-model="decisionForm.current_provider_account_id" required @change="decisionCurrentAccountChanged"><option v-for="row in decisionAccountRows" :key="`${row.provider_account_id}:${row.upstream_model}:${row.protocol}`" :value="row.provider_account_id">{{ row.provider_account_name || row.provider_account_id }}</option></select></div>
        <div class="field"><label>{{ t('effectivePricing.candidate') }}</label><select v-model="decisionForm.candidate_provider_account_id" required><option v-for="row in decisionCandidateRows" :key="`${row.provider_account_id}:${row.upstream_model}:${row.protocol}`" :value="row.provider_account_id">{{ row.provider_account_name || row.provider_account_id }}</option></select></div>
      </template>
    </div><footer class="modal-footer"><button class="button secondary" type="button" @click="dialog = null">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving || (dialog === 'probe' && !probeBudgetConfirmed) || (dialog === 'decision' && !decisionFormValid)"><Save :size="17" />{{ saving ? t('common.saving') : t('common.save') }}</button></footer></form></div>
  </main>
</template>

<style scoped>
.effective-pricing-page { display: grid; grid-template-columns: minmax(0,1fr); min-width: 0; gap: 16px; }
.effective-pricing-page > * { min-width: 0; }
.effective-tabs { display: flex; gap: 3px; padding: 3px; border-bottom: 1px solid var(--border); background: var(--surface); }
.effective-tabs button { display: inline-flex; min-height: 40px; align-items: center; gap: 7px; padding: 0 15px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--text-muted); cursor: pointer; font-weight: 700; }
.effective-tabs button.active { border-bottom-color: var(--primary-600); color: var(--primary-700); }
.effective-filters { display: flex; flex-wrap: wrap; align-items: end; gap: 10px; padding: 14px; border: 1px solid var(--border); background: var(--surface); }
.effective-filters label { display: grid; min-width: 170px; gap: 5px; color: var(--text-muted); font-size: 11px; font-weight: 700; }
.effective-filters input, .effective-filters select { min-height: 38px; padding: 0 11px; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); }
.effective-filters > .pill { margin-left: auto; }
.effective-panel { overflow: hidden; }
.effective-panel .panel-header { padding: 16px 18px; }
.effective-panel .panel-body { padding: 0; }
.ep-table td > span { display: block; color: var(--text-muted); font-size: 11px; }
.cache-grid, .probe-list { display: grid; gap: 0; }
.cache-row, .probe-row { display: grid; grid-template-columns: minmax(220px,1.4fr) repeat(3,minmax(100px,.6fr)) auto auto; gap: 14px; align-items: center; padding: 15px 18px; border-bottom: 1px solid var(--border); }
.cache-row > div, .probe-row > div { display: grid; gap: 2px; }
.cache-row span, .probe-row span, .cache-row small, .probe-row small, .probe-row time { color: var(--text-muted); font-size: 11px; }
.probe-row { grid-template-columns: minmax(230px,1.4fr) repeat(3,minmax(100px,.6fr)) auto 130px; }
.switch-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.switch-head h2 { margin: 0; font-size: 17px; }
.switch-head p { margin: 3px 0 0; color: var(--text-muted); }
.decision-grid { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 14px; }
.decision-card { padding: 18px; }
.decision-card header { display: flex; justify-content: space-between; gap: 12px; }
.decision-card h3 { margin: 9px 0 0; font-size: 15px; }
.decision-card p { margin: 3px 0 0; color: var(--text-muted); font-size: 11px; }
.decision-card header > strong { font-size: 22px; }
.decision-compare { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin: 16px 0; }
.decision-compare > div { display: grid; gap: 3px; padding: 12px; border: 1px solid var(--border); background: var(--surface-subtle); }
.decision-compare small, .decision-compare span { color: var(--text-muted); font-size: 11px; }
.decision-reasons { padding: 10px; border-left: 3px solid var(--warning); background: var(--warning-bg); }
.decision-card footer { justify-content: flex-end; margin-top: 14px; }
.drawer-backdrop { position: fixed; inset: 0; z-index: 80; background: rgb(15 23 42 / 35%); }
.evidence-drawer { position: absolute; inset: 0 0 0 auto; display: grid; grid-template-rows: auto minmax(0,1fr) auto; width: min(500px,100%); height: 100%; box-sizing: border-box; overflow: hidden; padding: 22px; background: var(--surface); box-shadow: var(--shadow-lg); }
.evidence-drawer header { display: flex; justify-content: space-between; gap: 12px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
.evidence-drawer h2 { margin: 0; font-size: 18px; }
.evidence-drawer p { margin: 4px 0 0; color: var(--text-muted); }
.evidence-body { min-width: 0; overflow-y: auto; }
.evidence-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; padding: 18px 0; border-bottom: 1px solid var(--border); }
.evidence-grid > div { display: grid; gap: 3px; }
.evidence-grid small { color: var(--text-muted); }
.evidence-section { padding: 18px 0; }
.evidence-section h3 { margin: 0 0 10px; font-size: 13px; }
.evidence-section p { overflow-wrap: anywhere; }
.evidence-drawer footer { display: flex; justify-content: flex-end; gap: 8px; padding-top: 14px; border-top: 1px solid var(--border); }
.effective-dialog { width: min(720px,calc(100vw - 28px)); }
.probe-confirmation { grid-column: 1 / -1; line-height: 1.45; }
@media (max-width: 900px) { .decision-grid { grid-template-columns: 1fr; } .cache-row, .probe-row { grid-template-columns: 1fr 1fr; } }
@media (max-width: 720px) {
  .effective-tabs { overflow-x: auto; }
  .effective-tabs button { flex: 0 0 auto; padding: 0 11px; }
  .effective-filters { display: grid; grid-template-columns: 1fr 1fr; }
  .effective-filters label:first-child { grid-column: 1 / -1; }
  .effective-filters > .pill { margin-left: 0; }
  .effective-panel .panel-header, .switch-head { align-items: flex-start; }
  .effective-panel .panel-header { display: grid; }
  .ep-table table { min-width: 0; }
  .ep-table table, .ep-table tbody, .ep-table tr, .ep-table td { display: block; width: 100%; }
  .ep-table thead { display: none; }
  .ep-table tr { padding: 12px; border-bottom: 1px solid var(--border); }
  .ep-table td { display: grid; grid-template-columns: minmax(105px,.45fr) minmax(0,1fr); gap: 9px; padding: 7px 0; border: 0; white-space: normal; }
  .ep-table td::before { content: attr(data-label); color: var(--text-muted); font-size: 11px; font-weight: 700; }
  .ep-table td[colspan] { display: block; }
  .ep-table td[colspan]::before { display: none; }
  .cache-row, .probe-row { grid-template-columns: 1fr 1fr; }
  .cache-row > div:first-child, .probe-row > div:first-child { grid-column: 1 / -1; }
  .switch-head { display: grid; }
  .evidence-drawer { width: 100%; padding: 16px; }
  .evidence-grid { grid-template-columns: 1fr; }
}
</style>
