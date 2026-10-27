import { defineMock, mockError } from '@/framework/request/mock'
import type { AiModel, AiUsage, AiUsageSummaryRow } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

let models: AiModel[] = [
  {
    id: '7440000000000000001',
    kind: 'llm',
    name: '主力对话模型',
    provider: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    model: 'gpt-4o',
    apiKey: '********',
    enabled: true,
    isDefault: true,
    createdAt: ago(60 * 24 * 30),
    updatedAt: ago(60 * 24),
  },
  {
    id: '7440000000000000002',
    kind: 'embedding',
    name: '向量模型',
    provider: 'bge',
    baseUrl: 'https://api.example.com/v1',
    model: 'bge-m3',
    apiKey: '********',
    dimension: 1024,
    enabled: true,
    isDefault: true,
    createdAt: ago(60 * 24 * 30),
    updatedAt: ago(60 * 24 * 3),
  },
]

const usages: AiUsage[] = [
  { id: '7441', modelId: 7440000000000000001, kind: 'llm', provider: 'openai', model: 'gpt-4o', promptTokens: 1200, completionTokens: 350, totalTokens: 1550, bizType: 'rag', bizNo: 'chat-101', createdAt: ago(12) },
  { id: '7442', modelId: 7440000000000000002, kind: 'embedding', provider: 'bge', model: 'bge-m3', promptTokens: 8000, completionTokens: 0, totalTokens: 8000, bizType: 'rag', bizNo: 'doc-42', createdAt: ago(30) },
  { id: '7443', modelId: 7440000000000000001, kind: 'llm', provider: 'openai', model: 'gpt-4o', promptTokens: 900, completionTokens: 120, totalTokens: 1020, bizType: 'office', bizNo: 'summary-7', createdAt: ago(90) },
]

const summary: AiUsageSummaryRow[] = [
  { kind: 'llm', calls: 2, promptTokens: 2100, completionTokens: 470, totalTokens: 2570 },
  { kind: 'embedding', calls: 1, promptTokens: 8000, completionTokens: 0, totalTokens: 8000 },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/aigateway/models',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = models.filter((m) => !kw || m.name.toLowerCase().includes(kw) || (m.model ?? '').toLowerCase().includes(kw))
      if (query.kind) rows = rows.filter((m) => m.kind === query.kind)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/aigateway/models',
    handler: ({ body }) => {
      if (!String(body.name ?? '').trim()) mockError(400, 'name required')
      const m: AiModel = {
        id: String(7440000000000000000n + BigInt(models.length + 1)),
        kind: body.kind,
        name: body.name,
        provider: body.provider ?? '',
        baseUrl: body.baseUrl ?? '',
        model: body.model ?? '',
        apiKey: body.apiKey ? '********' : '',
        dimension: Number(body.dimension) || 0,
        enabled: !!body.enabled,
        isDefault: !!body.isDefault,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      }
      models = [...models, m]
      return m
    },
  },
  {
    method: 'POST',
    url: '/plugins/aigateway/models/:id',
    handler: ({ params, body }) => {
      const m = models.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      m.name = body.name
      m.provider = body.provider ?? m.provider
      m.baseUrl = body.baseUrl ?? m.baseUrl
      m.model = body.model ?? m.model
      if (body.apiKey) m.apiKey = '********'
      m.dimension = Number(body.dimension) || m.dimension
      m.enabled = !!body.enabled
      m.isDefault = !!body.isDefault
      m.updatedAt = new Date().toISOString()
      return m
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/aigateway/models/:id',
    handler: ({ params }) => {
      models = models.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'POST',
    url: '/plugins/aigateway/models/:id/ping',
    handler: ({ params }) => {
      const m = models.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      if (!m.baseUrl) return { ok: false, detail: 'base url is empty' }
      return { ok: true, status: 200, latencyMs: 180 + Math.floor(Math.random() * 200) }
    },
  },
  {
    method: 'GET',
    url: '/plugins/aigateway/usage',
    handler: ({ query }) => {
      let rows = usages
      if (query.kind) rows = rows.filter((u) => u.kind === query.kind)
      if (query.bizType) rows = rows.filter((u) => u.bizType === query.bizType)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/aigateway/usage/summary',
    handler: () => summary,
  },
])
