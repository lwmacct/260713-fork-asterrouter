import { apiClient } from './client'
import type { AccountProfile, TOTPSetup } from '@/types'

export async function getAccountProfile(): Promise<AccountProfile> {
	return (await apiClient.get<AccountProfile>('/account/profile')).data
}

export async function updateAccountProfile(displayName: string, avatarDataURL: string): Promise<AccountProfile> {
	return (await apiClient.put<AccountProfile>('/account/profile', { display_name: displayName, avatar_data_url: avatarDataURL })).data
}

export async function changeAccountPassword(currentPassword: string, newPassword: string): Promise<void> {
	await apiClient.put('/account/password', { current_password: currentPassword, new_password: newPassword })
}

export async function beginTOTPSetup(): Promise<TOTPSetup> {
	return (await apiClient.post<TOTPSetup>('/account/totp/setup')).data
}

export async function confirmTOTP(code: string): Promise<void> {
	await apiClient.post('/account/totp/confirm', { code })
}

export async function generateTOTPRecoveryCodes(): Promise<string[]> {
	return (await apiClient.post<{ codes: string[] }>('/account/totp/recovery-codes')).data.codes
}

export async function disableTOTP(code: string): Promise<void> {
	await apiClient.delete('/account/totp', { data: { code } })
}

export async function unbindAccountIdentity(provider: string): Promise<AccountProfile> {
	return (await apiClient.delete<AccountProfile>(`/account/identities/${encodeURIComponent(provider)}`)).data
}

export async function beginAccountIdentityBinding(provider: string): Promise<string> {
	return (await apiClient.post<{ authorization_url: string }>(`/account/identities/${encodeURIComponent(provider)}/bind`)).data.authorization_url
}
