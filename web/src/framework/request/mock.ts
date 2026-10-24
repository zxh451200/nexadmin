import type { AxiosAdapter, AxiosResponse, InternalAxiosRequestConfig } from 'axios'

export type MockMethod = 'GET' | 'POST' | 'PUT' | 'DELETE'

export interface MockContext {
  params: Record<string, string>
  query: Record<string, unknown>
  // 请求体是任意 JSON，各 handler 按自身接口自行取字段；此处用 unknown 会迫使全部 mock 逐处断言
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  body: any
  headers: Record<string, string>
}

export interface MockRule {
  method: MockMethod
  /** 相对 baseURL，支持 :id */
  url: string
  handler: (ctx: MockContext) => unknown
}

class MockError extends Error {
  code: number
  reason?: string
  constructor(code: number, message: string, reason?: string) {
    super(message)
    this.code = code
    this.reason = reason
  }
}

export function mockError(code: number, message: string, reason?: string): never {
  throw new MockError(code, message, reason)
}

interface CompiledRule extends MockRule {
  segments: string[]
}

const rules: CompiledRule[] = []

const split = (url: string) => url.split('/').filter(Boolean)

export function defineMock(list: MockRule[]) {
  for (const r of list) rules.push({ ...r, segments: split(r.url) })
}

function match(method: string, path: string): { rule: CompiledRule; params: Record<string, string> } | null {
  const parts = split(path)
  for (const rule of rules) {
    if (rule.method !== method || rule.segments.length !== parts.length) continue
    const params: Record<string, string> = {}
    let ok = true
    for (let i = 0; i < parts.length; i++) {
      const seg = rule.segments[i]
      if (seg.startsWith(':')) params[seg.slice(1)] = decodeURIComponent(parts[i])
      else if (seg !== parts[i]) {
        ok = false
        break
      }
    }
    if (ok) return { rule, params }
  }
  return null
}

const PUBLIC_PREFIXES = [
  '/auth/login',
  '/auth/captcha',
  '/auth/sms-code',
  '/auth/refresh',
  '/auth/methods',
  '/auth/providers',
  '/auth/oauth',
  '/auth/forgot',
  '/settings/public',
  '/system/status',
  '/setup',
  '/i18n',
]

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const randomDelay = () => 150 + Math.floor(Math.random() * 150)

let idSeq = 0
/** 19 位数字字符串 ID */
export function genId(): string {
  idSeq = (idSeq + 1) % 1000
  const base = BigInt(Date.now()) * 1000000n + BigInt(Math.floor(Math.random() * 1000)) * 1000n + BigInt(idSeq)
  return base.toString().padStart(19, '1').slice(0, 19)
}

function parseBody(data: unknown) {
  if (typeof data !== 'string' || !data) return data ?? undefined
  try {
    return JSON.parse(data)
  } catch {
    return data
  }
}

function respond(config: InternalAxiosRequestConfig, code: number, message: string, data: unknown, reason?: string): AxiosResponse {
  return { data: { code, message, data, ...(reason ? { reason } : {}) }, status: 200, statusText: 'OK', headers: {}, config, request: {} }
}

export const mockAdapter: AxiosAdapter = async (config) => {
  await sleep(randomDelay())
  const rawUrl = config.url ?? ''
  const [path, qs] = rawUrl.split('?')
  const method = (config.method ?? 'get').toUpperCase()
  const query: Record<string, unknown> = {}
  new URLSearchParams(qs ?? '').forEach((v, k) => (query[k] = v))
  Object.assign(query, config.params ?? {})

  const isPublic = PUBLIC_PREFIXES.some((p) => path === p || path.startsWith(p + '/'))
  const auth = config.headers?.get?.('Authorization') as string | undefined
  if (!isPublic && !auth) return respond(config, 401, 'Unauthorized', null)

  const hit = match(method, path)
  if (!hit) return respond(config, 404, `Mock not found: ${method} ${path}`, null)
  try {
    const data = hit.rule.handler({
      params: hit.params,
      query,
      body: parseBody(config.data),
      headers: { authorization: auth ?? '' },
    })
    return respond(config, 0, 'ok', await data)
  } catch (e) {
    if (e instanceof MockError) return respond(config, e.code, e.message, null, e.reason)
    throw e
  }
}

export interface CrudMockOptions<T extends { id: string }> {
  base: string
  seed: T[]
  searchFields?: (keyof T & string)[]
  filterFields?: string[]
  sortField?: string
  /** 额外的自定义过滤（如日期范围），返回 false 则剔除该行 */
  extraFilter?: (row: T, query: Record<string, unknown>) => boolean
}

export interface CrudStore<T extends { id: string }> {
  all(): T[]
  get(id: string): T | undefined
  /** 移除并返回被移除的行（回收站接入用） */
  remove(id: string): T | undefined
  /** 把行放回列表头部（回收站还原用） */
  insert(row: T): void
}

export function createCrudMock<T extends { id: string }>(opts: CrudMockOptions<T>): CrudStore<T> {
  const { base, searchFields = [], filterFields = [], sortField, extraFilter } = opts
  let items: T[] = opts.seed.map((s) => ({ ...s }))
  const rec = (x: T) => x as unknown as Record<string, unknown>

  defineMock([
    {
      method: 'GET',
      url: base,
      handler: ({ query }) => {
        const page = Math.max(1, Number(query.page) || 1)
        const pageSize = Math.max(1, Number(query.pageSize) || 10)
        let rows = items.slice()
        const kw = String(query.keyword ?? '').trim().toLowerCase()
        if (kw && searchFields.length) {
          rows = rows.filter((r) => searchFields.some((f) => String(rec(r)[f] ?? '').toLowerCase().includes(kw)))
        }
        for (const f of filterFields) {
          const v = query[f]
          if (v === undefined || v === null || v === '') continue
          const set = String(v).split(',')
          rows = rows.filter((r) => set.includes(String(rec(r)[f])))
        }
        if (extraFilter) rows = rows.filter((r) => extraFilter(r, query))
        if (sortField) rows.sort((a, b) => Number(rec(a)[sortField] ?? 0) - Number(rec(b)[sortField] ?? 0))
        const total = rows.length
        return { list: rows.slice((page - 1) * pageSize, page * pageSize), total, page, pageSize }
      },
    },
    {
      method: 'PUT',
      url: `${base}/sort`,
      handler: ({ body }) => {
        const ids: string[] = body?.ids ?? []
        const field = sortField ?? 'sort'
        ids.forEach((id, i) => {
          const it = items.find((x) => x.id === id)
          if (it) rec(it)[field] = (i + 1) * 10
        })
        return null
      },
    },
    {
      method: 'GET',
      url: `${base}/:id`,
      handler: ({ params }) => items.find((x) => x.id === params.id) ?? mockError(404, 'Not found'),
    },
    {
      method: 'POST',
      url: base,
      handler: ({ body }) => {
        const now = new Date().toISOString()
        const row = { createdAt: now, updatedAt: now, ...body, id: genId() } as T
        items = [row, ...items]
        return row
      },
    },
    {
      method: 'PUT',
      url: `${base}/:id`,
      handler: ({ params, body }) => {
        const idx = items.findIndex((x) => x.id === params.id)
        if (idx < 0) mockError(404, 'Not found')
        items[idx] = { ...items[idx], ...body, id: params.id, updatedAt: new Date().toISOString() }
        return items[idx]
      },
    },
    {
      method: 'DELETE',
      url: `${base}/:id`,
      handler: ({ params }) => {
        items = items.filter((x) => x.id !== params.id)
        return null
      },
    },
    {
      method: 'DELETE',
      url: base,
      handler: ({ query }) => {
        const ids = String(query.ids ?? '').split(',').filter(Boolean)
        items = items.filter((x) => !ids.includes(x.id))
        return null
      },
    },
  ])

  return {
    all: () => items,
    get: (id) => items.find((x) => x.id === id),
    remove: (id) => {
      const hit = items.find((x) => x.id === id)
      items = items.filter((x) => x.id !== id)
      return hit
    },
    insert: (row) => {
      items = [row, ...items.filter((x) => x.id !== row.id)]
    },
  }
}
