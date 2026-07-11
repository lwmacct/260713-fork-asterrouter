import { apiClient } from './client'
import type {
  LicenseActivateRequest,
  LicenseImportRequest,
  OfficialCatalogStatus,
  OfficialLicenseStatus,
  Plugin,
  PluginCatalog,
  PluginConfig,
  PluginConfigRequest,
  PluginDeliveryAttempt,
  PluginPackage,
  PluginPackageInstallation,
  PluginPackageImportRequest,
  PluginPackageDownloadRequest,
  PluginPackageDownloadResult,
  SidecarRuntimeStatus
} from '@/types'

export async function getPluginCatalog(): Promise<PluginCatalog> {
  const response = await apiClient.get<PluginCatalog>('/admin/plugins')
  return response.data
}

export async function enablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<Plugin>(`/admin/plugins/${encodeURIComponent(id)}/enable`)
  return response.data
}

export async function disablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<Plugin>(`/admin/plugins/${encodeURIComponent(id)}/disable`)
  return response.data
}

export async function getPluginConfig(id: string): Promise<PluginConfig> {
  const response = await apiClient.get<PluginConfig>(`/admin/plugins/${encodeURIComponent(id)}/config`)
  return response.data
}

export async function updatePluginConfig(id: string, payload: PluginConfigRequest): Promise<PluginConfig> {
  const response = await apiClient.put<PluginConfig>(`/admin/plugins/${encodeURIComponent(id)}/config`, payload)
  return response.data
}

export async function getPluginDeliveries(id: string, params?: { limit?: number; offset?: number; status?: string; alert_id?: string }): Promise<PluginDeliveryAttempt[]> {
  const response = await apiClient.get<PluginDeliveryAttempt[]>(`/admin/plugins/${encodeURIComponent(id)}/deliveries`, { params })
  return response.data
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

export async function importOfficialLicense(payload: LicenseImportRequest): Promise<OfficialLicenseStatus> {
  const response = await apiClient.post<OfficialLicenseStatus>('/admin/plugins/license/import', payload)
  return response.data
}

export async function getPluginPackages(id: string): Promise<PluginPackage[]> {
  const response = await apiClient.get<PluginPackage[]>(`/admin/plugins/${encodeURIComponent(id)}/packages`)
  return response.data
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
