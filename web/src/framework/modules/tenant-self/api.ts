import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface TenantSelfQuota {
  users: number
  maxUsers: number
  storageMB: number
  maxStorageMB: number
  roles: number
  maxRoles: number
  expireAt: string
  daysLeft: number
}

export interface TenantSelfPoint {
  day: string
  users: number
  roles: number
  storageMB: number
}

/** GET /tenant-self：当前账号所在租户的资料、额度用量与近 30 天切片 */
export interface TenantSelf {
  name: string
  domain: string
  status: 'active' | 'suspended' | 'expired'
  packageName: string
  expireAt: string
  daysLeft: number
  companyName: string
  contactName: string
  contactPhone: string
  contactEmail: string
  logoUrl: string
  aiEnabled: boolean
  aiDailyQuota: number
  usage: TenantSelfQuota
  trend: TenantSelfPoint[]
}

/** 租户可自行修改的字段；名称/域名/套餐/额度/到期归平台管理 */
export interface TenantSelfInput {
  companyName: string
  contactName: string
  contactPhone: string
  contactEmail: string
  logoUrl: string
}

const KEY = ['tenant-self'] as const

export function useTenantSelf() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<TenantSelf>('/tenant-self') })
}

export function useSaveTenantSelf() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: TenantSelfInput) => api.put<TenantSelf>('/tenant-self', input),
    onSuccess: (data) => {
      qc.setQueryData(KEY, data)
      // 侧栏/登录页的品牌来自公开站点设置，保存后一并刷新
      void qc.invalidateQueries({ queryKey: ['settings', 'public'] })
    },
  })
}
