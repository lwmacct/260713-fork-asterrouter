<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Ban, CheckCircle2, Code2, FlaskConical, History, Plus, RefreshCw, Rocket, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  activatePricingRuleVersion,
  createPricingRule,
  disablePricingRule,
  getPricingEvaluation,
  getPricingRule,
  getPricingRules,
  publishPricingRule,
  simulatePricingRule,
  updatePricingRuleDraft,
  validatePricingRule
} from '@/api/control'
import { listOperatorResource } from '@/api/operator'
import type {
  OperatorPlan,
  PricingEvaluation,
  PricingPurpose,
  PricingRule,
  PricingRuleDetail,
  PricingRuleTestCase,
  PricingScopeType,
  PricingSimulationResult,
  PricingSurface,
  PricingValidationResult
} from '@/types'

const props = defineProps<{
  surface: PricingSurface
  title: string
  subtitle: string
}>()

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const rules = ref<PricingRule[]>([])
const detail = ref<PricingRuleDetail | null>(null)
const plans = ref<OperatorPlan[]>([])
const createOpen = ref(false)
const activeTab = ref<'editor' | 'simulation' | 'history' | 'evaluation'>('editor')
const validation = ref<PricingValidationResult | null>(null)
const simulation = ref<PricingSimulationResult | null>(null)
const evaluation = ref<PricingEvaluation | null>(null)
const evaluationID = ref('')
const acknowledgeImpact = ref(false)

const filters = reactive({ purpose: '' as '' | PricingPurpose, status: '', model: '' })
const editor = reactive({ name: '', authoring_mode: 'raw' as 'visual' | 'raw', expression: '', test_cases: '[]' })
const createForm = reactive({
  name: '', purpose: 'usage_cost' as PricingPurpose, scope_type: 'global' as PricingScopeType,
  scope_id: '', model: '*', authoring_mode: 'raw' as 'visual' | 'raw',
  expression: 'v1: fixed_line("request", "request", 0)', test_cases: '[]'
})
const simulationFacts = ref(JSON.stringify({
  total_input_tokens: 1000,
  uncached_input_tokens: 1000,
  output_tokens: 500,
  available_facts: {
    total_input_tokens: true,
    uncached_input_tokens: true,
    output_tokens: true
  },
  normalization_status: 'simulated',
  phase: 'estimate'
}, null, 2))

const purposeLocked = computed(() => props.surface !== 'admin')
const selectedPurpose = computed(() => detail.value?.rule.purpose || createForm.purpose)
const canPublish = computed(() => Boolean(
  detail.value?.draft &&
  (detail.value.rule.purpose !== 'customer_charge' || acknowledgeImpact.value)
))

function showError(err: unknown) {
  error.value = err instanceof Error ? err.message : t('common.failed')
  notice.value = ''
}

function showNotice(key: string) {
  notice.value = t(key)
  error.value = ''
}

function parseTestCases(value: string): PricingRuleTestCase[] {
  const parsed = JSON.parse(value) as unknown
  if (!Array.isArray(parsed)) throw new Error(t('pricingRules.invalidTestCases'))
  return parsed as PricingRuleTestCase[]
}

function applyDetail(value: PricingRuleDetail) {
  detail.value = value
  const version = value.draft || value.active_version
  editor.name = value.rule.name
  editor.authoring_mode = version?.authoring_mode || 'raw'
  editor.expression = version?.expression || ''
  editor.test_cases = JSON.stringify(version?.test_cases || [], null, 2)
  validation.value = null
  simulation.value = null
  evaluation.value = null
  acknowledgeImpact.value = false
}

async function selectRule(id: string) {
  try {
    applyDetail(await getPricingRule(props.surface, id))
  } catch (err) {
    showError(err)
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, string> = {}
    if (filters.purpose && props.surface === 'admin') params.purpose = filters.purpose
    if (filters.status) params.status = filters.status
    if (filters.model.trim()) params.model = filters.model.trim()
    rules.value = await getPricingRules(props.surface, params)
    const selectedID = detail.value?.rule.id
    const next = rules.value.find((rule) => rule.id === selectedID) || rules.value[0]
    if (next) await selectRule(next.id)
    else detail.value = null
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(createForm, {
    name: '',
    purpose: props.surface === 'operator' ? 'customer_charge' : 'usage_cost',
    scope_type: 'global',
    scope_id: '',
    model: '*',
    authoring_mode: 'raw',
    expression: 'v1: fixed_line("request", "request", 0)',
    test_cases: '[]'
  })
  createOpen.value = true
}

async function createRule() {
  saving.value = true
  try {
    const purpose: PricingPurpose = props.surface === 'operator' ? 'customer_charge' : props.surface === 'platform' ? 'usage_cost' : createForm.purpose
    const scopeType: PricingScopeType = purpose === 'usage_cost' || props.surface === 'platform' ? 'global' : createForm.scope_type
    const created = await createPricingRule(props.surface, {
      name: createForm.name,
      purpose,
      scope_type: scopeType,
      scope_id: scopeType === 'global' ? '' : createForm.scope_id,
      model: createForm.model,
      currency: 'USD',
      authoring_mode: createForm.authoring_mode,
      expression: createForm.expression,
      test_cases: parseTestCases(createForm.test_cases)
    })
    createOpen.value = false
    await load()
    await selectRule(created.rule.id)
    showNotice('pricingRules.created')
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}

async function runValidation(): Promise<PricingValidationResult | null> {
  try {
    validation.value = await validatePricingRule(props.surface, editor.expression, parseTestCases(editor.test_cases))
    if (validation.value.valid) showNotice('pricingRules.validationPassed')
    else error.value = t('pricingRules.validationFailed')
    return validation.value
  } catch (err) {
    showError(err)
    return null
  }
}

async function saveDraft(): Promise<PricingRuleDetail | null> {
  if (!detail.value) return null
  saving.value = true
  try {
    const updated = await updatePricingRuleDraft(props.surface, detail.value.rule.id, {
      expected_lock_version: detail.value.rule.lock_version,
      name: editor.name,
      currency: 'USD',
      authoring_mode: editor.authoring_mode,
      expression: editor.expression,
      test_cases: parseTestCases(editor.test_cases)
    })
    applyDetail(updated)
    rules.value = rules.value.map((rule) => rule.id === updated.rule.id ? updated.rule : rule)
    showNotice('pricingRules.saved')
    return updated
  } catch (err) {
    showError(err)
    return null
  } finally {
    saving.value = false
  }
}

async function publishRule() {
  const updated = await saveDraft()
  if (!updated?.draft) return
  const checked = await runValidation()
  if (!checked?.valid || !checked.expression_hash) return
  saving.value = true
  try {
    const published = await publishPricingRule(props.surface, updated.rule.id, {
      draft_version_id: updated.draft.id,
      expected_lock_version: updated.rule.lock_version,
      expected_active_version_id: updated.rule.active_version_id || '',
      expression_hash: checked.expression_hash,
      acknowledge_customer_impact: acknowledgeImpact.value
    })
    applyDetail(published)
    rules.value = rules.value.map((rule) => rule.id === published.rule.id ? published.rule : rule)
    showNotice('pricingRules.publishedSuccess')
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}

async function disableRule() {
  if (!detail.value || !window.confirm(t('pricingRules.confirmDisable'))) return
  try {
    await disablePricingRule(props.surface, detail.value.rule.id, detail.value.rule.lock_version)
    await load()
    showNotice('pricingRules.disabledSuccess')
  } catch (err) {
    showError(err)
  }
}

async function activateVersion(versionID: string) {
  if (!detail.value || !window.confirm(t('pricingRules.confirmActivate'))) return
  try {
    await activatePricingRuleVersion(props.surface, detail.value.rule.id, versionID, detail.value.rule.lock_version)
    await selectRule(detail.value.rule.id)
    showNotice('pricingRules.activated')
  } catch (err) {
    showError(err)
  }
}

async function runSimulation() {
  try {
    simulation.value = await simulatePricingRule(props.surface, {
      rule_version_id: '', expression: editor.expression, currency: 'USD', facts: JSON.parse(simulationFacts.value)
    })
    error.value = ''
  } catch (err) {
    showError(err)
  }
}

async function lookupEvaluation() {
  if (!evaluationID.value.trim()) return
  try {
    evaluation.value = await getPricingEvaluation(props.surface, evaluationID.value.trim())
    error.value = ''
  } catch (err) {
    showError(err)
  }
}

function money(micros?: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 6 }).format((micros || 0) / 1_000_000)
}

function date(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

onMounted(async () => {
  if (props.surface === 'operator') {
    try { plans.value = await listOperatorResource<OperatorPlan>('plans') } catch { plans.value = [] }
  }
  await load()
})
</script>

<template>
  <main class="content pricing-page">
    <section class="page-header">
      <div><h1>{{ title }}</h1><p>{{ subtitle }}</p></div>
      <div class="row-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load"><RefreshCw :size="17" />{{ t('common.refresh') }}</button>
        <button class="button" type="button" @click="openCreate"><Plus :size="17" />{{ t('pricingRules.newRule') }}</button>
      </div>
    </section>

    <section class="table-toolbar pricing-filters">
      <select v-if="!purposeLocked" v-model="filters.purpose" @change="load">
        <option value="">{{ t('pricingRules.allPurposes') }}</option>
        <option value="usage_cost">{{ t('pricingRules.usageCost') }}</option>
        <option value="customer_charge">{{ t('pricingRules.customerCharge') }}</option>
      </select>
      <select v-model="filters.status" @change="load">
        <option value="">{{ t('pricingRules.allStatuses') }}</option>
        <option value="active">{{ t('pricingRules.active') }}</option>
        <option value="disabled">{{ t('pricingRules.disabled') }}</option>
      </select>
      <label class="search-field"><Search :size="16" /><input v-model="filters.model" :placeholder="t('pricingRules.model')" @keyup.enter="load" /></label>
    </section>

    <div v-if="error" class="notice status-danger">{{ error }}</div>
    <div v-if="notice" class="notice status-success">{{ notice }}</div>

    <section class="pricing-layout">
      <aside class="pricing-rule-list" :aria-label="t('pricingRules.ruleList')">
        <button
          v-for="rule in rules"
          :key="rule.id"
          type="button"
          class="pricing-rule-item"
          :class="{ selected: detail?.rule.id === rule.id }"
          @click="selectRule(rule.id)"
        >
          <span><strong>{{ rule.name }}</strong><small>{{ rule.model }}</small></span>
          <span><i class="pill" :class="rule.status === 'active' ? 'status-success' : ''">{{ t(`pricingRules.${rule.status}`) }}</i><small>{{ t(`pricingRules.${rule.purpose === 'usage_cost' ? 'usageCost' : 'customerCharge'}`) }}</small></span>
        </button>
        <div v-if="!loading && !rules.length" class="empty-state">{{ t('pricingRules.noRules') }}</div>
      </aside>

      <section v-if="detail" class="pricing-workspace">
        <header class="pricing-workspace-header">
          <div>
            <div class="pricing-title-line"><h2>{{ detail.rule.name }}</h2><span class="pill" :class="detail.rule.status === 'active' ? 'status-success' : ''">{{ t(`pricingRules.${detail.rule.status}`) }}</span></div>
            <p>{{ detail.rule.model }} · {{ t(`pricingRules.${detail.rule.purpose === 'usage_cost' ? 'usageCost' : 'customerCharge'}`) }} · {{ detail.rule.scope_type === 'global' ? t('pricingRules.global') : detail.rule.scope_id }}</p>
          </div>
          <span class="lock-version">{{ t('pricingRules.lockVersion') }} {{ detail.rule.lock_version }}</span>
        </header>

        <nav class="segmented-control pricing-tabs" :aria-label="t('pricingRules.views')">
          <button type="button" :class="{ active: activeTab === 'editor' }" @click="activeTab='editor'"><Code2 :size="16" />{{ t('pricingRules.editor') }}</button>
          <button type="button" :class="{ active: activeTab === 'simulation' }" @click="activeTab='simulation'"><FlaskConical :size="16" />{{ t('pricingRules.simulation') }}</button>
          <button type="button" :class="{ active: activeTab === 'history' }" @click="activeTab='history'"><History :size="16" />{{ t('pricingRules.history') }}</button>
          <button type="button" :class="{ active: activeTab === 'evaluation' }" @click="activeTab='evaluation'"><Search :size="16" />{{ t('pricingRules.evaluation') }}</button>
        </nav>

        <form v-if="activeTab === 'editor'" class="pricing-editor" @submit.prevent="saveDraft">
          <div class="form-grid compact-grid">
            <div class="field"><label for="pricing-rule-name">{{ t('pricingRules.name') }}</label><input id="pricing-rule-name" v-model="editor.name" required /></div>
            <div class="field"><label for="pricing-authoring-mode">{{ t('pricingRules.authoringMode') }}</label><select id="pricing-authoring-mode" v-model="editor.authoring_mode"><option value="raw">{{ t('pricingRules.raw') }}</option><option value="visual">{{ t('pricingRules.visual') }}</option></select></div>
          </div>
          <div class="field"><label for="pricing-expression">{{ t('pricingRules.expression') }}</label><textarea id="pricing-expression" v-model="editor.expression" class="code-editor" rows="10" required spellcheck="false" /></div>
          <div class="field"><label for="pricing-test-cases">{{ t('pricingRules.testCases') }}</label><textarea id="pricing-test-cases" v-model="editor.test_cases" class="code-editor" rows="7" spellcheck="false" /></div>

          <section v-if="validation" class="validation-result" :class="validation.valid ? 'valid' : 'invalid'">
            <header><CheckCircle2 :size="17" /><strong>{{ validation.valid ? t('pricingRules.validationPassed') : t('pricingRules.validationFailed') }}</strong><code v-if="validation.expression_hash">{{ validation.expression_hash.slice(0, 16) }}</code></header>
            <div v-if="validation.analysis" class="analysis-grid">
              <span><small>{{ t('pricingRules.requiredFacts') }}</small><strong>{{ validation.analysis.required_facts.join(', ') || '-' }}</strong></span>
              <span><small>{{ t('pricingRules.lineCodes') }}</small><strong>{{ validation.analysis.line_codes.join(', ') || '-' }}</strong></span>
            </div>
            <ul v-if="validation.errors.length"><li v-for="item in validation.errors" :key="`${item.code}-${item.line}-${item.column}`"><code>{{ item.code }}</code> {{ item.message }}</li></ul>
          </section>

          <label v-if="selectedPurpose === 'customer_charge'" class="check-row"><input v-model="acknowledgeImpact" type="checkbox" />{{ t('pricingRules.acknowledgeImpact') }}</label>
          <div class="editor-actions">
            <button class="button secondary" type="button" @click="runValidation"><CheckCircle2 :size="17" />{{ t('pricingRules.validate') }}</button>
            <button class="button secondary" type="submit" :disabled="saving"><Save :size="17" />{{ t('pricingRules.saveDraft') }}</button>
            <button class="button" type="button" :disabled="saving || !canPublish" @click="publishRule"><Rocket :size="17" />{{ t('pricingRules.publish') }}</button>
            <button v-if="detail.rule.status === 'active'" class="button danger" type="button" @click="disableRule"><Ban :size="17" />{{ t('pricingRules.disable') }}</button>
          </div>
        </form>

        <section v-else-if="activeTab === 'simulation'" class="pricing-simulation">
          <div class="field"><label for="pricing-simulation-facts">{{ t('pricingRules.facts') }}</label><textarea id="pricing-simulation-facts" v-model="simulationFacts" class="code-editor" rows="13" spellcheck="false" /></div>
          <button class="button" type="button" @click="runSimulation"><FlaskConical :size="17" />{{ t('pricingRules.runSimulation') }}</button>
          <div v-if="simulation" class="simulation-output">
            <div class="analysis-grid"><span><small>{{ t('pricingRules.amount') }}</small><strong>{{ money(simulation.amount_micros) }}</strong></span><span><small>{{ t('pricingRules.tier') }}</small><strong>{{ simulation.matched_tier || '-' }}</strong></span></div>
            <div class="table-scroll"><table class="data-table"><thead><tr><th>{{ t('pricingRules.code') }}</th><th>{{ t('pricingRules.quantity') }}</th><th>{{ t('pricingRules.unit') }}</th><th>{{ t('pricingRules.rate') }}</th><th>{{ t('pricingRules.amount') }}</th></tr></thead><tbody><tr v-for="line in simulation.lines" :key="line.code"><td><code>{{ line.code }}</code></td><td>{{ line.quantity }}</td><td>{{ line.unit }}</td><td>{{ money(line.rate_micros) }}</td><td><strong>{{ money(line.amount_micros) }}</strong></td></tr><tr v-if="!simulation.lines.length"><td colspan="5" class="empty-cell">{{ t('pricingRules.emptyLines') }}</td></tr></tbody></table></div>
          </div>
        </section>

        <section v-else-if="activeTab === 'history'" class="table-scroll">
          <table class="data-table"><thead><tr><th>{{ t('pricingRules.revision') }}</th><th>{{ t('pricingRules.state') }}</th><th>{{ t('pricingRules.hash') }}</th><th>{{ t('pricingRules.publishedAt') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="version in detail.versions" :key="version.id"><td>#{{ version.revision }}</td><td><span class="pill" :class="version.state === 'published' ? 'status-success' : ''">{{ t(`pricingRules.${version.state}`) }}</span></td><td><code>{{ version.expression_hash.slice(0, 16) }}</code></td><td>{{ date(version.published_at || version.updated_at) }}</td><td><button v-if="version.state === 'published' && detail.rule.active_version_id !== version.id" class="button secondary compact" type="button" @click="activateVersion(version.id)">{{ t('pricingRules.activate') }}</button><span v-else-if="detail.rule.active_version_id === version.id" class="pill status-success">{{ t('pricingRules.activeVersion') }}</span></td></tr></tbody></table>
        </section>

        <section v-else class="evaluation-view">
          <form class="evaluation-search" @submit.prevent="lookupEvaluation"><div class="field"><label for="pricing-evaluation-id">{{ t('pricingRules.evaluationId') }}</label><input id="pricing-evaluation-id" v-model="evaluationID" required /></div><button class="button" type="submit"><Search :size="17" />{{ t('pricingRules.lookup') }}</button></form>
          <div v-if="evaluation" class="evaluation-result">
            <div class="analysis-grid"><span><small>{{ t('pricingRules.status') }}</small><strong>{{ evaluation.status }}</strong></span><span><small>{{ t('pricingRules.phase') }}</small><strong>{{ evaluation.phase }}</strong></span><span><small>{{ t('pricingRules.amount') }}</small><strong>{{ evaluation.amount_micros == null ? '-' : money(evaluation.amount_micros) }}</strong></span><span><small>{{ t('pricingRules.tier') }}</small><strong>{{ evaluation.matched_tier || '-' }}</strong></span></div>
            <code v-if="evaluation.failure_code" class="failure-code">{{ evaluation.failure_code }}</code>
            <pre>{{ JSON.stringify(evaluation.facts, null, 2) }}</pre>
          </div>
        </section>
      </section>

      <section v-else class="pricing-empty-workspace"><Code2 :size="28" /><p>{{ t('pricingRules.selectRule') }}</p></section>
    </section>

    <div v-if="createOpen" class="modal-backdrop" @click.self="createOpen=false">
      <form class="modal-card pricing-create-modal" role="dialog" aria-modal="true" aria-labelledby="pricing-create-title" @submit.prevent="createRule">
        <header class="modal-header"><h2 id="pricing-create-title">{{ t('pricingRules.newRule') }}</h2><button class="icon-button" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="createOpen=false"><X :size="18" /></button></header>
        <div class="modal-body form-grid">
          <div class="field"><label for="pricing-create-name">{{ t('pricingRules.name') }}</label><input id="pricing-create-name" v-model="createForm.name" required /></div>
          <div class="field"><label for="pricing-create-model">{{ t('pricingRules.model') }}</label><input id="pricing-create-model" v-model="createForm.model" required /></div>
          <div class="field"><label for="pricing-create-purpose">{{ t('pricingRules.purpose') }}</label><select id="pricing-create-purpose" v-model="createForm.purpose" :disabled="purposeLocked"><option value="usage_cost">{{ t('pricingRules.usageCost') }}</option><option value="customer_charge">{{ t('pricingRules.customerCharge') }}</option></select></div>
          <div class="field"><label for="pricing-create-scope">{{ t('pricingRules.scope') }}</label><select id="pricing-create-scope" v-model="createForm.scope_type" :disabled="createForm.purpose === 'usage_cost' || surface === 'platform'"><option value="global">{{ t('pricingRules.global') }}</option><option value="operator_plan">{{ t('pricingRules.operatorPlan') }}</option></select></div>
          <div v-if="createForm.scope_type === 'operator_plan'" class="field field-wide"><label for="pricing-create-plan">{{ t('pricingRules.plan') }}</label><select v-if="surface === 'operator' && plans.length" id="pricing-create-plan" v-model="createForm.scope_id" required><option value="" disabled>{{ t('pricingRules.selectPlan') }}</option><option v-for="plan in plans" :key="plan.id" :value="plan.id">{{ plan.name }}</option></select><input v-else id="pricing-create-plan" v-model="createForm.scope_id" required /></div>
          <div class="field field-wide"><label for="pricing-create-expression">{{ t('pricingRules.expression') }}</label><textarea id="pricing-create-expression" v-model="createForm.expression" class="code-editor" rows="7" required spellcheck="false" /></div>
          <div class="field field-wide"><label for="pricing-create-tests">{{ t('pricingRules.testCases') }}</label><textarea id="pricing-create-tests" v-model="createForm.test_cases" class="code-editor" rows="5" spellcheck="false" /></div>
        </div>
        <footer class="modal-footer"><button class="button secondary" type="button" @click="createOpen=false">{{ t('common.cancel') }}</button><button class="button" type="submit" :disabled="saving"><Plus :size="17" />{{ t('pricingRules.create') }}</button></footer>
      </form>
    </div>
  </main>
</template>

<style scoped>
.pricing-page { min-width: 0; }
.pricing-filters { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.pricing-filters select { min-width: 150px; }
.search-field { display: flex; align-items: center; gap: 8px; min-width: min(280px, 100%); }
.search-field input { min-width: 0; width: 100%; }
.pricing-layout { display: grid; grid-template-columns: minmax(240px, 290px) minmax(0, 1fr); gap: 18px; align-items: start; margin-top: 16px; }
.pricing-rule-list { display: grid; gap: 8px; max-height: calc(100vh - 260px); overflow: auto; }
.pricing-rule-item { width: 100%; min-height: 72px; display: flex; justify-content: space-between; gap: 12px; text-align: left; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); color: inherit; padding: 12px; cursor: pointer; }
.pricing-rule-item:hover, .pricing-rule-item.selected { border-color: var(--primary); background: var(--surface-muted); }
.pricing-rule-item > span { display: grid; gap: 6px; min-width: 0; }
.pricing-rule-item > span:last-child { justify-items: end; }
.pricing-rule-item strong, .pricing-rule-item small { overflow-wrap: anywhere; }
.pricing-rule-item i { font-style: normal; }
.pricing-workspace { min-width: 0; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); }
.pricing-workspace-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; padding: 18px 20px; border-bottom: 1px solid var(--border); }
.pricing-workspace-header h2 { margin: 0; font-size: 20px; }
.pricing-workspace-header p { margin: 6px 0 0; color: var(--text-muted); overflow-wrap: anywhere; }
.pricing-title-line { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.lock-version { white-space: nowrap; color: var(--text-muted); font-size: 13px; }
.pricing-tabs { margin: 14px 20px 0; display: flex; overflow-x: auto; }
.pricing-tabs button { min-width: 120px; display: inline-flex; align-items: center; justify-content: center; gap: 7px; }
.pricing-editor, .pricing-simulation, .evaluation-view { display: grid; gap: 16px; padding: 20px; }
.compact-grid { grid-template-columns: minmax(0, 2fr) minmax(150px, 1fr); }
.code-editor { width: 100%; resize: vertical; min-height: 120px; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 13px; line-height: 1.55; }
.editor-actions { display: flex; gap: 9px; flex-wrap: wrap; }
.check-row { display: flex; align-items: flex-start; gap: 9px; }
.check-row input { margin-top: 3px; }
.validation-result, .simulation-output, .evaluation-result { border: 1px solid var(--border); border-radius: 6px; padding: 14px; display: grid; gap: 13px; }
.validation-result.valid { border-color: var(--success); }
.validation-result.invalid { border-color: var(--danger); }
.validation-result header { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.validation-result header code { margin-left: auto; }
.validation-result ul { margin: 0; padding-left: 20px; }
.analysis-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; }
.analysis-grid span { display: grid; gap: 4px; min-width: 0; }
.analysis-grid small { color: var(--text-muted); }
.analysis-grid strong { overflow-wrap: anywhere; }
.pricing-simulation > .button { justify-self: start; }
.simulation-output { padding: 0; overflow: hidden; }
.simulation-output .analysis-grid { padding: 14px; }
.evaluation-search { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 10px; }
.evaluation-result pre { margin: 0; max-height: 360px; overflow: auto; background: var(--surface-muted); padding: 12px; border-radius: 4px; }
.failure-code { color: var(--danger); }
.pricing-empty-workspace { min-height: 280px; display: grid; place-content: center; justify-items: center; gap: 10px; color: var(--text-muted); border: 1px dashed var(--border); border-radius: 8px; }
.pricing-create-modal { width: min(760px, calc(100vw - 32px)); }
.empty-state { padding: 26px 12px; text-align: center; color: var(--text-muted); }
.button.compact { min-height: 30px; padding: 4px 10px; }

@media (max-width: 900px) {
  .pricing-layout { grid-template-columns: 1fr; }
  .pricing-rule-list { grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); max-height: none; }
}
@media (max-width: 620px) {
  .pricing-workspace-header { flex-direction: column; }
  .pricing-tabs { margin-inline: 12px; }
  .pricing-editor, .pricing-simulation, .evaluation-view { padding: 14px; }
  .compact-grid, .evaluation-search { grid-template-columns: 1fr; }
  .evaluation-search .button { justify-self: start; }
}
</style>
