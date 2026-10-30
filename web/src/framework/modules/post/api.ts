import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface SysPost {
  id: string
  name: string
  code: string
  deptId?: string
  sort: number
  status: '1' | '0'
  remark?: string
  createdAt?: string
  updatedAt?: string
}

export type SysPostInput = Omit<SysPost, 'id' | 'createdAt' | 'updatedAt'>

export function usePostList() {
  return useQuery({ queryKey: ['post', 'list'], queryFn: () => api.get<SysPost[]>('/posts') })
}

export interface DeptNode {
  id: string
  name: string
  children?: DeptNode[]
}

/** 仅用于把部门 ID 显示为名称；没有 dept:list 时不请求（否则 403 弹一脸），列退化为「-」 */
export function useDeptNodes(enabled = true) {
  return useQuery({ queryKey: ['post', 'deptTree'], queryFn: () => api.get<DeptNode[]>('/depts/tree'), enabled })
}

export function useCreatePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SysPostInput) => api.post<SysPost>('/posts', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['post'] }),
  })
}

export function useUpdatePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: SysPostInput & { id: string }) => api.put<SysPost>(`/posts/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['post'] }),
  })
}

export function useDeletePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/posts/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['post'] }),
  })
}

export function useSortPosts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.put<null>('/posts/sort', { parentId: null, ids }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['post'] }),
  })
}
