import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface SysDept {
  id: string
  name: string
  parentId: string | null
  sort: number
  leaderId?: string
  leaderName?: string
  phone?: string
  email?: string
  status: '1' | '0'
  createdAt?: string
  updatedAt?: string
  children?: SysDept[]
  /** 仅 scene=manage 的裁剪树里出现：为维持树结构保留的范围外祖先节点，置灰且不可操作 */
  readonly?: boolean
}

export type SysDeptInput = Omit<SysDept, 'id' | 'children' | 'createdAt' | 'updatedAt' | 'leaderName' | 'readonly'>

/** 部门管理页专用：scene=manage，后端按操作者数据范围裁剪；各下拉仍用完整树（不带 scene）。 */
export function useDeptTree(enabled = true) {
  return useQuery({
    queryKey: ['dept', 'tree', 'manage'],
    queryFn: () => api.get<SysDept[]>('/depts/tree', { scene: 'manage' }),
    enabled,
  })
}

/** 树里是否含只读祖先节点（即被数据范围裁剪过）。 */
export function isTrimmed(nodes: SysDept[]): boolean {
  return nodes.some((n) => n.readonly === true || isTrimmed(n.children ?? []))
}

export function useCreateDept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SysDeptInput) => api.post<SysDept>('/depts', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dept'] }),
  })
}

export function useUpdateDept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: SysDeptInput & { id: string }) => api.put<SysDept>(`/depts/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dept'] }),
  })
}

export function useDeleteDept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/depts/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dept'] }),
  })
}

export function useSortDepts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { parentId: string | null; ids: string[] }) => api.put<null>('/depts/sort', v),
    onSettled: () => qc.invalidateQueries({ queryKey: ['dept'] }),
  })
}

export function useMoveDept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, parentId, afterId }: { id: string; parentId: string | null; afterId?: string }) =>
      api.put<null>(`/depts/${id}/move`, { parentId, ...(afterId === undefined ? {} : { afterId }) }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['dept'] }),
  })
}

export function countDescendants(node: SysDept): number {
  return (node.children ?? []).reduce((n, c) => n + 1 + countDescendants(c), 0)
}
