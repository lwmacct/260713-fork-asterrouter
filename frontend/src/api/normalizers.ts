import type { Dashboard } from '@/types'

export type NullableList<T> = T[] | null | undefined
export type DashboardPayload = Omit<Dashboard, 'models' | 'recent_audit'> & {
  models?: string[] | null
  recent_audit?: Dashboard['recent_audit'] | null
}

export function listOrEmpty<T>(value: NullableList<T>): T[] {
  return Array.isArray(value) ? value : []
}

export function normalizeDashboard(value: DashboardPayload): Dashboard {
  return {
    ...value,
    models: listOrEmpty(value.models),
    recent_audit: listOrEmpty(value.recent_audit)
  }
}
