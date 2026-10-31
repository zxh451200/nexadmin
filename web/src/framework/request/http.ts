import axios, { type AxiosRequestConfig, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import i18n from 'i18next'
import { toast } from 'sonner'
import { MOCK_ENABLED } from '@/framework/env'
import { mockAdapter } from './mock'
import type { ApiResponse } from './types'

declare module 'axios' {
  interface AxiosRequestConfig {
    /** 不弹错误 toast */
    silent?: boolean
    _retry?: boolean
    /** 密码复核的 401 由调用页面处理，不刷新、重放或清理当前会话。 */
    preserveSessionOnUnauthorized?: boolean
  }
}

export const ACCESS_KEY = 'nex-access-token'
const REFRESH_KEY = 'nex-refresh-token'

export const tokenStore = {
  get access(): string | null {
    return localStorage.getItem(ACCESS_KEY)
  },
  get refresh(): string | null {
    return localStorage.getItem(REFRESH_KEY)
  },
  set(access: string, refresh?: string) {
    localStorage.setItem(ACCESS_KEY, access)
    if (refresh) localStorage.setItem(REFRESH_KEY, refresh)
  },
  clear() {
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
  },
}

export class ApiError extends Error {
  code: number
  status?: number
  /** 服务端返回的机器可识别原因，如 CAPTCHA_REQUIRED */
  reason?: string
  constructor(code: number, message: string, status?: number, reason?: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.reason = reason
  }
}

type UnauthorizedHandler = () => void
let unauthorizedHandler: UnauthorizedHandler | null = null
/** auth store 注册：刷新失败或无 token 时清理登录态 */
export function setUnauthorizedHandler(fn: UnauthorizedHandler) {
  unauthorizedHandler = fn
}

/** 租户过期或停用时服务端返回的 reason（HTTP 403） */
export const TENANT_EXPIRED = 'TENANT_EXPIRED'

let expiredToastAt = 0
/** 租户过期/停用：已登录则清理会话回到登录页，并给出（3 秒内去重的）明确提示 */
function handleTenantExpired(err: ApiError, silent?: boolean): ApiError {
  if (tokenStore.access) {
    tokenStore.clear()
    unauthorizedHandler?.()
  }
  const now = Date.now()
  if (!silent && now - expiredToastAt > 3000) {
    expiredToastAt = now
    toast.error(i18n.t('tenant.expiredNotice'))
  }
  return err
}

export const useMock = MOCK_ENABLED

export const http = axios.create({
  baseURL: import.meta.env.VITE_API_BASE || '/api/v1',
  timeout: 20000,
  ...(useMock ? { adapter: mockAdapter } : {}),
})

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = tokenStore.access
  if (token) config.headers.set('Authorization', `Bearer ${token}`)
  const lang = localStorage.getItem('nex-lang')
  if (lang) config.headers.set('Accept-Language', lang)
  return config
})

let refreshing: Promise<string> | null = null

function doRefresh(): Promise<string> {
  if (!refreshing) {
    const refreshToken = tokenStore.refresh
    refreshing = http
      .post<{ accessToken: string; refreshToken?: string }>(
        '/auth/refresh',
        { refreshToken },
        { silent: true, _retry: true },
      )
      .then((res) => {
        tokenStore.set(res.data.accessToken, res.data.refreshToken)
        return res.data.accessToken
      })
      .finally(() => {
        refreshing = null
      })
  }
  return refreshing
}

function notify(message: string) {
  toast.error(message)
}

async function handleUnauthorized(config: AxiosRequestConfig | undefined, err: ApiError): Promise<AxiosResponse> {
  if (config?.preserveSessionOnUnauthorized) throw err
  if (config && !config._retry && tokenStore.refresh) {
    try {
      const access = await doRefresh()
      const retry: AxiosRequestConfig = { ...config, _retry: true }
      retry.headers = { ...(config.headers as Record<string, unknown>), Authorization: `Bearer ${access}` } as never
      return http.request(retry)
    } catch {
      // 落到下面清理登录态
    }
  }
  tokenStore.clear()
  unauthorizedHandler?.()
  throw err
}

http.interceptors.response.use(
  (res) => {
    const body = res.data as ApiResponse | undefined
    if (body && typeof body === 'object' && typeof body.code === 'number') {
      if (body.code === 0) {
        res.data = body.data
        return res
      }
      const err = new ApiError(body.code, body.message, res.status, body.reason)
      if (body.code === 401) return handleUnauthorized(res.config, err)
      if (body.reason === TENANT_EXPIRED) return Promise.reject(handleTenantExpired(err, res.config.silent))
      // 423 账号锁定由登录页内联展示剩余时间，不弹 toast
      if (!res.config.silent && body.code !== 423) notify(body.message || i18n.t('error.unknown'))
      return Promise.reject(err)
    }
    return res
  },
  async (error) => {
    const res = error.response as AxiosResponse<ApiResponse> | undefined
    const config = error.config as AxiosRequestConfig | undefined
    // responseType=blob（文件导出）时，错误响应的 JSON 体也是 Blob：先解出来，才能拿到服务端的明确提示（如导出行数超限）
    if (res?.data instanceof Blob) {
      try {
        res.data = JSON.parse(await (res.data as Blob).text()) as ApiResponse
      } catch {
        // 不是 JSON：沿用 axios 的错误信息
      }
    }
    if (!res) {
      if (!config?.silent) notify(i18n.t('error.network'))
      return Promise.reject(new ApiError(-1, i18n.t('error.network')))
    }
    const message = res.data?.message || error.message
    const err = new ApiError(res.data?.code ?? res.status, message, res.status, res.data?.reason)
    if (res.status === 401) return handleUnauthorized(config, err)
    if (res.status === 403 && err.reason === TENANT_EXPIRED) return Promise.reject(handleTenantExpired(err, config?.silent))
    // 503 NOT_INITIALIZED 由 SetupGate 处理，不弹 toast
    if (res.status !== 503 && !config?.silent) {
      notify(res.status >= 500 ? i18n.t('error.server') : message)
    }
    return Promise.reject(err)
  },
)

export const api = {
  get: <T>(url: string, params?: unknown, config?: AxiosRequestConfig) =>
    http.get<T>(url, { ...config, params }).then((r) => r.data),
  post: <T>(url: string, body?: unknown, config?: AxiosRequestConfig) =>
    http.post<T>(url, body, config).then((r) => r.data),
  put: <T>(url: string, body?: unknown, config?: AxiosRequestConfig) =>
    http.put<T>(url, body, config).then((r) => r.data),
  del: <T>(url: string, params?: unknown, config?: AxiosRequestConfig) =>
    http.delete<T>(url, { ...config, params }).then((r) => r.data),
}
