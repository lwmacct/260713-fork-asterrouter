import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as control from '@/api/control'
import type { PricingRule, PricingRuleDetail, PricingRuleVersion } from '@/types'
import PricingRulesView from './PricingRulesView.vue'

vi.mock('@/api/control', () => ({
  activatePricingRuleVersion: vi.fn(),
  createPricingRule: vi.fn(),
  disablePricingRule: vi.fn(),
  getPricingEvaluation: vi.fn(),
  getPricingRule: vi.fn(),
  getPricingRules: vi.fn(),
  publishPricingRule: vi.fn(),
  simulatePricingRule: vi.fn(),
  updatePricingRuleDraft: vi.fn(),
  validatePricingRule: vi.fn()
}))

vi.mock('@/api/operator', () => ({ listOperatorResource: vi.fn().mockResolvedValue([]) }))

const rule: PricingRule = {
  id: 'rule-1', name: 'Token cost', purpose: 'usage_cost', scope_type: 'global', scope_id: '', model: '*',
  status: 'active', active_version_id: '', lock_version: 1, created_by: 'tester', updated_by: 'tester',
  created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-16T00:00:00Z'
}
const draft: PricingRuleVersion = {
  id: 'draft-1', rule_id: rule.id, revision: 0, engine_version: 1, currency: 'USD',
  expression: 'v1: fixed_line("request", "request", 1)', expression_hash: 'a'.repeat(64),
  analysis: { engine_version: 1, required_facts: [], tiers: [], line_codes: ['request'], visual_editable: true },
  authoring_mode: 'raw', test_cases: [], state: 'draft', created_by: 'tester',
  created_at: rule.created_at, updated_at: rule.updated_at
}
const detail: PricingRuleDetail = { rule, draft, versions: [draft] }

describe('PricingRulesView', () => {
  beforeEach(() => {
    setLocale('en-US')
    vi.clearAllMocks()
    vi.mocked(control.getPricingRules).mockResolvedValue([rule])
    vi.mocked(control.getPricingRule).mockResolvedValue(detail)
    vi.mocked(control.createPricingRule).mockResolvedValue(detail)
    vi.mocked(control.validatePricingRule).mockResolvedValue({
      valid: true, expression_hash: draft.expression_hash, analysis: draft.analysis, test_results: [], errors: []
    })
  })

  it('loads a surface rule, validates the expression, and creates only the new DTO', async () => {
    const wrapper = mount(PricingRulesView, {
      props: { surface: 'admin', title: 'Expression Pricing', subtitle: 'Rules' },
      global: { plugins: [i18n] }
    })
    await flushPromises()

    expect(control.getPricingRules).toHaveBeenCalledWith('admin', {})
    expect(control.getPricingRule).toHaveBeenCalledWith('admin', rule.id)
    expect(wrapper.text()).toContain('Token cost')

    const validate = wrapper.findAll('button').find((button) => button.text().includes('Validate'))
    expect(validate).toBeTruthy()
    await validate!.trigger('click')
    await flushPromises()
    expect(control.validatePricingRule).toHaveBeenCalledWith('admin', draft.expression, [])

    const create = wrapper.findAll('button').find((button) => button.text().includes('New rule'))
    await create!.trigger('click')
    await wrapper.get('.pricing-create-modal input').setValue('New cost rule')
    await wrapper.get('.pricing-create-modal').trigger('submit')
    await flushPromises()

    expect(control.createPricingRule).toHaveBeenCalledWith('admin', expect.objectContaining({
      name: 'New cost rule', purpose: 'usage_cost', scope_type: 'global', scope_id: '', currency: 'USD'
    }))
    const payload = vi.mocked(control.createPricingRule).mock.calls[0][1] as unknown as Record<string, unknown>
    expect(payload).not.toHaveProperty('input_price_cents_per_1m_tokens')
    expect(payload).not.toHaveProperty('rate_multiplier')
  })
})
