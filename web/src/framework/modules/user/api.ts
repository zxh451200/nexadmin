import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'
import type { RegionValue } from '@/framework/components/pickers/useRegionTree'

export interface User {
  id: string
  username: string
  nickname: string
  avatar: string
  phone: string
  email: string
  /** sys_gender: 0 未知 / 1 男 / 2 女 */
  gender: string
  deptId: string
  deptName: string
  postIds: string[]
  roleIds: string[]
  roleNames: string[]
  /** sys_status: '1' 启用 / '0' 停用 */
  status: string
  region: RegionValue
  remark: string
  /** 是否被登录锁定（连续密码错误） */
  locked?: boolean
  /** 锁定到期时间（ISO），未锁定为 null */
  lockedUntil?: string | null
  createdAt: string
  updatedAt: string
}

export interface UserQuery {
  page: number
  pageSize: number
  keyword?: string
  status?: string
  deptId?: string
  /** sys_gender: 1 男 / 2 女（AI 筛选产出） */
  gender?: string
  /** YYYY-MM-DD，创建时间闭区间（AI 筛选产出） */
  createdFrom?: string
  createdTo?: string
}

export type UserFilters = Pick<UserQuery, 'keyword' | 'status' | 'deptId' | 'gender' | 'createdFrom' | 'createdTo'>

/** deptName / roleNames 为 mock 阶段由前端补全的冗余展示字段，真实后端由服务端关联查询 */
export interface UserInput {
  username: string
  nickname: string
  avatar: string
  phone: string
  email: string
  gender: string
  deptId: string
  deptName: string
  postIds: string[]
  roleIds: string[]
  roleNames: string[]
  status: string
  region: RegionValue
  remark: string
}

export interface ImportResult {
  success: number
  /** 失败行：row 为文件中的行号（表头为第 1 行） */
  failed: { row: number; reason: string }[]
}

export const userKeys = {
  all: ['users'] as const,
  list: (q: UserQuery) => ['users', 'list', q] as const,
}

export function useUserList(query: UserQuery) {
  return useQuery({
    queryKey: userKeys.list(query),
    queryFn: () => api.get<Page<User>>('/users', query),
    placeholderData: keepPreviousData,
  })
}

export function fetchUsersForExport(filters: UserFilters) {
  return api.get<User[]>('/users/export', filters)
}

/**
 * RemoteSelect 的 resolveLabel 扩展点实现：按 id 走 GET /users/:id 取展示名（nickname 优先）。
 * 列表接口（GET /users）不支持按 id 精确取回，详情接口是现成能力；失败（已删除/无权限/超时）
 * 返回 undefined 由组件回退显示原值，silent 避免回显失败弹全局错误 toast。
 */
export async function resolveUserLabel(id: string, signal: AbortSignal): Promise<string | undefined> {
  try {
    const u = await api.get<User>(`/users/${id}`, undefined, { signal, silent: true })
    return u?.nickname || u?.username || undefined
  } catch {
    return undefined
  }
}

/** 由 Content-Type 推断下载文件扩展名：mock 阶段返回 CSV 文本，真实后端返回 .xlsx */
export function blobExt(blob: Blob): 'csv' | 'xlsx' {
  return blob.type.includes('csv') ? 'csv' : 'xlsx'
}

/** 服务端导出 .xlsx（沿用同样的筛选条件与数据范围）；真实后端返回文件（Blob），mock 阶段返回 CSV 文本 */
export async function exportUsersFile(filters: UserFilters): Promise<Blob> {
  const res = await http.get<unknown>('/users/export', { params: { ...filters, format: 'xlsx' }, responseType: 'blob' })
  return res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/csv;charset=utf-8' })
}

export interface ExportPlan {
  /** 按当前筛选条件与数据范围匹配的行数 */
  total: number
  /** export.sync_limit：不超过它时同步直出 */
  syncLimit: number
  async: boolean
}

/** 导出前先问服务端该走同步还是异步（失败时返回 null，调用方退回同步导出） */
export function fetchExportPlan(filters: UserFilters): Promise<ExportPlan | null> {
  return api.get<ExportPlan>('/users/export-plan', filters, { silent: true }).catch(() => null)
}

/** 创建异步导出任务（同样的筛选参数），立即返回任务 ID；进度与下载见导出中心 */
export function createExportJob(filters: UserFilters) {
  return api.post<{ id: string }>('/users/export-async', filters)
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: userKeys.all })
}

export function useCreateUser() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: UserInput) => api.post<User>('/users', input),
    onSuccess: invalidate,
  })
}

export function useUpdateUser() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UserInput }) => api.put<User>(`/users/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteUsers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/users', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

export function useUpdateUserStatus() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => api.put<null>(`/users/${id}/status`, { status }),
    onSuccess: invalidate,
  })
}

export function useResetPassword() {
  return useMutation({
    mutationFn: ({ id, password }: { id: string; password: string }) =>
      api.post<null>(`/users/${id}/reset-password`, { password }),
  })
}

export function useUnlockUser() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<null>(`/users/${id}/unlock`),
    onSuccess: invalidate,
  })
}

/** 上传 .xlsx 或 .csv（multipart，字段 file）；部分成功，失败行在结果里返回 */
export function useImportUsers() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (file: File) => {
      const fd = new FormData()
      fd.append('file', file)
      return api.post<ImportResult>('/users/import', fd)
    },
    onSuccess: invalidate,
  })
}

/** 下载服务端导入模板（默认 .xlsx） */
export async function fetchImportTemplate(): Promise<Blob> {
  const res = await http.get<unknown>('/users/import-template', { responseType: 'blob' })
  // 真实后端返回文件（Blob）；mock 阶段返回 CSV 文本
  return res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/csv;charset=utf-8' })
}
