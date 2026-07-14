import { apiClient } from './client'
import { listOrEmpty, normalizeDashboard, type DashboardPayload } from './normalizers'
import type { APIKeyCreateRequest, APIKeyCreateResponse, APIKeyRecord, APIKeyUpdateRequest, Dashboard, ExternalAuthIntegration, ExternalAuthIntegrationCreateResponse, ExternalAuthIntegrationRequest, GatewayPrincipal, GatewayPrincipalRequest, PlatformTenant, PlatformTenantRequest, PlatformUsageDeliveryEvent, PlatformUsageSink, PlatformUsageSinkCreateResponse, PlatformUsageSinkRequest } from '@/types'

export async function getPlatformDashboard(): Promise<Dashboard> {
  const response = await apiClient.get<DashboardPayload>('/platform/dashboard')
  return normalizeDashboard(response.data)
}

export async function getPlatformAPIKeys(): Promise<APIKeyRecord[]> {
  return listOrEmpty((await apiClient.get<APIKeyRecord[] | null>('/platform/api-keys')).data)
}

export async function createPlatformAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  return (await apiClient.post<APIKeyCreateResponse>('/platform/api-keys', payload)).data
}

export async function updatePlatformAPIKey(id: string, payload: APIKeyUpdateRequest): Promise<APIKeyRecord> {
  return (await apiClient.put<APIKeyRecord>(`/platform/api-keys/${encodeURIComponent(id)}`, payload)).data
}

export async function rotatePlatformAPIKey(id: string): Promise<APIKeyCreateResponse> {
  return (await apiClient.post<APIKeyCreateResponse>(`/platform/api-keys/${encodeURIComponent(id)}/rotate`)).data
}

export async function disablePlatformAPIKey(id: string): Promise<void> {
  await apiClient.post(`/platform/api-keys/${encodeURIComponent(id)}/disable`)
}

export async function getPlatformTenants(): Promise<PlatformTenant[]> {
  return listOrEmpty((await apiClient.get<PlatformTenant[] | null>('/platform/tenants')).data)
}

export async function createPlatformTenant(payload: PlatformTenantRequest): Promise<PlatformTenant> {
  return (await apiClient.post<PlatformTenant>('/platform/tenants', payload)).data
}

export async function updatePlatformTenant(id: string, payload: PlatformTenantRequest): Promise<PlatformTenant> {
  return (await apiClient.put<PlatformTenant>(`/platform/tenants/${encodeURIComponent(id)}`, payload)).data
}

export async function getGatewayPrincipals(): Promise<GatewayPrincipal[]> {
  return listOrEmpty((await apiClient.get<GatewayPrincipal[] | null>('/platform/gateway-principals')).data)
}

export async function createGatewayPrincipal(payload: GatewayPrincipalRequest): Promise<GatewayPrincipal> {
  return (await apiClient.post<GatewayPrincipal>('/platform/gateway-principals', payload)).data
}

export async function updateGatewayPrincipal(id: string, payload: GatewayPrincipalRequest): Promise<GatewayPrincipal> {
  return (await apiClient.put<GatewayPrincipal>(`/platform/gateway-principals/${encodeURIComponent(id)}`, payload)).data
}

export async function getExternalAuthIntegrations(): Promise<ExternalAuthIntegration[]> {
  return listOrEmpty((await apiClient.get<ExternalAuthIntegration[] | null>('/platform/external-auth-integrations')).data)
}

export async function createExternalAuthIntegration(payload: ExternalAuthIntegrationRequest): Promise<ExternalAuthIntegrationCreateResponse> {
  return (await apiClient.post<ExternalAuthIntegrationCreateResponse>('/platform/external-auth-integrations', payload)).data
}

export async function updateExternalAuthIntegration(id: string, payload: ExternalAuthIntegrationRequest): Promise<ExternalAuthIntegration> {
  return (await apiClient.put<ExternalAuthIntegration>(`/platform/external-auth-integrations/${encodeURIComponent(id)}`, payload)).data
}

export async function rotateExternalAuthIntegrationSecret(id: string): Promise<ExternalAuthIntegrationCreateResponse> {
  return (await apiClient.post<ExternalAuthIntegrationCreateResponse>(`/platform/external-auth-integrations/${encodeURIComponent(id)}/rotate-secret`)).data
}

export async function getPlatformUsageSinks(): Promise<PlatformUsageSink[]> {
  return listOrEmpty((await apiClient.get<PlatformUsageSink[] | null>('/platform/usage-sinks')).data)
}

export async function createPlatformUsageSink(payload: PlatformUsageSinkRequest): Promise<PlatformUsageSinkCreateResponse> {
  return (await apiClient.post<PlatformUsageSinkCreateResponse>('/platform/usage-sinks', payload)).data
}

export async function updatePlatformUsageSink(id: string, payload: PlatformUsageSinkRequest): Promise<PlatformUsageSink> {
  return (await apiClient.put<PlatformUsageSink>(`/platform/usage-sinks/${encodeURIComponent(id)}`, payload)).data
}

export async function rotatePlatformUsageSinkEndpoint(id: string, endpointURL: string, signingSecret?: string): Promise<PlatformUsageSinkCreateResponse> {
  return (await apiClient.post<PlatformUsageSinkCreateResponse>(`/platform/usage-sinks/${encodeURIComponent(id)}/rotate-endpoint`, {
    endpoint_url: endpointURL,
    signing_secret: signingSecret || ''
  })).data
}

export async function getPlatformUsageDeliveries(sinkID: string, status = ''): Promise<PlatformUsageDeliveryEvent[]> {
  return listOrEmpty((await apiClient.get<PlatformUsageDeliveryEvent[] | null>(`/platform/usage-sinks/${encodeURIComponent(sinkID)}/deliveries`, { params: status ? { status } : undefined })).data)
}

export async function requeuePlatformUsageDelivery(sinkID: string, deliveryID: string): Promise<void> {
  await apiClient.post(`/platform/usage-sinks/${encodeURIComponent(sinkID)}/deliveries/${encodeURIComponent(deliveryID)}/requeue`)
}
