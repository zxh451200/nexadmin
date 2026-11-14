import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, tokenStore } from '@/framework/request/http'

export interface KbDocument {
  id: string
  title: string
  sourceName: string
  mime: string
  size: number
  status: 'ready' | 'failed'
  chunkCount: number
  /** 向量化时的 embedding 模型；与当前配置不一致时列表提示「需重新向量化」。 */
  model: string
  dimension: number
  error?: string
  createdAt: string
}

export interface KbHit {
  documentId: string
  title: string
  excerpt: string
  score: number
}

export interface KbAnswer {
  answer: string
  hits: KbHit[]
  /** true = 检索未命中，没有调用模型；前端提示走人工。 */
  handoff: boolean
}

export interface KbSettings {
  threshold: number
  topK: number
  systemPrompt: string
}

export interface KbStatus {
  embedReady: boolean
  chatReady: boolean
  model?: string
  embedModel?: string
}

const BASE = '/plugins/kb'
const KEY = ['plugin_kb'] as const

export function useDocuments() {
  return useQuery({
    queryKey: [...KEY, 'documents'],
    queryFn: () => api.get<KbDocument[]>(`${BASE}/documents`),
  })
}

export function useKbStatus() {
  return useQuery({
    queryKey: [...KEY, 'status'],
    queryFn: () => api.get<KbStatus>(`${BASE}/status`),
  })
}

export function useKbSettings() {
  return useQuery({
    queryKey: [...KEY, 'settings'],
    queryFn: () => api.get<KbSettings>(`${BASE}/settings`),
  })
}

export function useSaveKbSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: KbSettings) => api.put<KbSettings>(`${BASE}/settings`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useDeleteDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/documents/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useReembedDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<KbDocument>(`${BASE}/documents/${id}/reembed`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 测试提问：不走 IM，直接看检索命中与模型回答。 */
export function useAsk() {
  return useMutation({
    mutationFn: (query: string) => api.post<KbAnswer>(`${BASE}/ask`, { query }),
  })
}

/**
 * 上传文档（multipart）。与导入中心的 runImport 同款走 fetch：axios 实例默认 JSON 序列化，
 * 文件上传需要 FormData 直传 + 手动带 token。
 */
export function uploadDocument(file: File, title?: string): Promise<KbDocument> {
  const form = new FormData()
  form.append('file', file)
  if (title) form.append('title', title)
  return fetch('/api/v1/plugins/kb/documents', {
    method: 'POST',
    headers: { Authorization: `Bearer ${tokenStore.access ?? ''}` },
    body: form,
  }).then(async (res) => {
    const json = await res.json()
    if (json.code !== 0) throw new Error(json.message || 'upload failed')
    return json.data as KbDocument
  })
}
