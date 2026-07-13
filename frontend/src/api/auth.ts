import { apiClient } from './client'
import type { AuthUser, LoginResult } from '@/types'

export async function login(username: string, password: string, agreementAccepted = false, turnstileToken = ''): Promise<LoginResult> {
  const response = await apiClient.post<LoginResult>('/auth/login', { username, password, agreement_accepted: agreementAccepted, turnstile_token: turnstileToken })
  return response.data
}

export async function getCurrentUser(): Promise<AuthUser> {
  const response = await apiClient.get<AuthUser>('/auth/me')
  return response.data
}

export async function completeTOTPLogin(challenge: string, code: string): Promise<LoginResult> {
	const response = await apiClient.post<LoginResult>('/auth/totp/login', { challenge, code })
	return response.data
}

export async function register(email: string, password: string, displayName: string, invitationCode = '', agreementAccepted = false) { return (await apiClient.post('/auth/register', { email, password, display_name: displayName, invitation_code: invitationCode, agreement_accepted: agreementAccepted })).data }
export async function verifyEmail(token: string) { return (await apiClient.post('/auth/verify-email', { token })).data }
export async function resendVerification(email: string) { return (await apiClient.post('/auth/resend-verification', { email })).data }
export async function forgotPassword(email: string) { return (await apiClient.post('/auth/forgot-password', { email })).data }
export async function resetPassword(token: string, password: string) { return (await apiClient.post('/auth/reset-password', { token, password })).data }
