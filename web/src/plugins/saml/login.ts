import { api } from '@/framework/request/http'
import { useAuth, type LoginContinuation } from '@/framework/auth/useAuth'
import type { ChallengeResult, LoginResult } from '@/framework/auth/types'

/**
 * ACS 回来后的一次性交换码：登录页加载时兑换成登录态。
 * 成功时在 acceptLogin 内建立 token（或得到二次验证挑战），返回给登录页。
 */
export function resumeSamlLogin(): Promise<LoginContinuation> | null {
  const params = new URLSearchParams(window.location.search)
  const code = params.get('samlCode')
  if (!code) return null
  // IdP 发起登录时带来的深链（服务端已按站内路径校验过，这里再兜一次），登录后回到该页。
  const returnTo = params.get('returnTo') ?? ''
  const next =
    returnTo.startsWith('/') && !returnTo.startsWith('//') && !returnTo.startsWith('/login') && !/[\\\u0000-\u0020\u007f]/.test(returnTo)
      ? returnTo
      : '/'
  // 立即从地址栏移除 code：刷新不会重放，失败也要求用户重新发起 SSO。
  params.delete('samlCode')
  const qs = params.toString()
  window.history.replaceState({}, '', window.location.pathname + (qs ? '?' + qs : '') + window.location.hash)
  return api.post<LoginResult | ChallengeResult>('/auth/saml/exchange', { code }, { silent: true }).then((res) => {
    const out = useAuth.getState().acceptLogin(res)
    return { challenge: 'challenge' in out ? out.challenge : null, next }
  })
}
