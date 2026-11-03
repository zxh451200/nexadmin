import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'

export type Kind = 'string' | 'text' | 'int' | 'float' | 'bool' | 'date' | 'datetime' | 'id' | ''
export type Control = 'input' | 'textarea' | 'number' | 'switch' | 'date' | 'datetime' | 'dict'
export type FieldKind = 'string' | 'text' | 'int' | 'float' | 'bool' | 'date' | 'datetime'

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
  /** 字典编码（control = dict 时必填）：选项在运行时从 /dicts/:code/items 按语言取 */
  dict?: string
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
  /** 推荐配置携带的字典编码（仅新建表定义了 dict 字段时非空） */
  dict?: string
  config: ColumnConfig
}

export interface TableSchema {
  table: string
  shape: 'tenant' | 'platform' | 'plain'
  tenant: boolean
  /** 树形表：有 parent_id 自引用列，生成树形列表与父级下拉 */
  tree: boolean
  suggestedModule: string
  columns: ColumnInfo[]
}

/** 主子表模式的子表：table 必须已存在，foreignKey 是子表里指向主表的 BIGINT *_id 列。 */
export interface SubTableSpec {
  table: string
  foreignKey: string
  columns: ColumnConfig[]
}

export interface GenRequest {
  table?: string
  module: string
  goModule?: string
  columns: ColumnConfig[]
  define?: TableDef
  sub?: SubTableSpec
}

/** 新建表的一列。length 只对 string 生效；required 只对可判空的类型生效；dict 给了时固定 VARCHAR(64)。 */
export interface FieldDef {
  name: string
  kind: FieldKind
  length?: number
  required: boolean
  comment?: string
  /** 字典编码：非空表示这是字典下拉列，选项在系统字典里维护 */
  dict?: string
}

/** 新建表定义：生成物附带 mysql / mariadb / postgres 三份迁移，插件本身不建表。 */
export interface TableDef {
  name: string
  tenant: boolean
  softDelete: boolean
  migration: string
  fields: FieldDef[]
}

export interface GenFile {
  path: string
  content: string
  lang: 'go' | 'ts' | 'tsx' | 'json' | 'sql'
}

export interface GenNote {
  code: 'import_line' | 'api_prefix' | 'null_zero' | 'migration'
  value?: string
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
export const DICT_CODE_RE = /^[a-z][a-z0-9_]{0,47}$/

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

/** 已有表与新建表共用：新建表时返回按定义合成的列与推荐配置。 */
export function useSchema() {
  return useMutation({
    mutationFn: (req: { table: string } | { define: TableDef }) => api.post<TableSchema>(`${BASE}/schema`, req),
  })
}

export function usePreview() {
  return useMutation({ mutationFn: (req: GenRequest) => api.post<GenOutput>(`${BASE}/preview`, req) })
}

export function saveBlob(blob: Blob, filename: string) {
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

/** 直写项目回执：written 是已写入的文件，skipped 是因已存在被跳过的（overwrite=false 时）。 */
export interface InstallResult {
  written: string[]
  skipped: string[]
}

/** 写入项目根目录：后端进程工作目录（可用 NEXADMIN_ROOT 指定），默认不覆盖已有文件。 */
export function useInstall() {
  return useMutation({ mutationFn: (req: GenRequest & { overwrite: boolean }) => api.post<InstallResult>(`${BASE}/install`, req) })
}

/** 保存下来的生成配置：载入后向导恢复到保存时的状态，继续改。 */
export interface SavedGenConfig {
  id: string
  name: string
  module: string
  goModule: string
  table: string
  define?: TableDef
  columns?: ColumnConfig[]
  sub?: SubTableSpec
}

export function useConfigs() {
  return useQuery({
    queryKey: [...KEY, 'configs'],
    queryFn: () => api.get<SavedGenConfig[]>(`${BASE}/configs`),
  })
}

export interface SaveConfigReq {
  name: string
  module: string
  goModule?: string
  table?: string
  define?: TableDef
  columns?: ColumnConfig[]
  sub?: SubTableSpec
}

/** 按名字保存（同名即更新）；只存配置不存生成物。 */
export function useSaveConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: SaveConfigReq) => api.post<SavedGenConfig>(`${BASE}/configs`, req),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'configs'] }),
  })
}

export function useDeleteConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/configs/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'configs'] }),
  })
}

/** 批量删除保存的配置：deleted 是后端实删条数（已被别人删掉 / 非法的 id 不计入，一条都没删到时报 404）。
 *  路径写静态字面量：contract_check 靠静态解析这里的第一个实参对照后端路由。 */
export function useDeleteConfigs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>('/plugins/codegen/configs', { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'configs'] }),
  })
}

/** 与服务端一致：哪些类型支持作为查询条件 / 必填，以及每种类型允许的表单控件（第一个是默认）。 */
export const QUERYABLE: Kind[] = ['string', 'int', 'id', 'bool', 'date']
export const REQUIRABLE: Kind[] = ['string', 'text', 'date', 'datetime']
export const CONTROLS: Record<string, Control[]> = {
  string: ['input', 'textarea', 'dict'],
  text: ['textarea', 'input'],
  int: ['number', 'input'],
  float: ['number', 'input'],
  id: ['input'],
  bool: ['switch'],
  date: ['date'],
  datetime: ['datetime'],
}
