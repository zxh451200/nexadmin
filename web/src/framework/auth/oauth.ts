import { api } from '@/framework/request/http'
import { ADMIN_BASE } from '@/framework/router/base'

const PENDING_KEY = 'nexadmin.oauth.pending'

export interface OAuthPending {
  /** login：登录页发起；bind：个人中心发起绑定 */
  mode: 'login' | 'bind'
  /** 登录成功后回跳的站内路径 */
  next: string
}

/** 第三方授权完成后浏览器跳回的前端页面；必须出现在服务端 auth.providers.<name>.redirect_uris 白名单里 */
export function oauthRedirectUri(provider: string) {
  return `${window.location.origin}${ADMIN_BASE}/login/oauth/${provider}`
}

export function readPending(): OAuthPending | null {
  try {
    const raw = sessionStorage.getItem(PENDING_KEY)
    return raw ? (JSON.parse(raw) as OAuthPending) : null
  } catch {
    return null
  }
}

export function clearPending() {
  sessionStorage.removeItem(PENDING_KEY)
}

/** 只允许站内相对路径，防开放重定向 */
export function safeNext(raw: string | undefined | null) {
  return raw && raw.startsWith('/') && !raw.startsWith('//') && !raw.startsWith('/login') ? raw : '/'
}

function go(authUrl: string, pending: OAuthPending) {
  sessionStorage.setItem(PENDING_KEY, JSON.stringify(pending))
  window.location.assign(authUrl)
}

/** 登录页：向服务端要授权地址（服务端生成一次性 state 并校验回调白名单），然后整页跳转 */
export async function startOAuthLogin(provider: string, next: string) {
  const res = await api.get<{ authUrl: string }>(`/auth/oauth/${provider}/start`, { redirect: oauthRedirectUri(provider) })
  go(res.authUrl, { mode: 'login', next })
}

/** 个人中心：发起绑定（需已登录），返回授权地址后整页跳转 */
export async function startOAuthBind(provider: string) {
  const res = await api.post<{ authUrl: string }>(`/profile/bindings/${provider}`, { redirect: oauthRedirectUri(provider) })
  go(res.authUrl, { mode: 'bind', next: '/profile?tab=bindings' })
}
