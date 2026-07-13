import { apiClient } from '@/api/client'

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

export async function getCustomerBilling(): Promise<CustomerBillingOverview> {
  const response = await apiClient.get<CustomerBillingOverview>('/customer/billing')
  return response.data
}

export async function getCustomerBillingEntries(query: CustomerBillingQuery = {}): Promise<CustomerBillingEntries> {
  const response = await apiClient.get<CustomerBillingEntries>('/customer/billing/entries', { params: query })
  return response.data
}

export async function redeemCustomerCode(code: string): Promise<CustomerRedeemResult> {
  const response = await apiClient.post<CustomerRedeemResult>('/customer/billing/redeem', { code })
  return response.data
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
