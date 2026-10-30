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

export interface OrderQuery extends PageQuery {
  status?: string
  channelCode?: string
  subjectId?: string
}
export interface CallbackQuery extends PageQuery {
  channelCode?: string
}
export interface RefundQuery extends PageQuery {
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
}

export interface PayStatsFilter {
  status?: string
  bizType?: string
  channelCode?: string
  scene?: string
  keyword?: string
  startDate?: string
  endDate?: string
}

export function usePayStats(filter: PayStatsFilter) {
  return useQuery({
    // 挂在 KEY 下：关单/退款/同步后既有失效逻辑会一并刷新统计，不留旧数。
    queryKey: [...KEY, 'stats', filter],
    queryFn: () => api.get<PayStats>(`${BASE}/orders/stats`, filter),
    placeholderData: keepPreviousData,
  })
}
