import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
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
  /** 客户标签（规范化存储，最多 10 个、每个 20 字）。 */
  tags?: string[]
  /** 自定义字段值（code → 值），由启用中的字段定义渲染。 */
  customValues?: Record<string, unknown>
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
  /** 自定义字段整份值（未定义/停用的 code 后端会 400）。 */
  custom?: Record<string, unknown>
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
  /** 逗号分隔多值 */
  status?: string
  /** 逗号分隔多值 */
  source?: string
  /** 客户类型：person / company，逗号分隔多值，空 = 不限。 */
  type?: string
  /** 等级（A/B/C…），逗号分隔多值，空 = 不限。 */
  level?: string
  /** 标签筛选（逗号分隔多值；后端按逗号边界匹配，多标签并集）。 */
  tag?: string
  /** 建档时间范围（YYYY-MM-DD，含端点）。 */
  createdFrom?: string
  createdTo?: string
  /** 排序字段（后端白名单：created_at / name / code / level / owner），空 = 默认 id DESC。 */
  sortKey?: string
  /** 排序方向；false = 倒序（默认）。 */
  sort?: 'asc' | 'desc'
  /** 只看我的：owner = 当前账号（与角色数据范围叠加）。 */
  mine?: boolean
}

/** 筛选草稿（页面 state）：枚举/标签字段是多选数组，搜索时用 toCustomerQuery 拼成 CSV */
export interface CustomerDraft {
  keyword?: string
  type?: string[]
  status?: string[]
  source?: string[]
  level?: string[]
  tag?: string[]
  createdFrom?: string
  createdTo?: string
  sortKey?: string
  sort?: 'asc' | 'desc'
}

export function toCustomerQuery(d: CustomerDraft): Omit<CustomerQuery, 'page' | 'pageSize'> {
  return {
    keyword: d.keyword,
    type: d.type?.length ? d.type.join(',') : undefined,
    status: d.status?.length ? d.status.join(',') : undefined,
    source: d.source?.length ? d.source.join(',') : undefined,
    level: d.level?.length ? d.level.join(',') : undefined,
    tag: d.tag?.length ? d.tag.join(',') : undefined,
    createdFrom: d.createdFrom,
    createdTo: d.createdTo,
    sortKey: d.sortKey,
    sort: d.sort,
  }
}

export interface OpportunityQuery extends PageQuery {
  /** 逗号分隔多值 */
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
/** system 只由服务层写入（归属/合并/状态变更等留痕），不出现在人工写跟进的类型里。 */
export type ActivityKind = 'call' | 'visit' | 'note' | 'todo' | 'system'

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

/** CRM 概览：按类型/状态的客户计数 + 公海与在途商机数。 */
export interface CrmDailyCount {
  date: string
  count: number
}

export interface CrmStats {
  persons: number
  companies: number
  active: number
  inactive: number
  pool: number
  openDeals: number
  /** 六阶段商机计数（含 won/lost 终态，漏斗才看得出转化）。 */
  stages: Record<string, number>
  /** 六阶段金额漏斗：stage → 币种 → 金额（amountMinor 累加；不同币种的钱不相加）。 */
  stageAmounts: Record<string, Record<string, number>>
  /** 客户来源分布（none = 无锚点/手工录入）。 */
  sources: Record<string, number>
  /** 近 30 天建档趋势，缺天补零。 */
  createdDaily: CrmDailyCount[]
}

export function useCrmStats() {
  return useQuery({
    queryKey: [...KEY, 'stats'],
    queryFn: () => api.get<CrmStats>(`${BASE}/stats`),
  })
}

// 公海池：owner 为空的客户对全员开放。领取用条件 UPDATE，抢同一客户的后来者 409。
export function useCustomerPool(query: CustomerQuery) {
  return useQuery({
    queryKey: [...KEY, 'pool', query],
    queryFn: () => api.get<Page<CrmCustomer>>(`${BASE}/customers/pool`, query),
    placeholderData: keepPreviousData,
  })
}

/** 我的待跟进：到期未完成的跟进记录（可见性继承客户归属）。 */
export interface CrmTodo {
  id: string
  customerId: string
  customerName: string
  kind: ActivityKind
  content: string
  nextAt: string
  author?: string
  /** 已过期（next_at 早于今天），清单里标红。 */
  overdue: boolean
}

export function useCustomerTodos(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'todos'],
    queryFn: () => api.get<{ items: CrmTodo[]; total: number }>(`${BASE}/customers/todos`),
    enabled,
  })
}

/** 拉单个客户（待跟进清单「查看客户」用）：清单行只有 id + 名，详情面板要完整资料。 */
export function fetchCustomer(id: string): Promise<CrmCustomer> {
  return api.get<CrmCustomer>(`${BASE}/customers/${id}`)
}

export function useClaimCustomer() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<CrmCustomer>(`${BASE}/customers/${id}/claim`, {}),
    onSuccess: invalidate,
  })
}

export function useReleaseCustomer() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<CrmCustomer>(`${BASE}/customers/${id}/release`, {}),
    onSuccess: invalidate,
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

/** 使用中的标签（按使用次数倒序）：筛选下拉与打标建议共用。 */
export function useCustomerTags(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'tags'],
    queryFn: () => api.get<string[]>(`${BASE}/customers/tags`),
    enabled,
  })
}

/** 整份替换客户标签（最多 10 个、每个 20 字，后端校验）。 */
export function useSetCustomerTags() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, tags }: { id: string; tags: string[] }) =>
      api.put<CrmCustomer>(`${BASE}/customers/${id}/tags`, { tags }),
    onSuccess: invalidate,
  })
}

/** 批量改归属/状态/等级：范围外的 id 由后端静默跳过，返回实改数。 */
export function useBatchUpdateCustomers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: { ids: string[]; owner?: string; status?: CustomerStatus; level?: string }) =>
      api.post<{ updated: number }>(`${BASE}/customers/batch-update`, input),
    onSuccess: invalidate,
  })
}

/** 批量追加标签：已有该词的客户跳过，返回实改数。 */
export function useBatchAddCustomerTags() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: { ids: string[]; tags: string[] }) =>
      api.post<{ updated: number }>(`${BASE}/customers/batch-tags`, input),
    onSuccess: invalidate,
  })
}

/** 疑似重复分组（手机号/证件号/信用代码同值多客户），打开时拉一次。 */
export interface CrmDuplicateGroup {
  key: 'phone' | 'id_no' | 'tax_no'
  value: string
  customers: CrmCustomer[]
}

export function useCustomerDuplicates(enabled: boolean) {
  return useQuery({
    queryKey: [...KEY, 'duplicates'],
    queryFn: () => api.get<CrmDuplicateGroup[]>(`${BASE}/customers/duplicates`),
    enabled,
  })
}

/** 一条自定义字段定义（平台级，全租户共享）。 */
export interface CrmField {
  id: string
  code: string
  label: string
  type: 'text' | 'number' | 'select' | 'date' | 'bool'
  options: string[]
  required: boolean
  enabled: boolean
  sort: number
}

/** 全部字段定义（含停用的：管理页要能重新启用）。 */
export function useCustomerFields(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'fields'],
    queryFn: () => api.get<CrmField[]>(`${BASE}/customers/fields`),
    enabled,
  })
}

export function useSaveCustomerField() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: CrmField & { id?: string }) =>
      data.id
        ? api.put<CrmField>(`${BASE}/customers/fields/${data.id}`, data)
        : api.post<CrmField>(`${BASE}/customers/fields`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteCustomerField() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/customers/fields/${id}`),
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

/** 合并重复客户：source 并入 target，关联记录整体迁移后删除 source。 */
export function useMergeCustomers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { targetId: string; sourceId: string }) =>
      api.post<CrmCustomer>(`${BASE}/customers/merge`, data),
    onSuccess: invalidate,
  })
}

export interface CrmMergeFieldPreview {
  column: string
  value: string
}

/** 自定义字段的补空预览：列名是管理员起的 label，值可能是数字/布尔（已转成文本）。 */
export interface CrmMergeCustomFieldPreview {
  code: string
  label: string
  value: string
}

/** 合并预览：与合并同一套规则的只读投影（补空字段 / 标签并集 / 自定义字段 / 关联迁移量）。 */
export interface CrmMergePreview {
  target: CrmCustomer
  source: CrmCustomer
  contacts: number
  opportunities: number
  activities: number
  fillFields: CrmMergeFieldPreview[]
  newTags: string[]
  newCustomFields: CrmMergeCustomFieldPreview[]
}

export function useMergePreview(targetId?: string, sourceId?: string) {
  return useQuery({
    queryKey: [...KEY, 'merge-preview', targetId, sourceId],
    queryFn: () => api.post<CrmMergePreview>(`${BASE}/customers/merge-preview`, { targetId, sourceId }),
    enabled: !!targetId && !!sourceId && targetId !== sourceId,
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

// ---- Excel / CSV 导入 ----
// 解析、模板、逐行校验、失败行报告与台账都由通用导入中心负责；crm 只是已注册的导入源
// （source=crm_customers，列定义见 plugins/crm/import.go 的 ImportSources）。

export const CRM_IMPORT_SOURCE = 'crm_customers'

export interface CrmImportJob {
  id: string
  source: string
  fileName: string
  totalRows: number
  okRows: number
  failRows: number
  status: 'pending' | 'running' | 'done' | 'failed' | 'cancelled'
  /** 失败明细只带前若干条；完整报告走 errors.csv。 */
  errors?: { row: number; key?: string; message: string }[]
}

export function useImportCustomers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (file: File) => {
      const form = new FormData()
      form.append('source', CRM_IMPORT_SOURCE)
      form.append('file', file)
      return api.post<CrmImportJob>('/plugins/imports', form)
    },
    onSuccess: invalidate,
  })
}

// 下载接口要带 Bearer 令牌，window.open 拿不到，只能取 Blob 再触发保存。
async function fetchBlob(url: string, params?: Record<string, string>): Promise<Blob> {
  const res = await http.get<unknown>(url, { params, responseType: 'blob' })
  return res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/csv;charset=utf-8' })
}

/** 模板由服务端按导入源生成：表头、必填标记与示例行和真实校验口径同源。 */
export function fetchCustomerImportTemplate(format: 'xlsx' | 'csv') {
  return fetchBlob('/plugins/imports/template', { source: CRM_IMPORT_SOURCE, format })
}

/** 失败行报告（行号 / 列 / 原因），修正原文件后可整份重导。 */
export function fetchCustomerImportErrors(jobId: string) {
  return fetchBlob(`/plugins/imports/${jobId}/errors.csv`)
}
