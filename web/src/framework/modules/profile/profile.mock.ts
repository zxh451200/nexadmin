import { defineMock, mockError } from '@/framework/request/mock'
import { mockOAuthStart, onOAuthBound } from '@/framework/auth/auth.mock'
import type { AccessToken, Bindings, LoginSession, Profile } from './api'

const MIN = 60000
const ago = (m: number) => new Date(Date.now() - m * MIN).toISOString()
const mkId = (n: number) => (1860000000000000000n + BigInt(n)).toString()

let profile: Profile = {
  id: mkId(1),
  username: 'admin',
  nickname: '系统管理员',
  avatar: '',
  gender: '1',
  phone: '13800138000',
  email: 'admin@example.com',
  province: '440000',
  city: '440300',
  district: '440305',
  bio: '负责平台的日常运维与权限管理。',
}

let bindings: Bindings = {
  phone: '13800138000',
  providers: [
    { key: 'wecom', name: '企业微信', bound: true, externalName: '系统管理员', boundAt: ago(60 * 24 * 40) },
    { key: 'dingtalk', name: '钉钉', bound: false },
    { key: 'feishu', name: '飞书', bound: true, externalName: 'admin@corp', boundAt: ago(60 * 24 * 12) },
    { key: 'google', name: 'Google', bound: false },
    { key: 'github', name: 'GitHub', bound: false },
  ],
}

let sessions: LoginSession[] = [
  { id: mkId(11), device: 'web', browser: 'Chrome 126', os: 'Windows 11', ip: '10.20.1.15', location: '广东省深圳市', loginAt: ago(180), lastActiveAt: ago(0), current: true },
  { id: mkId(12), device: 'mobile', browser: 'Safari 17', os: 'iOS 17', ip: '10.20.1.88', location: '广东省深圳市', loginAt: ago(60 * 20), lastActiveAt: ago(75), current: false },
  { id: mkId(13), device: 'tablet', browser: 'Safari 17', os: 'iPadOS 17', ip: '192.168.8.50', location: '广东省广州市', loginAt: ago(60 * 52), lastActiveAt: ago(60 * 9), current: false },
  { id: mkId(14), device: 'web', browser: 'Firefox 127', os: 'Ubuntu 22.04', ip: '172.20.4.12', location: '北京市', loginAt: ago(60 * 80), lastActiveAt: ago(60 * 30), current: false },
]

/** 演示账号有登录密码；改成 false 可演示“解绑保底”提示 */
const profileHasPassword = true

onOAuthBound((key) => {
  bindings = {
    ...bindings,
    providers: bindings.providers.map((p) =>
      p.key === key ? { ...p, bound: true, externalName: `${profile.username}@${key}`, boundAt: new Date().toISOString() } : p,
    ),
  }
})

const maskPhone = (p?: string) => (p ? p.replace(/^(\d{3})\d{4}(\d{4})$/, '$1****$2') : p)

defineMock([
  { method: 'GET', url: '/profile', handler: () => profile },
  {
    method: 'PUT',
    url: '/profile',
    handler: ({ body }) => {
      profile = { ...profile, ...body, id: profile.id, username: profile.username, phone: profile.phone }
      return profile
    },
  },
  {
    method: 'PUT',
    url: '/profile/password',
    handler: ({ body }) => {
      if (!body?.oldPassword || !body?.newPassword) mockError(400, 'Invalid password')
      if (body.oldPassword === body.newPassword) mockError(400, 'New password must differ from the old one')
      return null
    },
  },
  {
    method: 'GET',
    url: '/profile/bindings',
    handler: () => ({ ...bindings, phone: maskPhone(bindings.phone) }),
  },
  {
    method: 'POST',
    url: '/profile/bindings/phone',
    handler: ({ body }) => {
      if (!/^1\d{10}$/.test(String(body?.phone ?? ''))) mockError(400, 'Invalid phone')
      if (!/^\d{6}$/.test(String(body?.code ?? ''))) mockError(400, 'Invalid code')
      bindings = { ...bindings, phone: body.phone }
      profile = { ...profile, phone: body.phone }
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/profile/bindings/phone',
    handler: () => {
      bindings = { ...bindings, phone: undefined }
      profile = { ...profile, phone: undefined }
      return null
    },
  },
  {
    // 发起绑定：返回授权地址，授权回来后由 /auth/oauth/:provider/callback 完成（见 auth.mock）
    method: 'POST',
    url: '/profile/bindings/:key',
    handler: ({ params, body }) => {
      if (bindings.providers.find((p) => p.key === params.key)?.bound) mockError(409, 'Already bound')
      return mockOAuthStart(params.key, String(body?.redirect ?? ''), 'bind')
    },
  },
  {
    method: 'DELETE',
    url: '/profile/bindings/:key',
    handler: ({ params }) => {
      const target = bindings.providers.find((p) => p.key === params.key)
      if (!target?.bound) return null
      // 解绑保底：至少保留一种可登录方式（演示账号有密码，所以只在没有密码时才会拦截）
      if (!profileHasPassword && bindings.providers.filter((p) => p.bound).length <= 1) {
        mockError(400, 'This is your only sign-in method. Set a password or link another account first.')
      }
      bindings = {
        ...bindings,
        providers: bindings.providers.map((p) =>
          p.key === params.key ? { ...p, bound: false, externalName: undefined, boundAt: undefined } : p,
        ),
      }
      return null
    },
  },
  { method: 'GET', url: '/profile/sessions', handler: () => sessions },
  {
    method: 'DELETE',
    url: '/profile/sessions/:id',
    handler: ({ params }) => {
      const s = sessions.find((x) => x.id === params.id)
      if (!s) mockError(404, 'Not found')
      if (s.current) mockError(400, 'Cannot sign out the current session here')
      sessions = sessions.filter((x) => x.id !== params.id)
      return null
    },
  },
])

// 个人访问令牌：明文只在创建时返回一次，列表只给前缀
const tokenHex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')
const inDays = (d: number) => new Date(Date.now() + d * 86400000).toISOString()

let tokens: AccessToken[] = [
  { id: mkId(21), name: 'CI 流水线', scope: 'write', prefix: 'nxp_9f3a', lastUsedAt: ago(35), expiresAt: inDays(60), createdAt: ago(60 * 24 * 30) },
  { id: mkId(22), name: '报表脚本', scope: 'read', prefix: 'nxp_c71e', lastUsedAt: ago(60 * 26), expiresAt: inDays(3), createdAt: ago(60 * 24 * 87) },
  { id: mkId(23), name: '旧的测试令牌', scope: 'read', prefix: 'nxp_02bd', lastUsedAt: null, expiresAt: inDays(-5), createdAt: ago(60 * 24 * 95) },
  { id: mkId(24), name: '本机调试', scope: 'write', prefix: 'nxp_e8c4', lastUsedAt: ago(60 * 5), expiresAt: null, createdAt: ago(60 * 24 * 12) },
]

defineMock([
  { method: 'GET', url: '/profile/tokens', handler: () => tokens.map((x) => ({ ...x })) },
  {
    method: 'POST',
    url: '/profile/tokens',
    handler: ({ body }) => {
      const name = String(body?.name ?? '').trim()
      if (!name) mockError(400, 'name is required')
      if (body?.scope !== 'read' && body?.scope !== 'write') mockError(400, 'Invalid scope')
      if (body.expiresAt && !(new Date(body.expiresAt).getTime() > Date.now())) mockError(400, 'expiresAt must be in the future')
      const raw = `nxp_${tokenHex(40)}`
      const row: AccessToken = {
        id: mkId(100 + tokens.length + Math.floor(Math.random() * 1e6)),
        name,
        scope: body.scope,
        prefix: raw.slice(0, 8),
        lastUsedAt: null,
        expiresAt: body.expiresAt ?? null,
        createdAt: new Date().toISOString(),
      }
      tokens = [row, ...tokens]
      return { ...row, token: raw }
    },
  },
  {
    method: 'DELETE',
    url: '/profile/tokens/:id',
    handler: ({ params }) => {
      if (!tokens.some((x) => x.id === params.id)) mockError(404, 'Not found')
      tokens = tokens.filter((x) => x.id !== params.id)
      return null
    },
  },
])
