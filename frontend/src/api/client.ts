import axios, { AxiosError } from 'axios'
import type { ApiResponse } from '@/types'
import i18n, { getLocale } from '@/i18n'

export class ApiClientError extends Error {
  status: number
  code?: number | string
  payload?: unknown

  constructor(message: string, status: number, code?: number | string, payload?: unknown) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
    this.payload = payload
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
  const path = String(config.url || '')
  const sharedPrefixes = [
    '/dashboard',
    '/providers',
    '/provider-health-checks',
    '/routing-groups',
    '/provider-accounts',
    '/provider-account-health-checks',
    '/gateway-models',
    '/model-routes',
    '/gateway-simulator',
    '/api-keys',
    '/policies',
    '/pricing-rules',
    '/usage',
    '/gateway-traces',
    '/alerts',
    '/audit-logs',
    '/plugins',
    '/settings',
    '/system',
    '/cost-allocation'
  ]
  if (
    path.startsWith('/admin/') &&
    sharedPrefixes.some((prefix) => path === `/admin${prefix}` || path.startsWith(`/admin${prefix}/`))
  ) {
    if (window.location.pathname.startsWith('/console')) config.url = path.replace(/^\/admin/, '/console')
    else if (window.location.pathname.startsWith('/operator')) config.url = path.replace(/^\/admin/, '/operator')
		else if (window.location.pathname.startsWith('/platform')) config.url = path.replace(/^\/admin/, '/platform')
  }
  config.headers['Accept-Language'] = getLocale()
  const token = localStorage.getItem('asterrouter_admin_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

type ErrorEnvelope = ApiResponse<unknown> & { error?: { code?: string; message?: string } }

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
  (error: AxiosError<ErrorEnvelope>) => {
    const status = error.response?.status || 0
    if (status === 401) redirectToLogin()
    const payload = error.response?.data
    const message = normalizeMessage(status, payload?.error?.message || payload?.message || error.message || t('common.networkError'))
    return Promise.reject(new ApiClientError(message, status, payload?.error?.code || payload?.code, payload))
  }
)
