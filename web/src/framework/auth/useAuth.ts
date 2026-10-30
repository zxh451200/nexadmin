import type { ComponentType } from 'react'
import { create } from 'zustand'
import { clearPwaRuntimeCache } from '@/framework/pwa/register'
import { api, setUnauthorizedHandler, tokenStore } from '@/framework/request/http'
import type { AuthUser, ChallengeResult, LoginChallenge, LoginPayload, LoginResult, LoginStep } from './types'

/** 插件回调交接：challenge 为空表示已完成登录（令牌已写入），非空表示仍需二次验证 */
export interface LoginContinuation {
  challenge: LoginChallenge | null
  next: string
}

// 插件可以把自己的登录入口（如企业 SSO）挂到登录页，框架不感知具体接口。
const loginPanelComponents: ComponentType[] = []
export function registerLoginPanel(c: ComponentType) {
  if (!loginPanelComponents.includes(c)) loginPanelComponents.push(c)
}
export const loginPanels = loginPanelComponents

const SAML_SOURCE_KEY = 'nexadmin.samlSource'

// 插件提供回调交接，框架登录页不依赖具体插件或其接口路径。
const loginContinuations = new Map<string, () => Promise<LoginContinuation> | null>()
export function registerLoginContinuation(name: string, resume: () => Promise<LoginContinuation> | null) {
  loginContinuations.set(name, resume)
}
export function resumeLoginContinuation(): Promise<LoginContinuation> | null {
  for (const resume of loginContinuations.values()) {
    const pending = resume()
    if (pending) return pending
  }
  return null
}

interface AuthState {
  user: AuthUser | null
  token: string | null
  perms: string[]
  /** 账号密码 / 手机号登录；服务端要求二次验证时返回 { challenge }，此时尚未登录 */
  login: (payload: LoginPayload) => Promise<LoginStep>
  /** 第三方登录 / 绑定已有账号成功后，用服务端返回的令牌建立登录态 */
  setSession: (res: LoginResult) => AuthUser
  /** 处理可能要求二次验证的登录响应：有令牌则建立登录态，否则原样返回挑战 */
  acceptLogin: (res: LoginResult | ChallengeResult) => LoginStep
  /** 提交二次验证码（或恢复码）换取令牌 */
  verifyLogin: (challengeId: string, code: string, kind?: string) => Promise<LoginStep>
  logout: () => Promise<void>
  fetchMe: () => Promise<AuthUser>
  reset: () => void
}

export const useAuth = create<AuthState>((set, get) => ({
  user: null,
  token: tokenStore.access,
  perms: [],
  login: async (payload) => {
    const res = await api.post<LoginResult | ChallengeResult>('/auth/login', payload)
    return get().acceptLogin(res)
  },
  acceptLogin: (res) => {
    if ('challenge' in res) return res
    tokenStore.set(res.accessToken, res.refreshToken)
    if (res.samlSourceId) sessionStorage.setItem(SAML_SOURCE_KEY, res.samlSourceId)
    else sessionStorage.removeItem(SAML_SOURCE_KEY)
    set({ token: res.accessToken, user: res.user, perms: res.user.perms })
    return { user: res.user }
  },
  verifyLogin: async (challengeId, code, kind) => {
    // 错误由调用方（二次验证对话框）在界面内显示，不再全局 toast
    const res = await api.post<LoginResult | ChallengeResult>('/auth/login/verify', { challengeId, code, ...(kind ? { kind } : {}) }, { silent: true })
    return get().acceptLogin(res)
  },
  setSession: (res) => {
    tokenStore.set(res.accessToken, res.refreshToken)
    set({ token: res.accessToken, user: res.user, perms: res.user.perms })
    return res.user
  },
  logout: async () => {
    // SAML 会话：先让后端吊销并拿到 IdP 的 SLO 跳转地址，再清本地并整页跳转。
    const samlId = sessionStorage.getItem(SAML_SOURCE_KEY)
    if (samlId) {
      try {
        const base = import.meta.env.BASE_URL || '/'
        const next = base.endsWith('/') ? base + 'login' : base + '/login'
        const r = await api.post<{ redirect: string }>(
          `/plugins/saml/sources/${encodeURIComponent(samlId)}/logout?next=${encodeURIComponent(next)}`,
          undefined,
          { silent: true },
        )
        sessionStorage.removeItem(SAML_SOURCE_KEY)
        tokenStore.clear()
        clearPwaRuntimeCache()
        set({ token: null, user: null, perms: [] })
        window.location.href = r.redirect
        return
      } catch {
        // 回落本地登出
      }
    }
    try {
      await api.post('/auth/logout', undefined, { silent: true })
    } catch {
      // 忽略：本地登录态照常清理
    }
    sessionStorage.removeItem(SAML_SOURCE_KEY)
    tokenStore.clear()
    clearPwaRuntimeCache()
    set({ token: null, user: null, perms: [] })
  },
  fetchMe: async () => {
    const user = await api.get<AuthUser>('/auth/me')
    set({ user, perms: user.perms, token: tokenStore.access })
    return user
  },
  reset: () => {
    tokenStore.clear()
    clearPwaRuntimeCache()
    set({ token: null, user: null, perms: [] })
  },
}))

setUnauthorizedHandler(() => useAuth.getState().reset())
