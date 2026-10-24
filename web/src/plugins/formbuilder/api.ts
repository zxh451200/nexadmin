import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { http, api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type FieldType =
  | 'text'
  | 'textarea'
  | 'number'
  | 'radio'
  | 'checkbox'
  | 'select'
  | 'date'
  | 'switch'
  | 'rating'
  | 'file'
  | 'section'

export type CondOp = 'eq' | 'ne' | 'in' | 'contains' | 'empty' | 'notEmpty' | 'gt' | 'gte' | 'lt' | 'lte'
export type Scalar = string | number | boolean

/** 声明式显隐条件：叶子 {field, op, value}，或 {all: [...]} / {any: [...]}。只做数据比较，不执行任何代码。 */
export interface Cond {
  all?: Cond[]
  any?: Cond[]
  field?: string
  op?: CondOp
  value?: Scalar | Scalar[]
}

export interface FieldOption {
  label: string
  value: string
}

export interface FormField {
  key: string
  type: FieldType
  title: string
  description?: string
  placeholder?: string
  required?: boolean
  default?: unknown
  min?: number
  max?: number
  minLen?: number
  maxLen?: number
  integer?: boolean
  pattern?: string
  minDate?: string
  maxDate?: string
  multiple?: boolean
  options?: FieldOption[]
  dictType?: string
  showIf?: Cond
}

export interface FormSettings {
  allowRepeat: boolean
  deadline?: string | null
  maxSubmissions: number
  successMessage?: string
}

export interface FormDefinition {
  name: string
  description?: string
  fields: FormField[]
  settings: FormSettings
}

export interface Audience {
  roles: string[]
  users: string[]
}

export type FormStatus = 'draft' | 'published' | 'offline'

export interface FormSummary {
  id: string
  name: string
  description: string
  status: FormStatus
  publishedVersion: number
  publishedAt: string | null
  hasUnpublished: boolean
  allowRepeat: boolean
  deadlineAt: string | null
  maxSubmissions: number
  submissionCount: number
  createdAt: string
  updatedAt: string
}

export interface FormDetail extends FormSummary {
  definition: FormDefinition
  audience: Audience
}

export interface VersionInfo {
  version: number
  fieldCount: number
  createdAt: string
  createdBy: string
}

export type ClosedReason = '' | 'expired' | 'full' | 'submitted'

export interface FillItem {
  id: string
  name: string
  description: string
  publishedVersion: number
  deadlineAt: string | null
  maxSubmissions: number
  submissionCount: number
  allowRepeat: boolean
  submitted: boolean
  closed: ClosedReason
}

export interface FillDetail extends FillItem {
  definition: FormDefinition
}

export interface SubmitResult {
  id: string
  version: number
  message: string
  replayed: boolean
}

export interface SubmissionRow {
  id: string
  version: number
  userId: string
  username: string
  createdAt: string
  data: Record<string, unknown>
}

export interface SubmissionDetail extends SubmissionRow {
  definition: FormDefinition
}

export interface SubmissionMeta {
  id: string
  name: string
  version: number
  versions: number[]
  definition: FormDefinition
}

export interface SubmissionPage extends Page<SubmissionRow> {
  truncated: boolean
}

export interface SubmissionQuery extends PageQuery {
  version?: string
  username?: string
  field?: string
  value?: string
  from?: string
  to?: string
}

export interface RoleOption {
  id: string
  name: string
}

export interface UserOption {
  id: string
  username: string
  nickname: string
}

export interface DefinitionFile {
  format: 'nexadmin.formbuilder'
  schemaVersion: 1
  exportedAt?: string
  definition: FormDefinition
}

export const BASE = '/plugins/formbuilder'
const KEY = ['plugin_formbuilder'] as const

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

// ---- 设计 ----

export function useForms(query: PageQuery & { status?: string }) {
  return useQuery({
    queryKey: [...KEY, 'forms', query],
    queryFn: () => api.get<Page<FormSummary>>(`${BASE}/forms`, query),
    placeholderData: keepPreviousData,
  })
}

export function useFormDetail(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'form', id],
    queryFn: () => api.get<FormDetail>(`${BASE}/forms/${id}`),
    enabled: !!id,
  })
}

export function useCreateForm() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { name: string; description: string }) => api.post<FormDetail>(`${BASE}/forms`, data),
    onSuccess: invalidate,
  })
}

export function useSaveForm() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, definition, audience }: { id: string; definition: FormDefinition; audience: Audience }) =>
      api.put<FormDetail>(`${BASE}/forms/${id}`, { definition, audience }),
    onSuccess: invalidate,
  })
}

export function useDeleteForm() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/forms/${id}`), onSuccess: invalidate })
}

export function usePublish() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.post<FormDetail>(`${BASE}/forms/${id}/publish`), onSuccess: invalidate })
}

export function useSetOnline() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, online }: { id: string; online: boolean }) =>
      api.post<FormDetail>(`${BASE}/forms/${id}/${online ? 'online' : 'offline'}`),
    onSuccess: invalidate,
  })
}

export function useVersions(id?: string, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'versions', id],
    queryFn: () => api.get<VersionInfo[]>(`${BASE}/forms/${id}/versions`),
    enabled: !!id && enabled,
  })
}

export function useImportForm() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (file: DefinitionFile) => api.post<FormDetail>(`${BASE}/forms/import`, file),
    onSuccess: invalidate,
  })
}

/** 复制表单：新建一个草稿，再把源表单的草稿定义与可见范围写进去（后端没有专门的复制接口） */
export function useCopyForm() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      const src = await api.get<FormDetail>(`${BASE}/forms/${id}`)
      const created = await api.post<FormDetail>(`${BASE}/forms`, { name, description: src.description })
      return api.put<FormDetail>(`${BASE}/forms/${created.id}`, { definition: { ...src.definition, name }, audience: src.audience })
    },
    onSuccess: invalidate,
  })
}

export function useUserOptions(keyword: string, enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'users', keyword],
    queryFn: () => api.get<UserOption[]>(`${BASE}/users`, { keyword }),
    enabled,
    staleTime: 30_000,
  })
}

export function useRoleOptions() {
  return useQuery({
    queryKey: [...KEY, 'roles'],
    queryFn: () => api.get<RoleOption[]>(`${BASE}/roles`),
    staleTime: 60_000,
  })
}

export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

/** 导出表单定义 JSON（version 缺省 = 草稿） */
export async function downloadDefinition(id: string, name: string, version?: number) {
  const file = await api.get<DefinitionFile>(`${BASE}/forms/${id}/definition`, version ? { version } : undefined)
  const text = JSON.stringify(file, null, 2)
  saveBlob(new Blob([text], { type: 'application/json' }), `${name || 'form'}${version ? `-v${version}` : ''}.formbuilder.json`)
}

// ---- 填写 ----

export function useFillList() {
  return useQuery({ queryKey: [...KEY, 'fill'], queryFn: () => api.get<FillItem[]>(`${BASE}/fill/forms`) })
}

export function useFillDetail(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'fill', id],
    queryFn: () => api.get<FillDetail>(`${BASE}/fill/forms/${id}`),
    enabled: !!id,
    staleTime: 0,
  })
}

export function useFillStats(opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: [...KEY, 'fill-stats'],
    queryFn: () => api.get<{ pending: number }>(`${BASE}/fill/stats`),
    refetchInterval: opts?.refetchInterval,
  })
}

export function useSubmit() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data, idemKey }: { id: string; data: Record<string, unknown>; idemKey: string }) =>
      // silent：校验失败由填写页按字段回显，不再弹全局 toast
      api.post<SubmitResult>(`${BASE}/fill/forms/${id}/submit`, { data, idemKey }, { silent: true }),
    onSuccess: invalidate,
  })
}

// ---- 收集 ----

export function useSubmissionMeta(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'submeta', id],
    queryFn: () => api.get<SubmissionMeta>(`${BASE}/forms/${id}/submission-meta`),
    enabled: !!id,
  })
}

export function useSubmissions(id: string | undefined, query: SubmissionQuery) {
  return useQuery({
    queryKey: [...KEY, 'subs', id, query],
    queryFn: () => api.get<SubmissionPage>(`${BASE}/forms/${id}/submissions`, query),
    enabled: !!id,
    placeholderData: keepPreviousData,
  })
}

export function useSubmissionDetail(formId?: string, id?: string) {
  return useQuery({
    queryKey: [...KEY, 'sub', formId, id],
    queryFn: () => api.get<SubmissionDetail>(`${BASE}/forms/${formId}/submissions/${id}`),
    enabled: !!formId && !!id,
  })
}

export function useDeleteSubmission() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ formId, id }: { formId: string; id: string }) => api.del(`${BASE}/forms/${formId}/submissions/${id}`),
    onSuccess: invalidate,
  })
}

/** 提交到导出中心；返回任务 ID */
export function useExportJob() {
  return useMutation({
    mutationFn: ({ id, query }: { id: string; query: SubmissionQuery }) =>
      api.post<{ id: string }>(`${BASE}/forms/${id}/export-job`, undefined, { params: query }),
  })
}

/** 直接导出 xlsx（行数有上限，超出请用导出中心） */
export async function downloadXlsx(id: string, name: string, query: SubmissionQuery) {
  const res = await http.get<unknown>(`${BASE}/forms/${id}/export`, { params: query, responseType: 'blob' })
  if (res.data instanceof Blob) {
    saveBlob(res.data, `${name || 'form'}.xlsx`)
    return
  }
  saveBlob(new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' }), `${name || 'form'}.mock.json`)
}
