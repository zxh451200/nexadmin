import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

// 客户主数据 + 线索/商机台账（收敛方案第 3 步最小骨架）。
export type CustomerStatus = 'active' | 'inactive'
/** 客户是自然人还是单位：决定哪一组身份字段有值（个人=国籍+证件，企业=信用代码+行业+地址）。 */
export type CustomerType = 'person' | 'company'
export type OppStage = 'lead' | 'qualified' | 'proposal' | 'negotiate' | 'won' | 'lost'

export interface CrmCustomer {
  id: string
  name: string
  type: CustomerType
  code?: string
  level?: string
  status: CustomerStatus
  externalSource?: string | null
  externalNo?: string | null
  owner?: string
  phone?: string
  email?: string
  /** ISO 3166-1 alpha-2 国家/地区代码，空 = 未登记。 */
  country?: string
  /** ISO 3166-1 alpha-2 国籍代码；企业客户恒为空。 */
  nationality?: string
  /** 个人客户：证件类型（id_card / passport / officer_card / hmt_permit / other）。 */
  idType?: string
  idNo?: string
  /** 企业客户：统一社会信用代码 / 行业 / 注册地址。 */
  taxNo?: string
  industry?: string
  address?: string
  remark?: string
  createdAt: string
}

export interface CrmCustomerForm {
  name: string
  type?: CustomerType
  code?: string
  level?: string
  status?: CustomerStatus
  externalSource?: string
  externalNo?: string
  owner?: string
  phone?: string
  email?: string
  country?: string
  nationality?: string
  idType?: string
  idNo?: string
  taxNo?: string
  industry?: string
  address?: string
  remark?: string
}

export interface CrmOpportunity {
  id: string
  /** 雪花 ID 一律按字符串传，避免超过 JS 安全整数后被改写。 */
  customerId: string
  customerName?: string
  /** 所属客户是个人还是单位（展示用，来自客户主数据）。 */
  customerType?: CustomerType
  /** 客户下主对接联系人（可选）。 */
  contactId?: string
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
  customerId?: string
  contactId?: string
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
  /** 客户类型：person / company，空 = 不限。 */
  type?: string
  /** 只看我的：owner = 当前账号（与角色数据范围叠加）。 */
  mine?: boolean
}
export interface OpportunityQuery extends PageQuery {
  stage?: string
  customerId?: string
  mine?: boolean
}

// 联系人：一个客户多个联系人，isPrimary 标主联系人。
export interface CrmContact {
  id: string
  customerId: string
  name: string
  title?: string
  phone?: string
  email?: string
  isPrimary: boolean
  remark?: string
}

export interface CrmContactForm {
  name: string
  title?: string
  phone?: string
  email?: string
  isPrimary?: boolean
  remark?: string
}

// 跟进记录：call/visit/note/todo；todo 用 done/nextAt 表待办。
export type ActivityKind = 'call' | 'visit' | 'note' | 'todo'

export interface CrmActivity {
  id: string
  customerId: string
  opportunityId?: string
  kind: ActivityKind
  content: string
  nextAt?: string
  done: boolean
  author?: string
  createdAt: string
}

export interface CrmActivityForm {
  opportunityId?: string
  kind?: ActivityKind
  content: string
  nextAt?: string
}

export interface ActivityQuery extends PageQuery {
  opportunityId?: string
  kind?: string
  /** 只看待办（done=false）。 */
  openOnly?: boolean
}

/**
 * 批量删除的结果：deleted 是后端实删条数，blocked 是「点名了但没删」的条数
 * （数据范围外的、或客户名下仍挂商机的）。部分删除不是失败，页面要照实说。
 */
export interface CrmBatchDeleteResult {
  deleted: number
  blocked: number
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

/** 导出当前筛选下的全部客户（数据范围生效；上限 10000，超限后端报错）。silent：错误由页面 toast。 */
export function fetchCustomersForExport(query: CustomerQuery): Promise<CrmCustomer[]> {
  const filters: Record<string, unknown> = {
    keyword: query.keyword, type: query.type, status: query.status,
    source: query.source, mine: query.mine,
  }
  return api.get<CrmCustomer[]>(`${BASE}/customers/export`, filters, { silent: true })
}

export function useOpportunities(query: OpportunityQuery, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'opportunities', query],
    queryFn: () => api.get<Page<CrmOpportunity>>(`${BASE}/opportunities`, query),
    placeholderData: keepPreviousData,
    enabled,
  })
}

// 明文证件号：单独接口，权限点 realname:reveal，后端每次查看都记操作日志。
export interface IdNoReveal {
  idType: string
  idNo: string
  kind?: string
  regionNames?: string[]
  birthday?: string
  sex?: string
  age?: number
  valid?: boolean
  reason?: string
}

export function useRevealIdNo() {
  return useMutation({ mutationFn: (id: string) => api.get<IdNoReveal>(`${BASE}/customers/${id}/idcard`) })
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

/** 批量删除客户：连带清掉其联系人与跟进；名下仍挂商机的由后端计入 blocked，不删。 */
export function useDeleteCustomers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<CrmBatchDeleteResult>(`${BASE}/customers`, { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

export function useBindExternal() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { customerId: string; source: string; externalNo: string }) =>
      api.post<CrmCustomer>(`${BASE}/customers/bind-external`, data),
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

/** 批量删除线索/商机：数据范围外的计入 blocked，不删。 */
export function useDeleteOpportunities() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<CrmBatchDeleteResult>(`${BASE}/opportunities`, { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

// ---- 联系人 ----

export function useContacts(customerId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'contacts', customerId],
    queryFn: () => api.get<CrmContact[]>(`${BASE}/customers/${customerId}/contacts`),
    enabled: enabled && !!customerId,
  })
}

export function useSaveContact() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ customerId, id, data }: { customerId: string; id?: string; data: CrmContactForm }) =>
      id ? api.put<CrmContact>(`${BASE}/contacts/${id}`, data) : api.post<CrmContact>(`${BASE}/customers/${customerId}/contacts`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteContact() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/contacts/${id}`),
    onSuccess: invalidate,
  })
}

// ---- 跟进记录 ----

export function useActivities(query: ActivityQuery, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'activities', query],
    queryFn: () => api.get<Page<CrmActivity>>(`${BASE}/customers/${query.customerId}/activities`, query),
    placeholderData: keepPreviousData,
    enabled: enabled && !!query.customerId,
  })
}

export function useCreateActivity() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ customerId, data }: { customerId: string; data: CrmActivityForm }) =>
      api.post<CrmActivity>(`${BASE}/customers/${customerId}/activities`, data),
    onSuccess: invalidate,
  })
}

export function useToggleActivityDone() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, done }: { id: string; done: boolean }) =>
      api.put(`${BASE}/activities/${id}/done`, { done }),
    onSuccess: invalidate,
  })
}

export function useDeleteActivity() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/activities/${id}`),
    onSuccess: invalidate,
  })
}
