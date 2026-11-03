import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export type MenuType = 'dir' | 'menu' | 'button'

export interface SysMenu {
  id: string
  parentId: string | null
  type: MenuType
  /** i18n key 或直接文本 */
  title: string
  icon?: string
  path?: string
  component?: string
  perm?: string
  sort: number
  keepAlive: boolean
  hidden: boolean
  external: boolean
  status: '1' | '0'
  createdAt?: string
  updatedAt?: string
  children?: SysMenu[]
}

export type SysMenuInput = Omit<SysMenu, 'id' | 'children' | 'createdAt' | 'updatedAt'>

const KEY = ['sysMenu', 'tree'] as const

export function useMenuTree() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<SysMenu[]>('/menus/tree') })
}

export function useCreateMenu() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SysMenuInput) => api.post<SysMenu>('/menus', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}

export function useUpdateMenu() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: SysMenuInput & { id: string }) => api.put<SysMenu>(`/menus/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}

export function useDeleteMenu() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/menus/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}

export function useSortMenus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { parentId: string | null; ids: string[] }) => api.put<null>('/menus/sort', v),
    onSettled: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}

export function countDescendants(node: SysMenu): number {
  return (node.children ?? []).reduce((n, c) => n + 1 + countDescendants(c), 0)
}

/**
 * 把勾选集合按完整树展开成「真正会被删掉的行集合」：每个命中节点连同它的全部下级。
 * 不能按可见树算——菜单列表默认折叠掉按钮、筛选时还会剪掉不命中的分支，
 * 而后端是按真实子树级联删除的，只报可见行会让确认框里的数字小于实删条数。
 */
export function collectSubtreeIds(nodes: SysMenu[], ids: string[]): string[] {
  const picked = new Set(ids)
  const out = new Set<string>()
  const walk = (list: SysMenu[], inSubtree: boolean) => {
    for (const n of list) {
      const hit = inSubtree || picked.has(n.id)
      if (hit) out.add(n.id)
      if (n.children?.length) walk(n.children, hit)
    }
  }
  walk(nodes, false)
  return [...out]
}

export function useDeleteMenus() {
  const qc = useQueryClient()
  return useMutation({
    // 与后端 DELETE /menus?ids= 同口径：每项连同子树，返回真正被删掉的条数
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/menus', { ids: ids.join(',') }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}

export function useMoveMenu() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, parentId, afterId }: { id: string; parentId: string | null; afterId?: string }) =>
      api.put<null>(`/menus/${id}/move`, { parentId, ...(afterId === undefined ? {} : { afterId }) }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}
