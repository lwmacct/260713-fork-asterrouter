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
  getEffectivePricingDecisionEvaluations,
  getEffectivePricingDecisions,
  getEffectivePricingReport,
  getGatewayModels,
  getProviderAccounts,
  getProviderBillingSourceEvidence,
  getProviderBillingSources,
  getProviderCacheCapabilities,
  getProviderCacheProbeRuns,
  inspectProviderBillingSource,
  runProviderCacheProbe,
  syncProviderBillingSource,
  updateProviderBillingSource,
  updateProviderCacheCapability,
  updateEffectivePricingPolicy
} from '@/api/control'
import type {
  CacheProbeRequest,
  EffectivePricingDecision,
  EffectivePricingDecisionEvaluation,
  EffectivePricingPolicyRequest,
  EffectivePricingReport,
  EffectivePricingReportRow,
  GatewayModel,
  ProcurementPriceRequest,
  ProviderAccount,
  ProviderBillingLineRequest,
  ProviderBillingSource,
  ProviderBillingSourceEvidence,
  ProviderBillingSourceInspection,
  ProviderBillingSourceRequest,
  ProviderCacheCapability,
  ProviderCacheCapabilityRequest,
  ProviderCacheProbeRun
} from '@/types'

type PricingTab = 'pricing' | 'cache' | 'switches' | 'probes' | 'sources'
type DialogKind = 'price' | 'billing' | 'policy' | 'decision' | 'probe' | 'capability' | null

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
const gatewayModels = ref<GatewayModel[]>([])
const billingSources = ref<ProviderBillingSource[]>([])
const billingSourceInspection = ref<ProviderBillingSourceInspection | null>(null)
const billingSourceEvidence = ref<ProviderBillingSourceEvidence | null>(null)
const billingSourceAccountID = ref('')
const inspectingBillingSource = ref(false)
const savingBillingSource = ref(false)
const syncingBillingSource = ref(false)
const selectedRow = ref<EffectivePricingReportRow | null>(null)
const selectedDecision = ref<EffectivePricingDecision | null>(null)
const decisionEvaluations = ref<EffectivePricingDecisionEvaluation[]>([])
const dialog = ref<DialogKind>(null)
const probeBudgetConfirmed = ref(false)
const modelFilter = ref('')
const protocolFilter = ref('openai_chat_completions')
const windowHours = ref(24)

const tabs: Array<{ id: PricingTab; icon: typeof BadgeDollarSign }> = [
  { id: 'pricing', icon: BadgeDollarSign },
  { id: 'cache', icon: Activity },
  { id: 'switches', icon: Route },
  { id: 'probes', icon: FlaskConical },
  { id: 'sources', icon: FileCheck2 }
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
  account_affinity_ttl_seconds: 1800, automatic_actions_enabled: false, evaluation_interval_minutes: 60,
  promotion_window_count: 3, degradation_window_count: 2, probe_enabled: false, probe_daily_token_budget: 100000,
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
const capabilityForm = reactive<ProviderCacheCapabilityRequest>({
  provider_account_id: '', upstream_model: '', protocol: 'openai_chat_completions',
  support_status: 'claimed', pool_affinity_grade: 'unknown', affinity_transport: 'none',
  affinity_field: '', cache_control_mode: 'passthrough_if_present', usage_schema: 'auto'
})
const billingSourceForm = reactive<Omit<ProviderBillingSourceRequest, 'provider_account_id' | 'adapter_id' | 'version'>>({
  status: 'observe_only', automatic_sync_enabled: false, sync_interval_seconds: 3600
})

const rows = computed(() => report.value?.rows || [])
const metrics = computed(() => {
  const comparable = rows.value.filter((row) => row.cost_available)
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
const activeGatewayModels = computed(() => gatewayModels.value.filter((model) => model.status === 'active'))
const selectedBillingSource = computed(() => billingSources.value.find((source) => source.provider_account_id === billingSourceAccountID.value))
const billingSourceFormValid = computed(() => Number.isInteger(billingSourceForm.sync_interval_seconds) && billingSourceForm.sync_interval_seconds >= 60 && billingSourceForm.sync_interval_seconds <= 86400)
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
function formatMultiplier(value: number, available = value > 0): string { return available ? `${value.toFixed(2)}x` : '-' }
function formatMoneyMicros(value: number, currency = 'USD', available = true): string {
  if (!available) return '-'
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: currency || 'USD', maximumFractionDigits: 4 }).format(value / 1_000_000)
}
function formatPriceRatio(value: number, uncached: number, available: boolean): string {
  if (!available || uncached <= 0) return '-'
  return `${(value / uncached).toFixed(2)}x`
}
function formatDate(value?: string): string { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value)) : '-' }
function statusClass(value: string): string {
  if (['exact', 'derived', 'matched', 'preferred', 'active', 'billed_verified', 'succeeded', 'verified', 'healthy', 'schema_match'].includes(value)) return 'status-success'
  if (['blocked', 'degraded', 'fragmented', 'rolled_back', 'failed', 'lease_expired', 'reduce_weight'].includes(value)) return 'status-danger'
  if (['estimated', 'pending', 'canary', 'recommended', 'observe', 'observe_only', 'ambiguous', 'inconclusive'].includes(value)) return 'status-warning'
  return ''
}
function accountName(id: string): string { return accounts.value.find((account) => account.id === id)?.name || id || '-' }
function accountModelOptions(accountID: string, current = ''): string[] {
  const models = accounts.value.find((account) => account.id === accountID)?.models || []
  return [...new Set([current.trim(), ...models].filter(Boolean))]
}
function gatewayModelOptions(current = ''): GatewayModel[] {
  const historical = current ? gatewayModels.value.find((model) => model.model_id === current && model.status !== 'active') : undefined
  return historical ? [historical, ...activeGatewayModels.value] : activeGatewayModels.value
}
function selectAccountModel(target: { provider_account_id: string; upstream_model: string }, allowEmpty = false) {
  const models = accounts.value.find((account) => account.id === target.provider_account_id)?.models || []
  if (!models.includes(target.upstream_model)) target.upstream_model = allowEmpty ? '' : models[0] || ''
}
function reportRowForDecision(decision: EffectivePricingDecision, accountID: string): EffectivePricingReportRow | undefined {
  return rows.value.find((row) => row.provider_account_id === accountID && row.upstream_model === decision.upstream_model && row.protocol === decision.protocol)
}
function formatLatency(value?: number): string { return value && value > 0 ? `${formatNumber(value)} ms` : '-' }
function capabilityLabel(enabled: boolean): string { return t(enabled ? 'effectivePricing.available' : 'effectivePricing.unavailable') }
function billingWarningLabel(code: string): string {
  const labels: Record<string, string> = {
    adapter_schema_match_does_not_prove_vendor_identity: t('effectivePricing.adapterIdentityWarning'),
    usage_cost_lines_unavailable: t('effectivePricing.lineItemsUnavailableWarning'),
    aggregate_totals_are_not_billing_lines: t('effectivePricing.aggregateNotLinesWarning'),
    remaining_is_quota_not_wallet_balance: t('effectivePricing.quotaNotBalanceWarning'),
    remaining_may_be_subscription_period_limit: t('effectivePricing.subscriptionLimitWarning'),
    subscription_remaining_unlimited: t('effectivePricing.subscriptionUnlimitedWarning'),
    account_key_reported_invalid: t('effectivePricing.accountKeyInvalidWarning')
  }
  return labels[code] || code
}
function billingHealthReasonLabel(code: string): string {
  const labels: Record<string, string> = {
    provider_billing_key_invalid: t('effectivePricing.routingHealthReasons.keyInvalid'),
    provider_billing_auth_rejected: t('effectivePricing.routingHealthReasons.authRejected'),
    provider_billing_key_quota_exhausted: t('effectivePricing.routingHealthReasons.keyQuotaExhausted'),
    provider_billing_subscription_exhausted: t('effectivePricing.routingHealthReasons.subscriptionExhausted'),
    provider_billing_sync_unhealthy: t('effectivePricing.routingHealthReasons.syncUnhealthy'),
    provider_billing_evidence_stale: t('effectivePricing.routingHealthReasons.evidenceStale'),
    provider_billing_evidence_missing: t('effectivePricing.routingHealthReasons.evidenceMissing'),
    provider_billing_source_observe_only: t('effectivePricing.routingHealthReasons.sourceObserveOnly'),
    provider_billing_source_disabled: t('effectivePricing.routingHealthReasons.sourceDisabled')
  }
  return labels[code] || code
}
function billingHealthStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    healthy: t('effectivePricing.routingHealthStatus.healthy'),
    degraded: t('effectivePricing.routingHealthStatus.degraded'),
    blocked: t('effectivePricing.routingHealthStatus.blocked'),
    observe_only: t('effectivePricing.routingHealthStatus.observeOnly'),
    disabled: t('effectivePricing.routingHealthStatus.disabled')
  }
  return labels[status] || status || '-'
}
function balanceKindLabel(kind: string): string {
  const labels: Record<string, string> = {
    wallet_balance: t('effectivePricing.walletBalance'),
    api_key_quota_remaining: t('effectivePricing.keyQuotaRemaining'),
    subscription_period_remaining: t('effectivePricing.subscriptionRemaining')
  }
  return labels[kind] || kind
}
function aggregateScopeLabel(aggregate: { scope: string; model?: string }): string {
  if (aggregate.scope === 'model_30d' && aggregate.model) return `${aggregate.model} · ${t('effectivePricing.last30Days')}`
  return t(`effectivePricing.aggregateScopes.${aggregate.scope}`)
}

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
    const [nextReport, nextCapabilities, nextProbes, nextDecisions, nextAccounts, nextBillingSources, nextGatewayModels] = await Promise.all([
      getEffectivePricingReport({ model: modelFilter.value.trim() || undefined, protocol: protocolFilter.value || undefined, window_hours: windowHours.value }),
      getProviderCacheCapabilities(), getProviderCacheProbeRuns(100), getEffectivePricingDecisions(), getProviderAccounts(), getProviderBillingSources(), getGatewayModels()
    ])
    report.value = nextReport
    capabilities.value = nextCapabilities
    probes.value = nextProbes
    decisions.value = nextDecisions
    accounts.value = nextAccounts
    billingSources.value = nextBillingSources
    gatewayModels.value = nextGatewayModels
    if (!nextAccounts.some((account) => account.id === billingSourceAccountID.value)) {
      billingSourceAccountID.value = nextAccounts.find((account) => account.status === 'active')?.id || ''
      billingSourceInspection.value = null
    }
    await applyBillingSourceSelection()
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
    upstream_model: row?.upstream_model || account?.models?.[0] || '', protocol: row?.protocol || protocolFilter.value, currency: row?.currency || 'USD',
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
    model: activeGatewayModels.value[0]?.model_id || '', upstream_model: source?.upstream_model || '', protocol: source?.protocol || '',
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
function openCapabilityDialog(row: EffectivePricingReportRow) {
  const capability = capabilityFor(row)
  Object.assign(capabilityForm, {
    provider_account_id: row.provider_account_id,
    upstream_model: row.upstream_model,
    protocol: row.protocol,
    support_status: capability?.support_status || 'claimed',
    pool_affinity_grade: capability?.pool_affinity_grade || 'unknown',
    affinity_transport: capability?.affinity_transport || 'none',
    affinity_field: capability?.affinity_field || '',
    cache_control_mode: capability?.cache_control_mode || 'passthrough_if_present',
    usage_schema: capability?.usage_schema || 'auto'
  })
  dialog.value = 'capability'
}
function capabilityTransportChanged() {
  if (capabilityForm.affinity_transport === 'none') capabilityForm.affinity_field = ''
}
function priceAccountChanged() {
  priceForm.provider_id = accounts.value.find((account) => account.id === priceForm.provider_account_id)?.provider_id || ''
  selectAccountModel(priceForm)
}
function billingAccountChanged() {
  billingForm.provider_id = accounts.value.find((account) => account.id === billingForm.provider_account_id)?.provider_id || ''
  selectAccountModel(billingForm, true)
}
function probeAccountChanged() {
  selectAccountModel(probeForm)
}
function capabilityAccountChanged() {
  selectAccountModel(capabilityForm)
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
    if (dialog.value === 'capability') await updateProviderCacheCapability(capabilityForm)
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

async function openDecisionEvaluationHistory(decision: EffectivePricingDecision) {
  error.value = ''
  selectedDecision.value = decision
  decisionEvaluations.value = []
  try {
    decisionEvaluations.value = await getEffectivePricingDecisionEvaluations(decision.id, 100)
  } catch (err) {
    selectedDecision.value = null
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function inspectBillingSource() {
  if (!billingSourceAccountID.value) return
  inspectingBillingSource.value = true
  error.value = ''
  message.value = ''
  try {
    billingSourceInspection.value = await inspectProviderBillingSource(billingSourceAccountID.value)
    message.value = t('effectivePricing.sourceInspected')
  } catch (err) {
    billingSourceInspection.value = null
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    inspectingBillingSource.value = false
  }
}

async function applyBillingSourceSelection() {
  const source = selectedBillingSource.value
  Object.assign(billingSourceForm, {
    status: source?.status || 'observe_only',
    automatic_sync_enabled: source?.automatic_sync_enabled || false,
    sync_interval_seconds: source?.sync_interval_seconds || 3600
  })
  if (!source) {
    billingSourceEvidence.value = null
    return
  }
  billingSourceEvidence.value = await getProviderBillingSourceEvidence(source.id, 100)
}

async function billingSourceAccountChanged() {
  billingSourceInspection.value = null
  error.value = ''
  try {
    await applyBillingSourceSelection()
  } catch (err) {
    billingSourceEvidence.value = null
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function saveBillingSource() {
  if (!billingSourceAccountID.value) return
  savingBillingSource.value = true
  error.value = ''
  message.value = ''
  try {
    const current = selectedBillingSource.value
    const stored = await updateProviderBillingSource({
      provider_account_id: billingSourceAccountID.value,
      adapter_id: billingSourceInspection.value?.adapter_id || current?.adapter_id || 'sub2api_compatible',
      status: billingSourceForm.status,
      automatic_sync_enabled: billingSourceForm.automatic_sync_enabled,
      sync_interval_seconds: billingSourceForm.sync_interval_seconds,
      version: current?.version
    })
    const index = billingSources.value.findIndex((source) => source.id === stored.id)
    if (index >= 0) billingSources.value.splice(index, 1, stored)
    else billingSources.value.push(stored)
    billingSourceEvidence.value = await getProviderBillingSourceEvidence(stored.id, 100)
    message.value = t('effectivePricing.sourceSaved')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    savingBillingSource.value = false
  }
}

async function syncBillingSourceNow() {
  const source = selectedBillingSource.value
  if (!source) return
  syncingBillingSource.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await syncProviderBillingSource(source.id)
    const index = billingSources.value.findIndex((item) => item.id === result.source.id)
    if (index >= 0) billingSources.value.splice(index, 1, result.source)
    billingSourceEvidence.value = await getProviderBillingSourceEvidence(source.id, 100)
    if (result.run.status === 'succeeded') message.value = t('effectivePricing.sourceSynced')
    else error.value = t('effectivePricing.sourceSyncFailed')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    syncingBillingSource.value = false
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
      <label><span>{{ t('usage.model') }}</span><input v-model="modelFilter" :placeholder="t('effectivePricing.upstreamModel')" @keyup.enter="load" /></label>
      <label><span>{{ t('effectivePricing.protocol') }}</span><select v-model="protocolFilter" @change="load"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option><option value="gemini_generate_content">Gemini Generate Content</option></select></label>
      <label><span>{{ t('effectivePricing.window') }}</span><select v-model.number="windowHours" @change="load"><option :value="24">24h</option><option :value="168">7d</option><option :value="720">30d</option></select></label>
      <span class="pill" :class="statusClass(report?.policy.mode || '')">{{ report?.policy.mode || 'observe_only' }}</span>
    </section>

    <section class="metric-grid effective-metrics">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card"><span class="metric-icon"><component :is="metric.icon" :size="20" /></span><div><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong><small>{{ metric.sub }}</small></div></article>
    </section>

    <template v-if="activeTab === 'pricing'">
      <section class="panel effective-panel">
        <header class="panel-header split-header"><div><h2>{{ t('effectivePricing.priceComparison') }}</h2><p>{{ t('effectivePricing.priceComparisonHelp') }}</p></div><div class="row-actions"><button class="button secondary" type="button" @click="openBillingDialog()"><FileCheck2 :size="16" />{{ t('effectivePricing.importBill') }}</button><button class="button" type="button" @click="openPriceDialog()"><Plus :size="16" />{{ t('effectivePricing.addPrice') }}</button></div></header>
	        <div class="panel-body table-scroll ep-table"><table class="data-table"><thead><tr><th>{{ t('effectivePricing.supplierAccount') }}</th><th>{{ t('effectivePricing.quoted') }}</th><th>{{ t('effectivePricing.billed') }}</th><th>{{ t('effectivePricing.effective') }}</th><th>{{ t('effectivePricing.effectiveCost') }}</th><th>{{ t('effectivePricing.priceStructure') }}</th><th>{{ t('effectivePricing.cacheHit') }}</th><th>{{ t('effectivePricing.quality') }}</th><th>{{ t('effectivePricing.coverage') }}</th><th>{{ t('effectivePricing.confidence') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody>
	          <tr v-for="row in rows" :key="`${row.provider_account_id}:${row.upstream_model}:${row.protocol}`"><td :data-label="t('effectivePricing.supplierAccount')"><strong>{{ row.provider_name || row.provider_id }}</strong><span>{{ row.provider_account_name || row.provider_account_id }} · {{ row.upstream_model }}</span></td><td :data-label="t('effectivePricing.quoted')"><strong>{{ formatMultiplier(row.quoted_multiplier) }}</strong></td><td :data-label="t('effectivePricing.billed')"><strong>{{ formatMultiplier(row.billed_multiplier) }}</strong><span>{{ formatPercent(row.billing_consistency_rate) }}</span></td><td :data-label="t('effectivePricing.effective')"><strong>{{ formatMultiplier(row.effective_multiplier, row.cost_available) }}</strong></td><td :data-label="t('effectivePricing.effectiveCost')"><strong>{{ formatMoneyMicros(row.effective_cost_micros_per_1m, row.currency, row.cost_available) }}</strong><span>{{ t('effectivePricing.uncachedEquivalent') }} {{ formatMoneyMicros(row.uncached_cost_micros_per_1m, row.currency, Boolean(row.price_id)) }}</span><span>{{ t('effectivePricing.cacheSavings') }} {{ formatCacheSavings(row) }}</span></td><td :data-label="t('effectivePricing.priceStructure')"><strong>{{ t('effectivePricing.cacheReadPrice') }} {{ formatPriceRatio(row.cache_read_micros_per_1m_tokens, row.uncached_input_micros_per_1m_tokens, Boolean(row.price_id)) }}</strong><span>5m {{ formatPriceRatio(row.cache_write_5m_micros_per_1m_tokens, row.uncached_input_micros_per_1m_tokens, Boolean(row.price_id)) }} · 1h {{ formatPriceRatio(row.cache_write_1h_micros_per_1m_tokens, row.uncached_input_micros_per_1m_tokens, Boolean(row.price_id)) }}</span></td><td :data-label="t('effectivePricing.cacheHit')"><strong>{{ formatPercent(row.cache_token_hit_rate) }}</strong><span>{{ formatNumber(row.request_count) }} {{ t('usage.requests') }}</span></td><td :data-label="t('effectivePricing.quality')"><strong>{{ formatPercent(row.error_rate) }} {{ t('effectivePricing.errorRate') }}</strong><span>P95 {{ formatLatency(row.p95_latency_ms) }}</span></td><td :data-label="t('effectivePricing.coverage')">{{ formatPercent(row.metrics_coverage) }}</td><td :data-label="t('effectivePricing.confidence')"><span class="pill" :class="statusClass(row.cost_confidence)">{{ row.cost_confidence }}</span></td><td :data-label="t('common.actions')"><div class="row-actions"><button class="button ghost" type="button" @click="selectedRow = row">{{ t('effectivePricing.evidence') }}</button><button class="button ghost" type="button" :disabled="!hasComparableAccount(row)" @click="openDecisionDialog(row)">{{ t('effectivePricing.evaluate') }}</button></div></td></tr>
	          <tr v-if="!rows.length"><td colspan="11" class="empty-cell">{{ loading ? t('common.loading') : t('effectivePricing.empty') }}</td></tr>
        </tbody></table></div>
      </section>
    </template>

    <template v-else-if="activeTab === 'cache'">
      <section class="panel effective-panel"><header class="panel-header split-header"><div><h2>{{ t('effectivePricing.cacheQuality') }}</h2><p>{{ t('effectivePricing.cacheQualityHelp') }}</p></div></header><div class="panel-body cache-grid">
	        <article v-for="item in capabilityRows" :key="item.row.provider_account_id" class="cache-row"><div><strong>{{ item.row.provider_name || item.row.provider_id }}</strong><span>{{ item.row.provider_account_name || item.row.provider_account_id }} · {{ item.row.upstream_model }}</span></div><div><small>{{ t('effectivePricing.cacheHit') }}</small><strong>{{ formatPercent(item.row.cache_token_hit_rate) }}</strong></div><div><small>{{ t('effectivePricing.coverage') }}</small><strong>{{ formatPercent(item.row.metrics_coverage) }}</strong></div><div><small>{{ t('effectivePricing.affinity') }}</small><strong>{{ formatPercent(item.row.affinity_consistency_rate) }}</strong></div><span class="pill" :class="statusClass(item.row.cache_support_status)">{{ item.row.cache_support_status }}</span><span class="pill" :class="statusClass(item.row.pool_affinity_grade)">{{ item.row.pool_affinity_grade }}</span><button class="button ghost" type="button" @click="openCapabilityDialog(item.row)"><Settings2 :size="15" />{{ t('effectivePricing.configureCapability') }}</button></article>
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
          <div class="decision-monitoring">
            <span><small>{{ t('effectivePricing.lastVerdict') }}</small><strong class="pill" :class="statusClass(decision.last_evaluation_verdict)">{{ decision.last_evaluation_verdict || '-' }}</strong></span>
            <span><small>{{ t('effectivePricing.healthyWindows') }}</small><strong>{{ decision.healthy_window_count }} / {{ report?.policy.promotion_window_count || 0 }}</strong></span>
            <span><small>{{ t('effectivePricing.degradedWindows') }}</small><strong>{{ decision.degraded_window_count }} / {{ report?.policy.degradation_window_count || 0 }}</strong></span>
            <span><small>{{ t('effectivePricing.lastWindow') }}</small><strong>{{ formatDate(decision.last_evaluated_window_end) }}</strong></span>
          </div>
          <p v-if="decision.last_evaluation_reason_codes?.length" class="decision-reasons">{{ decision.last_evaluation_reason_codes.join(' · ') }}</p>
          <p v-if="decision.reason_codes.length" class="decision-reasons">{{ decision.reason_codes.join(' · ') }}</p>
          <footer class="row-actions"><button class="button ghost" type="button" @click="openDecisionEvaluationHistory(decision)"><Activity :size="15" />{{ t('effectivePricing.windowEvidence') }}</button><button v-if="decision.status === 'recommended'" class="button" type="button" @click="decisionAction(decision, 'approve_canary')">{{ t('effectivePricing.startCanary') }}</button><button v-if="decision.status === 'canary'" class="button" type="button" @click="decisionAction(decision, 'activate')">{{ t('effectivePricing.activate') }}</button><button v-if="['canary','active','degraded'].includes(decision.status)" class="button secondary" type="button" @click="decisionAction(decision, 'rollback')">{{ t('effectivePricing.rollback') }}</button></footer>
        </article>
        <div v-if="!decisions.length" class="panel empty-cell">{{ t('effectivePricing.noDecisions') }}</div>
      </section>
    </template>

    <template v-else-if="activeTab === 'probes'">
      <section class="panel effective-panel"><header class="panel-header split-header"><div><h2>{{ t('effectivePricing.probeRecords') }}</h2><p>{{ t('effectivePricing.probeHelp') }}</p></div><button class="button secondary" type="button" :disabled="!report?.policy.probe_enabled || !accountOptions.length" :title="report?.policy.probe_enabled ? '' : t('effectivePricing.probeBudgetRequired')" @click="openProbeDialog()"><FlaskConical :size="16" />{{ t('effectivePricing.runProbe') }}</button></header><div class="panel-body probe-list"><article v-for="probe in probes" :key="probe.id" class="probe-row"><div><strong>{{ accountName(probe.provider_account_id) }}</strong><span>{{ probe.upstream_model }} · {{ probe.probe_series_id }}</span><span v-if="probe.failure_reason">{{ probe.failure_reason }}</span></div><div><small>{{ t('effectivePricing.reuseRead') }}</small><strong>{{ formatNumber(probe.reuse_cache_read_tokens) }}</strong></div><div><small>TTFT</small><strong>{{ probe.reuse_ttft_ms }} ms</strong></div><div><small>{{ t('usage.cost') }}</small><strong>{{ formatMoneyMicros(probe.estimated_cost_micros) }}</strong></div><span class="pill" :class="statusClass(probe.status)">{{ probe.status }}</span><time>{{ formatDate(probe.started_at) }}</time></article><div v-if="!probes.length" class="empty-cell">{{ t('effectivePricing.noProbes') }}</div></div></section>
    </template>

    <template v-else>
      <section class="panel effective-panel billing-source-panel">
        <header class="panel-header split-header"><div><h2>{{ t('effectivePricing.billingSources') }}</h2><p>{{ t('effectivePricing.billingSourcesHelp') }}</p></div></header>
        <div class="panel-body billing-source-body">
          <div class="billing-source-controls">
            <label><span>{{ t('admin.providerAccounts') }}</span><select v-model="billingSourceAccountID" @change="billingSourceAccountChanged"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></label>
            <button class="button" type="button" :disabled="!billingSourceAccountID || inspectingBillingSource" @click="inspectBillingSource"><RefreshCw :size="16" />{{ inspectingBillingSource ? t('common.loading') : t('effectivePricing.inspectSource') }}</button>
          </div>
          <div v-if="billingSourceInspection || selectedBillingSource" class="billing-source-config">
            <label><span>{{ t('effectivePricing.sourceStatus') }}</span><select v-model="billingSourceForm.status"><option value="observe_only">observe_only</option><option value="active">active</option><option value="disabled">disabled</option></select></label>
            <label><span>{{ t('effectivePricing.syncInterval') }}</span><input v-model.number="billingSourceForm.sync_interval_seconds" type="number" min="60" max="86400" step="60" /></label>
            <label class="source-auto-sync"><input v-model="billingSourceForm.automatic_sync_enabled" type="checkbox" />{{ t('effectivePricing.automaticSync') }}</label>
            <div class="row-actions"><button class="button secondary" type="button" :disabled="savingBillingSource || !billingSourceFormValid || (!billingSourceInspection && !selectedBillingSource)" @click="saveBillingSource"><Save :size="16" />{{ savingBillingSource ? t('common.saving') : t('common.save') }}</button><button class="button" type="button" :disabled="!selectedBillingSource || syncingBillingSource || billingSourceForm.status === 'disabled'" @click="syncBillingSourceNow"><RefreshCw :size="16" />{{ syncingBillingSource ? t('common.loading') : t('effectivePricing.syncNow') }}</button></div>
          </div>
          <div v-if="billingSourceInspection" class="billing-source-result">
            <div class="source-result-head"><div><span class="pill" :class="statusClass(billingSourceInspection.detection_status)">{{ billingSourceInspection.detection_status }}</span><h3>{{ billingSourceInspection.provider_name }} / {{ billingSourceInspection.provider_account_name }}</h3><p>{{ billingSourceInspection.adapter_id }} · {{ billingSourceInspection.contract_version }} · {{ formatDate(billingSourceInspection.observed_at) }}</p></div><code>{{ billingSourceInspection.evidence_hash.slice(0, 16) }}</code></div>
            <dl class="source-capabilities">
              <div><dt>{{ t('effectivePricing.usageCostLines') }}</dt><dd :class="billingSourceInspection.capabilities.usage_cost_lines ? 'capability-yes' : 'capability-no'">{{ capabilityLabel(billingSourceInspection.capabilities.usage_cost_lines) }}</dd></div>
              <div><dt>{{ t('effectivePricing.aggregateUsage') }}</dt><dd :class="billingSourceInspection.capabilities.aggregate_usage ? 'capability-yes' : 'capability-no'">{{ capabilityLabel(billingSourceInspection.capabilities.aggregate_usage) }}</dd></div>
              <div><dt>{{ t('effectivePricing.balanceCapability') }}</dt><dd :class="billingSourceInspection.capabilities.balance ? 'capability-yes' : 'capability-no'">{{ capabilityLabel(billingSourceInspection.capabilities.balance) }}</dd></div>
              <div><dt>{{ t('effectivePricing.incrementalSync') }}</dt><dd :class="billingSourceInspection.capabilities.incremental_sync ? 'capability-yes' : 'capability-no'">{{ capabilityLabel(billingSourceInspection.capabilities.incremental_sync) }}</dd></div>
              <div><dt>{{ t('effectivePricing.priceFeed') }}</dt><dd :class="billingSourceInspection.capabilities.price_feed ? 'capability-yes' : 'capability-no'">{{ capabilityLabel(billingSourceInspection.capabilities.price_feed) }}</dd></div>
            </dl>
            <div v-if="billingSourceInspection.balance" class="source-balance"><span>{{ balanceKindLabel(billingSourceInspection.balance.kind) }}</span><strong>{{ billingSourceInspection.balance.unlimited ? t('effectivePricing.unlimited') : formatMoneyMicros(billingSourceInspection.balance.amount_micros, billingSourceInspection.balance.currency) }}</strong><small>{{ formatDate(billingSourceInspection.balance.observed_at) }}</small></div>
            <div v-if="billingSourceInspection.usage_aggregates.length" class="table-scroll"><table class="data-table source-aggregate-table"><thead><tr><th>{{ t('effectivePricing.aggregateScope') }}</th><th>{{ t('usage.requests') }}</th><th>{{ t('usage.inputTokens') }}</th><th>{{ t('usage.outputTokens') }}</th><th>{{ t('effectivePricing.cacheReadTokens') }}</th><th>{{ t('effectivePricing.listCost') }}</th><th>{{ t('effectivePricing.actualCost') }}</th></tr></thead><tbody><tr v-for="aggregate in billingSourceInspection.usage_aggregates" :key="`${aggregate.scope}:${aggregate.model || ''}`"><td :data-label="t('effectivePricing.aggregateScope')">{{ aggregateScopeLabel(aggregate) }}</td><td :data-label="t('usage.requests')">{{ formatNumber(aggregate.request_count) }}</td><td :data-label="t('usage.inputTokens')">{{ formatNumber(aggregate.input_tokens) }}</td><td :data-label="t('usage.outputTokens')">{{ formatNumber(aggregate.output_tokens) }}</td><td :data-label="t('effectivePricing.cacheReadTokens')">{{ formatNumber(aggregate.cache_read_tokens) }}</td><td :data-label="t('effectivePricing.listCost')">{{ formatMoneyMicros(aggregate.list_cost_micros || 0, billingSourceInspection.currency, aggregate.list_cost_micros !== undefined) }}</td><td :data-label="t('effectivePricing.actualCost')">{{ formatMoneyMicros(aggregate.actual_cost_micros || 0, billingSourceInspection.currency, aggregate.actual_cost_micros !== undefined) }}</td></tr></tbody></table></div>
            <ul class="source-warnings"><li v-for="warning in billingSourceInspection.warnings" :key="warning">{{ billingWarningLabel(warning) }}</li></ul>
          </div>
          <div v-if="billingSourceEvidence" class="billing-source-evidence">
            <div class="source-state-summary"><span><small>{{ t('effectivePricing.sourceStatus') }}</small><strong class="pill" :class="statusClass(billingSourceEvidence.source.status)">{{ billingSourceEvidence.source.status }}</strong></span><span><small>{{ t('effectivePricing.lastSuccess') }}</small><strong>{{ formatDate(billingSourceEvidence.source.last_success_at) }}</strong></span><span><small>{{ t('effectivePricing.nextSync') }}</small><strong>{{ formatDate(billingSourceEvidence.source.next_sync_at) }}</strong></span><span><small>{{ t('effectivePricing.consecutiveFailures') }}</small><strong>{{ billingSourceEvidence.source.consecutive_failures }}</strong></span></div>
            <div v-if="billingSourceEvidence.source.routing_health" class="routing-health-summary">
              <span><small>{{ t('effectivePricing.routingHealth') }}</small><strong class="pill" :class="statusClass(billingSourceEvidence.source.routing_health.status)">{{ billingHealthStatusLabel(billingSourceEvidence.source.routing_health.status) }}</strong></span>
              <span><small>{{ t('effectivePricing.hardBlocked') }}</small><strong>{{ billingSourceEvidence.source.routing_health.hard_blocked ? t('common.yes') : t('common.no') }}</strong></span>
              <span><small>{{ t('effectivePricing.economicSwitchEligible') }}</small><strong>{{ billingSourceEvidence.source.routing_health.economic_switch_eligible ? t('common.yes') : t('common.no') }}</strong></span>
              <span><small>{{ t('effectivePricing.evidenceObservedAt') }}</small><strong>{{ formatDate(billingSourceEvidence.source.routing_health.evidence_observed_at) }}</strong></span>
              <p v-if="billingSourceEvidence.source.routing_health?.reason_codes?.length">{{ t('effectivePricing.routingReasons') }}: {{ billingSourceEvidence.source.routing_health.reason_codes.map(billingHealthReasonLabel).join(' · ') }}</p>
            </div>
            <section class="source-history-section"><h3>{{ t('effectivePricing.syncHistory') }}</h3><div class="table-scroll"><table class="data-table source-history-table"><thead><tr><th>{{ t('effectivePricing.sourceStatus') }}</th><th>{{ t('effectivePricing.syncTrigger') }}</th><th>{{ t('effectivePricing.adapter') }}</th><th>{{ t('effectivePricing.errorCode') }}</th><th>{{ t('effectivePricing.startedAt') }}</th><th>{{ t('effectivePricing.finishedAt') }}</th></tr></thead><tbody><tr v-for="run in billingSourceEvidence.runs" :key="run.id"><td :data-label="t('effectivePricing.sourceStatus')"><span class="pill" :class="statusClass(run.status)">{{ run.status }}</span></td><td :data-label="t('effectivePricing.syncTrigger')">{{ run.trigger }}</td><td :data-label="t('effectivePricing.adapter')">{{ run.adapter_id }}</td><td :data-label="t('effectivePricing.errorCode')">{{ run.error_code || '-' }}</td><td :data-label="t('effectivePricing.startedAt')">{{ formatDate(run.started_at) }}</td><td :data-label="t('effectivePricing.finishedAt')">{{ formatDate(run.finished_at) }}</td></tr><tr v-if="!billingSourceEvidence.runs.length"><td colspan="6" class="empty-cell">{{ t('effectivePricing.noSyncHistory') }}</td></tr></tbody></table></div></section>
            <section v-if="billingSourceEvidence.balances.length" class="source-history-section"><h3>{{ t('effectivePricing.balanceHistory') }}</h3><div class="table-scroll"><table class="data-table source-history-table"><thead><tr><th>{{ t('effectivePricing.balanceCapability') }}</th><th>{{ t('usage.cost') }}</th><th>{{ t('effectivePricing.observedAt') }}</th><th>{{ t('effectivePricing.evidenceHash') }}</th></tr></thead><tbody><tr v-for="balance in billingSourceEvidence.balances" :key="balance.id"><td :data-label="t('effectivePricing.balanceCapability')">{{ balanceKindLabel(balance.kind) }}</td><td :data-label="t('usage.cost')"><strong>{{ balance.unlimited ? t('effectivePricing.unlimited') : formatMoneyMicros(balance.amount_micros, balance.currency) }}</strong></td><td :data-label="t('effectivePricing.observedAt')">{{ formatDate(balance.observed_at) }}</td><td :data-label="t('effectivePricing.evidenceHash')"><code>{{ balance.evidence_hash.slice(0, 16) }}</code></td></tr></tbody></table></div></section>
            <section v-if="billingSourceEvidence.aggregates.length" class="source-history-section"><h3>{{ t('effectivePricing.aggregateHistory') }}</h3><div class="table-scroll"><table class="data-table source-aggregate-table"><thead><tr><th>{{ t('effectivePricing.aggregateScope') }}</th><th>{{ t('usage.requests') }}</th><th>{{ t('effectivePricing.cacheReadTokens') }}</th><th>{{ t('effectivePricing.listCost') }}</th><th>{{ t('effectivePricing.actualCost') }}</th><th>{{ t('effectivePricing.observedAt') }}</th></tr></thead><tbody><tr v-for="aggregate in billingSourceEvidence.aggregates" :key="aggregate.id"><td :data-label="t('effectivePricing.aggregateScope')">{{ aggregateScopeLabel(aggregate) }}</td><td :data-label="t('usage.requests')">{{ formatNumber(aggregate.request_count) }}</td><td :data-label="t('effectivePricing.cacheReadTokens')">{{ formatNumber(aggregate.cache_read_tokens) }}</td><td :data-label="t('effectivePricing.listCost')">{{ formatMoneyMicros(aggregate.list_cost_micros || 0, aggregate.currency, aggregate.list_cost_micros !== undefined) }}</td><td :data-label="t('effectivePricing.actualCost')">{{ formatMoneyMicros(aggregate.actual_cost_micros || 0, aggregate.currency, aggregate.actual_cost_micros !== undefined) }}</td><td :data-label="t('effectivePricing.observedAt')">{{ formatDate(aggregate.observed_at) }}</td></tr></tbody></table></div></section>
          </div>
          <div v-if="!billingSourceInspection && !billingSourceEvidence" class="empty-cell">{{ t('effectivePricing.noSourceInspection') }}</div>
        </div>
      </section>
    </template>

    <div v-if="selectedRow" class="drawer-backdrop" @click.self="selectedRow = null">
      <aside class="evidence-drawer" role="dialog" aria-modal="true">
        <header><div><h2>{{ selectedRow.provider_name || selectedRow.provider_id }}</h2><p>{{ selectedRow.provider_account_name || selectedRow.provider_account_id }} · {{ selectedRow.upstream_model }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="selectedRow = null"><X :size="18" /></button></header>
        <div class="evidence-body">
	          <div class="evidence-grid"><div><small>{{ t('effectivePricing.quoted') }}</small><strong>{{ formatMultiplier(selectedRow.quoted_multiplier) }}</strong></div><div><small>{{ t('effectivePricing.effective') }}</small><strong>{{ formatMultiplier(selectedRow.effective_multiplier, selectedRow.cost_available) }}</strong></div><div><small>{{ t('effectivePricing.effectiveCost') }}</small><strong>{{ formatMoneyMicros(selectedRow.effective_cost_micros_per_1m, selectedRow.currency, selectedRow.cost_available) }}</strong></div><div><small>{{ t('effectivePricing.uncachedEquivalent') }}</small><strong>{{ formatMoneyMicros(selectedRow.uncached_cost_micros_per_1m, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.cacheSavings') }}</small><strong>{{ formatCacheSavings(selectedRow) }}</strong></div><div><small>{{ t('effectivePricing.cacheSavingsAmount') }}</small><strong>{{ formatMoneyMicros(selectedRow.cache_savings_micros_per_1m, selectedRow.currency, selectedRow.cache_economics_available) }}</strong></div><div><small>{{ t('effectivePricing.uncachedPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.uncached_input_micros_per_1m_tokens, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.cacheReadPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.cache_read_micros_per_1m_tokens, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.cacheWrite5mPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.cache_write_5m_micros_per_1m_tokens, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.cacheWrite1hPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.cache_write_1h_micros_per_1m_tokens, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.outputPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.output_micros_per_1m_tokens, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.requestPrice') }}</small><strong>{{ formatMoneyMicros(selectedRow.request_micros, selectedRow.currency, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.rechargeMultiplier') }}</small><strong>{{ formatMultiplier(selectedRow.recharge_multiplier, Boolean(selectedRow.price_id)) }}</strong></div><div><small>{{ t('effectivePricing.confidence') }}</small><strong>{{ selectedRow.cost_confidence }}</strong></div><div><small>{{ t('effectivePricing.cacheHit') }}</small><strong>{{ formatPercent(selectedRow.cache_token_hit_rate) }}</strong></div><div><small>{{ t('effectivePricing.affinity') }}</small><strong>{{ formatPercent(selectedRow.affinity_consistency_rate) }}</strong></div><div><small>{{ t('effectivePricing.errorRate') }}</small><strong>{{ formatPercent(selectedRow.error_rate) }}</strong></div><div><small>{{ t('effectivePricing.p95Latency') }}</small><strong>{{ formatLatency(selectedRow.p95_latency_ms) }}</strong></div></div>
          <div class="evidence-section"><h3>{{ t('effectivePricing.evidence') }}</h3><p>Price: {{ selectedRow.price_id || '-' }}</p><p>Window: {{ formatDate(report?.window_start) }} → {{ formatDate(report?.window_end) }}</p><p>Reasons: {{ selectedRow.reason_codes.join(' · ') || '-' }}</p></div>
        </div>
        <footer><button class="button secondary" type="button" @click="openBillingDialog(selectedRow); selectedRow = null">{{ t('effectivePricing.importBill') }}</button><button class="button" type="button" @click="openPriceDialog(selectedRow); selectedRow = null">{{ t('effectivePricing.addPrice') }}</button></footer>
      </aside>
    </div>

    <div v-if="selectedDecision" class="drawer-backdrop" @click.self="selectedDecision = null">
      <aside class="evidence-drawer" role="dialog" aria-modal="true">
        <header><div><h2>{{ t('effectivePricing.windowEvidence') }}</h2><p>{{ selectedDecision.model }} · {{ selectedDecision.id }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="selectedDecision = null"><X :size="18" /></button></header>
        <div class="evidence-body evaluation-history">
          <div v-for="evaluation in decisionEvaluations" :key="evaluation.id" class="evaluation-row">
            <div><span class="pill" :class="statusClass(evaluation.verdict)">{{ evaluation.verdict }}</span><strong>{{ formatDate(evaluation.window_end) }}</strong><small>{{ formatDate(evaluation.window_start) }} → {{ formatDate(evaluation.window_end) }}</small></div>
            <div><small>{{ t('effectivePricing.costImprovement') }}</small><strong>{{ formatPercent(evaluation.cost_improvement) }}</strong></div>
            <div><small>{{ t('effectivePricing.cacheHit') }}</small><strong>{{ formatPercent(evaluation.current_cache_token_hit_rate) }} → {{ formatPercent(evaluation.candidate_cache_token_hit_rate) }}</strong></div>
            <div><small>{{ t('effectivePricing.errorRate') }}</small><strong>{{ formatPercent(evaluation.current_error_rate) }} → {{ formatPercent(evaluation.candidate_error_rate) }}</strong></div>
            <p>{{ evaluation.reason_codes.join(' · ') || '-' }}</p>
            <strong v-if="evaluation.automatic_action" class="automatic-action">{{ t('effectivePricing.automaticAction') }}: {{ evaluation.automatic_action }}</strong>
          </div>
          <div v-if="!decisionEvaluations.length" class="empty-cell">{{ t('effectivePricing.noWindowEvidence') }}</div>
        </div>
      </aside>
    </div>

    <div v-if="dialog" class="modal-backdrop"><form class="modal-card effective-dialog" role="dialog" aria-modal="true" aria-labelledby="effective-dialog-title" aria-describedby="effective-dialog-description" @submit.prevent="saveDialog"><header class="modal-header"><div><h2 id="effective-dialog-title">{{ t(`effectivePricing.dialogs.${dialog}`) }}</h2><p id="effective-dialog-description">{{ t(`effectivePricing.dialogHelp.${dialog}`) }}</p></div><button class="icon-button" type="button" :aria-label="t('common.close')" @click="dialog = null"><X :size="18" /></button></header><div class="modal-body form-grid">
	      <template v-if="dialog === 'price'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="priceForm.provider_account_id" required @change="priceAccountChanged"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><select v-if="accountModelOptions(priceForm.provider_account_id, priceForm.upstream_model).length" v-model="priceForm.upstream_model" required><option v-for="model in accountModelOptions(priceForm.provider_account_id, priceForm.upstream_model)" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="priceForm.upstream_model" required /></div><div class="field"><label>{{ t('effectivePricing.protocol') }}</label><select v-model="priceForm.protocol"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option><option value="gemini_generate_content">Gemini Generate Content</option></select></div><div class="field"><label>{{ t('effectivePricing.currency') }}</label><input v-model="priceForm.currency" maxlength="3" required /></div><div class="field"><label>{{ t('effectivePricing.uncachedPrice') }}</label><input v-model.number="priceForm.uncached_input_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.cacheReadPrice') }}</label><input v-model.number="priceForm.cache_read_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.cacheWrite5mPrice') }}</label><input v-model.number="priceForm.cache_write_5m_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.cacheWrite1hPrice') }}</label><input v-model.number="priceForm.cache_write_1h_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.outputPrice') }}</label><input v-model.number="priceForm.output_micros_per_1m_tokens" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.requestPrice') }}</label><input v-model.number="priceForm.request_micros" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.quoted') }}</label><input v-model.number="priceForm.quoted_multiplier" type="number" min="0" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.rechargeMultiplier') }}</label><input v-model.number="priceForm.recharge_multiplier" type="number" min="0" step="0.01" /></div><div class="field"><label>{{ t('effectivePricing.referenceInput') }}</label><input v-model.number="priceForm.reference_input_micros_per_1m_tokens" type="number" min="0" /></div><div class="field"><label>{{ t('effectivePricing.referenceOutput') }}</label><input v-model.number="priceForm.reference_output_micros_per_1m_tokens" type="number" min="0" /></div><div class="field"><label>{{ t('effectivePricing.confidence') }}</label><select v-model="priceForm.confidence"><option value="exact">exact</option><option value="derived">derived</option><option value="estimated">estimated</option></select></div><div class="field"><label>{{ t('effectivePricing.sourceReference') }}</label><input v-model="priceForm.source_reference" /></div></template>
      <template v-else-if="dialog === 'billing'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="billingForm.provider_account_id" required @change="billingAccountChanged"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><select v-if="accountModelOptions(billingForm.provider_account_id, billingForm.upstream_model).length" v-model="billingForm.upstream_model"><option value="">-</option><option v-for="model in accountModelOptions(billingForm.provider_account_id, billingForm.upstream_model)" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="billingForm.upstream_model" /></div><div class="field"><label>{{ t('effectivePricing.externalLine') }}</label><input v-model="billingForm.external_line_id" required /></div><div class="field"><label>{{ t('effectivePricing.upstreamRequest') }}</label><input v-model="billingForm.external_request_id" /></div><div class="field"><label>{{ t('effectivePricing.amountMicros') }}</label><input v-model.number="billingForm.amount_micros" type="number" min="0" required /></div><div class="field"><label>{{ t('effectivePricing.confidence') }}</label><select v-model="billingForm.confidence"><option value="exact">exact</option><option value="derived">derived</option><option value="unallocated">unallocated</option><option value="unknown">unknown</option></select></div></template>
	      <template v-else-if="dialog === 'policy'">
	        <div class="field"><label>{{ t('effectivePricing.mode') }}</label><select v-model="policyForm.mode"><option value="observe_only">observe_only</option><option value="recommend">recommend</option><option value="canary">canary</option><option value="balanced">balanced</option><option value="cost_first">cost_first</option><option value="fixed_route">fixed_route</option></select></div>
	        <div class="field"><label>{{ t('effectivePricing.minSamples') }}</label><input v-model.number="policyForm.min_sample_count" type="number" min="1" /></div>
	        <div class="field"><label>{{ t('effectivePricing.minMetricsCoverage') }}</label><input v-model.number="policyForm.min_metrics_coverage" type="number" min="0" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.minBillingConsistency') }}</label><input v-model.number="policyForm.min_billing_consistency" type="number" min="0" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.minCostImprovement') }}</label><input v-model.number="policyForm.min_cost_improvement" type="number" min="0" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.minCacheImprovement') }}</label><input v-model.number="policyForm.min_cache_hit_rate_improvement" type="number" min="0.01" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.minAffinityImprovement') }}</label><input v-model.number="policyForm.min_affinity_improvement" type="number" min="0.01" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.maxCacheCostRegression') }}</label><input v-model.number="policyForm.max_cache_tiebreak_cost_regression" type="number" min="0" max="1" step="0.01" /></div>
	        <div class="field"><label>{{ t('effectivePricing.maxErrorRegression') }}</label><input v-model.number="policyForm.max_error_rate_regression" type="number" min="0" max="1" step="0.001" /></div>
	        <div class="field"><label>{{ t('effectivePricing.maxP95Regression') }}</label><input v-model.number="policyForm.max_p95_latency_regression" type="number" min="0" max="1" step="0.01" /></div>
	        <div class="field"><label for="effective-evaluation-interval">{{ t('effectivePricing.evaluationInterval') }}</label><input id="effective-evaluation-interval" v-model.number="policyForm.evaluation_interval_minutes" type="number" min="1" max="1440" /></div>
	        <div class="field"><label for="effective-promotion-windows">{{ t('effectivePricing.promotionWindows') }}</label><input id="effective-promotion-windows" v-model.number="policyForm.promotion_window_count" type="number" min="1" max="24" /></div>
	        <div class="field"><label for="effective-degradation-windows">{{ t('effectivePricing.degradationWindows') }}</label><input id="effective-degradation-windows" v-model.number="policyForm.degradation_window_count" type="number" min="1" max="24" /></div>
	        <div class="field"><label>{{ t('effectivePricing.supplierTTL') }}</label><input v-model.number="policyForm.supplier_affinity_ttl_seconds" type="number" min="1" /></div>
	        <div class="field"><label>{{ t('effectivePricing.accountTTL') }}</label><input v-model.number="policyForm.account_affinity_ttl_seconds" type="number" min="1" /></div>
	        <div class="field"><label>{{ t('effectivePricing.canaryPercent') }}</label><input v-model.number="policyForm.canary_percent" type="number" min="1" max="100" /></div>
	        <div class="field"><label>{{ t('effectivePricing.probeDailyTokens') }}</label><input v-model.number="policyForm.probe_daily_token_budget" type="number" min="0" /></div>
	        <div class="field"><label>{{ t('effectivePricing.probeDailyCost') }}</label><input v-model.number="policyForm.probe_daily_cost_budget_micros" type="number" min="0" /></div>
	        <div class="field"><label>{{ t('effectivePricing.probeCooldown') }}</label><input v-model.number="policyForm.probe_cooldown_seconds" type="number" min="0" /></div>
	        <label class="checkbox-row"><input v-model="policyForm.automatic_actions_enabled" type="checkbox" />{{ t('effectivePricing.enableAutomaticActions') }}</label>
	        <label class="checkbox-row"><input v-model="policyForm.probe_enabled" type="checkbox" />{{ t('effectivePricing.enableProbes') }}</label>
	      </template>
	      <template v-else-if="dialog === 'probe'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="probeForm.provider_account_id" required @change="probeAccountChanged"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><select v-if="accountModelOptions(probeForm.provider_account_id, probeForm.upstream_model).length" v-model="probeForm.upstream_model" required><option v-for="model in accountModelOptions(probeForm.provider_account_id, probeForm.upstream_model)" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="probeForm.upstream_model" required /></div><div class="field"><label>{{ t('effectivePricing.protocol') }}</label><select v-model="probeForm.protocol"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option><option value="gemini_generate_content">Gemini Generate Content</option></select></div><div class="field"><label>{{ t('effectivePricing.probePrefixTokens') }}</label><input v-model.number="probeForm.prefix_tokens" type="number" min="256" max="32768" required /></div><div class="field"><label>{{ t('effectivePricing.probeMaxCost') }}</label><input v-model.number="probeForm.max_cost_micros" type="number" min="1" required /></div><label class="checkbox-row probe-confirmation"><input v-model="probeBudgetConfirmed" type="checkbox" />{{ t('effectivePricing.probeConfirm') }}</label></template>
	      <template v-else-if="dialog === 'capability'"><div class="field"><label>{{ t('admin.providerAccounts') }}</label><select v-model="capabilityForm.provider_account_id" required @change="capabilityAccountChanged"><option v-for="account in accountOptions" :key="account.id" :value="account.id">{{ account.name }} · {{ account.id }}</option></select></div><div class="field"><label>{{ t('usage.model') }}</label><select v-if="accountModelOptions(capabilityForm.provider_account_id, capabilityForm.upstream_model).length" v-model="capabilityForm.upstream_model" required><option v-for="model in accountModelOptions(capabilityForm.provider_account_id, capabilityForm.upstream_model)" :key="model" :value="model">{{ model }}</option></select><input v-else v-model="capabilityForm.upstream_model" required /></div><div class="field"><label>{{ t('effectivePricing.protocol') }}</label><select v-model="capabilityForm.protocol"><option value="openai_chat_completions">OpenAI Chat</option><option value="anthropic_messages">Anthropic Messages</option><option value="gemini_generate_content">Gemini Generate Content</option></select></div><div class="field"><label>{{ t('effectivePricing.supportStatus') }}</label><select v-model="capabilityForm.support_status" :disabled="!['unknown','claimed','accepted'].includes(capabilityForm.support_status)"><option v-if="!['unknown','claimed','accepted'].includes(capabilityForm.support_status)" :value="capabilityForm.support_status">{{ capabilityForm.support_status }}</option><option value="unknown">unknown</option><option value="claimed">claimed</option><option value="accepted">accepted</option></select></div><div class="field"><label>{{ t('effectivePricing.poolAffinityGrade') }}</label><input v-model="capabilityForm.pool_affinity_grade" disabled /></div><div class="field"><label>{{ t('effectivePricing.affinityTransport') }}</label><select v-model="capabilityForm.affinity_transport" @change="capabilityTransportChanged"><option value="none">none</option><option value="header">header</option><option value="body">body</option></select></div><div class="field"><label>{{ t('effectivePricing.affinityField') }}</label><input v-model="capabilityForm.affinity_field" :disabled="capabilityForm.affinity_transport === 'none'" :required="capabilityForm.affinity_transport !== 'none'" /></div><div class="field"><label>{{ t('effectivePricing.cacheControlMode') }}</label><select v-model="capabilityForm.cache_control_mode"><option value="passthrough_if_present">passthrough_if_present</option><option value="prompt_cache_key">prompt_cache_key</option></select></div><div class="field"><label>{{ t('effectivePricing.usageSchema') }}</label><input v-model="capabilityForm.usage_schema" /></div></template>
	      <template v-else>
        <div class="field"><label>{{ t('effectivePricing.routeModel') }}</label><select v-model="decisionForm.model" required><option v-if="!gatewayModelOptions(decisionForm.model).length" value="" disabled>{{ t('apiKeys.noActiveModels') }}</option><option v-for="model in gatewayModelOptions(decisionForm.model)" :key="model.id" :value="model.model_id">{{ model.model_id }} · {{ model.name }}<template v-if="model.status !== 'active'"> · {{ t('apiKeys.historicalModels') }}</template></option></select></div>
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
.billing-source-body { display: grid; gap: 18px; padding: 18px !important; }
.billing-source-controls { display: flex; align-items: end; gap: 12px; }
.billing-source-controls label { display: grid; flex: 1 1 340px; max-width: 560px; gap: 6px; color: var(--text-muted); font-size: 12px; font-weight: 700; }
.billing-source-controls select { width: 100%; min-height: 40px; }
.billing-source-config { display: grid; grid-template-columns: minmax(170px,.8fr) minmax(170px,.8fr) minmax(180px,1fr) auto; align-items: end; gap: 12px; padding: 14px; border: 1px solid var(--border); background: var(--surface-subtle); }
.billing-source-config > label:not(.source-auto-sync) { display: grid; gap: 6px; color: var(--text-muted); font-size: 12px; font-weight: 700; }
.billing-source-config select, .billing-source-config input[type="number"] { min-height: 38px; width: 100%; }
.source-auto-sync { display: flex; min-height: 38px; align-items: center; gap: 8px; color: var(--text); font-size: 12px; font-weight: 700; }
.billing-source-result { display: grid; gap: 16px; border-top: 1px solid var(--border); padding-top: 18px; }
.source-result-head { display: flex; align-items: start; justify-content: space-between; gap: 16px; }
.source-result-head h3 { margin: 7px 0 2px; font-size: 16px; }
.source-result-head p { margin: 0; color: var(--text-muted); font-size: 12px; }
.source-result-head code { color: var(--text-muted); font-size: 12px; }
.source-capabilities { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); margin: 0; border: 1px solid var(--border); }
.source-capabilities > div { min-width: 0; padding: 12px; border-right: 1px solid var(--border); }
.source-capabilities > div:last-child { border-right: 0; }
.source-capabilities dt { color: var(--text-muted); font-size: 11px; }
.source-capabilities dd { margin: 6px 0 0; font-weight: 800; }
.capability-yes { color: var(--success); }
.capability-no { color: var(--text-muted); }
.source-balance { display: grid; grid-template-columns: minmax(160px, 1fr) auto auto; align-items: baseline; gap: 16px; padding: 12px 0; border-bottom: 1px solid var(--border); }
.source-balance span, .source-balance small { color: var(--text-muted); }
.source-balance strong { font-size: 22px; }
.source-warnings { display: grid; gap: 7px; margin: 0; padding: 12px 12px 12px 30px; border-left: 3px solid var(--warning); background: var(--surface-subtle); color: var(--text-muted); font-size: 12px; }
.billing-source-evidence { display: grid; gap: 18px; border-top: 1px solid var(--border); padding-top: 18px; }
.source-state-summary { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 1px; border: 1px solid var(--border); background: var(--border); }
.source-state-summary > span { display: grid; min-width: 0; gap: 5px; padding: 12px; background: var(--surface); }
.source-state-summary small { color: var(--text-muted); font-size: 11px; }
.source-state-summary strong { overflow-wrap: anywhere; font-size: 12px; }
.routing-health-summary { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 1px; border: 1px solid var(--border); background: var(--border); }
.routing-health-summary > span { display: grid; min-width: 0; gap: 5px; padding: 12px; background: var(--surface); }
.routing-health-summary small { color: var(--text-muted); font-size: 11px; }
.routing-health-summary strong { overflow-wrap: anywhere; font-size: 12px; }
.routing-health-summary p { grid-column: 1 / -1; margin: 0; padding: 10px 12px; background: var(--surface-subtle); color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }
.source-history-section { min-width: 0; }
.source-history-section h3 { margin: 0 0 10px; font-size: 14px; }
.source-history-table code { color: var(--text-muted); font-size: 11px; }
.effective-filters { display: flex; flex-wrap: wrap; align-items: end; gap: 10px; padding: 14px; border: 1px solid var(--border); background: var(--surface); }
.effective-filters label { display: grid; min-width: 170px; gap: 5px; color: var(--text-muted); font-size: 11px; font-weight: 700; }
.effective-filters input, .effective-filters select { min-height: 38px; padding: 0 11px; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); }
.effective-filters > .pill { margin-left: auto; }
.effective-panel { overflow: hidden; }
.effective-panel .panel-header { padding: 16px 18px; }
.effective-panel .panel-body { padding: 0; }
.ep-table td > span { display: block; color: var(--text-muted); font-size: 11px; }
.cache-grid, .probe-list { display: grid; gap: 0; }
.cache-row, .probe-row { display: grid; grid-template-columns: minmax(220px,1.4fr) repeat(3,minmax(100px,.6fr)) auto auto auto; gap: 14px; align-items: center; padding: 15px 18px; border-bottom: 1px solid var(--border); }
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
.decision-monitoring { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 8px; padding: 12px 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
.decision-monitoring > span { display: grid; align-content: start; gap: 4px; min-width: 0; }
.decision-monitoring small { color: var(--text-muted); font-size: 11px; }
.decision-monitoring strong { overflow-wrap: anywhere; font-size: 12px; }
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
.evaluation-history { padding: 8px 0; }
.evaluation-row { display: grid; grid-template-columns: minmax(160px,1.4fr) repeat(3,minmax(90px,1fr)); gap: 10px; align-items: start; padding: 14px 0; border-bottom: 1px solid var(--border); }
.evaluation-row > div { display: grid; gap: 4px; min-width: 0; }
.evaluation-row > div:first-child { grid-template-columns: auto 1fr; align-items: center; }
.evaluation-row > div:first-child small { grid-column: 1 / -1; }
.evaluation-row small, .evaluation-row p { color: var(--text-muted); font-size: 11px; }
.evaluation-row p, .evaluation-row .automatic-action { grid-column: 1 / -1; margin: 0; overflow-wrap: anywhere; }
.automatic-action { color: var(--success); font-size: 12px; }
.evidence-drawer footer { display: flex; justify-content: flex-end; gap: 8px; padding-top: 14px; border-top: 1px solid var(--border); }
.effective-dialog { width: min(720px,calc(100vw - 28px)); }
.probe-confirmation { grid-column: 1 / -1; line-height: 1.45; }
@media (max-width: 900px) { .decision-grid { grid-template-columns: 1fr; } .cache-row, .probe-row { grid-template-columns: 1fr 1fr; } }
@media (max-width: 720px) {
  .effective-tabs { overflow-x: auto; }
  .effective-tabs button { flex: 0 0 auto; padding: 0 11px; }
  .billing-source-controls { align-items: stretch; flex-direction: column; }
  .billing-source-controls label { flex-basis: auto; max-width: none; }
  .billing-source-config { grid-template-columns: 1fr; align-items: stretch; }
  .billing-source-config .row-actions { justify-content: stretch; }
  .billing-source-config .row-actions .button { flex: 1 1 0; }
  .source-result-head { align-items: stretch; flex-direction: column; }
  .source-capabilities { grid-template-columns: 1fr 1fr; }
  .source-capabilities > div { border-bottom: 1px solid var(--border); }
  .source-balance { grid-template-columns: 1fr; gap: 4px; }
  .source-state-summary { grid-template-columns: 1fr 1fr; }
  .routing-health-summary { grid-template-columns: 1fr 1fr; }
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
  .source-aggregate-table, .source-history-table { min-width: 0; }
  .source-aggregate-table, .source-aggregate-table tbody, .source-aggregate-table tr, .source-aggregate-table td, .source-history-table, .source-history-table tbody, .source-history-table tr, .source-history-table td { display: block; width: 100%; }
  .source-aggregate-table thead, .source-history-table thead { display: none; }
  .source-aggregate-table tr, .source-history-table tr { padding: 10px 0; }
  .source-aggregate-table td, .source-history-table td { display: grid; grid-template-columns: minmax(125px, .65fr) minmax(0, 1fr); gap: 9px; padding: 7px 0; border: 0; white-space: normal; }
  .source-aggregate-table td::before, .source-history-table td::before { content: attr(data-label); color: var(--text-muted); font-size: 11px; font-weight: 700; }
  .cache-row, .probe-row { grid-template-columns: 1fr 1fr; }
  .cache-row > div:first-child, .probe-row > div:first-child { grid-column: 1 / -1; }
  .switch-head { display: grid; }
  .decision-monitoring { grid-template-columns: 1fr 1fr; }
  .evaluation-row { grid-template-columns: 1fr 1fr; }
  .evaluation-row > div:first-child { grid-column: 1 / -1; }
  .evidence-drawer { width: 100%; padding: 16px; }
  .evidence-grid { grid-template-columns: 1fr; }
}
</style>
