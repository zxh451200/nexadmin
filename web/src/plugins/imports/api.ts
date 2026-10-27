import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http, tokenStore } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export interface ImportColumn {
  key: string
  label: string
  required: boolean
  example: string
}

export interface ImportSourceMeta {
  name: string
  title: string
  columns: ImportColumn[]
}

export interface ImportRowError {
  row: number
  key: string
  message: string
}

export interface ImportJob {
  id: string
  source: string
  fileName: string
  totalRows: number
  okRows: number
  failRows: number
  status: 'pending' | 'running' | 'done' | 'failed'
  durationMs: number
  errors?: ImportRowError[]
  createdBy: string
  createdAt: string
}

export interface ImportJobQuery extends PageQuery {
  source?: string
  status?: string
}

const BASE = '/plugins/imports'
const KEY = ['plugin_imports'] as const

export function useSources() {
  return useQuery({
    queryKey: [...KEY, 'sources'],
    queryFn: () => api.get<ImportSourceMeta[]>(`${BASE}/sources`),
    staleTime: 5 * 60_000,
  })
}

export function useJobs(query: ImportJobQuery) {
  return useQuery({
    queryKey: [...KEY, 'jobs', query],
    queryFn: () => api.get<Page<ImportJob>>(`${BASE}`, query),
    placeholderData: keepPreviousData,
  })
}

export function useDeleteImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useRunImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: { source: string; file: File }) => {
      const form = new FormData()
      form.append('source', input.source)
      form.append('file', input.file)
      const res = await fetch('/api/v1/plugins/imports', {
        method: 'POST',
        headers: { Authorization: `Bearer ${tokenStore.access ?? ''}` },
        body: form,
      })
      const json = await res.json()
      if (json.code !== 0) throw new Error(json.message || 'import failed')
      return json.data as ImportJob
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

export type TemplateFormat = 'csv' | 'xlsx'

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

// 下载接口要带 Bearer 令牌，window.open 拿不到，只能走 axios 取 Blob 再触发保存。
// 文件名优先取服务端 Content-Disposition（模板格式由服务端决定，避免名实不符）。
async function download(url: string, params?: Record<string, string>, fallbackName = 'download') {
  const res = await http.get<unknown>(url, { params, responseType: 'blob' })
  const disposition = String((res.headers as Record<string, unknown> | undefined)?.['content-disposition'] ?? '')
  const match = /filename="?([^";]+)"?/i.exec(disposition)
  const name = match ? match[1] : fallbackName
  const blob = res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/plain;charset=utf-8' })
  saveBlob(blob, name)
}

export function downloadTemplate(source: string, format: TemplateFormat) {
  return download(`${BASE}/template`, { source, format }, `import-template-${source}.${format}`)
}

export function downloadErrorsCsv(id: string) {
  return download(`${BASE}/${id}/errors.csv`, undefined, `import-errors-${id}.csv`)
}
