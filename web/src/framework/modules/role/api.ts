import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export interface Role {
  id: string
  name: string
  code: string
  sort: number
  /** sys_status: '1' 启用 / '0' 停用 */
  status: string
  /** data_scope: all / custom / dept / deptAndChild / self */
  dataScope: string
  deptIds: string[]
  remark: string
  /** 内置角色不可删除 */
  builtin?: boolean
  /** 是否允许被下级管理员分配给用户；内置超管角色恒为 false */
  assignable: boolean
  createdAt: string
  updatedAt: string
}

export type RoleInput = Pick<Role, 'name' | 'code' | 'sort' | 'status' | 'dataScope' | 'deptIds' | 'remark' | 'assignable'>

export interface PermissionTreeNode {
  id: string
  title: string
  type: 'dir' | 'menu' | 'button'
  perm?: string
  icon?: string
  path?: string
  children?: PermissionTreeNode[]
}

export const roleKeys = {
  all: ['roles'] as const,
  list: ['roles', 'list'] as const,
  menus: (id: string) => ['roles', 'menus', id] as const,
}

export function useRoleList() {
  return useQuery({
    queryKey: roleKeys.list,
    queryFn: () => api.get<Page<Role>>('/roles', { page: 1, pageSize: 1000 }),
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: roleKeys.all }),
      qc.invalidateQueries({ queryKey: ['remote-select'] }),
    ])
}

export function useCreateRole() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: RoleInput) => api.post<Role>('/roles', input),
    onSuccess: invalidate,
  })
}

export function useUpdateRole() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: RoleInput }) => api.put<Role>(`/roles/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteRole() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/roles/${id}`),
    onSuccess: invalidate,
  })
}

export function useSortRoles() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ids: string[]) => api.put<null>('/roles/sort', { ids }),
    onSuccess: invalidate,
  })
}

export function useMenuTree(enabled = true) {
  return useQuery({
    queryKey: ['menus', 'tree'],
    queryFn: () => api.get<PermissionTreeNode[]>('/menus/tree'),
    enabled,
    staleTime: 60 * 1000,
  })
}

export function useRoleMenus(id: string | undefined) {
  return useQuery({
    queryKey: roleKeys.menus(id ?? ''),
    queryFn: () => api.get<string[]>(`/roles/${id}/menus`),
    enabled: !!id,
    gcTime: 0,
  })
}

export function useAssignRoleMenus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, menuIds }: { id: string; menuIds: string[] }) => api.put<null>(`/roles/${id}/menus`, { menuIds }),
    onSuccess: (_d, v) => qc.invalidateQueries({ queryKey: roleKeys.menus(v.id) }),
  })
}
