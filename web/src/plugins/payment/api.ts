import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type PayStatus = 'pending' | 'paid' | 'failed' | 'closed' | 'refunding' | 'partially_refunded' | 'refunded'
export type RefundStatus = 'pending' | 'succeeded' | 'failed'

export interface PayOrder {
  id: string
  paymentNo: string
  bizType: string
  bizNo: string
  subject: string
  amountMinor: number
  refundedMinor: number
  currency: string
  channel: string
  channelTxnId?: string
  status: PayStatus
  failReason?: string
  payUrl?: string
  paidAt?: string | null
  expireAt?: string | null
  createdAt: string
}

export interface PayChannel {
  id: string
  code: string
  name: string
  enabled: boolean
  sort: number
  hasConfig: boolean
  updatedAt: string
}

export interface PayChannelForm {
  code: string
  name: string
  enabled: boolean
  sort: number
  config: Record<string, string>
}

export interface PayCallback {
  id: string
  channel: string
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
  bizType: string
  bizNo: string
  amountMinor: number
  currency: string
  reason?: string
  status: RefundStatus
  channelRefundId?: string
  succeededAt?: string | null
  operator?: string
  createdAt: string
}

export interface OrderQuery extends PageQuery {
  status?: string
  channel?: string
}
export interface CallbackQuery extends PageQuery {
  channel?: string
}
export interface RefundQuery extends PageQuery {
  status?: string
}

const BASE = '/plugins/payment'
const KEY = ['plugin_payment'] as const

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
    queryFn: () => api.get<string[]>(`${BASE}/providers`),
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

export function useSaveChannel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: PayChannelForm }) =>
      id ? api.put<PayChannel>(`${BASE}/channels/${id}`, data) : api.post<PayChannel>(`${BASE}/channels`, data),
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
