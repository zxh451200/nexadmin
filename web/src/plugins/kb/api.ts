import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, tokenStore } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

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
  /** keyword = 字面关键词兜底命中（向量分低于阈值但查询词几乎全中）。 */
  match?: 'keyword' | 'vector'
}

export interface KbChunk {
  id: string
  seq: number
  /** 该块的入库文本（与向量一起参与检索的原文）。 */
  content: string
}

export interface KbAnswer {
  answer: string
  hits: KbHit[]
  /** true = 检索未命中，没有调用模型；前端提示走人工。 */
  handoff: boolean
  /** 召回调试：handoff 时不设阈值的 topK 候选——看「最接近的候选差多少分」再调阈值。 */
  candidates?: KbHit[]
}

export interface KbSettings {
  threshold: number
  topK: number
  systemPrompt: string
  /** hybrid = 向量 + 关键词融合（默认）；vector = 只看向量。 */
  mode: 'hybrid' | 'vector'
  /** 单块最大字符数。 */
  chunkSize: number
  /** 相邻块重叠字符数。 */
  chunkOverlap: number
}

/** 全库统计：文档 / 知识块 / 就绪 / 未就绪 / 占用体积（后端聚合，不受列表分页影响）。 */
export interface KbStats {
  docs: number
  chunks: number
  ready: number
  failed: number
  size: number
  /** 未命中问题池里待补的条数。 */
  pendingMiss: number
}

/** 一条「访客问了但知识库答不上来」的问题。 */
export interface KbMiss {
  id: string
  question: string
  /** 同一问题（忽略空白与首尾标点）累计被问的次数。 */
  askCount: number
  status: 'pending' | 'resolved' | 'ignored'
  /** 转成 QA 后生成的文档 id；仅 resolved 有效。 */
  documentId: string
  firstAt: string
  lastAt: string
}

export interface KbStatus {
  embedReady: boolean
  chatReady: boolean
  model?: string
  embedModel?: string
}

const BASE = '/plugins/kb'
const KEY = ['plugin_kb'] as const

/** 文档列表：后端分页 + 状态筛选（status=stale 表示「需重新向量化」）+ 表头排序。 */
export function useDocuments(status?: string, sort?: string, desc?: boolean, page = 1, pageSize = 20) {
  return useQuery({
    queryKey: [...KEY, 'documents', status ?? '', sort ?? '', desc ?? null, page, pageSize],
    queryFn: () =>
      api.get<Page<KbDocument>>(`${BASE}/documents`, {
        status: status || undefined,
        sort,
        desc,
        page,
        pageSize,
      }),
  })
}

/** 全库统计（文档 / 知识块 / 未就绪 / 体积）：页头统计条的数据源。 */
export function useKbStats() {
  return useQuery({
    queryKey: [...KEY, 'stats'],
    queryFn: () => api.get<KbStats>(`${BASE}/stats`),
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

/** 知识块预览：一篇文档切出来的全部块（按切分顺序），排查「AI 为什么这么答」用。 */
export function useDocumentChunks(id: string | null) {
  return useQuery({
    queryKey: [...KEY, 'documents', id, 'chunks'],
    queryFn: () =>
      api.get<{ document: KbDocument; chunks: KbChunk[] }>(`${BASE}/documents/${id}/chunks`),
    enabled: !!id,
  })
}

/** 重命名：只改展示标题，切块与向量不动（无需重新向量化）。 */
export function useRenameDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api.put<KbDocument>(`${BASE}/documents/${id}`, { title }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 按当前分块参数预览重切结果（不落库，只看切出来长什么样）。 */
export function useRechunkPreview() {
  return useMutation({
    mutationFn: (id: string) =>
      api.get<{ document: KbDocument; chunks: string[]; count: number }>(`${BASE}/documents/${id}/rechunk`),
  })
}

/** 按当前分块参数重切并重新向量化（替换该文档全部切块）。 */
export function useRechunkDocument() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<KbDocument>(`${BASE}/documents/${id}/rechunk`),
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

/**
 * 从文件库选一篇已上传文档建知识库文档：后端按 fileId 读内容，走与直传完全相同的
 * 格式校验与文本抽取管线，不重复占存储。库里的文件名即默认标题。
 */
export function uploadDocumentFromLibrary(fileId: string, title?: string): Promise<KbDocument> {
  return api.post<KbDocument>(`${BASE}/documents/from-file`, { fileId, title: title || undefined })
}

// ===== 未命中问题池 =====

/** 问题池列表：后端分页 + 状态筛选 + 排序，默认按被问次数降序。 */
export function useMisses(status?: string, sort?: string, desc?: boolean, page = 1, pageSize = 20) {
  return useQuery({
    queryKey: [...KEY, 'misses', status ?? '', sort ?? '', desc ?? null, page, pageSize],
    queryFn: () =>
      api.get<Page<KbMiss>>(`${BASE}/misses`, {
        status: status || undefined,
        sort,
        desc,
        page,
        pageSize,
      }),
  })
}

/** 补一条答案并转成 QA 文档：复用入库管线，转完问题项标为已解决。 */
export function useResolveMiss() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, answer }: { id: string; answer: string }) =>
      api.post<KbDocument>(`${BASE}/misses/${id}/resolve`, { answer }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 批量忽略（后端只改 pending 的项，返回实际改动条数）。 */
export function useIgnoreMisses() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.post<{ count: number }>(`${BASE}/misses/ignore`, { ids }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 把已忽略 / 已解决的项放回待补队列。 */
export function useReopenMiss() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<KbMiss>(`${BASE}/misses/${id}/reopen`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 删除问题项；已转出的 QA 文档不受影响。 */
export function useDeleteMiss() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/misses/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}
