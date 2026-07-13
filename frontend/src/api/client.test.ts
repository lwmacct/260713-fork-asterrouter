import type { AxiosRequestConfig, AxiosResponse } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '@/i18n'
import { ApiClientError, apiClient, isNotFoundError } from './client'

const originalAdapter = apiClient.defaults.adapter

function response(config: AxiosRequestConfig, data: unknown, status = 200): AxiosResponse {
  return {
    config: config as AxiosResponse['config'],
    data,
    headers: {},
    status,
    statusText: String(status)
  }
}

describe('api client', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/admin/dashboard')
    setLocale('en-US')
  })

  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
  })

  it('adds auth and locale headers and unwraps successful envelopes', async () => {
    localStorage.setItem('asterrouter_admin_token', 'test-token')
    let captured: AxiosRequestConfig | undefined
    apiClient.defaults.adapter = async (config) => {
      captured = config
      return response(config, { code: 0, message: 'ok', data: { id: 'provider-1' } })
    }

    const result = await apiClient.get('/admin/providers')

    expect(result.data).toEqual({ id: 'provider-1' })
    expect(captured?.headers?.Authorization).toBe('Bearer test-token')
    expect(captured?.headers?.['Accept-Language']).toBe('en-US')
  })

  it.each([
    ['/console/overview', '/console/providers'],
    ['/operator/overview', '/operator/providers'],
    ['/admin/dashboard', '/admin/providers']
  ])('maps shared admin endpoints for the active surface %s', async (browserPath, expectedURL) => {
    window.history.replaceState({}, '', browserPath)
    let capturedURL = ''
    apiClient.defaults.adapter = async (config) => {
      capturedURL = String(config.url)
      return response(config, { code: 0, message: 'ok', data: [] })
    }

    await apiClient.get('/admin/providers')

    expect(capturedURL).toBe(expectedURL)
  })

  it('normalizes API errors and identifies not-found responses', async () => {
    apiClient.defaults.adapter = async (config) => response(config, { code: 40401, message: 'not found', data: null }, 404)

    const error = await apiClient.get('/admin/missing').catch((value) => value)

    expect(error).toBeInstanceOf(ApiClientError)
    expect(error).toMatchObject({ status: 404, code: 40401 })
    expect(error.message).not.toBe('not found')
    expect(isNotFoundError(error)).toBe(true)
    expect(isNotFoundError(new Error('not found'))).toBe(false)
  })
})
