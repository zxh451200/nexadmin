import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

// AI 网关：模型接入配置（密钥打码）+ 按租户 token 用量台账。
export type AiKind = 'llm' | 'embedding' | 'rerank' | 'asr' | 'tts' | 'ocr'

export const AI_KINDS: AiKind[] = ['llm', 'embedding', 'rerank', 'asr', 'tts', 'ocr']

export interface AiModel {
  id: string
  kind: AiKind
  name: string
  provider?: string
  baseUrl?: string
  model?: string
  apiKey?: string // ******** 表示已配置
  dimension?: number
  enabled: boolean
  isDefault: boolean
  createdAt: string
  updatedAt: string
}

export interface AiModelForm {
  kind: AiKind
  name: string
  provider?: string
  baseUrl?: string
  model?: string
  apiKey?: string
  dimension?: number
  enabled: boolean
  isDefault: boolean
}

export interface AiUsage {
  id: string
  modelId: number
  kind: AiKind
  provider?: string
  model?: string
  promptTokens: number
  completionTokens: number
  totalTokens: number
  bizType?: string
  bizNo?: string
  createdAt: string
}

export interface AiUsageSummaryRow {
  kind: string
  calls: number
  promptTokens: number
  completionTokens: number
  totalTokens: number
}

export interface ModelQuery extends PageQuery {
  kind?: string
}
export interface UsageQuery extends PageQuery {
  kind?: string
  bizType?: string
}

const BASE = '/plugins/aigateway'
const KEY = ['plugin_aigateway'] as const

export function useModels(query: ModelQuery) {
  return useQuery({
    queryKey: [...KEY, 'models', query],
    queryFn: () => api.get<Page<AiModel>>(`${BASE}/models`, query),
    placeholderData: keepPreviousData,
  })
}

export function useUsage(query: UsageQuery) {
  return useQuery({
    queryKey: [...KEY, 'usage', query],
    queryFn: () => api.get<Page<AiUsage>>(`${BASE}/usage`, query),
    placeholderData: keepPreviousData,
  })
}

export function useUsageSummary() {
  return useQuery({
    queryKey: [...KEY, 'usage-summary'],
    queryFn: () => api.get<AiUsageSummaryRow[]>(`${BASE}/usage/summary`),
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveModel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: AiModelForm }) =>
      id ? api.post<AiModel>(`${BASE}/models/${id}`, data) : api.post<AiModel>(`${BASE}/models`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteModel() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/models/${id}`),
    onSuccess: invalidate,
  })
}

export function usePingModel() {
  return useMutation({
    mutationFn: (id: string) => api.post<{ ok: boolean; status?: number; latencyMs?: number; detail?: string }>(`${BASE}/models/${id}/ping`),
  })
}
