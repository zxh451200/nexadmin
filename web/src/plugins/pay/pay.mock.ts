import { defineMock, mockError } from '@/framework/request/mock'
import type { PayCallback, PayChannel, PayOrder, PayRefund } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const payNo = (n: number) => `PAY7420000000000000${String(n).padStart(2, '0')}`

const mkOrder = (n: number, p: Partial<PayOrder>): PayOrder => {
  const base: PayOrder = {
    id: String(7420000000000000000n + BigInt(n)),
    paymentNo: payNo(n),
    bizType: 'trade_group',
    bizNo: `G-${n}`,
    subjectId: 'shop-001',
    channelCode: 'mock',
    method: 'native',
    scene: 'online',
    amountMinor: 12800 + n * 100,
    refundedMinor: 0,
    feeMinor: 0,
    currency: 'CNY',
    fxRateMicro: 1000000,
    tenantId: '1',
    status: 'pending',
    createdAt: ago(60 * (n + 1)),
  }
  // 先收成 PayOrder 再叠加 Partial：直接展开 Partial 会让必填字段在类型上变成可空
  return { ...base, ...p }
}

const orders: PayOrder[] = [
  mkOrder(1, { status: 'succeeded', channelTxnId: 'mock_a1', feeMinor: 77, paidAt: ago(55) }),
  mkOrder(2, { status: 'pending', expireAt: ago(-30) }),
  mkOrder(3, { status: 'partially_refunded', refundedMinor: 500, feeMinor: 77, channelTxnId: 'mock_a3', paidAt: ago(160) }),
  mkOrder(4, { status: 'failed', failReason: 'user closed cashier' }),
  mkOrder(5, { status: 'closed' }),
  mkOrder(6, { status: 'refunded', refundedMinor: 9900, channelTxnId: 'mock_a6', paidAt: ago(300) }),
]

let channels: PayChannel[] = [
  {
    id: '1',
    code: 'mock',
    name: '模拟渠道',
    provider: 'mock',
    feeRateBp: 60,
    enabled: true,
    sort: 0,
    hasConfig: true,
    updatedAt: ago(60 * 24 * 30),
  },
]

const callbacks: PayCallback[] = orders
  .filter((o) => o.status === 'succeeded' || o.status === 'failed')
  .map((o, i) => ({
    id: String(7421000000000000000n + BigInt(i)),
    channelCode: 'mock',
    paymentNo: o.paymentNo,
    ok: true,
    result: o.status === 'succeeded' ? 'accepted' : 'ignored_state_pending',
    createdAt: o.paidAt ?? o.createdAt,
  }))

let refunds: PayRefund[] = [
  {
    id: '7422',
    refundNo: 'RF7422000000000000001',
    paymentNo: payNo(3),
    subjectId: 'shop-001',
    bizType: 'trade_refund',
    bizNo: 'R-1',
    amountMinor: 500,
    feeRefundMinor: 3,
    currency: 'CNY',
    fxRateMicro: 1000000,
    reason: '部分退款',
    status: 'succeeded',
    channelRefundId: 'mockrf_1',
    succeededAt: ago(30),
    createdAt: ago(31),
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
    url: '/plugins/pay/orders',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = orders.filter((o) => !kw || o.paymentNo.toLowerCase().includes(kw) || o.bizNo.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((o) => o.status === query.status)
      if (query.channelCode) rows = rows.filter((o) => o.channelCode === query.channelCode)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/channels',
    handler: () => channels,
  },
  { method: 'GET', url: '/plugins/pay/providers', handler: () => ['mock'] },
  {
    method: 'GET',
    url: '/plugins/pay/callbacks',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = callbacks.filter((c) => !kw || c.paymentNo.toLowerCase().includes(kw))
      if (query.channelCode) rows = rows.filter((c) => c.channelCode === query.channelCode)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/refunds',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = refunds.filter((r) => !kw || r.refundNo.toLowerCase().includes(kw) || r.paymentNo.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((r) => r.status === query.status)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/orders/:no/close',
    handler: ({ params }) => {
      const o = orders.find((x) => x.paymentNo === params.no) ?? mockError(404, 'order not found')
      if (o.status !== 'pending') mockError(409, 'not closable')
      o.status = 'closed'
      return o
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/orders/:no/sync',
    handler: ({ params }) => orders.find((x) => x.paymentNo === params.no) ?? mockError(404, 'order not found'),
  },
  {
    method: 'POST',
    url: '/plugins/pay/channels',
    handler: ({ body }) => {
      const code = String(body.code ?? '').trim()
      if (!code || !body.name || !body.provider) mockError(400, 'code/name/provider required')
      if (channels.some((c) => c.code === code && c.id !== String(body.id ?? ''))) mockError(409, 'code taken')
      const existing = channels.find((c) => c.code === code)
      if (existing) {
        existing.name = body.name
        existing.feeRateBp = Number(body.feeRateBp) || 0
        existing.enabled = !!body.enabled
        existing.sort = body.sort ?? 0
        if (body.config && Object.keys(body.config).length > 0) existing.hasConfig = true
        existing.updatedAt = new Date().toISOString()
        return existing
      }
      const ch: PayChannel = {
        id: String(channels.length + 1),
        code,
        name: body.name,
        provider: body.provider,
        feeRateBp: Number(body.feeRateBp) || 0,
        enabled: !!body.enabled,
        sort: body.sort ?? 0,
        hasConfig: Object.keys(body.config ?? {}).length > 0,
        updatedAt: new Date().toISOString(),
      }
      channels = [...channels, ch]
      return ch
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/pay/channels/:id',
    handler: ({ params }) => {
      channels = channels.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/refunds',
    handler: ({ body }) => {
      const o = orders.find((x) => x.paymentNo === body.paymentNo) ?? mockError(404, 'order not found')
      if (!['succeeded', 'partially_refunded'].includes(o.status)) mockError(409, 'not refundable')
      const amount = Number(body.amountMinor) || 0
      if (amount <= 0 || amount > o.amountMinor - o.refundedMinor) mockError(409, 'refund exceeded')
      o.refundedMinor += amount
      o.status = o.refundedMinor >= o.amountMinor ? 'refunded' : 'partially_refunded'
      const r: PayRefund = {
        id: String(7422000000000000000n + BigInt(refunds.length + 1)),
        refundNo: `RF74220000000000000${refunds.length + 1}`,
        paymentNo: o.paymentNo,
        subjectId: o.subjectId,
        bizType: 'manual',
        bizNo: `manual-${refunds.length + 1}`,
        amountMinor: amount,
        feeRefundMinor: 0,
        currency: o.currency,
        fxRateMicro: o.fxRateMicro,
        reason: body.reason ?? '',
        status: 'succeeded',
        channelRefundId: 'mockrf_mock',
        succeededAt: new Date().toISOString(),
        operatorId: 'manual',
        createdAt: new Date().toISOString(),
      }
      refunds = [...refunds, r]
      return r
    },
  },
])
