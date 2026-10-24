import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type TenantStatus = 'active' | 'suspended' | 'expired'

export interface TenantRow {
  id: string
  name: string
  domain: string
  packageId: string
  /** ISO 时间 */
  expireAt: string
  status: TenantStatus
  adminUsername: string
  /** 租户级额度覆盖：null / 缺省 = 沿用套餐，0 = 不限制 */
  maxUsers?: number | null
  maxRoles?: number | null
  maxStorageMb?: number | null
  /** 租户是否可用 AI（缺省 = 可用） */
  aiEnabled?: boolean
  /** AI 每日调用次数覆盖：null / 缺省 = 沿用套餐，0 = 不限制 */
  aiDailyQuota?: number | null
  createdAt: string
}

export interface TenantInput {
  name: string
  domain: string
  packageId: string
  expireAt: string
  status: string
  adminUsername: string
  /** 仅新建时有效：同时创建租户管理员（用户名 / 初始密码 / 邮箱） */
  createAdmin?: boolean
  /** 仅新建时提交；服务端只存哈希，不回显 */
  adminPassword?: string
  adminEmail?: string
  maxUsers?: number | null
  maxRoles?: number | null
  maxStorageMb?: number | null
  aiEnabled?: boolean
  aiDailyQuota?: number | null
}

/** GET /tenants/:id/usage；max* 为有效额度（租户覆盖优先，其次套餐），0 = 不限制 */
export interface TenantUsage {
  users: number
  maxUsers: number
  storageMB: number
  maxStorageMB: number
  roles: number
  maxRoles: number
  expireAt: string
  daysLeft: number
}

const KEY = ['tenant'] as const

export function useTenants(query: PageQuery) {
  return useQuery({
    queryKey: [...KEY, 'list', query],
    queryFn: () => api.get<Page<TenantRow>>('/tenants', query),
    placeholderData: keepPreviousData,
  })
}

export function useTenantUsage(id: string) {
  return useQuery({
    queryKey: [...KEY, 'usage', id],
    queryFn: () => api.get<TenantUsage>(`/tenants/${id}/usage`, undefined, { silent: true }),
    staleTime: 30 * 1000,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveTenant() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: TenantInput }) =>
      id ? api.put<TenantRow>(`/tenants/${id}`, body) : api.post<TenantRow>('/tenants', body),
    onSuccess: invalidate,
  })
}

export function useRenewTenant() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, expireAt }: { id: string; expireAt: string }) =>
      api.put<TenantRow>(`/tenants/${id}/renew`, { expireAt }),
    onSuccess: invalidate,
  })
}

export function useSetTenantStatus() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: TenantStatus }) =>
      api.put<TenantRow>(`/tenants/${id}/status`, { status }),
    onSuccess: invalidate,
  })
}

export function useDeleteTenants() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/tenants', { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}
