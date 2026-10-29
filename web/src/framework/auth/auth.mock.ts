import { defineMock, mockError, type MockContext } from '@/framework/request/mock'
import { getSecurity } from '@/framework/modules/settings/settings.mock'
import { DEMO_TENANT_ID } from '@/framework/modules/tenant/tenant.mock'
import type { AuthUser } from './types'

const ALL = ['*']

const users: Record<string, AuthUser & { password: string; phone: string }> = {
  admin: {
    id: '1790000000000000001',
    username: 'admin',
    nickname: '超级管理员',
    avatar: '',
    tenantId: '1790000000000000100',
    roles: ['admin'],
    perms: ALL,
    password: 'admin123',
    phone: '13800000000',
  },
  demo: {
    id: '1790000000000000002',
    username: 'demo',
    nickname: 'Demo User',
    avatar: '',
    tenantId: DEMO_TENANT_ID,
    roles: ['operator'],
    perms: ['dashboard:view', 'user:list', 'role:list', 'dept:list', 'post:list', 'notice:list', 'message:list', 'profile:view'],
    password: 'demo123',
    phone: '13900000000',
    // 演示：租户 9 天后到期，登录后顶栏出现“租户即将到期”提示条
    tenantExpireSoon: { expireAt: new Date(Date.now() + 9 * 86400000).toISOString(), daysLeft: 9 },
  },
}

// 演示二次验证：secure / secure123 登录后要求 6 位验证码（123456）或恢复码（RECOVERY-1）。
// 真实部署只有安装了提供 LoginGate 的插件才会出现这一步；其余账号的登录流程不变。
users.secure = {
  ...users.demo,
  id: '1790000000000000003',
  username: 'secure',
  nickname: 'Secure User',
  password: 'secure123',
  phone: '13700000000',
  tenantExpireSoon: undefined,
}
const SECOND_FACTOR_USERS = new Set(['secure'])
const OTP_CODE = '123456'
const RECOVERY_CODE = 'RECOVERY-1'
const CHALLENGE_TRIES = 5
const CHALLENGE_TTL_SEC = 300
const challenges = new Map<string, { username: string; tries: number; expires: number }>()

const captchas = new Map<string, string>()
const SMS_CODE = '123456'

/** 账号锁定：连续密码错误次数与锁定截止时间（内存，刷新页面即清空） */
export const LOCKED_CODE = 423
const locks = new Map<string, { fails: number; until: number }>()

/** 锁定状态变化订阅（user.mock 用它同步用户的 locked / lockedUntil）；until 为 null 表示已解除 */
type LockListener = (username: string, until: number | null) => void
const lockListeners: LockListener[] = []
export function onLockChange(fn: LockListener) {
  lockListeners.push(fn)
}
const emitLock = (username: string, until: number | null) => lockListeners.forEach((fn) => fn(username, until))

function lockRemainingSec(username?: string) {
  const l = locks.get(username ?? '')
  return l && l.until > Date.now() ? Math.ceil((l.until - Date.now()) / 1000) : 0
}

/** 记一次密码错误；达到上限则锁定并返回锁定秒数，否则返回 0 */
function recordFailure(username?: string) {
  const name = username ?? ''
  const { maxAttempts, lockMinutes } = getSecurity()
  const fails = (locks.get(name)?.fails ?? 0) + 1
  if (fails >= maxAttempts) {
    const until = Date.now() + lockMinutes * 60000
    locks.set(name, { fails: 0, until })
    emitLock(name, until)
    return lockMinutes * 60
  }
  locks.set(name, { fails, until: 0 })
  return 0
}

/** 管理员解锁（user.mock 的 /users/:id/unlock 调用） */
export function unlockAccount(username: string) {
  const had = locks.delete(username)
  emitLock(username, null)
  return had
}

const ADMIN_TPL = users.admin

/** 初始化向导里创建的管理员：创建后只有它能登录（默认的 admin/admin123 随之失效） */
function syncAdmin() {
  try {
    const raw = localStorage.getItem('mock-admin')
    if (!raw) return
    const a = JSON.parse(raw) as { username: string; password: string }
    delete users.admin
    users[a.username] = { ...ADMIN_TPL, username: a.username, nickname: a.username, password: a.password }
  } catch {
    // 本地数据损坏时忽略，沿用默认账号
  }
}

function pub(u: AuthUser & { password?: string; phone?: string }): AuthUser {
  const rest = { ...u }
  delete rest.password
  delete rest.phone
  return rest
}

function tokenFor(username: string, kind: 'a' | 'r') {
  return `mock.${kind}.${username}.${Date.now()}`
}

function userFromCtx(ctx: MockContext, kind: 'a' | 'r' = 'a') {
  syncAdmin()
  const token = ctx.headers.authorization.replace(/^Bearer\s+/i, '')
  const [, k, username] = token.split('.')
  const u = users[username]
  if (!u || k !== kind) mockError(401, 'Unauthorized')
  return u
}

function svgCaptcha(text: string) {
  const chars = text
    .split('')
    .map((c, i) => {
      const rot = Math.floor(Math.random() * 30) - 15
      return `<text x="${14 + i * 24}" y="30" font-size="24" font-family="monospace" font-weight="700" fill="#4b6cb7" transform="rotate(${rot} ${14 + i * 24} 24)">${c}</text>`
    })
    .join('')
  const lines = Array.from({ length: 4 })
    .map(
      () =>
        `<line x1="${Math.random() * 110}" y1="${Math.random() * 40}" x2="${Math.random() * 110}" y2="${Math.random() * 40}" stroke="#8a9bb8" stroke-width="1" opacity="0.6"/>`,
    )
    .join('')
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="110" height="40" viewBox="0 0 110 40"><rect width="110" height="40" fill="#eef2f8"/>${lines}${chars}</svg>`
  return `data:image/svg+xml;base64,${btoa(svg)}`
}

function checkCaptcha(key?: string, text?: string, reason?: string) {
  const expected = key ? captchas.get(key) : undefined
  if (key) captchas.delete(key)
  if (!expected || !text || expected.toLowerCase() !== text.toLowerCase()) mockError(400, '验证码错误', reason)
}

/** 与后端一致：同一用户名连续密码错误达到该次数后，登录必须带图形验证码（锁定逻辑不变，仍按安全设置） */
const CAPTCHA_AFTER_FAILS = 3
export const CAPTCHA_REQUIRED = 'CAPTCHA_REQUIRED'
const loginFails = new Map<string, number>()

function issue(u: AuthUser & { password?: string; phone?: string }) {
  return { accessToken: tokenFor(u.username, 'a'), refreshToken: tokenFor(u.username, 'r'), user: pub(u) }
}

/** 所有登录路径（密码、手机号、第三方、绑定）的统一出口：需要二次验证的账号只返回挑战，不发令牌 */
function session(u: AuthUser & { password?: string; phone?: string }) {
  if (!SECOND_FACTOR_USERS.has(u.username)) return issue(u)
  const id = Array.from({ length: 4 }, () => Math.random().toString(16).slice(2, 18).padEnd(16, '0')).join('')
  challenges.set(id, { username: u.username, tries: 0, expires: Date.now() + CHALLENGE_TTL_SEC * 1000 })
  return { challenge: { id, methods: ['totp', 'recovery'], expiresIn: CHALLENGE_TTL_SEC } }
}

defineMock([
  {
    method: 'GET',
    url: '/auth/captcha',
    handler: () => {
      const text = Array.from({ length: 4 }, () => 'ABCDEFGHJKMNPQRSTUVWXYZ23456789'[Math.floor(Math.random() * 31)]).join('')
      const key = Math.random().toString(36).slice(2)
      captchas.set(key, text)
      return { key, image: svgCaptcha(text) }
    },
  },
  {
    method: 'POST',
    url: '/auth/sms-code',
    handler: ({ body }) => {
      if (!/^1\d{10}$/.test(body?.phone ?? '')) mockError(400, '手机号格式不正确')
      checkCaptcha(body?.captchaKey, body?.captcha)
      console.info(`[mock] SMS code for ${body.phone}: ${SMS_CODE}`)
      return { cooldown: 60 }
    },
  },
  {
    method: 'POST',
    url: '/auth/login',
    handler: ({ body }) => {
      syncAdmin()
      if (body?.type === 'phone') {
        const u = Object.values(users).find((x) => x.phone === body.phone)
        if (!u || body.code !== SMS_CODE) mockError(400, '手机号或验证码错误')
        return session(u)
      }
      const lockedLeft = lockRemainingSec(body?.username)
      if (lockedLeft > 0) mockError(LOCKED_CODE, `LOCKED:${lockedLeft}`)
      const name = String(body?.username ?? '')
      // 防枚举：是否要求验证码只看提交的用户名，与账号是否存在无关
      if ((loginFails.get(name) ?? 0) >= CAPTCHA_AFTER_FAILS) checkCaptcha(body?.captchaKey, body?.captcha, CAPTCHA_REQUIRED)
      const u = users[body?.username ?? '']
      if (!u || u.password !== body.password) {
        const fails = (loginFails.get(name) ?? 0) + 1
        loginFails.set(name, fails)
        const left = recordFailure(body?.username)
        if (left > 0) mockError(LOCKED_CODE, `LOCKED:${left}`)
        mockError(400, '用户名或密码错误', fails >= CAPTCHA_AFTER_FAILS ? CAPTCHA_REQUIRED : undefined)
      }
      loginFails.delete(name)
      locks.delete(u.username)
      emitLock(u.username, null)
      return session(u)
    },
  },
  {
    method: 'POST',
    url: '/auth/login/verify',
    handler: ({ body }) => {
      syncAdmin()
      const id = String(body?.challengeId ?? '')
      const ch = challenges.get(id)
      if (!ch || ch.expires < Date.now()) mockError(400, '验证已失效，请重新登录')
      ch.tries += 1 // 先计数再校验；超过上限挑战作废
      const ok = body?.kind === 'recovery' ? body?.code === RECOVERY_CODE : body?.code === OTP_CODE
      if (!ok) {
        if (ch.tries >= CHALLENGE_TRIES) challenges.delete(id)
        mockError(400, '验证失败，请重试')
      }
      challenges.delete(id) // 一次性
      const u = users[ch.username]
      if (!u) mockError(400, '验证已失效，请重新登录')
      return issue(u)
    },
  },
  {
    method: 'POST',
    url: '/auth/refresh',
    handler: (ctx) => {
      syncAdmin()
      const token = String(ctx.body?.refreshToken ?? '')
      const [, k, username] = token.split('.')
      const u = users[username]
      if (!u || k !== 'r') mockError(401, 'Unauthorized')
      return { accessToken: tokenFor(u.username, 'a'), refreshToken: tokenFor(u.username, 'r') }
    },
  },
  { method: 'POST', url: '/auth/logout', handler: () => null },
  { method: 'GET', url: '/auth/me', handler: (ctx) => pub(userFromCtx(ctx)) },
  {
    method: 'GET',
    url: '/auth/methods',
    handler: () => ({
      methods: ['password', 'phone'],
      forgotEnabled: true,
      forgotChannels: ['email', 'phone'],
      registerEnabled: true,
      captchaEnabled: true,
      providers: [
        { key: 'wecom', name: '企业微信' },
        { key: 'dingtalk', name: '钉钉' },
        { key: 'feishu', name: '飞书' },
        { key: 'google', name: 'Google' },
        { key: 'github', name: 'GitHub' },
        { key: 'oidc', name: 'OIDC' },
      ],
    }),
  },
  {
    method: 'POST',
    url: '/auth/register',
    handler: (ctx) => {
      const username = String(ctx.body?.username ?? '').trim()
      const password = String(ctx.body?.password ?? '')
      if (!/^[A-Za-z][A-Za-z0-9_]{2,31}$/.test(username)) mockError(400, '账号需以字母开头，3-32 位，仅限字母、数字、下划线')
      if (!password || password.length > 72) mockError(400, '密码长度为 1-72 字节')
      if (String(ctx.body?.captcha ?? '').length !== 4) mockError(400, '验证码错误或已过期')
      if (users[username]) mockError(400, '账号已存在')
      users[username] = { ...users.admin, username, id: String(Date.now()), nickname: username }
      // 注册即登录：与 /auth/login 同形返回令牌
      return { accessToken: tokenFor(username, 'a'), refreshToken: tokenFor(username, 'r'), user: pub(users[username]) }
    },
  },
])

// ---- 第三方快捷登录（与后端契约一致：start 返回 {authUrl}；callback 返回登录结果 / needBind / bound）----
// 整页跳转会重置内存，所以一次性 state 与待绑定令牌放 sessionStorage。

const OAUTH_KEY = 'mock-oauth'
const OAUTH_PROVIDERS = [
  { key: 'wecom', name: '企业微信' },
  { key: 'dingtalk', name: '钉钉' },
  { key: 'feishu', name: '飞书' },
  { key: 'google', name: 'Google' },
  { key: 'github', name: 'GitHub' },
  { key: 'oidc', name: 'OIDC' },
]
/** 演示：这些 provider 首次登录匹配不到账号，会弹出「绑定已有账号」 */
const NEED_BIND = new Set(['google', 'oidc'])

interface OAuthStore {
  states: Record<string, { provider: string; purpose: 'login' | 'bind' }>
  binds: Record<string, string>
}

const loadOAuth = (): OAuthStore => {
  try {
    return JSON.parse(sessionStorage.getItem(OAUTH_KEY) ?? '') as OAuthStore
  } catch {
    return { states: {}, binds: {} }
  }
}
const saveOAuth = (s: OAuthStore) => sessionStorage.setItem(OAUTH_KEY, JSON.stringify(s))

/** 个人中心绑定完成时通知 profile.mock 更新绑定列表 */
const boundListeners: ((provider: string) => void)[] = []
export function onOAuthBound(fn: (provider: string) => void) {
  boundListeners.push(fn)
}

/** 登记一次性 state 并返回授权地址（演示里授权页就是回调页本身，带上 code 和 state） */
export function mockOAuthStart(provider: string, redirect: string, purpose: 'login' | 'bind') {
  if (redirect !== `${window.location.origin}/admin/login/oauth/${provider}`) mockError(400, '回调地址不在允许范围内')
  const s = loadOAuth()
  const state = Math.random().toString(36).slice(2)
  s.states[state] = { provider, purpose }
  saveOAuth(s)
  return { authUrl: `${redirect}?code=mock-code&state=${state}` }
}

const OAUTH_FAILED = '第三方登录失败，请重试'

defineMock([
  { method: 'GET', url: '/auth/providers', handler: () => OAUTH_PROVIDERS },
  {
    method: 'GET',
    url: '/auth/oauth/:provider/start',
    handler: ({ params, query }) => {
      if (!OAUTH_PROVIDERS.some((p) => p.key === params.provider)) mockError(404, 'Not found')
      return mockOAuthStart(params.provider, String(query.redirect ?? ''), 'login')
    },
  },
  {
    method: 'POST',
    url: '/auth/oauth/:provider/callback',
    handler: (ctx) => {
      syncAdmin()
      const s = loadOAuth()
      const rec = s.states[String(ctx.body?.state ?? '')]
      delete s.states[String(ctx.body?.state ?? '')] // 一次性：取出即作废
      saveOAuth(s)
      if (!rec || rec.provider !== ctx.params.provider || !ctx.body?.code) mockError(400, OAUTH_FAILED)
      if (rec.purpose === 'bind') {
        userFromCtx(ctx) // 绑定必须带登录令牌
        boundListeners.forEach((fn) => fn(rec.provider))
        return { bound: true, provider: rec.provider }
      }
      if (NEED_BIND.has(rec.provider)) {
        const bindToken = `mock-bind-${Math.random().toString(36).slice(2)}`
        s.binds[bindToken] = rec.provider
        saveOAuth(s)
        return { needBind: true, bindToken }
      }
      return session(rec.provider === 'github' ? users.admin : users.demo)
    },
  },
  {
    method: 'POST',
    url: '/auth/oauth/bind',
    handler: ({ body }) => {
      syncAdmin()
      const s = loadOAuth()
      if (!s.binds[String(body?.bindToken ?? '')]) mockError(400, '绑定已失效，请重新使用第三方登录')
      const u = users[String(body?.username ?? '')]
      if (!u || u.password !== body?.password) mockError(400, '用户名或密码错误')
      delete s.binds[body.bindToken]
      saveOAuth(s)
      return session(u)
    },
  },
])
