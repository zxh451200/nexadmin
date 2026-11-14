import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

// 状态机与字段名对应 02-openapi-pay-v1.yaml：OrderStatus / RefundStatus。
export type PayStatus =
  | 'pending'
  | 'succeeded'
  | 'failed'
  | 'closed'
  | 'refunding'
  | 'partially_refunded'
  | 'refunded'
export type RefundStatus = 'pending' | 'succeeded' | 'failed'

export interface PayOrder {
  id: string
  tenantId: string
  subjectType?: string
  subjectId?: string
  subject?: string
  paymentNo: string
  bizType: string
  bizNo: string
  channelCode: string
  method?: string
  scene?: string
  status: PayStatus
  amountMinor: number
  refundedMinor: number
  feeMinor: number
  currency: string
  fxRateMicro: number
  channelTxnId?: string
  gatewayStatus?: string
  failReason?: string
  payerId?: string
  operatorId?: string
  metadata?: Record<string, string>
  payUrl?: string
  payParams?: Record<string, string>
  paidAt?: string | null
  expireAt?: string | null
  closedAt?: string | null
  createdAt: string
}

export interface PayChannel {
  id: string
  code: string
  name: string
  provider: string
  method?: string
  scene?: string
  subjectId?: string
  feeRateBp: number
  notifyUrl?: string
  enabled: boolean
  sort: number
  hasConfig: boolean
  updatedAt: string
}

export interface PayChannelForm {
  code: string
  name: string
  provider: string
  method?: string
  scene?: string
  subjectId?: string
  feeRateBp?: number
  notifyUrl?: string
  enabled: boolean
  sort: number
  config?: Record<string, string>
}

/** 渠道凭据字段（后端 Provider.ConfigSchema）：前端据此后渲染表单，不要求商户手写 JSON。 */
export interface PayProviderField {
  key: string
  type: 'text' | 'password' | 'url' | 'select'
  required: boolean
  secret: boolean
  options?: string[]
  placeholder?: string
}

export interface PayProvider {
  name: string
  fields: PayProviderField[]
}

/**
 * 渠道能力矩阵（后端 Capabilities）：决定币种/支付方式下拉里哪些选项可用，
 * 以及退款查询、预授权、渠道账单、渠道代付四个动作在该渠道是否可用。
 * 前端据此禁用只是体验，服务端仍会独立判一次并拒绝。
 */
export interface ProviderCapabilities {
  currencies: string[]
  methods: string[]
  refundQuery: boolean
  auth: boolean
  bill: boolean
  payout: boolean
}

export interface PayProviderCapability {
  provider: string
  capabilities: ProviderCapabilities
}

/** 出站箱状态（后端 plg_pay_outbox.status）：dead=死信，discarded=人工确认丢弃。 */
export type OutboxStatus = 'pending' | 'sent' | 'dead' | 'discarded'

/** 资金事件出站箱行；载荷只在详情接口返回（列表不带，避免整页塞 JSON）。 */
export interface PayOutboxEvent {
  id: string
  tenantId: string
  eventId: string
  eventName: string
  status: OutboxStatus | string
  attempts: number
  lastError?: string
  nextRetryAt?: string | null
  sentAt?: string | null
  deadAt?: string | null
  createdAt: string
  payload?: string
}

export interface OutboxCounts {
  pending: number
  dead: number
  sent: number
  discarded: number
}

export interface PayCallback {
  id: string
  channelCode: string
  paymentNo: string
  ok: boolean
  result: string
  payload?: string
  createdAt: string
}

/** 重放回调的可行判定（与后端 replayableResult 同口径）：只有已验签且落账失败的。 */
export const callbackReplayable = (cb: PayCallback): boolean =>
  cb.ok && (cb.result === 'order_not_found' || cb.result === 'amount_mismatch' || cb.result.startsWith('ignored_state:'))

export function useReplayCallback() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<PayCallback>(`${BASE}/callbacks/${id}/replay`),
    onSuccess: invalidate,
  })
}

export interface PayRefund {
  id: string
  refundNo: string
  paymentNo: string
  subjectId?: string
  bizType: string
  bizNo: string
  status: RefundStatus
  amountMinor: number
  feeRefundMinor: number
  currency: string
  fxRateMicro: number
  channelRefundId?: string
  reason?: string
  failReason?: string
  operatorId?: string
  succeededAt?: string | null
  /**
   * 退款单所属渠道编码：补查按钮要按「该渠道是否支持查退款结果」禁用。
   * 后端 RefundView 目前不带这个字段，缺省时前端按支付单详情兜底解析。
   */
  channelCode?: string
  /** 已向渠道补查的次数（后端 syncAttempts）：卡住的退款要能看出「查过没有」 */
  syncAttempts?: number
  lastSyncAt?: string | null
  createdAt: string
}

// authorizing = 已占位、渠道冻结结论未回；failed = 渠道明确拒绝冻结（钱没冻住，可换单重试）。
export type AuthStatus =
  | 'authorizing'
  | 'authorized'
  | 'partial_captured'
  | 'captured'
  | 'voided'
  | 'expired'
  | 'failed'
export type ReconcileStatus = 'open' | 'closed'
export type DiffKind = 'long' | 'short' | 'missing'
export type DiffStatus = 'open' | 'resolved' | 'ignored'
export type SettlementStatus = 'draft' | 'confirmed' | 'paid'
// partial = 批次内有的笔成功、有的失败；failed = 全部失败。渠道代付才会出现这两种。
export type PayoutStatus = 'draft' | 'paid' | 'partial' | 'failed'
// 逐笔代付结论：initiated = 已向渠道发起、终局未回（由后台补查收敛）。
export type PayoutItemStatus = 'pending' | 'initiated' | 'success' | 'failed'

export interface PayAuthorization {
  id: string
  tenantId: string
  authNo: string
  subjectId?: string
  bizType: string
  bizNo: string
  channelCode: string
  method?: string
  paymentNo?: string
  status: AuthStatus
  amountMinor: number
  capturedMinor: number
  currency: string
  fxRateMicro: number
  channelAuthNo?: string
  /** 渠道侧买家标识（支付宝 buyer_id 等）；真实渠道冻结必需 */
  payerId?: string
  cardLast4?: string
  remark?: string
  /** 渠道拒绝冻结 / 撤销的原因 */
  failReason?: string
  expiresAt?: string | null
  voidedAt?: string | null
  createdAt: string
}

export interface PayAuthForm {
  bizType: string
  bizNo: string
  subjectId?: string
  channelCode: string
  method?: string
  amountMinor: number
  currency?: string
  /** 渠道侧买家标识（支付宝 buyer_id / buyer_logon_id）：真实渠道冻结必需 */
  payerId?: string
  cardLast4?: string
  validDays?: number
  remark?: string
}

export type ReconcileSource = 'manual' | 'channel'

export interface PayReconcileTask {
  id: string
  tenantId: string
  subjectId?: string
  channelCode: string
  billDate: string
  status: ReconcileStatus
  totalLocalMinor: number
  totalChannelMinor: number
  diffCount: number
  source: string
  /** 渠道账单原文的 SHA-256（账单本体不落库）；source=channel 时用于核对取数 */
  billDigest?: string
  /** 解析出的账单行数：区分「账单为空」与「解析失败/没取到数」 */
  billRows?: number
  /** 对账失败原因：失败的任务不能只呈现一张空差异表 */
  failReason?: string
  createdAt: string
  updatedAt: string
}

export interface PayReconcileDiff {
  id: string
  tenantId: string
  taskId: string
  kind: DiffKind
  paymentNo?: string
  refundNo?: string
  localAmountMinor: number
  channelAmountMinor: number
  diffAmountMinor: number
  status: DiffStatus
  resolveNote?: string
  resolvedBy?: string
  resolvedAt?: string | null
}

export interface ReconcileAmount {
  paymentNo: string
  amountMinor: number
}

export interface PayCommissionRule {
  id: string
  tenantId: string
  subjectId?: string
  bizType: string
  rateBp: number
  fixedMinor: number
  currency: string
  status: string
  createdAt?: string
  updatedAt?: string
}

export interface PaySettlement {
  id: string
  tenantId: string
  subjectId?: string
  periodStart: string
  periodEnd: string
  grossMinor: number
  commissionMinor: number
  refundMinor: number
  feeMinor: number
  netMinor: number
  orderCount: number
  currency: string
  status: SettlementStatus
  createdAt: string
}

export interface PayPayoutBatch {
  id: string
  tenantId: string
  batchNo: string
  status: PayoutStatus
  totalMinor: number
  currency: string
  settlementCount: number
  method?: string
  note?: string
  receiptNo?: string
  /** 走渠道代付时用的渠道实例（人工回执路径为空） */
  channelCode?: string
  channelBatchNo?: string
  failReason?: string
  paidAt?: string | null
  createdAt: string
}

/** 批次内逐笔代付结论：批次为 partial/failed 时必须能下钻到哪几笔失败、为什么失败。 */
export interface PayPayoutItem {
  id: string
  batchId: string
  settlementId: string
  netMinor: number
  receiverId?: string
  status: PayoutItemStatus
  channelNo?: string
  failReason?: string
}

/** 渠道自检：单项结论。skipped = 没查（缺可选配置），既不算通过也不算失败。 */
export type ProbeState = 'pass' | 'fail' | 'skipped'
export interface PayProbeCheck {
  name: 'credentials' | 'privateKey' | 'alipayPublicKey' | 'apiV3Key' | 'platformCerts' | 'signing' | 'gateway' | string
  state: ProbeState
  detail?: string
}
export interface PayChannelProbe {
  channelCode: string
  provider: string
  checks: PayProbeCheck[]
  failed: number
  skipped: number
  checkedAt: string
}

export interface OrderQuery extends PageQuery {
  /** 逗号分隔多值：?status=paid,refunded */
  status?: string
  /** 逗号分隔多值渠道 */
  channelCode?: string
  /** 统计下钻 chips 的语义是单主体，保持单值 */
  subjectId?: string
  /** 逗号分隔多值 */
  bizType?: string
  /** 逗号分隔多值 */
  scene?: string
  /** 逗号分隔多值 */
  failReason?: string
  /** 下单日期范围（含两端，yyyy-mm-dd，按应用时区）；后端对非法值显式报错 */
  createdFrom?: string
  createdTo?: string
}
export interface CallbackQuery extends PageQuery {
  channelCode?: string
  ok?: boolean
}
export interface RefundQuery extends PageQuery {
  status?: string
}
export interface AuthQuery extends PageQuery {
  status?: string
}
export interface OutboxQuery extends PageQuery {
  status?: string
}

const BASE = '/plugins/pay'
const KEY = ['plugin_pay'] as const

export function useOrders(query: OrderQuery) {
  return useQuery({
    queryKey: [...KEY, 'orders', query],
    queryFn: () => api.get<Page<PayOrder>>(`${BASE}/orders`, query),
    placeholderData: keepPreviousData,
  })
}

/**
 * 创建支付单导出任务：按当前筛选提交到导出中心，立即返回任务 ID；
 * 进度与下载见导出中心，创建失败由 http 拦截器统一弹出原因。
 */
export function createOrderExportJob(query: OrderQuery) {
  const filters: Record<string, unknown> = {
    status: query.status, channelCode: query.channelCode, subjectId: query.subjectId,
    bizType: query.bizType, scene: query.scene, failReason: query.failReason,
    createdFrom: query.createdFrom, createdTo: query.createdTo,
  }
  return api.post<{ id: string }>(`${BASE}/orders/export-async`, null, { params: filters })
}

export function useChannels() {
  return useQuery({
    queryKey: [...KEY, 'channels'],
    queryFn: () => api.get<PayChannel[]>(`${BASE}/channels`),
  })
}

export function useProviders() {
  return useQuery({
    queryKey: [...KEY, 'providers'],
    queryFn: () => api.get<PayProvider[]>(`${BASE}/providers`),
    staleTime: 5 * 60_000,
  })
}

/** 渠道能力矩阵：一次取回全部渠道类型，按 provider 索引后驱动表单禁用（契约第 1 条）。 */
export function useCapabilities() {
  return useQuery({
    queryKey: [...KEY, 'capabilities'],
    // silent：旧后端没有该接口时只让前端「不禁用」，不要每页弹一次错误 toast
    queryFn: () => api.get<PayProviderCapability[]>(`${BASE}/capabilities`, undefined, { silent: true }),
    staleTime: 5 * 60_000,
  })
}

export function useCallbacks(query: CallbackQuery) {
  return useQuery({
    queryKey: [...KEY, 'callbacks', query],
    queryFn: () => api.get<Page<PayCallback>>(`${BASE}/callbacks`, query),
    placeholderData: keepPreviousData,
  })
}

export function useRefunds(query: RefundQuery) {
  return useQuery({
    queryKey: [...KEY, 'refunds', query],
    queryFn: () => api.get<Page<PayRefund>>(`${BASE}/refunds`, query),
    placeholderData: keepPreviousData,
  })
}

export function useOrder(paymentNo: string | null) {
  return useQuery({
    queryKey: [...KEY, 'order', paymentNo],
    queryFn: () => api.get<PayOrder>(`${BASE}/orders/${paymentNo}`),
    enabled: !!paymentNo,
  })
}

export function useAuthorizations(query: AuthQuery) {
  return useQuery({
    queryKey: [...KEY, 'authorizations', query],
    queryFn: () => api.get<Page<PayAuthorization>>(`${BASE}/authorizations`, query),
    placeholderData: keepPreviousData,
  })
}

export function useReconcileTasks(query: { billDate?: string; channelCode?: string; status?: string }) {
  return useQuery({
    queryKey: [...KEY, 'reconcile-tasks', query],
    queryFn: () => api.get<PayReconcileTask[]>(`${BASE}/reconcile/tasks`, query),
    placeholderData: keepPreviousData,
  })
}

export function useReconcileDiffs(taskId: string | null) {
  return useQuery({
    queryKey: [...KEY, 'reconcile-diffs', taskId],
    queryFn: () => api.get<PayReconcileDiff[]>(`${BASE}/reconcile/tasks/${taskId}/diffs`),
    enabled: !!taskId,
  })
}

export function useCommissionRules() {
  return useQuery({
    queryKey: [...KEY, 'commission-rules'],
    queryFn: () => api.get<PayCommissionRule[]>(`${BASE}/commission-rules`),
  })
}

export function useSettlements(query: { subjectId?: string; status?: string }) {
  return useQuery({
    queryKey: [...KEY, 'settlements', query],
    queryFn: () => api.get<PaySettlement[]>(`${BASE}/settlements`, query),
    placeholderData: keepPreviousData,
  })
}

export function usePayouts(query: { status?: string }) {
  return useQuery({
    queryKey: [...KEY, 'payouts', query],
    queryFn: () => api.get<PayPayoutBatch[]>(`${BASE}/payouts`, query),
    placeholderData: keepPreviousData,
  })
}

/** 批次内逐笔代付结论（契约第 10 条）：只在打开批次详情时取。 */
export function usePayoutItems(batchId?: string) {
  return useQuery({
    queryKey: [...KEY, 'payout-items', batchId ?? ''],
    queryFn: () => api.get<PayPayoutItem[]>(`${BASE}/payouts/${batchId}/items`),
    enabled: !!batchId,
  })
}

/** 出站箱分页列表（契约第 2 条）：列表不带载荷，载荷走详情接口。 */
export function useOutboxEvents(query: OutboxQuery) {
  return useQuery({
    queryKey: [...KEY, 'outbox', query],
    queryFn: () => api.get<Page<PayOutboxEvent>>(`${BASE}/outbox`, query),
    placeholderData: keepPreviousData,
  })
}

/** 出站箱各状态行数（契约第 3 条）：页签上的 pending/dead 告警徽标。 */
export function useOutboxCounts() {
  return useQuery({
    queryKey: [...KEY, 'outbox-counts'],
    // silent：徽标是锦上添花，无权限/旧后端时静默降级为不显示数字
    queryFn: () => api.get<OutboxCounts>(`${BASE}/outbox/counts`, undefined, { silent: true }),
    placeholderData: keepPreviousData,
  })
}

/** 单条出站箱详情（契约第 4 条）：带完整事件载荷，供 JSON 查看。 */
export function useOutboxEvent(eventId: string | null) {
  return useQuery({
    queryKey: [...KEY, 'outbox-event', eventId],
    queryFn: () => api.get<PayOutboxEvent>(`${BASE}/outbox/${eventId}`),
    enabled: !!eventId,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useCloseOrder() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (paymentNo: string) => api.post<PayOrder>(`${BASE}/orders/${paymentNo}/close`),
    onSuccess: invalidate,
  })
}

export function useSyncOrder() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (paymentNo: string) => api.post<PayOrder>(`${BASE}/orders/${paymentNo}/sync`),
    onSuccess: invalidate,
  })
}

export function useSaveChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: PayChannelForm) => api.post<PayChannel>(`${BASE}/channels`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/channels/${id}`),
    onSuccess: invalidate,
  })
}

/**
 * 批量删除渠道：DELETE /plugins/pay/channels?ids= —— 与单条同口径，没有额外护栏，
 * 越权 / 不存在的 id 不计入实删数，所以 deleted 可能少于提交条数。
 * 路径写死成字面量：scripts/contract_check 靠静态解析核对前后端契约。
 */
export function useDeleteChannelsBatch() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/plugins/pay/channels', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

export function useCreateRefund() {
  const invalidate = useInvalidate()
  return useMutation({
    // idempotencyKey 必传：退款是真实出款，超时/失败重试必须复用同一个键，
    // 否则每次重试都是新键，渠道侧各拿各的 out_request_no，第一笔已到达时第二笔就重复出款。
    mutationFn: ({ data, idempotencyKey }: { data: { paymentNo: string; amountMinor: number; reason: string }; idempotencyKey: string }) =>
      api.post<PayRefund>(`${BASE}/refunds`, data, { headers: { 'Idempotency-Key': idempotencyKey } }),
    onSuccess: invalidate,
  })
}

/** 主动向渠道查退款结果（契约第 7 条）：回调丢失时的补偿路径，返回最新退款单。 */
export function useSyncRefund() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (refundNo: string) => api.post<PayRefund>(`${BASE}/refunds/${refundNo}/sync`),
    onSuccess: invalidate,
  })
}

export function useCreateAuth() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: PayAuthForm) => api.post<PayAuthorization>(`${BASE}/authorizations`, data),
    onSuccess: invalidate,
  })
}

export function useCaptureAuth() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (v: { authNo: string; amountMinor: number; bizType: string; bizNo: string }) =>
      api.post<PayAuthorization>(`${BASE}/authorizations/${v.authNo}/capture`, {
        amountMinor: v.amountMinor,
        bizType: v.bizType,
        bizNo: v.bizNo,
      }),
    onSuccess: invalidate,
  })
}

export function useVoidAuth() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (authNo: string) => api.post<PayAuthorization>(`${BASE}/authorizations/${authNo}/void`),
    onSuccess: invalidate,
  })
}

export function useRunReconcileTask() {
  const invalidate = useInvalidate()
  return useMutation({
    // source 缺省 manual（向后兼容）；channel 由后端下载渠道账单，此时不需要 channelAmounts。
    mutationFn: (data: {
      channelCode: string
      billDate: string
      subjectId?: string
      source?: ReconcileSource
      channelAmounts?: ReconcileAmount[]
    }) => api.post<PayReconcileTask>(`${BASE}/reconcile/tasks`, data),
    onSuccess: invalidate,
  })
}

export function useResolveDiff() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (v: { id: string; action: 'resolved' | 'ignored'; note: string }) =>
      api.post<PayReconcileDiff>(`${BASE}/reconcile/diffs/${v.id}/resolve`, { action: v.action, note: v.note }),
    onSuccess: invalidate,
  })
}

export function useSaveCommissionRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: Omit<PayCommissionRule, 'id' | 'tenantId' | 'createdAt' | 'updatedAt'>) =>
      api.post<PayCommissionRule>(`${BASE}/commission-rules`, data),
    onSuccess: invalidate,
  })
}

export function useGenerateSettlements() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { subjectId?: string; periodStart: string; periodEnd: string }) =>
      api.post<PaySettlement[]>(`${BASE}/settlements/generate`, data),
    onSuccess: invalidate,
  })
}

export function useConfirmSettlement() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<PaySettlement>(`${BASE}/settlements/${id}/confirm`),
    onSuccess: invalidate,
  })
}

export function useCreatePayout() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { settlementIds: string[]; method?: string; note?: string }) =>
      api.post<PayPayoutBatch>(`${BASE}/payouts`, data),
    onSuccess: invalidate,
  })
}

export function useMarkPayoutPaid() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (v: { id: string; receiptNo: string }) =>
      api.post<PayPayoutBatch>(`${BASE}/payouts/${v.id}/mark-paid`, { receiptNo: v.receiptNo }),
    onSuccess: invalidate,
  })
}

/** 渠道凭据与连通性自检（契约第 11 条）：只读，不下单不退款不打款。 */
export function useProbeChannel() {
  return useMutation({
    mutationFn: (code: string) => api.post<PayChannelProbe>(`${BASE}/channels/${code}/probe`),
  })
}

/** 走渠道代付（契约第 9 条）：由渠道真实出款并回传批次号，区别于人工回填回执。 */
export function usePayPayoutViaChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (v: { id: string; channelCode: string }) =>
      api.post<PayPayoutBatch>(`${BASE}/payouts/${v.id}/pay`, { channelCode: v.channelCode }),
    onSuccess: invalidate,
  })
}

/** 人工重投一条死信（契约第 5 条）：仅 dead → pending。 */
export function useRetryOutbox() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (eventId: string) => api.post<PayOutboxEvent>(`${BASE}/outbox/${eventId}/retry`),
    onSuccess: invalidate,
  })
}

/** 人工确认丢弃一条死信（契约第 6 条）：note 必填，后端写进 lastError 留痕。 */
export function useDiscardOutbox() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (v: { eventId: string; note: string }) =>
      api.post<PayOutboxEvent>(`${BASE}/outbox/${v.eventId}/discard`, { note: v.note }),
    onSuccess: invalidate,
  })
}

/** 支付统计（与后端 /plugins/pay/orders/stats 一一对应）。
 *  金额一律以「分」为单位随币种输出：后端不跨币种求和，前端也不许相加。 */
export interface StatsNameValue { name: string; value: number }
export interface PayDayPoint { date: string; orders: number; paid: number }
export interface CurrencyTotals {
  currency: string
  orders: number
  paid: number
  gmvMinor: number
  refundedMinor: number
  feeMinor: number
  netMinor: number
  refundRate: number
}
export interface ChannelTotals { channel: string; currency: string; paid: number; gmvMinor: number }
export interface PayStats {
  from: string
  to: string
  orders: number
  paid: number
  sampled: boolean
  days: PayDayPoint[]
  byCurrency: CurrencyTotals[]
  byChannel: ChannelTotals[]
  byStatus: StatsNameValue[]
  byScene: StatsNameValue[]
  byMethod: StatsNameValue[]
  byBizType: StatsNameValue[]
  failReasons: StatsNameValue[]
  /** 筛选下拉的可选值（按时间窗与渠道收敛；旧后端可能没有） */
  facets?: { bizTypes: string[]; scenes: string[] }
}

export interface PayStatsFilter {
  status?: string
  bizType?: string
  channelCode?: string
  scene?: string
  keyword?: string
  startDate?: string
  endDate?: string
  /** 全量历史：忽略起始时间（后端受 2 万笔采样上限约束并回 sampled） */
  all?: boolean
}

export function usePayStats(filter: PayStatsFilter, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    // 挂在 KEY 下：关单/退款/同步后既有失效逻辑会一并刷新统计，不留旧数。
    queryKey: [...KEY, 'stats', filter],
    queryFn: () => api.get<PayStats>(`${BASE}/orders/stats`, filter),
    placeholderData: keepPreviousData,
    refetchInterval: opts?.refetchInterval,
  })
}
