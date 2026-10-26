import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, tokenStore } from '@/framework/request/http'
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

export function templateUrl(source: string) {
  return `/api/v1/plugins/imports/template?source=${encodeURIComponent(source)}`
}

export function errorsCSVUrl(id: string) {
  return `/api/v1/plugins/imports/${id}/errors.csv`
}
