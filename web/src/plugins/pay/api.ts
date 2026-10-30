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

export interface PayCallback {
  id: string
  channelCode: string
  paymentNo: string
  ok: boolean
  result: string
  payload?: string
  createdAt: string
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
  createdAt: string
}

export type AuthStatus = 'authorized' | 'partial_captured' | 'captured' | 'voided' | 'expired'
export type ReconcileStatus = 'open' | 'closed'
export type DiffKind = 'long' | 'short' | 'missing'
export type DiffStatus = 'open' | 'resolved' | 'ignored'
export type SettlementStatus = 'draft' | 'confirmed' | 'paid'
export type PayoutStatus = 'draft' | 'paid'

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
  cardLast4?: string
  remark?: string
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
  cardLast4?: string
  validDays?: number
  remark?: string
}

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
  paidAt?: string | null
  createdAt: string
}

export interface OrderQuery extends PageQuery {
  status?: string
  channelCode?: string
  subjectId?: string
  bizType?: string
  scene?: string
  failReason?: string
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

const BASE = '/plugins/pay'
const KEY = ['plugin_pay'] as const

export function useOrders(query: OrderQuery) {
  return useQuery({
    queryKey: [...KEY, 'orders', query],
    queryFn: () => api.get<Page<PayOrder>>(`${BASE}/orders`, query),
    placeholderData: keepPreviousData,
  })
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

export function useCreateRefund() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { paymentNo: string; amountMinor: number; reason: string }) =>
      api.post<PayRefund>(`${BASE}/refunds`, data),
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
    mutationFn: (data: { channelCode: string; billDate: string; subjectId?: string; channelAmounts: ReconcileAmount[] }) =>
      api.post<PayReconcileTask>(`${BASE}/reconcile/tasks`, data),
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
