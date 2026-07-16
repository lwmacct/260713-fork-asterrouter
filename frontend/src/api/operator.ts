import { apiClient } from './client'
import { listOrEmpty, normalizeAPIKeyCreateResponse, normalizeAPIKeyRecord, normalizeUsageReport, type APIKeyCreateResponsePayload, type APIKeyRecordPayload, type UsageReportPayload } from './normalizers'
import type { APIKeyCreateRequest, APIKeyCreateResponse, APIKeyRecord, GatewayRiskBlock, OperatorBalanceEntry, OperatorCustomer, OperatorCustomerGroup, OperatorDashboard, OperatorNotice, OperatorPlan, OperatorPricingRule, OperatorRiskRule, UsageReport } from '@/types'

export type OperatorResource = 'customer-groups'|'customers'|'plans'|'pricing-rules'|'risk-rules'|'notices'
export type OperatorEntity = OperatorCustomerGroup|OperatorCustomer|OperatorPlan|OperatorPricingRule|OperatorRiskRule|OperatorNotice

export async function getOperatorDashboard():Promise<OperatorDashboard>{return (await apiClient.get<OperatorDashboard>('/operator/dashboard')).data}
export async function listOperatorResource<T extends OperatorEntity>(resource:OperatorResource):Promise<T[]>{return listOrEmpty((await apiClient.get<T[] | null>(`/operator/${resource}`)).data)}
export async function createOperatorResource<T extends OperatorEntity>(resource:OperatorResource,payload:Record<string,unknown>):Promise<T>{return (await apiClient.post<T>(`/operator/${resource}`,payload)).data}
export async function updateOperatorResource<T extends OperatorEntity>(resource:OperatorResource,id:string,payload:Record<string,unknown>):Promise<T>{return (await apiClient.put<T>(`/operator/${resource}/${id}`,payload)).data}
export async function deleteOperatorResource(resource:OperatorResource,id:string):Promise<void>{await apiClient.delete(`/operator/${resource}/${id}`)}
export async function getOperatorBalances():Promise<OperatorBalanceEntry[]>{return listOrEmpty((await apiClient.get<OperatorBalanceEntry[] | null>('/operator/balance-entries')).data)}
export async function createOperatorBalance(payload:Record<string,unknown>):Promise<OperatorBalanceEntry>{return (await apiClient.post<OperatorBalanceEntry>('/operator/balance-entries',payload)).data}
export async function getOperatorCustomerKeys():Promise<APIKeyRecord[]>{return listOrEmpty((await apiClient.get<APIKeyRecordPayload[] | null>('/operator/customer-keys')).data).map(normalizeAPIKeyRecord)}
export async function rotateOperatorCustomerKey(id:string,gracePeriodSeconds=0):Promise<APIKeyCreateResponse>{return normalizeAPIKeyCreateResponse((await apiClient.post<APIKeyCreateResponsePayload>(`/operator/customer-keys/${id}/rotate`,{grace_period_seconds:gracePeriodSeconds})).data)}
export async function disableOperatorCustomerKey(id:string):Promise<void>{await apiClient.post(`/operator/customer-keys/${id}/disable`)}
export async function createOperatorCustomerKey(customerID:string,payload:APIKeyCreateRequest):Promise<APIKeyCreateResponse>{return normalizeAPIKeyCreateResponse((await apiClient.post<APIKeyCreateResponsePayload>(`/operator/customers/${customerID}/keys`,payload)).data)}
export async function getOperatorUsage(params?:Record<string,unknown>):Promise<UsageReport>{return normalizeUsageReport((await apiClient.get<UsageReportPayload>('/operator/usage',{params})).data)}
export async function getOperatorRiskBlocks():Promise<GatewayRiskBlock[]>{return listOrEmpty((await apiClient.get<GatewayRiskBlock[] | null>('/operator/risk-blocks')).data)}
export async function clearOperatorRiskBlock(apiKeyID:string):Promise<void>{await apiClient.delete(`/operator/risk-blocks/${encodeURIComponent(apiKeyID)}`)}
