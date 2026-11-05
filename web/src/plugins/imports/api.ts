import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { collectAllPages } from '@/framework/components/export/ExportCsvButton'
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
  /** 失败行的原始单元格；台账超预算时会被剥掉，剥掉的行不进待修复 CSV。 */
  data?: Record<string, string>
}

export interface ImportJob {
  id: string
  source: string
  fileName: string
  totalRows: number
  okRows: number
  failRows: number
  status: 'pending' | 'running' | 'done' | 'failed' | 'cancelled'
  durationMs: number
  /** 失败明细只随单条台账（GET /:id）返回；列表接口为控制载荷不带它。 */
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
    // 大文件导入是异步落库的：pending/running 未完成前定时刷新台账，
    // 否则用户只能手动刷新才能看到进度与结果。
    refetchInterval: (q) =>
      (q.state.data?.list ?? []).some((j) => j.status === 'pending' || j.status === 'running') ? 3000 : false,
  })
}

export function useDeleteImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/**
 * 批量删除台账：一次提交点名的 id（DELETE /plugins/imports?ids=）。
 * 后端与单条同口径——排队中/执行中的任务不允许删，且批次里有这种任务就整批失败；
 * 越权或不存在的 id 不计入实删数，所以 deleted 可能小于提交的条数。
 * 路径写死成字面量：scripts/contract_check 靠静态解析核对前后端契约。
 */
export function useDeleteImports() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/plugins/imports', { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 按当前筛选导出台账：走列表同一个分页接口逐页捞全，不新增后端端点。 */
export function fetchJobsForExport(query: Omit<ImportJobQuery, 'page' | 'pageSize'>) {
  return collectAllPages<ImportJob>((page, pageSize) => api.get<Page<ImportJob>>(BASE, { ...query, page, pageSize }))
}

/** 取消运行中/排队的异步导入：已处理的部分保留，状态记为 cancelled。 */
export function useCancelImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<ImportJob>(`${BASE}/${id}/cancel`),
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

/** 待修复 CSV：失败行按模板口径原样导出，修正单元格后可直接再次上传导入。 */
export function downloadRetryCsv(id: string) {
  return download(`${BASE}/${id}/retry.csv`, undefined, `import-retry-${id}.csv`)
}
