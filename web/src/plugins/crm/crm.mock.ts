import { defineMock, mockError } from '@/framework/request/mock'
import type { CrmCustomer, CrmOpportunity } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

let customers: CrmCustomer[] = [
  {
    id: '7430000000000000001',
    name: '杭州云栖酒店',
    code: 'C-001',
    level: 'A',
    status: 'active',
    externalSource: 'hotel',
    externalNo: 'M0001',
    owner: '王五',
    phone: '13800000000',
    email: 'a@example.com',
    remark: '',
    createdAt: ago(60 * 24 * 40),
  },
  {
    id: '7430000000000000002',
    name: '李某',
    code: '',
    level: '',
    status: 'active',
    externalSource: 'mall',
    externalNo: '10086',
    owner: '',
    phone: '',
    email: '',
    remark: '由 mall member 自动建档',
    createdAt: ago(60 * 24 * 10),
  },
]

let opportunities: CrmOpportunity[] = [
  {
    id: '7431000000000000001',
    customerId: 7430000000000000001,
    title: '秋季房务系统扩容',
    stage: 'proposal',
    amountMinor: 1280000,
    currency: 'CNY',
    expectedAt: '2026-11-30',
    owner: '王五',
    remark: '',
    createdAt: ago(60 * 24 * 5),
  },
  {
    id: '7431000000000000002',
    customerId: 7430000000000000001,
    title: '商城会员储值接入',
    stage: 'lead',
    amountMinor: 0,
    currency: 'CNY',
    expectedAt: '',
    owner: '',
    remark: '线索',
    createdAt: ago(60 * 24 * 2),
  },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/crm/customers',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = customers.filter(
        (c) => !kw || c.name.toLowerCase().includes(kw) || (c.code ?? '').toLowerCase().includes(kw),
      )
      if (query.status) rows = rows.filter((c) => c.status === query.status)
      if (query.source) rows = rows.filter((c) => c.externalSource === query.source)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers',
    handler: ({ body }) => {
      if (!String(body.name ?? '').trim()) mockError(400, 'name required')
      if (body.externalSource && body.externalNo) {
        const clash = customers.find(
          (c) => c.externalSource === body.externalSource && c.externalNo === body.externalNo,
        )
        if (clash) return clash // 锚点幂等
      }
      const id = String(7430000000000000000n + BigInt(customers.length + 1))
      const c: CrmCustomer = {
        id,
        name: body.name,
        // 与后端一致：编码留空时由 ID 派生。
        code: String(body.code ?? '').trim() || `C-${BigInt(id).toString(36).toUpperCase()}`,
        level: body.level ?? '',
        status: body.status ?? 'active',
        externalSource: body.externalSource || null,
        externalNo: body.externalNo || null,
        owner: body.owner ?? '',
        phone: body.phone ?? '',
        email: body.email ?? '',
        remark: body.remark ?? '',
        createdAt: new Date().toISOString(),
      }
      customers = [...customers, c]
      return c
    },
  },
  {
    method: 'PUT',
    url: '/plugins/crm/customers/:id',
    handler: ({ params, body }) => {
      const c = customers.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      c.name = body.name
      // 与后端一致：更新时编码留空表示沿用原编码。
      c.code = String(body.code ?? '').trim() || c.code
      c.level = body.level ?? c.level
      c.status = body.status ?? c.status
      c.owner = body.owner ?? c.owner
      c.phone = body.phone ?? c.phone
      c.email = body.email ?? c.email
      c.remark = body.remark ?? c.remark
      return c
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/customers/:id',
    handler: ({ params }) => {
      if (opportunities.some((o) => String(o.customerId) === params.id)) mockError(409, 'customer in use')
      customers = customers.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'GET',
    url: '/plugins/crm/opportunities',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = opportunities.filter((o) => !kw || o.title.toLowerCase().includes(kw))
      if (query.stage) rows = rows.filter((o) => o.stage === query.stage)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/opportunities',
    handler: ({ body }) => {
      if (!String(body.title ?? '').trim()) mockError(400, 'title required')
      const o: CrmOpportunity = {
        id: String(7431000000000000000n + BigInt(opportunities.length + 1)),
        customerId: Number(body.customerId) || 0,
        title: body.title,
        stage: body.stage ?? 'lead',
        amountMinor: Number(body.amountMinor) || 0,
        currency: body.currency ?? 'CNY',
        expectedAt: body.expectedAt ?? '',
        owner: body.owner ?? '',
        remark: body.remark ?? '',
        createdAt: new Date().toISOString(),
      }
      opportunities = [...opportunities, o]
      return o
    },
  },
  {
    method: 'PUT',
    url: '/plugins/crm/opportunities/:id',
    handler: ({ params, body }) => {
      const o = opportunities.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      if (['won', 'lost'].includes(o.stage) && body.stage !== o.stage) mockError(409, 'final stage')
      o.title = body.title
      o.stage = body.stage ?? o.stage
      o.amountMinor = Number(body.amountMinor) || 0
      o.expectedAt = body.expectedAt ?? o.expectedAt
      o.owner = body.owner ?? o.owner
      o.remark = body.remark ?? o.remark
      return o
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/opportunities/:id',
    handler: ({ params }) => {
      opportunities = opportunities.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
])
