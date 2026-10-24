import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface ScimToken {
  id: string
  name: string
  revoked: boolean
  lastUsed?: string
  createdAt: string
}

const key = ['plugin_scim', 'tokens']

export function useTokens() {
  return useQuery({ queryKey: key, queryFn: () => api.get<{ list: ScimToken[]; total: number }>('/plugins/scim/tokens') })
}

export function useCreateToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => api.post<{ token: string; item: ScimToken }>('/plugins/scim/tokens', { name }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  })
}

export function useRevokeToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`/plugins/scim/tokens/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  })
}
