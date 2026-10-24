import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export interface OpenApp {
  id: string
  name: string
  appKey: string
  description: string
  roleIds: string[]
  enabled: boolean
  /** IP / CIDR 白名单；空 = 不限制 */
  ipAllow: string[]
  createdAt: string
  lastUsedAt?: string | null
}

/** 创建成功时额外返回 appSecret，只显示一次 */
export type OpenAppCreated = OpenApp & { appSecret: string }

export interface OpenAppForm {
  name: string
  description: string
  roleIds: string[]
  enabled: boolean
  ipAllow: string[]
}

export interface Webhook {
  id: string
  name: string
  url: string
  events: string[]
  enabled: boolean
  createdAt: string
}

/** 创建成功时额外返回 secret，只显示一次 */
export type WebhookCreated = Webhook & { secret: string }

export interface WebhookForm {
  name: string
  url: string
  events: string[]
  enabled: boolean
}

export interface WebhookTestResult {
  success: boolean
  statusCode: number
  durationMs: number
  error?: string
}

export interface OpenEvent {
  name: string
  description: string
}

export type DeliveryStatus = 'success' | 'failed' | 'pending'
export const DELIVERY_STATUSES: DeliveryStatus[] = ['success', 'failed', 'pending']

export interface Delivery {
  id: string
  webhookId: string
  webhookName?: string
  event: string
  status: DeliveryStatus
  attempts: number
  responseStatus?: number | null
  responseBody?: string | null
  error?: string | null
  createdAt: string
  nextAttemptAt?: string | null
}

export interface DeliveryQuery {
  page: number
  pageSize: number
  webhookId?: string
  status?: string
}

const KEY = ['openapi'] as const

const toList = <T>(res: T[] | { list: T[] }): T[] => (Array.isArray(res) ? res : (res.list ?? []))

export function useOpenApps() {
  return useQuery({
    queryKey: [...KEY, 'apps'],
    queryFn: async () => toList(await api.get<OpenApp[] | { list: OpenApp[] }>('/openapi/apps')),
  })
}

export function useSaveOpenApp() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: OpenAppForm }) =>
      id ? api.put<OpenApp>(`/openapi/apps/${id}`, data) : api.post<OpenAppCreated>('/openapi/apps', data),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'apps'] }),
  })
}

export function useDeleteOpenApp() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`/openapi/apps/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'apps'] }),
  })
}

export function useSetOpenAppEnabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.put(`/openapi/apps/${id}/enabled`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'apps'] }),
  })
}

export function useResetOpenAppSecret() {
  return useMutation({
    mutationFn: (id: string) => api.post<{ appSecret: string }>(`/openapi/apps/${id}/reset-secret`),
  })
}

export function useWebhooks() {
  return useQuery({
    queryKey: [...KEY, 'webhooks'],
    queryFn: async () => toList(await api.get<Webhook[] | { list: Webhook[] }>('/openapi/webhooks')),
  })
}

export function useSaveWebhook() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: WebhookForm }) =>
      id ? api.put<Webhook>(`/openapi/webhooks/${id}`, data) : api.post<WebhookCreated>('/openapi/webhooks', data),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'webhooks'] }),
  })
}

export function useDeleteWebhook() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`/openapi/webhooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'webhooks'] }),
  })
}

export function useSetWebhookEnabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.put(`/openapi/webhooks/${id}/enabled`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'webhooks'] }),
  })
}

export function useResetWebhookSecret() {
  return useMutation({
    mutationFn: (id: string) => api.post<{ secret: string }>(`/openapi/webhooks/${id}/reset-secret`),
  })
}

export function useTestWebhook() {
  return useMutation({
    mutationFn: (id: string) => api.post<WebhookTestResult>(`/openapi/webhooks/${id}/test`),
  })
}

export function useOpenEvents() {
  return useQuery({
    queryKey: [...KEY, 'events'],
    queryFn: () => api.get<OpenEvent[]>('/openapi/events'),
    staleTime: 5 * 60 * 1000,
  })
}

export function useDeliveries(query: DeliveryQuery) {
  return useQuery({
    queryKey: [...KEY, 'deliveries', query],
    queryFn: () => api.get<Page<Delivery>>('/openapi/deliveries', { ...query }),
    placeholderData: keepPreviousData,
  })
}

export function useRetryDelivery() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post(`/openapi/deliveries/${id}/retry`),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'deliveries'] }),
  })
}

/** 事件名的分组前缀：user.created → user */
export const eventGroup = (name: string) => name.split('.')[0] ?? name

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)){3}$/

function isIPv6(raw: string): boolean {
  if (!/^[0-9a-fA-F:.]+$/.test(raw) || !raw.includes(':')) return false
  if ((raw.match(/::/g) ?? []).length > 1 || raw.includes(':::')) return false
  let groups = raw.split(':')
  const last = groups[groups.length - 1]
  let need = 8
  if (last.includes('.')) {
    if (!IPV4.test(last)) return false
    groups = groups.slice(0, -1)
    need = 6
  }
  const parts = groups.filter((g) => g !== '')
  if (!parts.every((g) => /^[0-9a-fA-F]{1,4}$/.test(g))) return false
  return raw.includes('::') ? parts.length < need : parts.length === need && groups.length === need
}

/** 单个 IPv4/IPv6 地址或 CIDR（如 10.0.0.0/8、2001:db8::/32） */
export function isValidIpOrCidr(raw: string): boolean {
  const [addr, mask, ...rest] = raw.trim().split('/')
  if (rest.length > 0 || !addr) return false
  const v4 = IPV4.test(addr)
  if (!v4 && !isIPv6(addr)) return false
  if (mask === undefined) return true
  if (!/^\d{1,3}$/.test(mask)) return false
  return Number(mask) <= (v4 ? 32 : 128)
}

/** Webhook 地址：https；开发环境允许 http://localhost（含 127.0.0.1） */
export function isValidWebhookUrl(raw: string): boolean {
  try {
    const u = new URL(raw)
    if (u.hash) return false
    if (u.protocol === 'https:') return !!u.hostname
    return u.protocol === 'http:' && (u.hostname === 'localhost' || u.hostname === '127.0.0.1')
  } catch {
    return false
  }
}

export const splitLines = (text: string) =>
  text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)

/** 签名调用示例（Node 18+）：语义与说明文字一致，secret 用占位符 */
export function buildSignExample(origin: string, appKey: string): string {
  const site = origin.replace(/\/+$/, '')
  return `import { createHash, createHmac, randomUUID } from 'node:crypto'

const APP_KEY = '${appKey}'
const APP_SECRET = 'YOUR_APP_SECRET' // 创建/重置时只显示一次

const method = 'POST'
const path = '/api/v1/open/users' // 示例路径，换成实际接口
const query = 'page=1' // 不含 ?，没有则为空串
const body = JSON.stringify({ name: 'demo' }) // GET 请求用空串

const timestamp = String(Math.floor(Date.now() / 1000)) // Unix 秒，与服务器偏差需在 5 分钟内
const nonce = randomUUID() // 每次请求唯一，防重放
const bodyHash = createHash('sha256').update(body).digest('hex')

// 签名串：METHOD \\n path \\n query \\n sha256hex(body) \\n timestamp \\n nonce
const stringToSign = [method, path, query, bodyHash, timestamp, nonce].join('\\n')
const signature = createHmac('sha256', APP_SECRET).update(stringToSign).digest('hex')

const res = await fetch('${site}' + path + (query ? '?' + query : ''), {
  method,
  headers: {
    'Content-Type': 'application/json',
    'X-App-Key': APP_KEY,
    'X-Timestamp': timestamp,
    'X-Nonce': nonce,
    'X-Signature': signature,
  },
  body: method === 'GET' ? undefined : body,
})
console.log(res.status, await res.text())`
}

/** GET /openapi/routes 的一项：由后端运行时路由清单生成 */
export interface ApiRoute {
  method: string
  /** 完整路径（含 /api/v1），路径参数为 :id 写法 */
  path: string
  module: string
  /** 权限码；多个以逗号连接，无则为空串 */
  perm: string
  /** 是否需要登录 */
  auth: boolean
  superOnly: boolean
  /** 令牌或应用签名至少一种可调用 */
  openapiCallable: boolean
  tokenCallable: boolean
  appCallable: boolean
  summary?: string
}

export function useApiRoutes() {
  return useQuery({
    queryKey: [...KEY, 'routes'],
    queryFn: () => api.get<ApiRoute[]>('/openapi/routes'),
  })
}

/** 带鉴权的 blob 下载（浏览器直接打开链接不会带 Authorization） */
export async function downloadOpenApiJson(): Promise<Blob> {
  const res = await http.get<unknown>('/openapi.json', { responseType: 'blob' })
  return res.data instanceof Blob ? res.data : new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' })
}

const BODY_METHODS = ['POST', 'PUT', 'PATCH']

/** 路径参数 :id → {id}，便于在 curl 里替换 */
const displayPath = (path: string) => path.replace(/:(\w+)/g, '{$1}')

/** 个人访问令牌方式的 curl 示例；请求体只给空对象占位，不臆造字段 */
export function buildTokenCurl(origin: string, r: Pick<ApiRoute, 'method' | 'path'>): string {
  const url = origin.replace(/\/+$/, '') + displayPath(r.path)
  const lines = [`curl -X ${r.method} '${url}'`, `  -H 'Authorization: Bearer nxp_YOUR_TOKEN'`]
  if (BODY_METHODS.includes(r.method)) lines.push(`  -H 'Content-Type: application/json'`, `  -d '{}'`)
  return lines.join(' \\\n')
}

/** 应用签名方式的 curl 示例（bash + openssl），签名串与后端 core/openapi 一致 */
export function buildSignCurl(origin: string, r: Pick<ApiRoute, 'method' | 'path'>): string {
  const url = origin.replace(/\/+$/, '') + displayPath(r.path)
  const hasBody = BODY_METHODS.includes(r.method)
  const curl = [`curl -X ${r.method} "${url}"`, `  -H "X-App-Key: $APP_KEY"`, `  -H "X-Timestamp: $TS"`, `  -H "X-Nonce: $NONCE"`, `  -H "X-Signature: $SIGN"`]
  if (hasBody) curl.push(`  -H "Content-Type: application/json"`, `  -d "$BODY"`)
  return [
    `APP_KEY=ak_xxx`,
    `APP_SECRET=YOUR_APP_SECRET`,
    `BODY='${hasBody ? '{}' : ''}'`,
    `QUERY='' # 规范化后的查询串（按 key 排序），没有则为空`,
    `TS=$(date +%s)`,
    `NONCE=$(openssl rand -hex 16)`,
    `BODY_HASH=$(printf '%s' "$BODY" | openssl dgst -sha256 | sed 's/^.* //')`,
    `SIGN=$(printf '%s\\n%s\\n%s\\n%s\\n%s\\n%s' '${r.method}' '${displayPath(r.path)}' "$QUERY" "$BODY_HASH" "$TS" "$NONCE" \\`,
    `  | openssl dgst -sha256 -hmac "$APP_SECRET" | sed 's/^.* //')`,
    curl.join(' \\\n'),
  ].join('\n')
}
