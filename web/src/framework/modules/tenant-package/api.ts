import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export interface TenantPackageRow {
  id: string
  name: string
  menuIds: string[]
  /** 0 表示不限制 */
  maxUsers: number
  /** 角色数上限，0 表示不限制 */
  maxRoles: number
  /** MB，0 表示不限制 */
  maxStorageMb: number
  /** AI 每日调用次数上限，0 表示不限制 */
  aiDailyQuota: number
  status: string
  remark?: string
  createdAt: string
}

export type TenantPackageInput = Omit<TenantPackageRow, 'id' | 'createdAt'>

interface MenuNodeDto {
  id: string
  title: string
  children?: MenuNodeDto[]
}

export interface MenuOption {
  id: string
  label: string
  children?: MenuOption[]
}

const KEY = ['tenantPkg'] as const

export function useTenantPackages(query: PageQuery) {
  return useQuery({
    queryKey: [...KEY, 'list', query],
    queryFn: () => api.get<Page<TenantPackageRow>>('/tenant-packages', query),
    placeholderData: keepPreviousData,
  })
}

export function useSaveTenantPackage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: TenantPackageInput }) =>
      id ? api.put<TenantPackageRow>(`/tenant-packages/${id}`, body) : api.post<TenantPackageRow>('/tenant-packages', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY })
      qc.invalidateQueries({ queryKey: ['remote-select'] })
    },
  })
}

export function useDeleteTenantPackages() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<null>('/tenant-packages', { ids: ids.join(',') }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY })
      qc.invalidateQueries({ queryKey: ['remote-select'] })
    },
  })
}

/** 菜单树（标题为 i18n key 时翻译），供 TreeSelect 使用 */
export function useMenuOptions() {
  const { t, i18n } = useTranslation()
  return useQuery({
    queryKey: [...KEY, 'menus', i18n.language],
    queryFn: () => api.get<MenuNodeDto[]>('/menus/tree'),
    staleTime: 60 * 1000,
    select: (nodes) => {
      const walk = (list: MenuNodeDto[]): MenuOption[] =>
        list.map((n) => ({
          id: n.id,
          label: i18n.exists(n.title) ? t(n.title) : n.title,
          children: n.children?.length ? walk(n.children) : undefined,
        }))
      return walk(nodes)
    },
  })
}
