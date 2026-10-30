import { defineMock, mockError } from '@/framework/request/mock'
import type {
  PayAuthorization,
  PayCallback,
  PayChannel,
  PayCommissionRule,
  PayOrder,
  PayPayoutBatch,
  PayReconcileDiff,
  PayReconcileTask,
  PayRefund,
  PaySettlement,
} from './api'

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
    subject: '示例商品',
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

let auths: PayAuthorization[] = [
  {
    id: '7431', tenantId: '1', authNo: 'AUTH7431000000000000001', subjectId: 'shop-001',
    bizType: 'hotel', bizNo: 'B-1001', channelCode: 'mock', method: 'native', status: 'authorized',
    amountMinor: 50000, capturedMinor: 0, currency: 'CNY', fxRateMicro: 1000000,
    cardLast4: '4242', expiresAt: ago(-60 * 24), createdAt: ago(10),
  },
  {
    id: '7432', tenantId: '1', authNo: 'AUTH7431000000000000002', subjectId: 'shop-002',
    bizType: 'hotel', bizNo: 'B-1002', channelCode: 'mock', status: 'partial_captured',
    amountMinor: 30000, capturedMinor: 12000, currency: 'CNY', fxRateMicro: 1000000,
    cardLast4: '1881', expiresAt: ago(-60 * 24), createdAt: ago(20),
  },
]

let reconcileTasks: PayReconcileTask[] = [
  {
    id: '7441', tenantId: '1', subjectId: '', channelCode: 'mock', billDate: dayKey(ago(0)),
    status: 'open', totalLocalMinor: 41799, totalChannelMinor: 40000, diffCount: 2, source: 'manual',
    createdAt: ago(5), updatedAt: ago(5),
  },
]

const reconcileDiffs: PayReconcileDiff[] = [
  {
    id: '7442', tenantId: '1', taskId: '7441', kind: 'short', paymentNo: payNo(1),
    localAmountMinor: 12900, channelAmountMinor: 12000, diffAmountMinor: 900,
    status: 'open', resolveNote: '', resolvedBy: '', resolvedAt: null,
  },
  {
    id: '7443', tenantId: '1', taskId: '7441', kind: 'missing', paymentNo: payNo(3),
    localAmountMinor: 13100, channelAmountMinor: 0, diffAmountMinor: 13100,
    status: 'open', resolveNote: '', resolvedBy: '', resolvedAt: null,
  },
]

let commissionRules: PayCommissionRule[] = [
  { id: '7451', tenantId: '1', subjectId: '', bizType: 'trade_group', rateBp: 60, fixedMinor: 0, currency: 'CNY', status: 'active' },
]

let settlements: PaySettlement[] = [
  {
    id: '7461', tenantId: '1', subjectId: '', periodStart: dayKey(ago(60 * 24 * 30)), periodEnd: dayKey(ago(60 * 24)),
    grossMinor: 50000, commissionMinor: 300, refundMinor: 0, feeMinor: 77, netMinor: 49623,
    orderCount: 3, currency: 'CNY', status: 'confirmed', createdAt: ago(60),
  },
  {
    id: '7462', tenantId: '1', subjectId: 'shop-001', periodStart: dayKey(ago(60 * 24 * 31)), periodEnd: dayKey(ago(60 * 24)),
    grossMinor: 20000, commissionMinor: 120, refundMinor: 500, feeMinor: 20, netMinor: 19360,
    orderCount: 1, currency: 'CNY', status: 'draft', createdAt: ago(70),
  },
]

let payouts: PayPayoutBatch[] = [
  {
    id: '7471', tenantId: '1', batchNo: 'PO7471000000000000001', status: 'paid', totalMinor: 19360,
    currency: 'CNY', settlementCount: 1, method: 'bank', note: '', receiptNo: 'RCPT-001', paidAt: ago(30), createdAt: ago(40),
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
      if (query.bizType) rows = rows.filter((o) => o.bizType === query.bizType)
      if (query.scene) rows = rows.filter((o) => o.scene === query.scene)
      if (query.failReason) rows = rows.filter((o) => (o as { failReason?: string }).failReason === query.failReason)
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
      if (query.ok !== undefined && query.ok !== '') rows = rows.filter((c) => String(c.ok) === String(query.ok))
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
      const all = String(query.all ?? '') === '1' || String(query.all ?? '') === 'true'
      const bizType = String(query.bizType ?? '')
      const scene = String(query.scene ?? '')
      const status = String(query.status ?? '')
      // facets 只按时间与渠道收敛，结构筛选只收窄主体统计（与后端一致）
      const scopeBase = orders.filter((o) => {
        const d = dayKey(o.createdAt)
        const inWindow = all || ((!start || d >= start) && (!end || d <= end))
        return inWindow && (!channelCode || o.channelCode === channelCode)
      })
      const scoped = scopeBase.filter(
        (o) => (!bizType || o.bizType === bizType) && (!scene || o.scene === scene) && (!status || o.status === status),
      )
      const uniq = (vals: (string | undefined)[]) =>
        [...new Set(vals.filter((v): v is string => !!v && v.trim() !== ''))].sort()
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
      // 与后端同口径：按请求窗口补齐日期（缺数据补 0）；all 画最近 182 天，没给窗口时退回最近 30 天
      const series: { date: string; orders: number; paid: number }[] = []
      const allStart = new Date(Date.now() - 181 * 86_400_000)
      const seriesStart = all ? dayKey(allStart.toISOString()) : start
      const cursor = seriesStart ? new Date(`${seriesStart}T12:00:00`) : new Date(Date.now() - 30 * 86_400_000)
      const stop = all ? new Date() : end ? new Date(`${end}T12:00:00`) : new Date()
      for (let i = 0; i < 182 && cursor <= stop; i++, cursor.setDate(cursor.getDate() + 1)) {
        const key = dayKey(cursor.toISOString())
        series.push(dayMap.get(key) ?? { date: key, orders: 0, paid: 0 })
      }
      const earliest = scoped.map((o) => dayKey(o.createdAt)).sort()[0]
      return {
        from: all ? (earliest ?? series[0]?.date ?? seriesStart) : (series[0]?.date ?? start),
        to: series[series.length - 1]?.date ?? end,
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
        failReasons: tally(scoped.filter((o) => o.status === 'failed').map((o) => (o as { failReason?: string }).failReason)),
        facets: {
          bizTypes: uniq(scopeBase.map((o) => o.bizType)),
          scenes: uniq(scopeBase.map((o) => o.scene)),
        },
      }
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/orders/:no',
    handler: ({ params }) => orders.find((x) => x.paymentNo === params.no) ?? mockError(404, 'order not found'),
  },
  {
    method: 'GET',
    url: '/plugins/pay/authorizations',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = auths.filter((a) => !kw || a.authNo.toLowerCase().includes(kw) || a.bizNo.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((a) => a.status === query.status)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/authorizations',
    handler: ({ body }) => {
      const amount = Number(body.amountMinor) || 0
      if (!body.channelCode || amount <= 0) mockError(400, 'channel and amount required')
      const n = auths.length + 1
      const a: PayAuthorization = {
        id: String(7431000000000000000n + BigInt(n)), tenantId: '1',
        authNo: `AUTH74310000000000000${n}`, subjectId: body.subjectId ?? '',
        bizType: body.bizType ?? 'manual', bizNo: body.bizNo ?? `manual-${Date.now()}`,
        channelCode: body.channelCode, method: body.method ?? '', status: 'authorized',
        amountMinor: amount, capturedMinor: 0, currency: body.currency ?? 'CNY', fxRateMicro: 1000000,
        cardLast4: body.cardLast4 ?? '', remark: body.remark ?? '',
        expiresAt: new Date(Date.now() + (Number(body.validDays) || 7) * 86_400_000).toISOString(),
        createdAt: new Date().toISOString(),
      }
      auths = [...auths, a]
      return a
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/authorizations/:no/capture',
    handler: ({ params, body }) => {
      const a = auths.find((x) => x.authNo === params.no) ?? mockError(404, 'authorization not found')
      if (a.status !== 'authorized' && a.status !== 'partial_captured') mockError(409, 'not capturable')
      const amount = Number(body.amountMinor) || 0
      if (amount <= 0 || amount > a.amountMinor - a.capturedMinor) mockError(409, 'capture exceeded')
      a.capturedMinor += amount
      a.status = a.capturedMinor >= a.amountMinor ? 'captured' : 'partial_captured'
      return a
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/authorizations/:no/void',
    handler: ({ params }) => {
      const a = auths.find((x) => x.authNo === params.no) ?? mockError(404, 'authorization not found')
      if (a.status !== 'authorized' && a.status !== 'partial_captured') mockError(409, 'not voidable')
      a.status = 'voided'
      a.voidedAt = new Date().toISOString()
      return a
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/reconcile/tasks',
    handler: ({ query }) => {
      let rows = reconcileTasks
      if (query.billDate) rows = rows.filter((t) => t.billDate === query.billDate)
      if (query.channelCode) rows = rows.filter((t) => t.channelCode === query.channelCode)
      if (query.status) rows = rows.filter((t) => t.status === query.status)
      return [...rows].reverse()
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/reconcile/tasks',
    handler: ({ body }) => {
      if (!body.channelCode || !body.billDate) mockError(400, 'channel and billDate required')
      const n = reconcileTasks.length + 1
      const t: PayReconcileTask = {
        id: String(7441000000000000000n + BigInt(n)), tenantId: '1', subjectId: body.subjectId ?? '',
        channelCode: body.channelCode, billDate: body.billDate, status: 'open',
        totalLocalMinor: 0, totalChannelMinor: (body.channelAmounts ?? []).reduce((s: number, e: { amountMinor?: number }) => s + (Number(e.amountMinor) || 0), 0),
        diffCount: 0, source: 'manual', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
      }
      reconcileTasks = [...reconcileTasks, t]
      return t
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/reconcile/tasks/:id/diffs',
    handler: ({ params }) => reconcileDiffs.filter((d) => d.taskId === params.id),
  },
  {
    method: 'POST',
    url: '/plugins/pay/reconcile/diffs/:id/resolve',
    handler: ({ params, body }) => {
      const d = reconcileDiffs.find((x) => x.id === params.id) ?? mockError(404, 'diff not found')
      if (d.status !== 'open') mockError(409, 'diff already handled')
      d.status = body.action === 'ignored' ? 'ignored' : 'resolved'
      d.resolveNote = String(body.note ?? '')
      d.resolvedBy = 'admin'
      d.resolvedAt = new Date().toISOString()
      const task = reconcileTasks.find((t) => t.id === d.taskId)
      if (task) {
        task.diffCount = reconcileDiffs.filter((x) => x.taskId === task.id && x.status === 'open').length
        if (task.diffCount === 0) task.status = 'closed'
      }
      return d
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/commission-rules',
    handler: () => commissionRules,
  },
  {
    method: 'POST',
    url: '/plugins/pay/commission-rules',
    handler: ({ body }) => {
      if (!body.bizType) mockError(400, 'bizType required')
      const existing = commissionRules.find((r) => r.subjectId === (body.subjectId ?? '') && r.bizType === body.bizType)
      if (existing) {
        existing.rateBp = Number(body.rateBp) || 0
        existing.fixedMinor = Number(body.fixedMinor) || 0
        existing.currency = body.currency ?? 'CNY'
        existing.status = body.status ?? 'active'
        return existing
      }
      const r: PayCommissionRule = {
        id: String(7451000000000000000n + BigInt(commissionRules.length + 1)), tenantId: '1',
        subjectId: body.subjectId ?? '', bizType: body.bizType, rateBp: Number(body.rateBp) || 0,
        fixedMinor: Number(body.fixedMinor) || 0, currency: body.currency ?? 'CNY', status: body.status ?? 'active',
      }
      commissionRules = [...commissionRules, r]
      return r
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/settlements',
    handler: ({ query }) => {
      let rows = settlements
      if (query.status) rows = rows.filter((s) => s.status === query.status)
      if (query.subjectId) rows = rows.filter((s) => s.subjectId === query.subjectId)
      return [...rows].reverse()
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/settlements/generate',
    handler: ({ body }) => {
      if (!body.periodStart || !body.periodEnd || body.periodEnd < body.periodStart) mockError(400, 'invalid period')
      const s: PaySettlement = {
        id: String(7461000000000000000n + BigInt(settlements.length + 1)), tenantId: '1',
        subjectId: body.subjectId ?? '', periodStart: body.periodStart, periodEnd: body.periodEnd,
        grossMinor: 0, commissionMinor: 0, refundMinor: 0, feeMinor: 0, netMinor: 0,
        orderCount: 0, currency: 'CNY', status: 'draft', createdAt: new Date().toISOString(),
      }
      settlements = [s, ...settlements]
      return [s]
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/settlements/:id/confirm',
    handler: ({ params }) => {
      const s = settlements.find((x) => x.id === params.id) ?? mockError(404, 'settlement not found')
      if (s.status !== 'draft') mockError(409, 'not a draft')
      s.status = 'confirmed'
      return s
    },
  },
  {
    method: 'GET',
    url: '/plugins/pay/payouts',
    handler: ({ query }) => {
      let rows = payouts
      if (query.status) rows = rows.filter((p) => p.status === query.status)
      return [...rows].reverse()
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/payouts',
    handler: ({ body }) => {
      const ids: string[] = Array.isArray(body.settlementIds) ? body.settlementIds.map(String) : []
      const picked = settlements.filter((s) => ids.includes(s.id))
      if (picked.length !== ids.length || picked.length === 0) mockError(400, 'settlements not found')
      if (picked.some((s) => s.status !== 'confirmed')) mockError(409, 'settlement not confirmed')
      const n = payouts.length + 1
      const p: PayPayoutBatch = {
        id: String(7471000000000000000n + BigInt(n)), tenantId: '1',
        batchNo: `PO74710000000000000${n}`, status: 'draft',
        totalMinor: picked.reduce((s, x) => s + x.netMinor, 0), currency: picked[0].currency,
        settlementCount: picked.length, method: body.method ?? '', note: body.note ?? '',
        receiptNo: '', paidAt: null, createdAt: new Date().toISOString(),
      }
      payouts = [p, ...payouts]
      return p
    },
  },
  {
    method: 'POST',
    url: '/plugins/pay/payouts/:id/mark-paid',
    handler: ({ params, body }) => {
      const p = payouts.find((x) => x.id === params.id) ?? mockError(404, 'payout not found')
      if (p.status !== 'draft') mockError(409, 'already paid')
      p.status = 'paid'
      p.receiptNo = String(body.receiptNo ?? '')
      p.paidAt = new Date().toISOString()
      return p
    },
  },
])
