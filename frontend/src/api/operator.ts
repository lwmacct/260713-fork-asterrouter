import { apiClient } from './client'
import type { APIKeyCreateRequest, APIKeyCreateResponse, APIKeyRecord, GatewayRiskBlock, OperatorBalanceEntry, OperatorCustomer, OperatorCustomerGroup, OperatorDashboard, OperatorNotice, OperatorPlan, OperatorPricingRule, OperatorRiskRule, UsageReport } from '@/types'

export type OperatorResource = 'customer-groups'|'customers'|'plans'|'pricing-rules'|'risk-rules'|'notices'
export type OperatorEntity = OperatorCustomerGroup|OperatorCustomer|OperatorPlan|OperatorPricingRule|OperatorRiskRule|OperatorNotice

export async function getOperatorDashboard():Promise<OperatorDashboard>{return (await apiClient.get<OperatorDashboard>('/operator/dashboard')).data}
export async function listOperatorResource<T extends OperatorEntity>(resource:OperatorResource):Promise<T[]>{return (await apiClient.get<T[]>(`/operator/${resource}`)).data || []}
export async function createOperatorResource<T extends OperatorEntity>(resource:OperatorResource,payload:Record<string,unknown>):Promise<T>{return (await apiClient.post<T>(`/operator/${resource}`,payload)).data}
export async function updateOperatorResource<T extends OperatorEntity>(resource:OperatorResource,id:string,payload:Record<string,unknown>):Promise<T>{return (await apiClient.put<T>(`/operator/${resource}/${id}`,payload)).data}
export async function deleteOperatorResource(resource:OperatorResource,id:string):Promise<void>{await apiClient.delete(`/operator/${resource}/${id}`)}
export async function getOperatorBalances():Promise<OperatorBalanceEntry[]>{return (await apiClient.get<OperatorBalanceEntry[]>('/operator/balance-entries')).data || []}
export async function createOperatorBalance(payload:Record<string,unknown>):Promise<OperatorBalanceEntry>{return (await apiClient.post<OperatorBalanceEntry>('/operator/balance-entries',payload)).data}
export async function getOperatorCustomerKeys():Promise<APIKeyRecord[]>{return (await apiClient.get<APIKeyRecord[]>('/operator/customer-keys')).data || []}
export async function rotateOperatorCustomerKey(id:string):Promise<APIKeyCreateResponse>{return (await apiClient.post<APIKeyCreateResponse>(`/operator/customer-keys/${id}/rotate`)).data}
export async function disableOperatorCustomerKey(id:string):Promise<void>{await apiClient.post(`/operator/customer-keys/${id}/disable`)}
export async function createOperatorCustomerKey(customerID:string,payload:APIKeyCreateRequest):Promise<APIKeyCreateResponse>{return (await apiClient.post<APIKeyCreateResponse>(`/operator/customers/${customerID}/keys`,payload)).data}
export async function getOperatorUsage(params?:Record<string,unknown>):Promise<UsageReport>{return (await apiClient.get<UsageReport>('/operator/usage',{params})).data}
export async function getOperatorRiskBlocks():Promise<GatewayRiskBlock[]>{return (await apiClient.get<GatewayRiskBlock[]>('/operator/risk-blocks')).data || []}
export async function clearOperatorRiskBlock(apiKeyID:string):Promise<void>{await apiClient.delete(`/operator/risk-blocks/${encodeURIComponent(apiKeyID)}`)}
