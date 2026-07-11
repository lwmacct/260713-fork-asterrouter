import axios, { AxiosError } from 'axios'
import type { ApiResponse } from '@/types'
import i18n, { getLocale } from '@/i18n'

export class ApiClientError extends Error {
  status: number
  code?: number

  constructor(message: string, status: number, code?: number) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
  }
}

export function isNotFoundError(err: unknown): boolean {
  return err instanceof ApiClientError && err.status === 404
}

function t(key: string): string {
  return i18n.global.t(key)
}

function redirectToLogin() {
  localStorage.removeItem('asterrouter_admin_token')
  if (!window.location.pathname.startsWith('/login')) {
    const redirect = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.location.assign(`/login?redirect=${encodeURIComponent(redirect)}`)
  }
}

function normalizeMessage(status: number, message?: string): string {
  const value = String(message || '').trim().toLowerCase()
  if (status === 401 || value === 'login required') return t('common.sessionExpired')
  if (status === 404 || value === 'not found') return t('common.resourceUnavailable')
  return message || t('common.failed')
}

export const apiClient = axios.create({
  baseURL: '/api/v1',
  timeout: 20000,
  headers: {
    'Content-Type': 'application/json'
  }
})

apiClient.interceptors.request.use((config) => {
  config.headers['Accept-Language'] = getLocale()
  const token = localStorage.getItem('asterrouter_admin_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

apiClient.interceptors.response.use(
  (response) => {
    const payload = response.data as ApiResponse<unknown>
    if (payload && typeof payload === 'object' && 'code' in payload) {
      if (payload.code === 0) {
        response.data = payload.data
        return response
      }
      const status = response.status
      if (status === 401) redirectToLogin()
      return Promise.reject(new ApiClientError(normalizeMessage(status, payload.message), status, payload.code))
    }
    return response
  },
  (error: AxiosError<ApiResponse<unknown>>) => {
    const status = error.response?.status || 0
    if (status === 401) redirectToLogin()
    const message = normalizeMessage(status, error.response?.data?.message || error.message || t('common.networkError'))
    return Promise.reject(new ApiClientError(message, status, error.response?.data?.code))
  }
)
