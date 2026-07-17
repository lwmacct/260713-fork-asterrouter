import { describe, expect, it } from 'vitest'
import type { APIKeyRecord } from '@/types'
import { apiKeyLifecycleClass, apiKeyLifecycleStatus, canDisableAPIKey, canRotateAPIKey } from './apiKeys'

function key(overrides: Partial<APIKeyRecord> = {}): APIKeyRecord {
  return {
    id: 'key-1', name: 'Key', fingerprint: 'fingerprint', prefix: 'ar_key', status: 'active', key_type: 'service',
    customer_id: '', owner_user_id: '', profile_scope: '', platform_tenant_id: '', gateway_principal_id: '', tenant_id: '',
    principal_type: '', principal_reference: '', policy_id: '', scopes: [], model_allowlist: [], allowed_modalities: [],
    allowed_operations: [], qps_limit: 0, rpm_limit: 0, tpm_limit: 0, concurrency_limit: 0, monthly_token_limit: 0,
    monthly_budget_micros: 0, monthly_image_limit: 0, monthly_video_seconds_limit: 0, monthly_audio_seconds_limit: 0,
    allowed_cidrs: [], lane_policy: '', artifact_policy: '', rotation_family_id: '', replaces_key_id: '', replaced_by_key_id: '',
    lifecycle_status: '', created_at: '2026-07-14T00:00:00Z', updated_at: '2026-07-14T00:00:00Z', ...overrides
  }
}

describe('API key lifecycle presentation', () => {
  it('uses the server lifecycle and restricts retiring credentials', () => {
    const retiring = key({ lifecycle_status: 'retiring', replaced_by_key_id: 'key-2' })
    expect(apiKeyLifecycleStatus(retiring)).toBe('retiring')
    expect(apiKeyLifecycleClass(retiring)).toBe('status-warning')
    expect(canRotateAPIKey(retiring)).toBe(false)
    expect(canDisableAPIKey(retiring)).toBe(true)
  })

  it('derives lifecycle for backward-compatible responses', () => {
    const retired = key({ replaced_by_key_id: 'key-2', rotation_grace_expires_at: '2026-07-14T01:00:00Z' })
    expect(apiKeyLifecycleStatus(retired, Date.parse('2026-07-14T02:00:00Z'))).toBe('retired')
    expect(canRotateAPIKey(retired)).toBe(false)
    expect(canDisableAPIKey(retired)).toBe(false)
  })
})
