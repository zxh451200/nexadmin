import { defineMock, mockError } from '@/framework/request/mock'
import type { CrmActivity, CrmCustomer, CrmOpportunity } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const dayFromNow = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString().slice(0, 10)

let customers: CrmCustomer[] = [
  {
    id: '7430000000000000001',
    name: '杭州云栖酒店',
    type: 'company',
    code: 'C-001',
    level: 'A',
    status: 'active',
    externalSource: 'hotel',
    externalNo: 'M0001',
    owner: '王五',
    phone: '13800000000',
    email: 'a@example.com',
    country: 'CN',
    nationality: '',
    taxNo: '91330100MA2AB1CD3X',
    industry: '住宿业',
    address: '浙江省杭州市西湖区云栖小镇',
    remark: '',
    createdAt: ago(60 * 24 * 40),
  },
  {
    id: '7430000000000000002',
    name: '李某',
    type: 'person',
    code: '',
    level: '',
    status: 'active',
    externalSource: 'mall',
    externalNo: '10086',
    owner: '',
    phone: '',
    email: '',
    country: '',
    nationality: 'CN',
    idType: 'id_card',
    idNo: '33010619900101001X',
    remark: '由 mall member 自动建档',
    createdAt: ago(60 * 24 * 10),
  },
]

let opportunities: CrmOpportunity[] = [
  {
    id: '7431000000000000001',
    customerId: '7430000000000000001',
    customerName: '杭州云栖酒店',
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
    customerId: '7430000000000000001',
    customerName: '杭州云栖酒店',
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

const customerName = (id: string) => customers.find((c) => c.id === id)?.name
const customerType = (id: string) => customers.find((c) => c.id === id)?.type

let activities: CrmActivity[] = [
  {
    id: '7432000000000000001',
    customerId: '7430000000000000001',
    kind: 'todo',
    content: '回访扩容方案报价，敲定预算区间',
    nextAt: dayFromNow(-3), // 已过期：待跟进清单的第一条
    done: false,
    author: '王五',
    createdAt: ago(60 * 24 * 6),
  },
  {
    id: '7432000000000000002',
    customerId: '7430000000000000001',
    kind: 'call',
    content: '电话沟通下一阶段验收标准',
    nextAt: dayFromNow(0), // 恰好今天：不算过期
    done: false,
    author: '王五',
    createdAt: ago(60 * 22),
  },
]

const withCustomerBrief = (o: CrmOpportunity): CrmOpportunity => ({
  ...o,
  customerName: o.customerId && o.customerId !== '0' ? customerName(o.customerId) : undefined,
  customerType: o.customerId && o.customerId !== '0' ? customerType(o.customerId) : undefined,
})

// 与后端同一套规则：类型决定哪组身份字段生效，另一组清空——
// 否则 mock 里会出现「企业客户有条护照号」，UI 回归照不出来。
function applyTypeFields(c: CrmCustomer) {
  if (c.type === 'company') {
    c.nationality = ''
    c.idType = ''
    c.idNo = ''
  } else {
    c.taxNo = ''
    c.industry = ''
    c.address = ''
  }
}

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/crm/stats',
    // 与后端 Stats 同口径：类型/状态分桶计数 + 未到终态的商机数 + 六阶段计数与金额
    // （金额按币种分开，与后端 stage×currency 分组一致）。
    // 没有这条规则时 mock 下六张卡与漏斗恒为 0，看板改动在开发态完全看不出来。
    handler: () => {
      const stageOf = (stage: string) => opportunities.filter((o) => o.stage === stage).length
      const amountsOf = (stage: string) => {
        const out: Record<string, number> = {}
        opportunities
          .filter((o) => o.stage === stage && o.amountMinor > 0)
          .forEach((o) => {
            out[o.currency] = (out[o.currency] ?? 0) + o.amountMinor
          })
        return out
      }
      const day = (offset: number) => new Date(Date.now() - offset * 86_400_000).toISOString().slice(0, 10)
      const createdDaily = Array.from({ length: 30 }, (_, i) => {
        const d = day(29 - i)
        return {
          date: d,
          count: customers.filter((c) => (c.createdAt ?? '').slice(0, 10) === d).length,
        }
      })
      return {
        persons: customers.filter((c) => c.type === 'person').length,
        companies: customers.filter((c) => c.type === 'company').length,
        active: customers.filter((c) => c.status === 'active').length,
        inactive: customers.filter((c) => c.status === 'inactive').length,
        pool: customers.filter((c) => !c.owner).length,
        openDeals: opportunities.filter((o) => o.stage !== 'won' && o.stage !== 'lost').length,
        stages: Object.fromEntries(['lead', 'qualified', 'proposal', 'negotiate', 'won', 'lost'].map((s) => [s, stageOf(s)])),
        stageAmounts: Object.fromEntries(['lead', 'qualified', 'proposal', 'negotiate', 'won', 'lost'].map((s) => [s, amountsOf(s)])),
        sources: Object.fromEntries(
          ['hotel', 'mall', 'order', 'manual'].map((src) => [src, customers.filter((c) => c.externalSource === src).length]),
        ),
        createdDaily,
      }
    },
  },
  {
    method: 'GET',
    url: '/plugins/crm/customers/todos',
    // 与后端同口径：done=false 且 next_at 非空并已到期（含当天），客户名回填，按到期日升序。
    handler: () => {
      const today = new Date().toISOString().slice(0, 10)
      const rows = activities
        .filter((a) => !a.done && a.nextAt && a.nextAt <= today)
        .sort((x, y) => ((x.nextAt ?? '') < (y.nextAt ?? '') ? -1 : (x.nextAt ?? '') > (y.nextAt ?? '') ? 1 : 0))
        .map((a) => ({
          id: a.id,
          customerId: a.customerId,
          customerName: customerName(a.customerId) ?? '',
          kind: a.kind,
          content: a.content,
          nextAt: a.nextAt ?? '',
          author: a.author,
          overdue: (a.nextAt ?? '') < today,
        }))
      return { items: rows.slice(0, 200), total: rows.length }
    },
  },
  {
    method: 'GET',
    url: '/plugins/crm/customers',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = customers.filter(
        (c) =>
          !kw ||
          c.name.toLowerCase().includes(kw) ||
          (c.code ?? '').toLowerCase().includes(kw) ||
          (c.phone ?? '').includes(kw) ||
          (c.email ?? '').toLowerCase().includes(kw) ||
          (c.externalNo ?? '').toLowerCase().includes(kw) ||
          (c.idNo ?? '').toLowerCase().includes(kw) ||
          (c.taxNo ?? '').toLowerCase().includes(kw),
      )
      if (query.type) rows = rows.filter((c) => c.type === query.type)
      if (query.status) rows = rows.filter((c) => c.status === query.status)
      if (query.source) rows = rows.filter((c) => c.externalSource === query.source)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/crm/customers/pool',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = customers.filter(
        (c) =>
          !c.owner &&
          (!kw ||
            c.name.toLowerCase().includes(kw) ||
            (c.code ?? '').toLowerCase().includes(kw) ||
            (c.phone ?? '').includes(kw) ||
            (c.email ?? '').toLowerCase().includes(kw)),
      )
      if (query.type) rows = rows.filter((c) => c.type === query.type)
      if (query.status) rows = rows.filter((c) => c.status === query.status)
      if (query.source) rows = rows.filter((c) => c.externalSource === query.source)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers/:id/claim',
    handler: ({ params }) => {
      const c = customers.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      if (c.owner) mockError(409, 'pool taken')
      c.owner = 'demo'
      return c
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers/:id/release',
    handler: ({ params }) => {
      const c = customers.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      c.owner = ''
      return c
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers/merge',
    handler: ({ body }) => {
      const target = customers.find((x) => x.id === String(body.targetId ?? '')) ?? mockError(404, 'not found')
      const source = customers.find((x) => x.id === String(body.sourceId ?? '')) ?? mockError(404, 'not found')
      if (target.id === source.id) mockError(400, 'merge self')
      // 关联记录整体迁移（mock 数据里只有商机挂 customerId）。
      opportunities.forEach((o) => {
        if (o.customerId === source.id) o.customerId = target.id
      })
      // 补空不覆盖。
      target.level = target.level || source.level
      target.owner = target.owner || source.owner
      target.phone = target.phone || source.phone
      target.email = target.email || source.email
      target.remark = target.remark || source.remark
      customers = customers.filter((c) => c.id !== source.id)
      return target
    },
  },
  {
    method: 'GET',
    url: '/plugins/crm/customers/export',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = customers.filter(
        (c) =>
          !kw ||
          c.name.toLowerCase().includes(kw) ||
          (c.code ?? '').toLowerCase().includes(kw) ||
          (c.phone ?? '').includes(kw) ||
          (c.email ?? '').toLowerCase().includes(kw) ||
          (c.externalNo ?? '').toLowerCase().includes(kw) ||
          (c.idNo ?? '').toLowerCase().includes(kw) ||
          (c.taxNo ?? '').toLowerCase().includes(kw),
      )
      if (query.type) rows = rows.filter((c) => c.type === query.type)
      if (query.status) rows = rows.filter((c) => c.status === query.status)
      if (query.source) rows = rows.filter((c) => c.externalSource === query.source)
      return [...rows].reverse()
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
        type: body.type === 'company' ? 'company' : 'person',
        level: body.level ?? '',
        status: body.status ?? 'active',
        externalSource: body.externalSource || null,
        externalNo: body.externalNo || null,
        owner: body.owner ?? '',
        phone: body.phone ?? '',
        email: body.email ?? '',
        country: body.country ?? '',
        nationality: body.nationality ?? '',
        idType: body.idType ?? '',
        idNo: body.idNo ?? '',
        taxNo: body.taxNo ?? '',
        industry: body.industry ?? '',
        address: body.address ?? '',
        remark: body.remark ?? '',
        createdAt: new Date().toISOString(),
      }
      applyTypeFields(c)
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
      c.type = body.type === 'company' ? 'company' : 'person'
      c.level = body.level ?? c.level
      c.status = body.status ?? c.status
      c.owner = body.owner ?? c.owner
      c.phone = body.phone ?? c.phone
      c.email = body.email ?? c.email
      c.country = body.country ?? c.country
      c.nationality = body.nationality ?? c.nationality
      c.idType = body.idType ?? c.idType
      c.idNo = body.idNo ?? c.idNo
      c.taxNo = body.taxNo ?? c.taxNo
      c.industry = body.industry ?? c.industry
      c.address = body.address ?? c.address
      c.remark = body.remark ?? c.remark
      applyTypeFields(c)
      return c
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers/bind-external',
    handler: ({ body }) => {
      const c = customers.find((x) => x.id === String(body.customerId ?? '')) ?? mockError(404, 'not found')
      const source = String(body.source ?? '')
      const externalNo = String(body.externalNo ?? '').trim()
      if (!source || !externalNo) mockError(400, 'external invalid')
      const clash = customers.find((x) => x.externalSource === source && x.externalNo === externalNo)
      if (clash && clash.id !== c.id) mockError(409, 'external taken')
      c.externalSource = source
      c.externalNo = externalNo
      return c
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/customers',
    handler: ({ query }) => {
      // 与后端同一套规则：ids 缺失/为空/任一项非法 → 400，绝不落到「无条件全表删」。
      const ids = String(query.ids ?? '')
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
      if (ids.length === 0 || ids.some((id) => !/^\d+$/.test(id))) mockError(400, 'ids required')
      const picked = customers.filter((c) => ids.includes(c.id))
      if (picked.length === 0) mockError(404, 'not found')
      // 名下仍有商机的客户不删，计入 blocked；全部占用才整批 409。
      const free = picked.filter((c) => !opportunities.some((o) => o.customerId === c.id))
      if (free.length === 0) mockError(409, 'customer in use')
      const gone = new Set(free.map((c) => c.id))
      customers = customers.filter((c) => !gone.has(c.id))
      return { deleted: gone.size, blocked: ids.length - gone.size }
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/customers/:id',
    handler: ({ params }) => {
      if (opportunities.some((o) => o.customerId === params.id)) mockError(409, 'customer in use')
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
      if (query.customerId) rows = rows.filter((o) => o.customerId === query.customerId)
      return paginate([...rows].reverse().map(withCustomerBrief), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/opportunities',
    handler: ({ body }) => {
      if (!String(body.title ?? '').trim()) mockError(400, 'title required')
      const customerId = String(body.customerId ?? '') || '0'
      if (customerId !== '0' && !customerName(customerId)) mockError(404, 'customer not found')
      const o: CrmOpportunity = {
        id: String(7431000000000000000n + BigInt(opportunities.length + 1)),
        customerId,
        customerName: customerName(customerId),
        customerType: customerType(customerId),
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
      const customerId = String(body.customerId ?? '') || '0'
      if (customerId !== '0' && !customerName(customerId)) mockError(404, 'customer not found')
      o.customerId = customerId
      o.customerName = customerName(customerId)
      o.customerType = customerType(customerId)
      o.title = body.title
      o.stage = body.stage ?? o.stage
      o.amountMinor = Number(body.amountMinor) || 0
      o.currency = body.currency ?? o.currency
      o.expectedAt = body.expectedAt ?? o.expectedAt
      o.owner = body.owner ?? o.owner
      o.remark = body.remark ?? o.remark
      return o
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/opportunities',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '')
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
      if (ids.length === 0 || ids.some((id) => !/^\d+$/.test(id))) mockError(400, 'ids required')
      const picked = opportunities.filter((o) => ids.includes(o.id))
      if (picked.length === 0) mockError(404, 'not found')
      const gone = new Set(picked.map((o) => o.id))
      opportunities = opportunities.filter((o) => !gone.has(o.id))
      return { deleted: gone.size, blocked: ids.length - gone.size }
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
  {
    method: 'GET',
    url: '/plugins/crm/customers/:id/activities',
    handler: ({ params, query }) => {
      let rows = activities.filter((a) => a.customerId === params.id)
      if (query.kind) rows = rows.filter((a) => a.kind === query.kind)
      if (query.openOnly === '1' || query.openOnly === 'true') rows = rows.filter((a) => !a.done)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/crm/customers/:id/activities',
    handler: ({ params, body }) => {
      if (!String(body.content ?? '').trim()) mockError(400, 'content required')
      const a: CrmActivity = {
        id: String(7432000000000000000n + BigInt(activities.length + 1)),
        customerId: params.id,
        kind: body.kind ?? 'note',
        content: body.content,
        nextAt: body.nextAt ?? '',
        done: false,
        author: '王五',
        createdAt: new Date().toISOString(),
      }
      activities = [...activities, a]
      return a
    },
  },
  {
    method: 'PUT',
    url: '/plugins/crm/activities/:id/done',
    handler: ({ params, body }) => {
      const a = activities.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      a.done = !!body.done
      return a
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/crm/activities/:id',
    handler: ({ params }) => {
      activities = activities.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
])
