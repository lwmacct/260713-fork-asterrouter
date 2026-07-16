import { apiClient } from './client'
import { listOrEmpty, stringListOrEmpty } from './normalizers'
import type { AdminSettings, EmailTemplate, LegalDocument, LocaleInfo, PublicSettings, RetentionCleanupResult } from '@/types'

function normalizePublicSettings<T extends PublicSettings>(settings: T): T {
  return {
    ...settings,
    enabled_profiles: stringListOrEmpty(settings.enabled_profiles),
    enabled_locales: stringListOrEmpty(settings.enabled_locales),
    legal_documents: listOrEmpty(settings.legal_documents),
    custom_endpoints: listOrEmpty(settings.custom_endpoints),
    custom_menu_items: listOrEmpty(settings.custom_menu_items)
  }
}

function normalizeAdminSettings(settings: AdminSettings): AdminSettings {
  return {
    ...normalizePublicSettings(settings),
    runtime_restart_reasons: stringListOrEmpty(settings.runtime_restart_reasons),
    allowed_email_domains: stringListOrEmpty(settings.allowed_email_domains),
    invitation_codes: stringListOrEmpty(settings.invitation_codes),
    email_templates: listOrEmpty(settings.email_templates),
    page_size_options: listOrEmpty(settings.page_size_options)
  }
}

export async function getPublicSettings(): Promise<PublicSettings> {
  const response = await apiClient.get<PublicSettings>('/settings/public')
  return normalizePublicSettings(response.data)
}

export async function getLegalDocument(slug: string): Promise<LegalDocument> {
  const response = await apiClient.get<LegalDocument>(`/legal/${encodeURIComponent(slug)}`)
  return response.data
}

export async function getAdminSettings(): Promise<AdminSettings> {
  const response = await apiClient.get<AdminSettings>('/admin/settings')
  return normalizeAdminSettings(response.data)
}

export async function updateAdminSettings(payload: AdminSettings): Promise<AdminSettings> {
  const response = await apiClient.put<AdminSettings>('/admin/settings', payload)
  return normalizeAdminSettings(response.data)
}

export async function runRetentionCleanup(): Promise<RetentionCleanupResult> {
	return (await apiClient.post<RetentionCleanupResult>('/admin/settings/retention/cleanup')).data
}

export async function testSMTP(recipient: string): Promise<void> {
	await apiClient.post('/admin/settings/smtp/test', { recipient })
}

export async function getDefaultEmailTemplates(): Promise<EmailTemplate[]> {
  const response = await apiClient.get<EmailTemplate[] | null>('/admin/settings/email-templates/defaults')
  return listOrEmpty(response.data)
}

export async function previewEmailTemplate(subject: string, html: string): Promise<{subject: string; html: string}> {
  const response = await apiClient.post<{subject: string; html: string}>('/admin/settings/email-templates/preview', { subject, html })
  return response.data
}

export async function testEmailTemplate(recipient: string, subject: string, html: string): Promise<void> {
  await apiClient.post('/admin/settings/email-templates/test', { recipient, subject, html })
}

export async function applySetupProfile(profile: string): Promise<PublicSettings> {
  const response = await apiClient.post<PublicSettings>('/setup/profiles', {
    profile
  })
  return normalizePublicSettings(response.data)
}

export async function getLocales(): Promise<LocaleInfo[]> {
  const response = await apiClient.get<LocaleInfo[] | null>('/i18n/locales')
  return listOrEmpty(response.data)
}
