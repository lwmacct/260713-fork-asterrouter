import { apiClient } from './client'
import type { AdminSettings, EmailTemplate, LegalDocument, LocaleInfo, PublicSettings, RetentionCleanupResult } from '@/types'

export async function getPublicSettings(): Promise<PublicSettings> {
  const response = await apiClient.get<PublicSettings>('/settings/public')
  return response.data
}

export async function getLegalDocument(slug: string): Promise<LegalDocument> {
  const response = await apiClient.get<LegalDocument>(`/legal/${encodeURIComponent(slug)}`)
  return response.data
}

export async function getAdminSettings(): Promise<AdminSettings> {
  const response = await apiClient.get<AdminSettings>('/admin/settings')
  return response.data
}

export async function updateAdminSettings(payload: AdminSettings): Promise<AdminSettings> {
  const response = await apiClient.put<AdminSettings>('/admin/settings', payload)
  return response.data
}

export async function runRetentionCleanup(): Promise<RetentionCleanupResult> {
	return (await apiClient.post<RetentionCleanupResult>('/admin/settings/retention/cleanup')).data
}

export async function testSMTP(recipient: string): Promise<void> {
	await apiClient.post('/admin/settings/smtp/test', { recipient })
}

export async function getDefaultEmailTemplates(): Promise<EmailTemplate[]> {
  const response = await apiClient.get<EmailTemplate[]>('/admin/settings/email-templates/defaults')
  return response.data
}

export async function previewEmailTemplate(subject: string, html: string): Promise<{subject: string; html: string}> {
  const response = await apiClient.post<{subject: string; html: string}>('/admin/settings/email-templates/preview', { subject, html })
  return response.data
}

export async function testEmailTemplate(recipient: string, subject: string, html: string): Promise<void> {
  await apiClient.post('/admin/settings/email-templates/test', { recipient, subject, html })
}

export async function applySetupProfiles(profiles: string[], defaultProfile: string): Promise<AdminSettings> {
  const response = await apiClient.post<AdminSettings>('/setup/profiles', {
    profiles,
    default_profile: defaultProfile
  })
  return response.data
}

export async function getLocales(): Promise<LocaleInfo[]> {
  const response = await apiClient.get<LocaleInfo[]>('/i18n/locales')
  return response.data
}
