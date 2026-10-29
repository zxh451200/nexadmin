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

export function useMoveMenu() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, parentId, afterId }: { id: string; parentId: string | null; afterId?: string }) =>
      api.put<null>(`/menus/${id}/move`, { parentId, ...(afterId === undefined ? {} : { afterId }) }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['sysMenu'] }),
  })
}
