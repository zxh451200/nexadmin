import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

// 客户主数据 + 线索/商机台账（收敛方案第 3 步最小骨架）。
export type CustomerStatus = 'active' | 'inactive'
export type OppStage = 'lead' | 'qualified' | 'proposal' | 'negotiate' | 'won' | 'lost'

export interface CrmCustomer {
  id: string
  name: string
  code?: string
  level?: string
  status: CustomerStatus
  externalSource?: string | null
  externalNo?: string | null
  owner?: string
  phone?: string
  email?: string
  remark?: string
  createdAt: string
}

export interface CrmCustomerForm {
  name: string
  code?: string
  level?: string
  status?: CustomerStatus
  externalSource?: string
  externalNo?: string
  owner?: string
  phone?: string
  email?: string
  remark?: string
}

export interface CrmOpportunity {
  id: string
  customerId: number
  title: string
  stage: OppStage
  amountMinor: number
  currency: string
  expectedAt?: string
  owner?: string
  remark?: string
  createdAt: string
}

export interface CrmOpportunityForm {
  customerId?: number
  title: string
  stage?: OppStage
  amountMinor?: number
  currency?: string
  expectedAt?: string
  owner?: string
  remark?: string
}

export interface CustomerQuery extends PageQuery {
  status?: string
  source?: string
}
export interface OpportunityQuery extends PageQuery {
  stage?: string
}

const BASE = '/plugins/crm'
const KEY = ['plugin_crm'] as const

export function useCustomers(query: CustomerQuery) {
  return useQuery({
    queryKey: [...KEY, 'customers', query],
    queryFn: () => api.get<Page<CrmCustomer>>(`${BASE}/customers`, query),
    placeholderData: keepPreviousData,
  })
}

export function useOpportunities(query: OpportunityQuery) {
  return useQuery({
    queryKey: [...KEY, 'opportunities', query],
    queryFn: () => api.get<Page<CrmOpportunity>>(`${BASE}/opportunities`, query),
    placeholderData: keepPreviousData,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveCustomer() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: CrmCustomerForm }) =>
      id ? api.put<CrmCustomer>(`${BASE}/customers/${id}`, data) : api.post<CrmCustomer>(`${BASE}/customers`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteCustomer() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/customers/${id}`),
    onSuccess: invalidate,
  })
}

export function useSaveOpportunity() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: CrmOpportunityForm }) =>
      id
        ? api.put<CrmOpportunity>(`${BASE}/opportunities/${id}`, data)
        : api.post<CrmOpportunity>(`${BASE}/opportunities`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteOpportunity() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/opportunities/${id}`),
    onSuccess: invalidate,
  })
}
