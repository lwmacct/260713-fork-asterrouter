import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { completeTOTPLogin, getCurrentUser, login as loginRequest } from '@/api/auth'
import type { AccountProfile, AuthUser } from '@/types'

const TOKEN_KEY = 'asterrouter_admin_token'
const USER_KEY = 'asterrouter_admin_user'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_KEY) || '')
  const user = ref<AuthUser | null>(readStoredUser())
  const loading = ref(false)
  const error = ref('')

  const isAuthenticated = computed(() => Boolean(token.value))

  async function login(username: string, password: string, agreementAccepted = false, turnstileToken = '') {
    loading.value = true
    error.value = ''
    try {
      const result = await loginRequest(username, password, agreementAccepted, turnstileToken)
      token.value = result.access_token
      user.value = result.user
      localStorage.setItem(TOKEN_KEY, result.access_token)
      localStorage.setItem(USER_KEY, JSON.stringify(result.user))
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Login failed'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return
    try {
      user.value = await getCurrentUser()
      localStorage.setItem(USER_KEY, JSON.stringify(user.value))
    } catch {
      logout()
    }
  }

  async function completeOIDCLogin() {
		token.value = 'oidc-cookie'
		localStorage.setItem(TOKEN_KEY, token.value)
		try {
			user.value = await getCurrentUser()
			localStorage.setItem(USER_KEY, JSON.stringify(user.value))
		} catch (err) {
			logout()
			throw err
		}
	}

		async function completeMFA(challenge: string, code: string) {
			loading.value = true; error.value = ''
			try { const result = await completeTOTPLogin(challenge, code); token.value = result.access_token; user.value = result.user; localStorage.setItem(TOKEN_KEY, result.access_token); localStorage.setItem(USER_KEY, JSON.stringify(result.user)) }
			catch (err) { error.value = err instanceof Error ? err.message : 'MFA failed'; throw err }
			finally { loading.value = false }
		}

	function applyAccountProfile(profile: AccountProfile) {
		if (!user.value) return
		user.value = {
			...user.value,
			username: profile.email || profile.display_name,
			display_name: profile.display_name,
			email: profile.email,
			avatar_data_url: profile.avatar_data_url
		}
		localStorage.setItem(USER_KEY, JSON.stringify(user.value))
	}

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  return {
    token,
    user,
    loading,
    error,
    isAuthenticated,
    login,
    loadCurrentUser,
		completeOIDCLogin,
		completeMFA,
		applyAccountProfile,
		logout
  }
})

function readStoredUser(): AuthUser | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as AuthUser
  } catch {
    return null
  }
}
