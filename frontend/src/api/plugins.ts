import { apiClient } from './client'
import { listOrEmpty, stringListOrEmpty } from './normalizers'
import type {
  ArtifactSinkDestination,
  ArtifactSinkDestinationRequest,
  LicenseActivateRequest,
  LicenseRedeemRequest,
  LicenseImportRequest,
  OfficialCatalogStatus,
  OfficialFeedClientInfo,
  OfficialFeedImportRequest,
  OfficialFeedSyncResult,
  OfficialFeedSyncRun,
  OfficialFeedStatus,
  OfficialLicenseStatus,
  Plugin,
  PluginCatalog,
  PluginConfig,
  PluginConfigRequest,
  PluginAPIToken,
  PluginAPITokenCreateRequest,
  PluginAPITokenCreateResult,
  PluginDeliveryAttempt,
  PluginPackage,
  PluginPackageInstallation,
  PluginPackageImportRequest,
  PluginPackageDownloadRequest,
  PluginPackageDownloadResult,
  SidecarRuntimeStatus
} from '@/types'

type PluginPayload = Omit<Plugin, 'surfaces' | 'packages'> & {
  surfaces?: string[] | null
  packages?: PluginPackage[] | null
}

function normalizePlugin(plugin: PluginPayload): Plugin {
  return {
    ...plugin,
    surfaces: stringListOrEmpty(plugin.surfaces),
    packages: listOrEmpty(plugin.packages)
  }
}

export async function getPluginCatalog(): Promise<PluginCatalog> {
  const response = await apiClient.get<Omit<PluginCatalog, 'plugins'> & { plugins?: PluginPayload[] | null }>('/admin/plugins')
  return { ...response.data, plugins: listOrEmpty(response.data.plugins).map(normalizePlugin) }
}

export async function enablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<PluginPayload>(`/admin/plugins/${encodeURIComponent(id)}/enable`)
  return normalizePlugin(response.data)
}

export async function disablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<PluginPayload>(`/admin/plugins/${encodeURIComponent(id)}/disable`)
  return normalizePlugin(response.data)
}

export async function getPluginConfig(id: string): Promise<PluginConfig> {
  const response = await apiClient.get<PluginConfig>(`/admin/plugins/${encodeURIComponent(id)}/config`)
  return response.data
}

export async function updatePluginConfig(id: string, payload: PluginConfigRequest): Promise<PluginConfig> {
  const response = await apiClient.put<PluginConfig>(`/admin/plugins/${encodeURIComponent(id)}/config`, payload)
  return response.data
}

export async function getArtifactSinkDestinations(pluginID: string): Promise<ArtifactSinkDestination[]> {
  const response = await apiClient.get<ArtifactSinkDestination[] | null>(`/admin/plugins/${encodeURIComponent(pluginID)}/artifact-sinks`)
  return listOrEmpty(response.data)
}

export async function upsertArtifactSinkDestination(pluginID: string, sinkID: string, payload: ArtifactSinkDestinationRequest): Promise<ArtifactSinkDestination> {
  const response = await apiClient.put<ArtifactSinkDestination>(
    `/admin/plugins/${encodeURIComponent(pluginID)}/artifact-sinks/${encodeURIComponent(sinkID)}`,
    payload
  )
  return response.data
}

export async function deleteArtifactSinkDestination(pluginID: string, sinkID: string): Promise<void> {
  await apiClient.delete(`/admin/plugins/${encodeURIComponent(pluginID)}/artifact-sinks/${encodeURIComponent(sinkID)}`)
}

export async function getPluginAPITokens(pluginID = ''): Promise<PluginAPIToken[]> {
  const response = await apiClient.get<PluginAPIToken[] | null>('/admin/plugins/api-tokens', { params: pluginID ? { plugin_id: pluginID } : undefined })
  return listOrEmpty(response.data).map((token) => ({
    ...token,
    scopes: stringListOrEmpty(token.scopes),
    surfaces: stringListOrEmpty(token.surfaces)
  }))
}

export async function createPluginAPIToken(payload: PluginAPITokenCreateRequest): Promise<PluginAPITokenCreateResult> {
  const response = await apiClient.post<PluginAPITokenCreateResult>('/admin/plugins/api-tokens', payload)
  return response.data
}

export async function revokePluginAPIToken(id: string): Promise<PluginAPIToken> {
  const response = await apiClient.delete<PluginAPIToken>(`/admin/plugins/api-tokens/${encodeURIComponent(id)}`)
  return response.data
}

export async function getOfficialFeedClientInfo(): Promise<OfficialFeedClientInfo> {
  const response = await apiClient.get<OfficialFeedClientInfo>('/admin/plugins/feeds/client')
  return response.data
}

export async function getOfficialFeedStatuses(serviceKey = ''): Promise<OfficialFeedStatus[]> {
  const response = await apiClient.get<OfficialFeedStatus[] | null>('/admin/plugins/feeds', { params: serviceKey ? { service_key: serviceKey } : undefined })
  return listOrEmpty(response.data)
}

export async function importOfficialFeed(payload: OfficialFeedImportRequest): Promise<OfficialFeedStatus> {
  const response = await apiClient.post<OfficialFeedStatus>('/admin/plugins/feeds/import', payload)
  return response.data
}

export async function syncOfficialFeed(serviceKey: string): Promise<OfficialFeedSyncResult> {
  const response = await apiClient.post<OfficialFeedSyncResult>('/admin/plugins/feeds/sync', { service_key: serviceKey })
  return response.data
}

export async function getOfficialFeedSyncRuns(serviceKey = '', limit = 20): Promise<OfficialFeedSyncRun[]> {
  const response = await apiClient.get<OfficialFeedSyncRun[] | null>('/admin/plugins/feeds/sync-runs', {
    params: { ...(serviceKey ? { service_key: serviceKey } : {}), limit }
  })
  return listOrEmpty(response.data)
}

export async function getPluginDeliveries(id: string, params?: { limit?: number; offset?: number; status?: string; alert_id?: string }): Promise<PluginDeliveryAttempt[]> {
  const response = await apiClient.get<PluginDeliveryAttempt[] | null>(`/admin/plugins/${encodeURIComponent(id)}/deliveries`, { params })
  return listOrEmpty(response.data)
}

export async function getOfficialCatalogStatus(): Promise<OfficialCatalogStatus> {
  const response = await apiClient.get<OfficialCatalogStatus>('/admin/plugins/catalog-sync/status')
  return response.data
}

export async function syncOfficialCatalog(): Promise<OfficialCatalogStatus> {
  const response = await apiClient.post<OfficialCatalogStatus>('/admin/plugins/catalog-sync')
  return response.data
}

export async function getOfficialLicenseStatus(): Promise<OfficialLicenseStatus> {
  const response = await apiClient.get<OfficialLicenseStatus>('/admin/plugins/license/status')
  return response.data
}

export async function activateOfficialLicense(payload: LicenseActivateRequest): Promise<OfficialLicenseStatus> {
  const response = await apiClient.post<OfficialLicenseStatus>('/admin/plugins/license/activate', payload)
  return response.data
}

export async function redeemOfficialLicense(payload: LicenseRedeemRequest): Promise<OfficialLicenseStatus> {
  const response = await apiClient.post<OfficialLicenseStatus>('/admin/plugins/license/redeem', payload)
  return response.data
}

export async function importOfficialLicense(payload: LicenseImportRequest): Promise<OfficialLicenseStatus> {
  const response = await apiClient.post<OfficialLicenseStatus>('/admin/plugins/license/import', payload)
  return response.data
}

export async function getPluginPackages(id: string): Promise<PluginPackage[]> {
  const response = await apiClient.get<PluginPackage[] | null>(`/admin/plugins/${encodeURIComponent(id)}/packages`)
  return listOrEmpty(response.data)
}

export async function downloadPluginPackage(id: string, packageID: string, payload: PluginPackageDownloadRequest = {}): Promise<PluginPackageDownloadResult> {
  const response = await apiClient.post<PluginPackageDownloadResult>(
    `/admin/plugins/${encodeURIComponent(id)}/packages/${encodeURIComponent(packageID)}/download`,
    payload
  )
  return response.data
}

export async function installPluginPackage(id: string, packageID: string): Promise<PluginPackageInstallation> {
  const response = await apiClient.post<PluginPackageInstallation>(`/admin/plugins/${encodeURIComponent(id)}/packages/${encodeURIComponent(packageID)}/install`)
  return response.data
}

export async function importPluginPackage(id: string, packageID: string, payload: PluginPackageImportRequest): Promise<PluginPackageDownloadResult> {
  const response = await apiClient.post<PluginPackageDownloadResult>(
    `/admin/plugins/${encodeURIComponent(id)}/packages/${encodeURIComponent(packageID)}/import`,
    payload
  )
  return response.data
}

export async function uninstallPluginPackage(id: string, packageID: string): Promise<PluginPackageInstallation> {
  const response = await apiClient.post<PluginPackageInstallation>(`/admin/plugins/${encodeURIComponent(id)}/packages/${encodeURIComponent(packageID)}/uninstall`)
  return response.data
}

export async function getSidecarRuntimeStatus(id: string): Promise<SidecarRuntimeStatus> {
  const response = await apiClient.get<SidecarRuntimeStatus>(`/admin/plugins/${encodeURIComponent(id)}/runtime/status`)
  return response.data
}
