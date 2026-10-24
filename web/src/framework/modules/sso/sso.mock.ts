import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { AuthorizeInfo, PortalApp, SsoApp, SsoAppForm, SsoKey } from './api'

const NOW = '2025-03-01T08:00:00.000Z'

const apps: SsoApp[] = [
  {
    id: '1860000000000000001',
    name: '企业邮箱',
    clientId: 'app_mail_7f3a9c',
    redirectUris: ['https://mail.example.com/oauth/callback'],
    logo: '',
    homeUrl: 'https://mail.example.com',
    description: '收发邮件、通讯录与日程',
    enabled: true,
    roleIds: [],
    allowOrgRead: false,
    createdAt: NOW,
  },
  {
    id: '1860000000000000002',
    name: 'OA 办公',
    clientId: 'app_oa_2b81de',
    redirectUris: ['https://oa.example.com/sso/callback', 'http://localhost:8080/sso/callback'],
    logo: '',
    homeUrl: 'https://oa.example.com',
    description: '审批、考勤、公文流转',
    enabled: true,
    roleIds: [],
    allowOrgRead: false,
    createdAt: NOW,
  },
  {
    id: '1860000000000000003',
    name: 'BI 报表',
    clientId: 'app_bi_c94f05',
    redirectUris: ['https://bi.example.com/login/callback'],
    logo: '',
    homeUrl: 'https://bi.example.com',
    description: '经营分析与数据看板',
    enabled: false,
    roleIds: [],
    allowOrgRead: false,
    createdAt: NOW,
  },
]

const rand = (n: number) =>
  Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')

const find = (id: string) => apps.find((a) => a.id === id) ?? mockError(404, 'Not found')

function sanitize(body: Partial<SsoAppForm> | undefined): SsoAppForm {
  if (!body?.name?.trim()) mockError(400, 'name is required')
  const uris = (body.redirectUris ?? []).map((u) => u.trim()).filter(Boolean)
  if (uris.length === 0) mockError(400, 'redirectUris is required')
  return {
    name: body.name.trim(),
    redirectUris: uris,
    logo: body.logo ?? '',
    homeUrl: body.homeUrl ?? '',
    description: body.description ?? '',
    enabled: body.enabled ?? true,
    roleIds: body.roleIds ?? [],
    allowOrgRead: body.allowOrgRead ?? false,
  }
}

// 签名密钥：新的在前；旧 key 被取代后在 JWKS 里再保留 7 天
const DAY = 86400000
const keys: SsoKey[] = [{ kid: rand(16), createdAt: '2025-03-01T08:00:00.000Z', status: 'active' }]

function rotateKeys(): SsoKey[] {
  const now = Date.now()
  keys[0].status = 'retiring'
  keys.unshift({ kid: rand(16), createdAt: new Date(now).toISOString(), status: 'active' })
  // 当前 key 之外的旧 key：从“被下一个 key 取代”起算 7 天；这里按取代时刻（下一个 key 的创建时间）过滤
  for (let i = keys.length - 1; i >= 1; i--) {
    const retiresAt = new Date(keys[i - 1].createdAt).getTime() + 7 * DAY
    if (retiresAt <= now) keys.splice(i, 1)
    else keys[i].retiresAt = new Date(retiresAt).toISOString()
  }
  return keys.map((k) => ({ ...k }))
}

defineMock([
  { method: 'GET', url: '/sso/keys', handler: (): SsoKey[] => keys.map((k) => ({ ...k })) },
  { method: 'POST', url: '/sso/keys/rotate', handler: (): SsoKey[] => rotateKeys() },
  { method: 'GET', url: '/sso/apps', handler: (): SsoApp[] => apps.map((a) => ({ ...a })) },
  {
    method: 'POST',
    url: '/sso/apps',
    handler: ({ body }) => {
      const app: SsoApp = { ...sanitize(body), id: genId(), clientId: `app_${rand(12)}`, createdAt: new Date().toISOString() }
      apps.push(app)
      return { ...app, clientSecret: rand(48) }
    },
  },
  {
    method: 'PUT',
    url: '/sso/apps/:id',
    handler: ({ params, body }) => {
      const app = find(params.id)
      Object.assign(app, sanitize(body))
      return { ...app }
    },
  },
  {
    method: 'DELETE',
    url: '/sso/apps/:id',
    handler: ({ params }) => {
      const idx = apps.findIndex((a) => a.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      apps.splice(idx, 1)
      return null
    },
  },
  {
    method: 'PUT',
    url: '/sso/apps/:id/enabled',
    handler: ({ params, body }) => {
      find(params.id).enabled = !!body?.enabled
      return null
    },
  },
  {
    method: 'POST',
    url: '/sso/apps/:id/reset-secret',
    handler: ({ params }) => {
      find(params.id)
      return { clientSecret: rand(48) }
    },
  },
  {
    method: 'GET',
    url: '/sso/portal',
    handler: (): PortalApp[] =>
      apps
        .filter((a) => a.enabled)
        .map(({ id, name, logo, homeUrl, description }) => ({ id, name, logo, homeUrl, description })),
  },
  {
    method: 'GET',
    url: '/oauth/authorize/info',
    handler: ({ query }): AuthorizeInfo => {
      const app = apps.find((a) => a.clientId === query.client_id)
      if (!app || !app.enabled) mockError(400, 'invalid client')
      if (!app.redirectUris.includes(String(query.redirect_uri ?? ''))) mockError(400, 'invalid redirect_uri')
      return { name: app.name, logo: app.logo, scopes: ['openid', 'profile', 'email'] }
    },
  },
  {
    method: 'POST',
    url: '/oauth/authorize',
    handler: ({ body }) => {
      const app = apps.find((a) => a.clientId === body?.clientId)
      if (!app || !app.redirectUris.includes(String(body?.redirectUri ?? ''))) mockError(400, 'invalid redirect_uri')
      const u = new URL(body.redirectUri)
      u.searchParams.set('code', 'mock')
      if (body.state) u.searchParams.set('state', body.state)
      return { redirectTo: u.toString() }
    },
  },
])
