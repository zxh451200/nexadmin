import { useMutation, useQuery } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'

export type Kind = 'string' | 'text' | 'int' | 'float' | 'bool' | 'date' | 'datetime' | 'id' | ''
export type Control = 'input' | 'textarea' | 'number' | 'switch' | 'date' | 'datetime'

export interface TableInfo {
  name: string
}

export interface ColumnConfig {
  name: string
  list: boolean
  query: boolean
  form: boolean
  required: boolean
  control: Control | ''
}

export interface ColumnInfo {
  name: string
  type: string
  kind: Kind
  nullable: boolean
  primaryKey: boolean
  comment: string
  system: boolean
  supported: boolean
  reason?: string
  length?: number
  config: ColumnConfig
}

export interface TableSchema {
  table: string
  shape: 'tenant' | 'platform' | 'plain'
  tenant: boolean
  suggestedModule: string
  columns: ColumnInfo[]
}

export interface GenRequest {
  table: string
  module: string
  goModule?: string
  columns: ColumnConfig[]
}

export interface GenFile {
  path: string
  content: string
  lang: 'go' | 'ts' | 'tsx' | 'json'
}

export interface GenNote {
  code: 'import_line' | 'api_prefix' | 'null_zero'
  value: string
}

export interface GenOutput {
  files: GenFile[]
  notes: GenNote[]
}

const BASE = '/plugins/codegen'
const KEY = ['plugin_codegen'] as const

/** 与服务端一致的白名单，提交前先在前端拦一遍（服务端仍会再校验）。 */
export const MODULE_RE = /^[a-z][a-z0-9]{0,30}$/
export const GO_MODULE_RE = /^[a-z0-9][a-z0-9._~-]*(\/[a-z0-9][a-z0-9._~-]*){0,6}$/

export function useTables() {
  return useQuery({
    queryKey: [...KEY, 'tables'],
    queryFn: () => api.get<TableInfo[]>(`${BASE}/tables`),
  })
}

export function useColumns(table?: string) {
  return useQuery({
    queryKey: [...KEY, 'columns', table],
    queryFn: () => api.get<TableSchema>(`${BASE}/tables/${encodeURIComponent(table ?? '')}/columns`),
    enabled: !!table,
  })
}

export function usePreview() {
  return useMutation({ mutationFn: (req: GenRequest) => api.post<GenOutput>(`${BASE}/preview`, req) })
}

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

/**
 * 下载 zip：走带令牌的 XHR（浏览器直接打开链接不会带 Authorization）。
 * mock 模式下没有真实二进制，返回的是文件列表，这里退化为把所有文件拼成一个 .txt 下载。
 */
export async function downloadZip(req: GenRequest): Promise<void> {
  const res = await http.post<unknown>(`${BASE}/download`, req, { responseType: 'blob', timeout: 60_000 })
  if (res.data instanceof Blob) {
    saveBlob(res.data, `${req.module}-codegen.zip`)
    return
  }
  const files = (res.data as GenOutput).files ?? []
  const text = files.map((f) => `===== ${f.path} =====\n${f.content}`).join('\n\n')
  saveBlob(new Blob([text], { type: 'text/plain' }), `${req.module}-codegen.txt`)
}

export function useDownload() {
  return useMutation({ mutationFn: downloadZip })
}

/** 与服务端一致：哪些类型支持作为查询条件 / 必填，以及每种类型允许的表单控件（第一个是默认）。 */
export const QUERYABLE: Kind[] = ['string', 'int', 'id', 'bool', 'date']
export const REQUIRABLE: Kind[] = ['string', 'text', 'date', 'datetime']
export const CONTROLS: Record<string, Control[]> = {
  string: ['input', 'textarea'],
  text: ['textarea', 'input'],
  int: ['number', 'input'],
  float: ['number', 'input'],
  id: ['input'],
  bool: ['switch'],
  date: ['date'],
  datetime: ['datetime'],
}
