import { defineMock, mockError } from '@/framework/request/mock'
import type { PayCallback, PayChannel, PayOrder, PayRefund } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const payNo = (n: number) => `PAY7420000000000000${String(n).padStart(2, '0')}`

const mkOrder = (n: number, p: Partial<PayOrder>): PayOrder => ({
  id: String(7420000000000000000n + BigInt(n)),
  paymentNo: payNo(n),
  bizType: 'trade_group',
  bizNo: `G-${n}`,
  subject: '示例订单',
  amountMinor: 12800 + n * 100,
  refundedMinor: 0,
  currency: 'CNY',
  channel: 'mock',
  status: 'pending',
  createdAt: ago(60 * (n + 1)),
  ...p,
})

const orders: PayOrder[] = [
  mkOrder(1, { status: 'paid', channelTxnId: 'mock_a1', paidAt: ago(55) }),
  mkOrder(2, { status: 'pending', expireAt: ago(-30) }),
  mkOrder(3, { status: 'partially_refunded', refundedMinor: 500, channelTxnId: 'mock_a3', paidAt: ago(160) }),
  mkOrder(4, { status: 'failed', failReason: 'user closed cashier' }),
  mkOrder(5, { status: 'closed' }),
  mkOrder(6, { status: 'refunded', refundedMinor: 9900, channelTxnId: 'mock_a6', paidAt: ago(300) }),
]

let channels: PayChannel[] = [
  { id: '1', code: 'mock', name: '模拟渠道', enabled: true, sort: 0, hasConfig: true, updatedAt: ago(60 * 24 * 30) },
]

const callbacks: PayCallback[] = orders
  .filter((o) => o.status === 'paid' || o.status === 'failed')
  .map((o, i) => ({
    id: String(7421000000000000000n + BigInt(i)),
    channel: 'mock',
    paymentNo: o.paymentNo,
    ok: true,
    result: o.status === 'paid' ? 'accepted' : 'ignored_state_pending',
    createdAt: o.paidAt ?? o.createdAt,
  }))

let refunds: PayRefund[] = [
  {
    id: '7422',
    refundNo: 'RF7422000000000000001',
    paymentNo: payNo(3),
    bizType: 'trade_refund',
    bizNo: 'R-1',
    amountMinor: 500,
    currency: 'CNY',
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
    url: '/plugins/payment/orders',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = orders.filter((o) => !kw || o.paymentNo.toLowerCase().includes(kw) || o.bizNo.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((o) => o.status === query.status)
      if (query.channel) rows = rows.filter((o) => o.channel === query.channel)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/payment/channels',
    handler: () => channels,
  },
  { method: 'GET', url: '/plugins/payment/providers', handler: () => ['mock'] },
  {
    method: 'GET',
    url: '/plugins/payment/callbacks',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = callbacks.filter((c) => !kw || c.paymentNo.toLowerCase().includes(kw))
      if (query.channel) rows = rows.filter((c) => c.channel === query.channel)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/payment/refunds',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = refunds.filter((r) => !kw || r.refundNo.toLowerCase().includes(kw) || r.paymentNo.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((r) => r.status === query.status)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/payment/orders/:no/close',
    handler: ({ params }) => {
      const o = orders.find((x) => x.paymentNo === params.no) ?? mockError(404, 'order not found')
      if (o.status !== 'pending') mockError(409, 'not closable')
      o.status = 'closed'
      return o
    },
  },
  {
    method: 'POST',
    url: '/plugins/payment/channels',
    handler: ({ body }) => {
      const ch: PayChannel = {
        id: String(channels.length + 1),
        code: body.code,
        name: body.name,
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
    method: 'PUT',
    url: '/plugins/payment/channels/:id',
    handler: ({ params, body }) => {
      const ch = channels.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      ch.name = body.name
      ch.enabled = !!body.enabled
      ch.sort = body.sort ?? 0
      ch.hasConfig = Object.keys(body.config ?? {}).length > 0
      ch.updatedAt = new Date().toISOString()
      return ch
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/payment/channels/:id',
    handler: ({ params }) => {
      channels = channels.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'POST',
    url: '/plugins/payment/refunds',
    handler: ({ body }) => {
      const o = orders.find((x) => x.paymentNo === body.paymentNo) ?? mockError(404, 'order not found')
      if (!['paid', 'partially_refunded'].includes(o.status)) mockError(409, 'not refundable')
      const amount = Number(body.amountMinor) || 0
      if (amount <= 0 || amount > o.amountMinor - o.refundedMinor) mockError(409, 'refund exceeded')
      o.refundedMinor += amount
      o.status = o.refundedMinor >= o.amountMinor ? 'refunded' : 'partially_refunded'
      const r: PayRefund = {
        id: String(7422000000000000000n + BigInt(refunds.length + 1)),
        refundNo: `RF74220000000000000${refunds.length + 1}`,
        paymentNo: o.paymentNo,
        bizType: 'manual',
        bizNo: `manual-${refunds.length + 1}`,
        amountMinor: amount,
        currency: o.currency,
        reason: body.reason ?? '',
        status: 'succeeded',
        channelRefundId: 'mockrf_mock',
        succeededAt: new Date().toISOString(),
        operator: 'manual',
        createdAt: new Date().toISOString(),
      }
      refunds = [...refunds, r]
      return r
    },
  },
])
