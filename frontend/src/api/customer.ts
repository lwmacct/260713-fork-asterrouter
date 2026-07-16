import { apiClient } from '@/api/client'
import { listOrEmpty } from '@/api/normalizers'

export interface CustomerPaymentChannel {
  id: 'wechat' | 'alipay'
  name: string
  enabled: boolean
}

export interface CustomerVoucher {
  id: string
  title: string
  amount_cents: number
  minimum_recharge_cents: number
  status: string
  expires_at?: string
  created_at: string
}

export interface CustomerBillingOverview {
  balance_cents: number
  gift_balance_cents: number
  profit_balance_cents: number
  total_cents: number
  recharge_options: number[]
  payment_channels: CustomerPaymentChannel[]
  vouchers: CustomerVoucher[]
}

export interface CustomerBillingEntry {
  id: string
  kind: string
  amount_cents: number
  balance_after_cents: number
  reference: string
  description: string
  created_at: string
}

export interface CustomerBillingEntries {
  items: CustomerBillingEntry[]
  total: number
  limit: number
  offset: number
}

export interface CustomerBillingQuery {
  kind?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export interface CustomerRedeemResult {
  entry: CustomerBillingEntry
  overview: CustomerBillingOverview
}

export type CustomerNotificationChannel = 'inapp' | 'email'

export interface CustomerNotificationPreference {
  event_type: string
  enabled: boolean
  channels: CustomerNotificationChannel[]
  threshold?: number
  marketing: boolean
  updated_at?: string
}

export interface CustomerNotificationSettings {
  preferences: CustomerNotificationPreference[]
}

export interface CustomerNotification {
  id: string
  type: string
  title: string
  content: string
  link?: string
  is_read: boolean
  read_at?: string
  created_at: string
}

export interface CustomerNotificationList {
  items: CustomerNotification[]
  total: number
  unread: number
  limit: number
  offset: number
}

function normalizeBillingOverview(overview: CustomerBillingOverview | null | undefined): CustomerBillingOverview {
  const payload = overview ?? {} as CustomerBillingOverview
  return {
    ...payload,
    recharge_options: listOrEmpty(payload.recharge_options),
    payment_channels: listOrEmpty(payload.payment_channels),
    vouchers: listOrEmpty(payload.vouchers)
  }
}

function normalizeNotificationSettings(settings: CustomerNotificationSettings | null | undefined): CustomerNotificationSettings {
  const payload = settings ?? {} as CustomerNotificationSettings
  return {
    ...payload,
    preferences: listOrEmpty(payload.preferences).map((preference) => ({
      ...preference,
      channels: listOrEmpty(preference.channels)
    }))
  }
}

export async function getCustomerBilling(): Promise<CustomerBillingOverview> {
  const response = await apiClient.get<CustomerBillingOverview>('/customer/billing')
  return normalizeBillingOverview(response.data)
}

export async function getCustomerBillingEntries(query: CustomerBillingQuery = {}): Promise<CustomerBillingEntries> {
  const response = await apiClient.get<CustomerBillingEntries>('/customer/billing/entries', { params: query })
  return { ...response.data, items: listOrEmpty(response.data.items) }
}

export async function redeemCustomerCode(code: string): Promise<CustomerRedeemResult> {
  const response = await apiClient.post<CustomerRedeemResult>('/customer/billing/redeem', { code })
  return { ...response.data, overview: normalizeBillingOverview(response.data?.overview) }
}

export async function createCustomerRechargeOrder(payload: {
  amount_cents: number
  payment_method: 'wechat' | 'alipay'
  voucher_id?: string
}): Promise<void> {
  await apiClient.post('/customer/billing/recharge-orders', payload)
}

export async function downloadCustomerBillingCSV(query: CustomerBillingQuery = {}): Promise<void> {
  const response = await apiClient.get<Blob>('/customer/billing/entries/export', { params: query, responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `billing-${new Date().toISOString().slice(0, 10)}.csv`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export async function getCustomerNotificationSettings(): Promise<CustomerNotificationSettings> {
  return normalizeNotificationSettings((await apiClient.get<CustomerNotificationSettings>('/customer/notification-settings')).data)
}

export async function updateCustomerNotificationSettings(preferences: CustomerNotificationPreference[]): Promise<CustomerNotificationSettings> {
  return normalizeNotificationSettings((await apiClient.put<CustomerNotificationSettings>('/customer/notification-settings', { preferences })).data)
}

export async function getCustomerNotifications(limit = 20, offset = 0): Promise<CustomerNotificationList> {
  const response = (await apiClient.get<CustomerNotificationList>('/customer/notifications', { params: { limit, offset } })).data
  return { ...response, items: listOrEmpty(response.items) }
}

export async function markCustomerNotificationRead(id: string): Promise<void> {
  await apiClient.post(`/customer/notifications/${encodeURIComponent(id)}/read`)
}

export async function markAllCustomerNotificationsRead(): Promise<void> {
  await apiClient.post('/customer/notifications/read-all')
}
