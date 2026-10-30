import { defineMock, mockError } from '@/framework/request/mock'
import type { PayCallback, PayChannel, PayOrder, PayRefund } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
/** 本地日号 YYYY-MM-DD：与后端按应用时区分桶保持一致 */
const dayKey = (iso: string) => {
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const payNo = (n: number) => `PAY7420000000000000${String(n).padStart(2, '0')}`

const SETTLED = new Set(['succeeded', 'partially_refunded', 'refunded', 'refunding'])

const mkOrder = (n: number, p: Partial<PayOrder>): PayOrder => {
  // 按天错开创建时间，让 /orders/stats 的按天趋势有多于一个点
  const createdAt = ago(24 * 60 * (7 - n))
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
    createdAt,
  }
  // 先收成 PayOrder 再叠加 Partial：直接展开 Partial 会让必填字段在类型上变成可空
  const o = { ...base, ...p }
  if (o.paidAt === undefined && SETTLED.has(o.status)) o.paidAt = ago(24 * 60 * (7 - n) - 40)
  return o
}

const orders: PayOrder[] = [
  mkOrder(1, { status: 'succeeded', channelTxnId: 'mock_a1', feeMinor: 77 }),
  mkOrder(2, { status: 'pending', expireAt: ago(24 * 60 * (7 - 2) - 60 * 23) }),
  mkOrder(3, { status: 'partially_refunded', refundedMinor: 500, feeMinor: 77, channelTxnId: 'mock_a3' }),
  mkOrder(4, { status: 'failed', failReason: 'user closed cashier' }),
  mkOrder(5, { status: 'closed' }),
  mkOrder(6, { status: 'refunded', refundedMinor: 9900, channelTxnId: 'mock_a6' }),
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
  {
    method: 'GET',
    url: '/plugins/pay/providers',
    // 与后端 ProviderInfos 同形：预览里也要看到真实渠道的凭据表单。
    handler: () => [
      { name: 'mock', fields: [] },
      {
        name: 'alipay',
        fields: [
          { key: 'appId', type: 'text', required: true, secret: false, placeholder: '2021000000000000' },
          { key: 'privateKey', type: 'password', required: true, secret: true, placeholder: '应用私钥（PKCS8，PEM 或纯 Base64）' },
          { key: 'alipayPublicKey', type: 'password', required: true, secret: true, placeholder: '支付宝公钥或公钥证书（PEM / 纯 Base64）' },
          { key: 'sellerId', type: 'text', required: false, secret: false, placeholder: '可选：校验回调的 seller_id' },
          { key: 'gateway', type: 'url', required: false, secret: false, placeholder: 'https://openapi.alipay.com/gateway.do' },
        ],
      },
      {
        name: 'wechat',
        fields: [
          { key: 'appId', type: 'text', required: true, secret: false, placeholder: 'wx0000000000000000' },
          { key: 'mchId', type: 'text', required: true, secret: false, placeholder: '1900000001' },
          { key: 'serialNo', type: 'text', required: true, secret: false, placeholder: '商户 API 证书序列号' },
          { key: 'privateKey', type: 'password', required: true, secret: true, placeholder: 'apiclient_key.pem（PEM）' },
          { key: 'apiV3Key', type: 'password', required: true, secret: true, placeholder: 'APIv3 密钥（32 位）' },
          { key: 'platformPublicKey', type: 'password', required: true, secret: true, placeholder: '微信支付公钥（PEM）' },
          { key: 'platformPublicKeyId', type: 'text', required: true, secret: false, placeholder: 'PUB_KEY_ID_0000000000000000' },
        ],
      },
      {
        name: 'epay',
        fields: [
          { key: 'gateway', type: 'url', required: true, secret: false, placeholder: 'https://pay.example.com' },
          { key: 'pid', type: 'text', required: true, secret: false, placeholder: '商户 ID' },
          { key: 'key', type: 'password', required: true, secret: true, placeholder: '商户密钥' },
          { key: 'payType', type: 'select', required: true, secret: false, options: ['alipay', 'wxpay', 'qqpay'] },
        ],
      },
    ],
  },
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
  {
    // 与真实 /orders/stats 同口径：金额按币种分列，趋势只给笔数，绝不跨币种相加
    method: 'GET',
    url: '/plugins/pay/orders/stats',
    handler: ({ query }) => {
      const settled = SETTLED
      const start = String(query.startDate ?? '')
      const end = String(query.endDate ?? '')
      const channelCode = String(query.channelCode ?? '')
      const scoped = orders.filter((o) => {
        const d = dayKey(o.createdAt)
        return (!start || d >= start) && (!end || d <= end) && (!channelCode || o.channelCode === channelCode)
      })
      const tally = (keys: (string | undefined)[]) => {
        const m = new Map<string, number>()
        keys.forEach((k) => { const key = k && k.trim() ? k : '-'; m.set(key, (m.get(key) ?? 0) + 1) })
        return [...m].map(([name, value]) => ({ name, value })).sort((a, b) => b.value - a.value || a.name.localeCompare(b.name)).slice(0, 8)
      }
      const curs = new Map<string, { orders: number; paid: number; gmv: number; refunded: number; fee: number }>()
      const chs = new Map<string, { channel: string; currency: string; paid: number; gmv: number }>()
      const dayMap = new Map<string, { date: string; orders: number; paid: number }>()
      scoped.forEach((o) => {
        const c = curs.get(o.currency) ?? { orders: 0, paid: 0, gmv: 0, refunded: 0, fee: 0 }
        c.orders += 1
        const key = dayKey(o.createdAt)
        const dp = dayMap.get(key) ?? { date: key, orders: 0, paid: 0 }
        dp.orders += 1
        dayMap.set(key, dp)
        if (settled.has(o.status)) {
          c.paid += 1
          c.gmv += o.amountMinor
          dp.paid += 1
          const ck = `${o.channelCode}|${o.currency}`
          const ch = chs.get(ck) ?? { channel: o.channelCode, currency: o.currency, paid: 0, gmv: 0 }
          ch.paid += 1
          ch.gmv += o.amountMinor
          chs.set(ck, ch)
        }
        c.refunded += o.refundedMinor ?? 0
        c.fee += o.feeMinor ?? 0
        curs.set(o.currency, c)
      })
      // 与后端同口径：按请求窗口补齐日期（缺数据补 0）；没给窗口时退回最近 30 天
      const series: { date: string; orders: number; paid: number }[] = []
      const cursor = start ? new Date(`${start}T12:00:00`) : new Date(Date.now() - 30 * 86_400_000)
      const stop = end ? new Date(`${end}T12:00:00`) : new Date()
      for (let i = 0; i < 180 && cursor <= stop; i++, cursor.setDate(cursor.getDate() + 1)) {
        const key = dayKey(cursor.toISOString())
        series.push(dayMap.get(key) ?? { date: key, orders: 0, paid: 0 })
      }
      return {
        from: series[0]?.date ?? start, to: series[series.length - 1]?.date ?? end,
        orders: scoped.length,
        paid: scoped.filter((o) => settled.has(o.status)).length,
        sampled: false,
        days: series,
        byCurrency: [...curs.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([currency, c]) => ({
          currency,
          orders: c.orders,
          paid: c.paid,
          gmvMinor: c.gmv,
          refundedMinor: c.refunded,
          feeMinor: c.fee,
          netMinor: c.gmv - c.refunded,
          refundRate: c.gmv ? Math.round((c.refunded / c.gmv) * 1000) / 10 : 0,
        })),
        byChannel: [...chs.values()].sort((a, b) => b.gmv - a.gmv || a.channel.localeCompare(b.channel))
          .map((c) => ({ channel: c.channel, currency: c.currency, paid: c.paid, gmvMinor: c.gmv })),
        byStatus: tally(scoped.map((o) => o.status)),
        byScene: tally(scoped.map((o) => o.scene)),
        byMethod: tally(scoped.map((o) => o.method)),
        byBizType: tally(scoped.map((o) => o.bizType)),
        failReasons: tally(scoped.filter((o) => !settled.has(o.status)).map((o) => (o as { failReason?: string }).failReason)),
      }
    },
  },
])
