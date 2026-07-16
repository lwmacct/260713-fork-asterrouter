import type {
  AIAttemptAdminRecord,
  AIJobAdminDetail,
  AIJobEvent,
  APIKeyCreateResponse,
  APIKeyRecord,
  ArtifactAdminDetail,
  ArtifactAdminRecord,
  ArtifactEvent,
  Dashboard,
  EffectivePricingDecision,
  EffectivePricingDecisionEvaluation,
  EffectivePricingReport,
  EffectivePricingReportRow,
  GatewayPolicyExplanation,
  ProviderBillingRoutingHealth,
  ProviderBillingSource,
  ProviderBillingSourceInspection,
  ProviderBillingSyncResult,
  UsageModelSummary,
  UsageRecord,
  UsageReport
} from '@/types'

export type NullableList<T> = T[] | null | undefined
export type DashboardPayload = Omit<Dashboard, 'models' | 'recent_audit'> & {
  models?: string[] | null
  recent_audit?: Dashboard['recent_audit'] | null
}
export type APIKeyRecordPayload = Omit<APIKeyRecord, 'scopes' | 'model_allowlist' | 'allowed_modalities' | 'allowed_operations' | 'allowed_cidrs'> & {
  scopes?: string[] | null
  model_allowlist?: string[] | null
  allowed_modalities?: string[] | null
  allowed_operations?: string[] | null
  allowed_cidrs?: string[] | null
}
export type APIKeyCreateResponsePayload = Omit<APIKeyCreateResponse, 'record'> & { record: APIKeyRecordPayload }
export type UsageReportPayload = Omit<UsageReport, 'by_model' | 'recent'> & {
  by_model?: UsageModelSummary[] | null
  recent?: UsageRecord[] | null
}
export type ArtifactAdminDetailPayload = Omit<ArtifactAdminDetail, 'events'> & { events?: ArtifactEvent[] | null }
export type AIJobAdminDetailPayload = Omit<AIJobAdminDetail, 'attempts' | 'events' | 'artifacts'> & {
  attempts?: AIAttemptAdminRecord[] | null
  events?: AIJobEvent[] | null
  artifacts?: ArtifactAdminRecord[] | null
}
export type EffectivePricingDecisionPayload = Omit<EffectivePricingDecision, 'reason_codes' | 'last_evaluation_reason_codes'> & {
  reason_codes?: string[] | null
  last_evaluation_reason_codes?: string[] | null
}
export type EffectivePricingDecisionEvaluationPayload = Omit<EffectivePricingDecisionEvaluation, 'reason_codes'> & {
  reason_codes?: string[] | null
}
export type EffectivePricingReportRowPayload = Omit<EffectivePricingReportRow, 'reason_codes' | 'provider_billing_routing_health'> & {
  reason_codes?: string[] | null
  provider_billing_routing_health?: ProviderBillingRoutingHealthPayload | null
}
export type EffectivePricingReportPayload = Omit<EffectivePricingReport, 'rows' | 'decisions'> & {
  rows?: EffectivePricingReportRowPayload[] | null
  decisions?: EffectivePricingDecisionPayload[] | null
}
export type ProviderBillingRoutingHealthPayload = Omit<ProviderBillingRoutingHealth, 'reason_codes'> & { reason_codes?: string[] | null }
export type ProviderBillingSourcePayload = Omit<ProviderBillingSource, 'warnings' | 'routing_health'> & {
  warnings?: string[] | null
  routing_health?: ProviderBillingRoutingHealthPayload | null
}
export type ProviderBillingSourceInspectionPayload = Omit<ProviderBillingSourceInspection, 'usage_aggregates' | 'warnings'> & {
  usage_aggregates?: ProviderBillingSourceInspection['usage_aggregates'] | null
  warnings?: string[] | null
}
export type ProviderBillingSyncResultPayload = Omit<ProviderBillingSyncResult, 'source' | 'aggregates'> & {
  source: ProviderBillingSourcePayload
  aggregates?: ProviderBillingSyncResult['aggregates'] | null
}

export function listOrEmpty<T>(value: NullableList<T>): T[] {
  return Array.isArray(value) ? value : []
}

export function stringListOrEmpty(value: NullableList<unknown>): string[] {
  return listOrEmpty(value).filter((item): item is string => typeof item === 'string')
}

export function normalizeDashboard(value: DashboardPayload): Dashboard {
  return {
    ...value,
    models: listOrEmpty(value.models),
    recent_audit: listOrEmpty(value.recent_audit)
  }
}

export function normalizeAPIKeyRecord(value: APIKeyRecordPayload): APIKeyRecord {
  const payload = value ?? {} as APIKeyRecordPayload
  return {
    ...payload,
    scopes: stringListOrEmpty(payload.scopes),
    model_allowlist: stringListOrEmpty(payload.model_allowlist),
    allowed_modalities: stringListOrEmpty(payload.allowed_modalities),
    allowed_operations: stringListOrEmpty(payload.allowed_operations),
    allowed_cidrs: stringListOrEmpty(payload.allowed_cidrs)
  }
}

export function normalizeAPIKeyCreateResponse(value: APIKeyCreateResponsePayload): APIKeyCreateResponse {
  const payload = value ?? {} as APIKeyCreateResponsePayload
  return { ...payload, record: normalizeAPIKeyRecord(payload.record) }
}

export function normalizeUsageReport(value: UsageReportPayload): UsageReport {
  const payload = value ?? {} as UsageReportPayload
  return {
    ...payload,
    by_model: listOrEmpty(payload.by_model),
    recent: listOrEmpty(payload.recent)
  }
}

export function normalizeArtifactAdminDetail(value: ArtifactAdminDetailPayload): ArtifactAdminDetail {
  const payload = value ?? {} as ArtifactAdminDetailPayload
  return { ...payload, events: listOrEmpty(payload.events) }
}

export function normalizeAIJobAdminDetail(value: AIJobAdminDetailPayload): AIJobAdminDetail {
  const payload = value ?? {} as AIJobAdminDetailPayload
  return {
    ...payload,
    attempts: listOrEmpty(payload.attempts),
    events: listOrEmpty(payload.events),
    artifacts: listOrEmpty(payload.artifacts)
  }
}

export function normalizeEffectivePricingDecision(value: EffectivePricingDecisionPayload): EffectivePricingDecision {
  const payload = value ?? {} as EffectivePricingDecisionPayload
  return {
    ...payload,
    reason_codes: stringListOrEmpty(payload.reason_codes),
    last_evaluation_reason_codes: stringListOrEmpty(payload.last_evaluation_reason_codes)
  }
}

export function normalizeEffectivePricingDecisionEvaluation(value: EffectivePricingDecisionEvaluationPayload): EffectivePricingDecisionEvaluation {
  const payload = value ?? {} as EffectivePricingDecisionEvaluationPayload
  return { ...payload, reason_codes: stringListOrEmpty(payload.reason_codes) }
}

export function normalizeProviderBillingRoutingHealth(value: ProviderBillingRoutingHealthPayload | null | undefined): ProviderBillingRoutingHealth | undefined {
  if (!value) return undefined
  return { ...value, reason_codes: stringListOrEmpty(value.reason_codes) }
}

export function normalizeProviderBillingSource(value: ProviderBillingSourcePayload): ProviderBillingSource {
  const payload = value ?? {} as ProviderBillingSourcePayload
  return {
    ...payload,
    warnings: stringListOrEmpty(payload.warnings),
    routing_health: normalizeProviderBillingRoutingHealth(payload.routing_health)
  }
}

export function normalizeEffectivePricingReport(value: EffectivePricingReportPayload): EffectivePricingReport {
  const payload = value ?? {} as EffectivePricingReportPayload
  return {
    ...payload,
    rows: listOrEmpty(payload.rows).map((row) => ({
      ...row,
      reason_codes: stringListOrEmpty(row.reason_codes),
      provider_billing_routing_health: normalizeProviderBillingRoutingHealth(row.provider_billing_routing_health)
    })),
    decisions: listOrEmpty(payload.decisions).map(normalizeEffectivePricingDecision)
  }
}

export function normalizeProviderBillingSourceInspection(value: ProviderBillingSourceInspectionPayload): ProviderBillingSourceInspection {
  const payload = value ?? {} as ProviderBillingSourceInspectionPayload
  return {
    ...payload,
    usage_aggregates: listOrEmpty(payload.usage_aggregates),
    warnings: stringListOrEmpty(payload.warnings)
  }
}

export function normalizeProviderBillingSyncResult(value: ProviderBillingSyncResultPayload): ProviderBillingSyncResult {
  const payload = value ?? {} as ProviderBillingSyncResultPayload
  return {
    ...payload,
    source: normalizeProviderBillingSource(payload.source),
    aggregates: listOrEmpty(payload.aggregates)
  }
}

export function normalizeGatewayPolicyExplanation(value: GatewayPolicyExplanation | null | undefined): GatewayPolicyExplanation {
  const payload = value ?? {} as GatewayPolicyExplanation
  return { ...payload, candidates: listOrEmpty(payload.candidates) }
}
