import { apiClient } from './client'
import { listOrEmpty } from './normalizers'
import type { S3BackupObject, SystemApplyResult, SystemArchiveInfo, SystemRestoreResult, SystemUpdateInfo } from '@/types'

export interface SystemProfileBundle {
  enabled_profiles: string[]
  default_profile: string
}

export async function updateSystemProfiles(enabledProfiles: string[], defaultProfile: string): Promise<SystemProfileBundle> {
  const response = await apiClient.put<SystemProfileBundle>('/system/profiles', {
    enabled_profiles: enabledProfiles,
    default_profile: defaultProfile
  })
  return response.data
}

export async function checkSystemUpdates(force = false): Promise<SystemUpdateInfo> {
  const response = await apiClient.get<SystemUpdateInfo>('/admin/system/check-updates', {
    params: { force }
  })
  return response.data
}

export async function performSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/update')
  return response.data
}

export async function rollbackSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/rollback')
  return response.data
}

export async function restartSystem(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/restart')
  return response.data
}

export async function listSystemBackups(): Promise<SystemArchiveInfo[]> {
  const response = await apiClient.get<SystemArchiveInfo[] | null>('/admin/system/backups')
  return listOrEmpty(response.data)
}

export async function createSystemBackup(): Promise<SystemArchiveInfo> {
  const response = await apiClient.post<SystemArchiveInfo>('/admin/system/backups')
  return response.data
}

export async function testBackupS3(): Promise<void> {
  await apiClient.post('/admin/system/backups/s3/test')
}

export async function listS3Backups(): Promise<S3BackupObject[]> {
  return listOrEmpty((await apiClient.get<S3BackupObject[] | null>('/admin/system/backups/s3')).data)
}

export async function restoreS3Backup(key: string): Promise<SystemRestoreResult> {
  return (await apiClient.post<SystemRestoreResult>('/admin/system/backups/s3/restore', { key, confirm: true })).data
}

export async function downloadS3Backup(backup: S3BackupObject): Promise<void> {
  const path = `/admin/system/backups/s3/download?key=${encodeURIComponent(backup.key)}`
  await downloadArchive(path, `${backup.id}.tar.gz`)
}

export async function restoreSystemBackup(backupID: string): Promise<SystemRestoreResult> {
  const response = await apiClient.post<SystemRestoreResult>('/admin/system/backups/restore', {
    backup_id: backupID,
    confirm: true
  })
  return response.data
}

export async function downloadSystemBackup(backup: SystemArchiveInfo): Promise<void> {
  await downloadArchive(`/admin/system/backups/${encodeURIComponent(backup.id)}/download`, backup.path)
}

export async function createDiagnosticBundle(): Promise<SystemArchiveInfo> {
  const response = await apiClient.post<SystemArchiveInfo>('/admin/system/diagnostics')
  return response.data
}

export async function downloadDiagnosticBundle(bundle: SystemArchiveInfo): Promise<void> {
  await downloadArchive(`/admin/system/diagnostics/${encodeURIComponent(bundle.id)}/download`, bundle.path)
}

async function downloadArchive(path: string, filename: string): Promise<void> {
  const response = await apiClient.get<Blob>(path, { responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'application/gzip' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
