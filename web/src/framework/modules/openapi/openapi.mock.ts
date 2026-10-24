import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { Page } from '@/framework/request/types'
import type { ApiRoute, Delivery, DeliveryStatus, OpenApp, OpenAppForm, OpenEvent, Webhook, WebhookForm, WebhookTestResult } from './api'

const MIN = 60000
const ago = (m: number) => new Date(Date.now() - m * MIN).toISOString()
const rand = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')

const apps: OpenApp[] = [
  {
    id: '1870000000000000001',
    name: 'HR 同步服务',
    appKey: 'ak_hr_5d2e8a1c',
    description: '定时把组织架构同步到人事系统',
    roleIds: [],
    enabled: true,
    ipAllow: ['203.0.113.10', '10.20.0.0/16'],
    createdAt: ago(60 * 24 * 30),
    lastUsedAt: ago(12),
  },
  {
    id: '1870000000000000002',
    name: '数据看板',
    appKey: 'ak_bi_9c4f7b30',
    description: '只读拉取用户与部门统计',
    roleIds: [],
    enabled: false,
    ipAllow: [],
    createdAt: ago(60 * 24 * 9),
    lastUsedAt: null,
  },
]

const events: OpenEvent[] = [
  { name: 'user.created', description: '用户创建' },
  { name: 'user.updated', description: '用户信息变更' },
  { name: 'user.deleted', description: '用户删除' },
  { name: 'user.disabled', description: '用户被停用' },
  { name: 'dept.created', description: '部门创建' },
  { name: 'dept.updated', description: '部门信息变更' },
  { name: 'dept.deleted', description: '部门删除' },
  { name: 'role.created', description: '角色创建' },
  { name: 'role.updated', description: '角色权限变更' },
  { name: 'role.deleted', description: '角色删除' },
  { name: 'notice.published', description: '公告发布' },
  { name: 'notice.revoked', description: '公告撤回' },
]

const webhooks: Webhook[] = [
  {
    id: '1870000000000000011',
    name: '人事系统通知',
    url: 'https://hr.example.com/hooks/nexadmin',
    events: ['user.created', 'user.updated', 'user.deleted', 'dept.updated'],
    enabled: true,
    createdAt: ago(60 * 24 * 20),
  },
  {
    id: '1870000000000000012',
    name: '本地调试',
    url: 'http://localhost:9000/hook',
    events: ['notice.published'],
    enabled: false,
    createdAt: ago(60 * 24 * 3),
  },
]

const deliveries: Delivery[] = Array.from({ length: 20 }, (_, i) => {
  const wh = webhooks[i % 5 === 4 ? 1 : 0]
  const event = wh.events[i % wh.events.length]
  const status: DeliveryStatus = i % 4 === 1 ? 'failed' : i % 7 === 3 ? 'pending' : 'success'
  const failed = status === 'failed'
  return {
    id: `18700000000000001${String(i + 1).padStart(2, '0')}`,
    webhookId: wh.id,
    webhookName: wh.name,
    event,
    status,
    attempts: status === 'success' ? 1 : status === 'failed' ? 3 + (i % 3) : 1,
    responseStatus: status === 'pending' ? null : failed ? (i % 8 === 1 ? 500 : 502) : 200,
    responseBody:
      status === 'pending'
        ? null
        : failed
          ? i % 8 === 1
            ? '{"error":"internal error","detail":"' + 'x'.repeat(2400) + '"}'
            : '<html><body><h1>502 Bad Gateway</h1></body></html>'
          : '{"ok":true}',
    error: failed ? (i % 8 === 1 ? 'HTTP 500' : 'upstream connect error') : null,
    createdAt: ago(8 + i * 37),
    nextAttemptAt: status === 'pending' ? ago(-5) : null,
  }
})

const findApp = (id: string) => apps.find((a) => a.id === id) ?? mockError(404, 'Not found')
const findHook = (id: string) => webhooks.find((w) => w.id === id) ?? mockError(404, 'Not found')

const validIp = (s: string) => /^[0-9a-fA-F:.]+(\/\d{1,3})?$/.test(s)

function sanitizeApp(body: Partial<OpenAppForm> | undefined) {
  if (!body?.name?.trim()) mockError(400, 'name is required')
  const ipAllow = (body.ipAllow ?? []).map((s) => s.trim()).filter(Boolean)
  const bad = ipAllow.find((s) => !validIp(s))
  if (bad) mockError(400, `invalid ip: ${bad}`)
  return {
    name: String(body.name).trim(),
    description: body.description ?? '',
    roleIds: (body.roleIds ?? []) as string[],
    enabled: body.enabled ?? true,
    ipAllow,
  }
}

function sanitizeHook(body: Partial<WebhookForm> | undefined): WebhookForm {
  if (!body?.name?.trim()) mockError(400, 'name is required')
  const url = (body.url ?? '').trim()
  if (!/^https:\/\/.+/.test(url) && !/^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?(\/|$)/.test(url)) mockError(400, 'url must be https')
  if (!body.events?.length) mockError(400, 'events is required')
  return { name: body.name.trim(), url, events: body.events, enabled: body.enabled ?? true }
}

// 接口文档：[方法, 相对路径, 模块, 权限码, 认证(p 公开 / l 登录 / s 超管)]
type RouteRow = [string, string, string, string, 'p' | 'l' | 's']
const ROUTE_ROWS: RouteRow[] = [
  ['POST', '/auth/login', 'core', '', 'p'],
  ['GET', '/auth/captcha', 'core', '', 'p'],
  ['POST', '/auth/refresh', 'core', '', 'p'],
  ['POST', '/auth/logout', 'core', '', 'l'],
  ['GET', '/auth/me', 'core', '', 'l'],
  ['GET', '/system/status', 'core', '', 'p'],
  ['GET', '/users', 'user', 'user:list', 'l'],
  ['GET', '/users/:id', 'user', 'user:list', 'l'],
  ['POST', '/users', 'user', 'user:create', 'l'],
  ['PUT', '/users/:id', 'user', 'user:update', 'l'],
  ['DELETE', '/users/:id', 'user', 'user:delete', 'l'],
  ['PUT', '/users/:id/status', 'user', 'user:update', 'l'],
  ['POST', '/users/:id/reset-password', 'user', 'user:resetPwd', 'l'],
  ['GET', '/roles', 'role', 'role:list', 'l'],
  ['POST', '/roles', 'role', 'role:create', 'l'],
  ['PUT', '/roles/:id', 'role', 'role:update', 'l'],
  ['DELETE', '/roles/:id', 'role', 'role:delete', 'l'],
  ['GET', '/depts', 'dept', 'dept:list', 'l'],
  ['POST', '/depts', 'dept', 'dept:create', 'l'],
  ['GET', '/notices', 'notice', 'notice:list', 'l'],
  ['POST', '/notices', 'notice', 'notice:create', 'l'],
  ['GET', '/profile/tokens', 'openapi', '', 'l'],
  ['POST', '/profile/tokens', 'openapi', '', 'l'],
  ['DELETE', '/profile/tokens/:id', 'openapi', '', 'l'],
  ['GET', '/openapi/apps', 'openapi', 'openapi:list', 'l'],
  ['POST', '/openapi/apps', 'openapi', 'openapi:edit', 'l'],
  ['GET', '/openapi/routes', 'openapi', 'openapi:list', 'l'],
  ['GET', '/openapi.json', 'openapi', 'openapi:list', 'l'],
  ['GET', '/admin/tenants', 'tenant', '', 's'],
  ['POST', '/admin/tenants', 'tenant', '', 's'],
]

const startsWith = (p: string, prefix: string) => p === prefix || p.startsWith(`${prefix}/`)

// 规则与后端 core/openapi 一致：令牌不能碰 /auth /profile/tokens /oauth/authorize；应用另不能碰 /profile /openapi/apps
const apiRoutes: ApiRoute[] = ROUTE_ROWS.map(([method, rel, module, perm, kind]) => {
  const auth = kind !== 'p'
  const tokenCallable = auth && !['/auth', '/profile/tokens', '/oauth/authorize'].some((x) => startsWith(rel, x))
  const appCallable = tokenCallable && !['/profile', '/openapi/apps'].some((x) => startsWith(rel, x))
  return {
    method,
    path: `/api/v1${rel}`,
    module,
    perm,
    auth,
    superOnly: kind === 's',
    tokenCallable,
    appCallable,
    openapiCallable: tokenCallable || appCallable,
  }
})

function mockSpec() {
  const paths: Record<string, Record<string, unknown>> = {}
  for (const r of apiRoutes) {
    const p = r.path.replace('/api/v1', '').replace(/:(\w+)/g, '{$1}')
    paths[p] = { ...paths[p], [r.method.toLowerCase()]: { tags: [r.module], summary: `${r.method} ${p}`, 'x-permission': r.perm || undefined } }
  }
  return { openapi: '3.0.3', info: { title: 'NexAdmin API (mock)', version: '1.0.0' }, servers: [{ url: '/api/v1' }], paths }
}

defineMock([
  { method: 'GET', url: '/openapi/events', handler: (): OpenEvent[] => events },

  { method: 'GET', url: '/openapi/apps', handler: (): OpenApp[] => apps.map((a) => ({ ...a })) },
  {
    method: 'POST',
    url: '/openapi/apps',
    handler: ({ body }) => {
      const app: OpenApp = { ...sanitizeApp(body), id: genId(), appKey: `ak_${rand(12)}`, createdAt: new Date().toISOString(), lastUsedAt: null }
      apps.push(app)
      return { ...app, appSecret: rand(48) }
    },
  },
  {
    method: 'PUT',
    url: '/openapi/apps/:id',
    handler: ({ params, body }) => {
      const app = findApp(params.id)
      Object.assign(app, sanitizeApp(body))
      return { ...app }
    },
  },
  {
    method: 'DELETE',
    url: '/openapi/apps/:id',
    handler: ({ params }) => {
      const idx = apps.findIndex((a) => a.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      apps.splice(idx, 1)
      return null
    },
  },
  {
    method: 'PUT',
    url: '/openapi/apps/:id/enabled',
    handler: ({ params, body }) => {
      findApp(params.id).enabled = !!body?.enabled
      return null
    },
  },
  {
    method: 'POST',
    url: '/openapi/apps/:id/reset-secret',
    handler: ({ params }) => {
      findApp(params.id)
      return { appSecret: rand(48) }
    },
  },

  { method: 'GET', url: '/openapi/webhooks', handler: (): Webhook[] => webhooks.map((w) => ({ ...w })) },
  {
    method: 'POST',
    url: '/openapi/webhooks',
    handler: ({ body }) => {
      const hook: Webhook = { ...sanitizeHook(body), id: genId(), createdAt: new Date().toISOString() }
      webhooks.push(hook)
      return { ...hook, secret: `whsec_${rand(40)}` }
    },
  },
  {
    method: 'PUT',
    url: '/openapi/webhooks/:id',
    handler: ({ params, body }) => {
      const hook = findHook(params.id)
      Object.assign(hook, sanitizeHook(body))
      return { ...hook }
    },
  },
  {
    method: 'DELETE',
    url: '/openapi/webhooks/:id',
    handler: ({ params }) => {
      const idx = webhooks.findIndex((w) => w.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      webhooks.splice(idx, 1)
      return null
    },
  },
  {
    method: 'PUT',
    url: '/openapi/webhooks/:id/enabled',
    handler: ({ params, body }) => {
      findHook(params.id).enabled = !!body?.enabled
      return null
    },
  },
  {
    method: 'POST',
    url: '/openapi/webhooks/:id/reset-secret',
    handler: ({ params }) => {
      findHook(params.id)
      return { secret: `whsec_${rand(40)}` }
    },
  },
  {
    method: 'POST',
    url: '/openapi/webhooks/:id/test',
    handler: ({ params }): WebhookTestResult => {
      const hook = findHook(params.id)
      // 演示：本地地址模拟连接失败，其余成功
      if (hook.url.startsWith('http://localhost')) {
        return { success: false, statusCode: 0, durationMs: 3000, error: 'connect ECONNREFUSED 127.0.0.1:9000' }
      }
      return { success: true, statusCode: 200, durationMs: 80 + Math.floor(Math.random() * 220) }
    },
  },

  {
    method: 'GET',
    url: '/openapi/deliveries',
    handler: ({ query }): Page<Delivery> => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const rows = deliveries
        .filter((d) => !query.webhookId || d.webhookId === query.webhookId)
        .filter((d) => !query.status || d.status === query.status)
        .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return { list: rows.slice((page - 1) * pageSize, page * pageSize).map((d) => ({ ...d })), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'POST',
    url: '/openapi/deliveries/:id/retry',
    handler: ({ params }) => {
      const d = deliveries.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
      if (d.status !== 'failed') mockError(400, 'Only failed deliveries can be retried')
      d.status = 'pending'
      d.attempts += 1
      d.nextAttemptAt = ago(-1)
      return null
    },
  },

  { method: 'GET', url: '/openapi/routes', handler: (): ApiRoute[] => apiRoutes.map((r) => ({ ...r })) },
  {
    method: 'GET',
    url: '/openapi.json',
    handler: () => new Blob([JSON.stringify(mockSpec(), null, 2)], { type: 'application/json' }),
  },
])
