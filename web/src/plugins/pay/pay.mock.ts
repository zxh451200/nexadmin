import { defineMock, mockError } from '@/framework/request/mock'
import type {
  OutboxCounts,
  PayAuthorization,
  PayCallback,
  PayChannel,
  PayCommissionRule,
  PayOrder,
  PayOutboxEvent,
  PayPayoutBatch,
  PayPayoutItem,
  PayProviderCapability,
  PayReconcileDiff,
  PayReconcileTask,
  PayRefund,
  PaySettlement,
  ProviderCapabilities,
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
  // 四个渠道类型的故意差异（见下方 capabilities）：预览里也要真的能验到「禁用」
  {
    id: '2',
    code: 'ali01',
    name: '支付宝-主站',
    provider: 'alipay',
    method: 'page',
    feeRateBp: 60,
    enabled: true,
    sort: 10,
    hasConfig: true,
    updatedAt: ago(60 * 24 * 12),
  },
  {
    id: '3',
    code: 'wx01',
    name: '微信支付-服务商',
    provider: 'wechat',
    method: 'jsapi',
    feeRateBp: 60,
    enabled: true,
    sort: 20,
    hasConfig: true,
    updatedAt: ago(60 * 24 * 6),
  },
  {
    id: '4',
    code: 'epay01',
    name: '易支付-聚合',
    provider: 'epay',
    method: 'wap',
    feeRateBp: 120,
    enabled: true,
    sort: 30,
    hasConfig: true,
    updatedAt: ago(60 * 24 * 2),
  },
]

/**
 * 渠道能力矩阵（第二份契约）：四个 provider 故意各不相同——
 * epay 只有 CNY、四个动作全不支持（用来验证退款补查/预授权/对账/代付的禁用与原因文案）；
 * wechat 不支持预授权且只收 CNY/HKD；alipay 全能力含多币种；mock 只支持退款查询。
 */
const PROVIDER_CAPABILITIES: Record<string, ProviderCapabilities> = {
  mock: { currencies: ['CNY'], methods: ['native', 'page', 'wap'], refundQuery: true, auth: true, bill: false, payout: false },
  alipay: {
    currencies: ['CNY', 'USD', 'EUR', 'HKD'],
    methods: ['native', 'page', 'wap', 'jsapi', 'app', 'micropay'],
    refundQuery: true,
    auth: true,
    bill: true,
    payout: true,
  },
  wechat: {
    currencies: ['CNY', 'HKD'],
    methods: ['native', 'jsapi', 'app', 'h5'],
    refundQuery: true,
    auth: false,
    bill: true,
    payout: true,
  },
  epay: { currencies: ['CNY'], methods: ['page', 'wap'], refundQuery: false, auth: false, bill: false, payout: false },
}

const capabilitiesOf = (provider: string): ProviderCapabilities =>
  PROVIDER_CAPABILITIES[provider] ?? {
    currencies: ['CNY'],
    methods: [],
    refundQuery: false,
    auth: false,
    bill: false,
    payout: false,
  }

/** provider 由渠道编码反查：mock 里也要按「该渠道不支持」拒绝，和后端口径一致。 */
const providerOfCode = (channelCode?: string): string =>
  channels.find((c) => c.code === channelCode)?.provider ?? ''

const capabilitiesOfCode = (channelCode?: string): ProviderCapabilities => capabilitiesOf(providerOfCode(channelCode))

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
    channelCode: 'mock',
    channelRefundId: 'mockrf_1',
    succeededAt: ago(30),
    // 补查过一次才落到成功：留痕要能看到
    syncAttempts: 1,
    lastSyncAt: ago(28),
    createdAt: ago(31),
  },
  {
    id: '7423',
    refundNo: 'RF7422000000000000002',
    paymentNo: payNo(6),
    subjectId: 'shop-001',
    bizType: 'trade_refund',
    bizNo: 'R-2',
    amountMinor: 9900,
    feeRefundMinor: 60,
    currency: 'CNY',
    fxRateMicro: 1000000,
    reason: '整机退货，回调迟迟未到',
    status: 'pending',
    channelCode: 'ali01',
    channelRefundId: 'ali_rf_9900',
    // 卡住的退款：查过 3 次仍 pending —— 管理端要能看出「查过几次、最后一次何时」
    syncAttempts: 3,
    lastSyncAt: ago(12),
    createdAt: ago(180),
  },
  {
    id: '7424',
    refundNo: 'RF7422000000000000003',
    paymentNo: payNo(1),
    subjectId: 'shop-002',
    bizType: 'trade_refund',
    bizNo: 'R-3',
    amountMinor: 1200,
    feeRefundMinor: 0,
    currency: 'CNY',
    fxRateMicro: 1000000,
    reason: '重复支付退回',
    status: 'pending',
    channelCode: 'wx01',
    // 从未向渠道补查过
    syncAttempts: 0,
    createdAt: ago(45),
  },
  {
    id: '7425',
    refundNo: 'RF7422000000000000004',
    paymentNo: payNo(3),
    subjectId: 'shop-003',
    bizType: 'trade_refund',
    bizNo: 'R-4',
    amountMinor: 300,
    feeRefundMinor: 0,
    currency: 'CNY',
    fxRateMicro: 1000000,
    reason: '聚合渠道退款，回调丢失',
    status: 'pending',
    channelCode: 'epay01',
    // epay 在能力矩阵里 refundQuery=false：这一行的补查按钮必须置灰并说明原因
    syncAttempts: 0,
    createdAt: ago(20),
  },
  {
    id: '7426',
    refundNo: 'RF7422000000000000005',
    paymentNo: payNo(6),
    subjectId: 'shop-001',
    bizType: 'trade_refund',
    bizNo: 'R-5',
    amountMinor: 800,
    feeRefundMinor: 0,
    currency: 'CNY',
    fxRateMicro: 1000000,
    status: 'failed',
    channelCode: 'ali01',
    failReason: '渠道拒绝：超出可退余额',
    syncAttempts: 2,
    lastSyncAt: ago(6),
    createdAt: ago(200),
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
  {
    // 占位行：已按业务键抢到冻结权、渠道结论未回（authorizing），由后台补查收敛
    id: '7433', tenantId: '1', authNo: 'AUTH7431000000000000003', subjectId: 'shop-003',
    bizType: 'hotel', bizNo: 'B-1003', channelCode: 'ali01', method: 'native', status: 'authorizing',
    amountMinor: 20000, capturedMinor: 0, currency: 'CNY', fxRateMicro: 1000000,
    payerId: '2088101234567890', expiresAt: ago(-60 * 24), createdAt: ago(2),
  },
  {
    // 渠道明确拒绝冻结：钱没冻住，业务可换业务单号重试
    id: '7434', tenantId: '1', authNo: 'AUTH7431000000000000004', subjectId: 'shop-004',
    bizType: 'hotel', bizNo: 'B-1004', channelCode: 'ali01', method: 'native', status: 'failed',
    amountMinor: 15000, capturedMinor: 0, currency: 'CNY', fxRateMicro: 1000000,
    failReason: 'alipay: 商户未签约资金授权产品', expiresAt: ago(-60 * 24), createdAt: ago(3),
  },
]

let reconcileTasks: PayReconcileTask[] = [
  {
    id: '7441', tenantId: '1', subjectId: '', channelCode: 'mock', billDate: dayKey(ago(0)),
    status: 'open', totalLocalMinor: 41799, totalChannelMinor: 40000, diffCount: 2, source: 'manual',
    billDigest: '', billRows: 0, failReason: '',
    createdAt: ago(5), updatedAt: ago(5),
  },
  {
    id: '7444', tenantId: '1', subjectId: '', channelCode: 'ali01', billDate: dayKey(ago(24 * 60)),
    status: 'closed', totalLocalMinor: 288000, totalChannelMinor: 288000, diffCount: 0, source: 'channel',
    // 渠道账单取数成功：行数 + 摘要都要能看到
    billDigest: '3f2a6c1b9d0e4a75b8c1d2e3f40516a7b8c9d0e1f2031425364758697a8b9c0d', billRows: 128, failReason: '',
    createdAt: ago(60 * 20), updatedAt: ago(60 * 20),
  },
  {
    id: '7445', tenantId: '1', subjectId: '', channelCode: 'wx01', billDate: dayKey(ago(24 * 60)),
    status: 'open', totalLocalMinor: 0, totalChannelMinor: 0, diffCount: 0, source: 'channel',
    // 失败的对账：0 差异不等于「对平了」，必须带 failReason 呈现
    billDigest: '', billRows: 0, failReason: '渠道账单下载失败：downloading（账单生成中，请稍后重试）',
    createdAt: ago(90), updatedAt: ago(90),
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
  {
    // 走渠道代付成功的批次：有渠道编码与渠道批次号，没有人工回执
    id: '7472', tenantId: '1', batchNo: 'PO7471000000000000002', status: 'paid', totalMinor: 49623,
    currency: 'CNY', settlementCount: 1, method: 'balance', note: '', receiptNo: '',
    channelCode: 'ali01', channelBatchNo: 'ALIPO_20261030_000777', paidAt: ago(120), createdAt: ago(150),
  },
  {
    // 待打款批次：两条路径（渠道代付 / 人工回执）都能试
    id: '7473', tenantId: '1', batchNo: 'PO7471000000000000003', status: 'draft', totalMinor: 12800,
    currency: 'CNY', settlementCount: 1, method: '', note: '', receiptNo: '', paidAt: null, createdAt: ago(20),
  },
  {
    // 一笔成功一笔失败：后端「一笔失败不拖垮整批」的口径要在预览里看得见
    id: '7474', tenantId: '1', batchNo: 'PO7471000000000000004', status: 'partial', totalMinor: 27360,
    currency: 'CNY', settlementCount: 2, method: 'balance', note: '', receiptNo: '',
    channelCode: 'ali01', channelBatchNo: 'ALIPO_20261030_000778',
    failReason: '1 笔失败：结算单缺少收款方标识，无法代付', createdAt: ago(10),
  },
  {
    // 渠道已受理、终局未回：停在 initiated，由后台补查收敛
    id: '7475', tenantId: '1', batchNo: 'PO7471000000000000005', status: 'draft', totalMinor: 12800,
    currency: 'CNY', settlementCount: 1, method: 'balance', note: '', receiptNo: '',
    channelCode: 'ali01', channelBatchNo: 'ALIPO_20261030_000779', createdAt: ago(4),
  },
]

/**
 * 逐笔代付结论（第二份契约）：批次 partial 时必须能下钻到「哪几笔失败、为什么失败」。
 * 7472 = 渠道代付全成功；7473 = 未发起；7474 = 一笔成功一笔失败（partial）；
 * 7475 = 一笔已发起待终局（initiated，由后台补查收敛，本地不猜）。
 */
const payoutItems: PayPayoutItem[] = [
  { id: '7481', batchId: '7472', settlementId: '7461', netMinor: 49623, receiverId: 'shop-001', status: 'success', channelNo: 'ALI_DT_7481' },
  { id: '7482', batchId: '7474', settlementId: '7462', netMinor: 19360, receiverId: 'shop-001', status: 'success', channelNo: 'ALI_DT_7482' },
  { id: '7483', batchId: '7474', settlementId: '7463', netMinor: 8000, receiverId: '', status: 'failed', failReason: '结算单缺少收款方标识，无法代付' },
  { id: '7484', batchId: '7475', settlementId: '7462', netMinor: 12800, receiverId: 'shop-002', status: 'initiated', channelNo: 'ALI_DT_7484' },
]

/**
 * 资金事件出站箱（第二份契约）：dead 若干行 + 一条载荷超长的行，
 * 用来验列表徽标、重投/丢弃动作与详情里的 JSON 不顶破布局。
 */
const longPayload = JSON.stringify(
  {
    eventId: 'EV-LONG-0001',
    eventName: 'pay.succeeded',
    tenantId: '1',
    occurredAt: ago(70),
    data: {
      paymentNo: payNo(1),
      bizType: 'trade_group',
      bizNo: 'G-1',
      amountMinor: 12900,
      currency: 'CNY',
      channelCode: 'ali01',
      // 故意塞长的元数据：验证 pre 里的长 URL/字段不会把弹窗顶破
      metadata: {
        receiptUrl: `https://invoice.example.com/${'very-long-path'.repeat(12)}/download.pdf`,
        items: Array.from({ length: 12 }, (_, i) => ({ sku: `SKU-${i}`, title: `示例商品 ${i}`, qty: i + 1, minor: 1000 + i })),
      },
    },
  },
  null,
  0,
)

const outbox: PayOutboxEvent[] = [
  {
    id: '7481', tenantId: '1', eventId: 'EV-PAY-0000000000000000001', eventName: 'pay.succeeded', status: 'sent',
    attempts: 0, lastError: '', nextRetryAt: ago(300), sentAt: ago(300), createdAt: ago(302),
    payload: JSON.stringify({ eventId: 'EV-PAY-0000000000000000001', data: { paymentNo: payNo(1), amountMinor: 12900 } }),
  },
  {
    id: '7482', tenantId: '1', eventId: 'EV-PAY-0000000000000000002', eventName: 'pay.closed', status: 'pending',
    attempts: 2, lastError: '总线缓冲满，第 2 次重投失败', nextRetryAt: ago(-3), createdAt: ago(28),
    payload: JSON.stringify({ eventId: 'EV-PAY-0000000000000000002', data: { paymentNo: payNo(5) } }),
  },
  {
    id: '7483', tenantId: '1', eventId: 'EV-PAY-0000000000000000003', eventName: 'pay.refunded', status: 'dead',
    attempts: 12, lastError: '进程内总线连续拒绝投递，已达重试上限', nextRetryAt: ago(15), deadAt: ago(14), createdAt: ago(120),
    payload: JSON.stringify({ eventId: 'EV-PAY-0000000000000000003', data: { refundNo: 'RF7422000000000000002', amountMinor: 9900 } }),
  },
  {
    id: '7484', tenantId: '1', eventId: 'EV-PAY-0000000000000000004', eventName: 'pay.auth.captured', status: 'dead',
    attempts: 12, lastError: '载荷不可解析: unexpected end of JSON input', nextRetryAt: ago(40), deadAt: ago(39), createdAt: ago(240),
    payload: '{"eventId":"EV-PAY-0000000000000000004","data":',
  },
  {
    id: '7485', tenantId: '1', eventId: 'EV-LONG-0001', eventName: 'pay.succeeded', status: 'dead',
    attempts: 12, lastError: '进程内总线连续拒绝投递，已达重试上限', nextRetryAt: ago(60), deadAt: ago(58), createdAt: ago(300),
    payload: longPayload,
  },
  {
    id: '7486', tenantId: '1', eventId: 'EV-PAY-0000000000000000006', eventName: 'pay.failed', status: 'discarded',
    attempts: 12, lastError: '人工丢弃: 下游已通过对账补偿，无需重放', nextRetryAt: ago(300), deadAt: ago(320), createdAt: ago(400),
    payload: JSON.stringify({ eventId: 'EV-PAY-0000000000000000006', data: { paymentNo: payNo(4), failReason: 'user closed cashier' } }),
  },
  {
    id: '7487', tenantId: '1', eventId: 'EV-PAY-0000000000000000007', eventName: 'pay.succeeded', status: 'pending',
    attempts: 0, lastError: '', nextRetryAt: ago(1), createdAt: ago(3),
    payload: JSON.stringify({ eventId: 'EV-PAY-0000000000000000007', data: { paymentNo: payNo(3), amountMinor: 13100 } }),
  },
]

const outboxCounts = (): OutboxCounts => ({
  pending: outbox.filter((o) => o.status === 'pending').length,
  dead: outbox.filter((o) => o.status === 'dead').length,
  sent: outbox.filter((o) => o.status === 'sent').length,
  discarded: outbox.filter((o) => o.status === 'discarded').length,
})

/** 列表与动作响应都不回载荷（与后端 outboxView(row, false) 同口径）。 */
const withoutPayload = (o: PayOutboxEvent): PayOutboxEvent => {
  const copy = { ...o }
  delete copy.payload
  return copy
}

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
    // 渠道能力矩阵（契约第 1 条）：四个 provider 的差异见 PROVIDER_CAPABILITIES
    url: '/plugins/pay/capabilities',
    handler: (): PayProviderCapability[] =>
      Object.keys(PROVIDER_CAPABILITIES)
        .sort()
        .map((provider) => ({ provider, capabilities: PROVIDER_CAPABILITIES[provider] })),
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
        if (body.method !== undefined) existing.method = String(body.method)
        if (body.config && Object.keys(body.config).length > 0) existing.hasConfig = true
        existing.updatedAt = new Date().toISOString()
        return existing
      }
      const ch: PayChannel = {
        id: String(channels.length + 1),
        code,
        name: body.name,
        provider: body.provider,
        method: body.method ? String(body.method) : undefined,
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
        channelCode: o.channelCode,
        channelRefundId: 'mockrf_mock',
        succeededAt: new Date().toISOString(),
        syncAttempts: 0,
        operatorId: 'manual',
        createdAt: new Date().toISOString(),
      }
      refunds = [...refunds, r]
      return r
    },
  },
  {
    // 主动向渠道查退款结果（契约第 7 条）：能力不支持的渠道按 400 拒绝，口径同后端
    method: 'POST',
    url: '/plugins/pay/refunds/:no/sync',
    handler: ({ params }) => {
      const r = refunds.find((x) => x.refundNo === params.no) ?? mockError(404, 'refund not found')
      const caps = capabilitiesOfCode(r.channelCode)
      if (!caps.refundQuery) mockError(400, 'channel does not support refund query', 'PAY_CAPABILITY_UNSUPPORTED')
      const now = new Date().toISOString()
      r.syncAttempts = (r.syncAttempts ?? 0) + 1
      r.lastSyncAt = now
      // 模拟渠道侧结论：第一次补查就把卡住的退款确认成功（真实渠道可能仍返回处理中）
      if (r.status === 'pending' && r.syncAttempts >= 2) {
        r.status = 'succeeded'
        r.succeededAt = now
        r.channelRefundId = r.channelRefundId || `q_${r.refundNo}`
      }
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
      // 前端禁用只是体验，后端（这里＝mock）仍要独立拒一次
      const caps = capabilitiesOfCode(String(body.channelCode))
      if (!caps.auth) mockError(400, 'channel does not support authorization', 'PAY_CAPABILITY_UNSUPPORTED')
      const currency = String(body.currency ?? 'CNY')
      const allowedCurrencies = caps.currencies.length ? caps.currencies : ['CNY']
      if (!allowedCurrencies.includes(currency)) mockError(400, `currency ${currency} not supported`)
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
      // 缺省 manual（向后兼容）；source=channel 走渠道账单，要求该渠道有账单能力
      const source: string = body.source === 'channel' ? 'channel' : 'manual'
      const caps = capabilitiesOfCode(String(body.channelCode))
      if (source === 'channel' && !caps.bill) {
        mockError(400, 'channel does not provide bills', 'PAY_CAPABILITY_UNSUPPORTED')
      }
      const amounts: { amountMinor?: number }[] = Array.isArray(body.channelAmounts) ? body.channelAmounts : []
      const n = reconcileTasks.length + 1
      const digest = source === 'channel' ? `${(n * 2654435761).toString(16).padStart(8, '0').repeat(8)}` : ''
      const t: PayReconcileTask = {
        id: String(7441000000000000000n + BigInt(n)), tenantId: '1', subjectId: body.subjectId ?? '',
        channelCode: body.channelCode, billDate: body.billDate, status: 'open',
        totalLocalMinor: 0,
        totalChannelMinor: source === 'channel' ? 0 : amounts.reduce((s, e) => s + (Number(e.amountMinor) || 0), 0),
        diffCount: 0, source,
        billDigest: digest,
        billRows: source === 'channel' ? 42 : 0,
        failReason: '',
        createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
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
  {
    // 走渠道代付（契约第 9 条）：只有能力矩阵支持 payout 的渠道可用，口径同后端
    method: 'POST',
    url: '/plugins/pay/payouts/:id/pay',
    handler: ({ params, body }) => {
      const p = payouts.find((x) => x.id === params.id) ?? mockError(404, 'payout not found')
      if (p.status !== 'draft') mockError(409, 'already paid')
      const channelCode = String(body.channelCode ?? '').trim()
      if (!channelCode) mockError(400, 'channelCode required')
      const caps = capabilitiesOfCode(channelCode)
      if (!caps.payout) mockError(400, 'channel does not support payout', 'PAY_CAPABILITY_UNSUPPORTED')
      // 逐笔发起：缺收款方标识的笔直接落 failed，其余落 success（与后端的「前置校验不过不摸渠道」同口径）
      const mine = payoutItems.filter((it) => it.batchId === p.id)
      const targets = mine.length ? mine : [{ id: `gen-${p.id}`, batchId: p.id, settlementId: '7461', netMinor: p.totalMinor, receiverId: 'shop-001', status: 'pending' as const }]
      let ok = 0
      let firstFail = ''
      for (const it of targets) {
        if (it.status === 'success') { ok++; continue }
        if (!it.receiverId) {
          it.status = 'failed'
          it.failReason = '结算单缺少收款方标识，无法代付'
          firstFail = firstFail || it.failReason
          continue
        }
        it.status = 'success'
        it.channelNo = `MOCK_DT_${it.id}`
        it.failReason = ''
        ok++
      }
      p.channelCode = channelCode
      p.channelBatchNo = p.channelBatchNo || `PO_${channelCode}_${p.batchNo.slice(-3)}`
      p.method = 'channel'
      p.status = ok === targets.length ? 'paid' : ok > 0 ? 'partial' : 'failed'
      p.failReason = p.status === 'paid' ? '' : `${targets.length - ok} 笔失败：${firstFail}`
      if (p.status === 'paid') p.paidAt = new Date().toISOString()
      return p
    },
  },
  {
    // 渠道自检（契约第 11 条）：故意留下 fail 与 skipped 两类，验证面板不会把「没查」画成「通过」
    method: 'POST',
    url: '/plugins/pay/channels/:code/probe',
    handler: ({ params }) => {
      const code = String(params.code ?? '')
      const provider = providerOfCode(code)
      const base = {
        channelCode: code,
        provider,
        checkedAt: new Date().toISOString(),
      }
      if (provider === 'wechat') {
        const checks = [
          { name: 'credentials', state: 'pass', detail: '必填凭据齐全' },
          { name: 'privateKey', state: 'pass', detail: '商户 API 私钥可解析' },
          { name: 'apiV3Key', state: 'pass', detail: 'APIv3 密钥长度正确' },
          { name: 'platformCerts', state: 'fail', detail: '平台证书下载失败：sign error(mchid=1900000101)' },
          { name: 'gateway', state: 'fail', detail: '签名被渠道拒绝（私钥/公钥或算法不对）' },
        ]
        return { ...base, checks, failed: 2, skipped: 0 }
      }
      if (provider === 'epay') {
        const checks = [
          { name: 'credentials', state: 'pass', detail: '必填凭据齐全' },
          { name: 'signing', state: 'pass', detail: 'MD5 签名可生成' },
          { name: 'gateway', state: 'skipped', detail: '未配置主动查询地址，无法验证凭据是否被上游接受' },
        ]
        return { ...base, checks, failed: 0, skipped: 1 }
      }
      const checks = [
        { name: 'credentials', state: 'pass', detail: '必填凭据齐全' },
        { name: 'gateway', state: 'skipped', detail: '模拟渠道没有外部网关' },
      ]
      return { ...base, checks, failed: 0, skipped: 1 }
    },
  },
  {
    // 逐笔代付结论（契约第 10 条）
    method: 'GET',
    url: '/plugins/pay/payouts/:id/items',
    handler: ({ params }) => payoutItems.filter((it) => it.batchId === params.id),
  },
  {
    // 出站箱分页列表（契约第 2 条）：列表一律不带载荷
    method: 'GET',
    url: '/plugins/pay/outbox',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = outbox.filter((o) => !kw || o.eventId.toLowerCase().includes(kw) || o.eventName.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((o) => o.status === query.status)
      return paginate([...rows].reverse().map(withoutPayload), query)
    },
  },
  {
    // 各状态行数（契约第 3 条）：静态段必须注册在 /outbox/:eventId 之前，同后端约定
    method: 'GET',
    url: '/plugins/pay/outbox/counts',
    handler: () => outboxCounts(),
  },
  {
    method: 'GET',
    url: '/plugins/pay/outbox/:eventId',
    handler: ({ params }) => outbox.find((o) => o.eventId === params.eventId) ?? mockError(404, 'outbox event not found'),
  },
  {
    // 人工重投（契约第 5 条）：只允许 dead → pending，其它状态 409
    method: 'POST',
    url: '/plugins/pay/outbox/:eventId/retry',
    handler: ({ params }) => {
      const row = outbox.find((o) => o.eventId === params.eventId) ?? mockError(404, 'outbox event not found')
      if (row.status !== 'dead') mockError(409, 'only a dead event can be retried')
      row.status = 'pending'
      row.attempts = 0
      row.lastError = ''
      row.deadAt = null
      row.nextRetryAt = new Date().toISOString()
      return withoutPayload(row)
    },
  },
  {
    // 人工丢弃（契约第 6 条）：note 进 lastError 留痕
    method: 'POST',
    url: '/plugins/pay/outbox/:eventId/discard',
    handler: ({ params, body }) => {
      const row = outbox.find((o) => o.eventId === params.eventId) ?? mockError(404, 'outbox event not found')
      if (row.status !== 'dead') mockError(409, 'only a dead event can be discarded')
      const note = String(body.note ?? '').trim()
      if (!note) mockError(400, 'note required')
      row.status = 'discarded'
      row.lastError = `人工丢弃: ${note}`.slice(0, 500)
      return withoutPayload(row)
    },
  },
])
