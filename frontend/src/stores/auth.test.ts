import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { completeTOTPLogin, getCurrentUser, login as loginRequest } from '@/api/auth'
import { makeAuthUser } from '@/test/fixtures'
import type { AccountProfile } from '@/types'
import { useAuthStore } from './auth'

vi.mock('@/api/auth', () => ({
  completeTOTPLogin: vi.fn(),
  getCurrentUser: vi.fn(),
  login: vi.fn()
}))

const loginMock = vi.mocked(loginRequest)
const currentUserMock = vi.mocked(getCurrentUser)
const completeTOTPMock = vi.mocked(completeTOTPLogin)

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('logs in and persists the access token and user', async () => {
    const user = makeAuthUser()
    loginMock.mockResolvedValue({ access_token: 'token-1', token_type: 'Bearer', expires_at: '2099-01-01T00:00:00Z', user })
    const store = useAuthStore()

    await store.login('admin', 'secret', true, 'turnstile-token')

    expect(loginMock).toHaveBeenCalledWith('admin', 'secret', true, 'turnstile-token')
    expect(store.token).toBe('token-1')
    expect(store.user).toEqual(user)
    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('asterrouter_admin_token')).toBe('token-1')
    expect(JSON.parse(localStorage.getItem('asterrouter_admin_user') || '{}')).toEqual(user)
  })

  it('exposes login failure and always clears loading state', async () => {
    loginMock.mockRejectedValue(new Error('invalid credentials'))
    const store = useAuthStore()

    await expect(store.login('admin', 'wrong')).rejects.toThrow('invalid credentials')

    expect(store.loading).toBe(false)
    expect(store.error).toBe('invalid credentials')
    expect(store.isAuthenticated).toBe(false)
  })

  it('logs out when the persisted session cannot load the current user', async () => {
    localStorage.setItem('asterrouter_admin_token', 'expired-token')
    localStorage.setItem('asterrouter_admin_user', JSON.stringify(makeAuthUser()))
    currentUserMock.mockRejectedValue(new Error('session expired'))
    const store = useAuthStore()

    await store.loadCurrentUser()

    expect(store.token).toBe('')
    expect(store.user).toBeNull()
    expect(localStorage.getItem('asterrouter_admin_token')).toBeNull()
    expect(localStorage.getItem('asterrouter_admin_user')).toBeNull()
  })

  it('completes MFA and updates an existing account profile', async () => {
    const user = makeAuthUser({ role: 'developer' })
    completeTOTPMock.mockResolvedValue({ access_token: 'mfa-token', token_type: 'Bearer', expires_at: '2099-01-01T00:00:00Z', user })
    const store = useAuthStore()

    await store.completeMFA('challenge-1', '123456')
    store.applyAccountProfile({
      id: 'user-1',
      email: 'updated@example.com',
      display_name: 'Updated User',
      avatar_data_url: 'data:image/png;base64,AA==',
      status: 'active',
      role: 'developer',
      balance_cents: 0,
      concurrency_limit: 1,
      rpm_limit: 10,
      auth_identities: [],
      email_verified: true,
      password_enabled: true,
      totp_enabled: true,
      totp_available: true,
      managed_by_config: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      login_methods: []
    } satisfies AccountProfile)

    expect(completeTOTPMock).toHaveBeenCalledWith('challenge-1', '123456')
    expect(store.token).toBe('mfa-token')
    expect(store.user?.display_name).toBe('Updated User')
    expect(store.user?.email).toBe('updated@example.com')
  })
})
