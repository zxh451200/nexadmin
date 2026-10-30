import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type ColType = 'string' | 'number' | 'time' | 'bool'
export type Agg = 'count' | 'sum' | 'avg' | 'min' | 'max'
export type Op = '=' | '!=' | '>' | '<' | '>=' | '<=' | 'in' | 'like' | 'between'
export type ChartType = 'bar' | 'line' | 'pie' | 'table' | 'metric'
export type Bucket = '' | 'day' | 'month' | 'year'

export const OPS: Op[] = ['=', '!=', '>', '<', '>=', '<=', 'in', 'like', 'between']
export const AGGS: Agg[] = ['count', 'sum', 'avg', 'min', 'max']
export const CHART_TYPES: ChartType[] = ['bar', 'line', 'pie', 'table', 'metric']

export interface SourceCol {
  name: string
  label: string
  type: ColType
}
export interface Source {
  table: string
  label: string
  columns: SourceCol[]
}
export interface Filter {
  column: string
  op: Op
  value: unknown
}

export interface Dataset {
  id: string
  name: string
  source: string
  columns: string[]
  filters: Filter[]
  remark: string
  /** 编辑器下拉接口附带：所选列的元数据（label/type），图表与报表编辑者无需读取完整白名单 */
  fields?: SourceCol[]
  createdAt: string
  updatedAt: string
}
export type DatasetForm = Pick<Dataset, 'name' | 'source' | 'columns' | 'filters' | 'remark'>

export interface ChartConfig {
  dimensions: { column: string; bucket?: Bucket }[]
  measures: { agg: Agg; column?: string; label?: string }[]
  filters: Filter[]
  orderBy: { ref: string; desc?: boolean }[]
  limit: number
  colors: string[]
  stacked?: boolean
  unit?: string
}
export interface Chart {
  id: string
  name: string
  datasetId: string
  type: ChartType
  config: ChartConfig
  remark: string
  createdAt: string
  updatedAt: string
}
export type ChartForm = Pick<Chart, 'name' | 'datasetId' | 'type' | 'config' | 'remark'>

export interface ResultCol {
  key: string
  label: string
  kind: 'dim' | 'measure'
  type: ColType
  /** 结果列来自白名单的哪一列（维度=分组列，度量=被聚合列）；列名按「表+列」查字典才不串义 */
  field?: string
}
export interface QueryResult {
  columns: ResultCol[]
  /** 查询命中的白名单表名，列标题据此取字典 */
  source?: string
  rows: Record<string, string | number | boolean | null>[]
  truncated: boolean
}

export interface ReportItem {
  chartId: string
  group: string
  span: number
}
export interface ReportFilter {
  key: string
  label: string
  type: 'dateRange' | 'select'
  datasetId: string
  column: string
  multiple?: boolean
}
export interface ReportConfig {
  items: ReportItem[]
  filters: ReportFilter[]
}
export interface Report {
  id: string
  name: string
  groupName: string
  remark: string
  config: ReportConfig
  charts?: Chart[]
  createdAt: string
  updatedAt: string
}
export type ReportForm = Pick<Report, 'name' | 'groupName' | 'remark' | 'config'>

export interface ScreenItem {
  chartId: string
  x: number
  y: number
  w: number
  h: number
  title?: string
}
export interface ScreenConfig {
  items: ScreenItem[]
  refreshSec: number
  title?: string
}
export interface Screen {
  id: string
  name: string
  remark: string
  config: ScreenConfig
  roleIds?: string[]
  charts?: Chart[]
  createdAt: string
  updatedAt: string
}
export interface ScreenForm {
  name: string
  remark: string
  config: ScreenConfig
  roleIds: string[]
}
export interface ScreenBrief {
  id: string
  name: string
  remark: string
}
export interface RoleOption {
  id: string
  name: string
}
export interface Overview {
  reports: number
  screens: number
}

export type FilterValues = Record<string, unknown>

const BASE = '/plugins/report'
const KEY = ['plugin_report'] as const
const STALE = 30_000

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

// ---- 数据集 ----
export const useSources = () =>
  useQuery({ queryKey: [...KEY, 'sources'], queryFn: () => api.get<Source[]>(`${BASE}/sources`), staleTime: 5 * 60_000 })

export const useDatasets = (q: PageQuery) =>
  useQuery({
    queryKey: [...KEY, 'datasets', q],
    queryFn: () => api.get<Page<Dataset>>(`${BASE}/datasets`, q),
    placeholderData: keepPreviousData,
  })

export const useDatasetOptions = (scope: 'chart' | 'report', enabled = true) =>
  useQuery({
    queryKey: [...KEY, 'dataset-options', scope],
    queryFn: () => api.get<Dataset[]>(`${BASE}/${scope}-options/datasets`),
    staleTime: STALE,
    enabled,
  })

export function useSaveDataset() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: DatasetForm }) =>
      id ? api.put<Dataset>(`${BASE}/datasets/${id}`, data) : api.post<Dataset>(`${BASE}/datasets`, data),
    onSuccess: invalidate,
  })
}
export function useDeleteDataset() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/datasets/${id}`), onSuccess: invalidate })
}
export const previewDataset = (data: DatasetForm) => api.post<QueryResult>(`${BASE}/datasets/preview`, data)

// ---- 图表 ----
export const useCharts = (q: PageQuery) =>
  useQuery({
    queryKey: [...KEY, 'charts', q],
    queryFn: () => api.get<Page<Chart>>(`${BASE}/charts`, q),
    placeholderData: keepPreviousData,
  })

export const useChartOptions = (scope: 'report' | 'screen', enabled = true) =>
  useQuery({
    queryKey: [...KEY, 'chart-options', scope],
    queryFn: () => api.get<Chart[]>(`${BASE}/${scope}-options/charts`),
    staleTime: STALE,
    enabled,
  })

export function useSaveChart() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: ChartForm }) =>
      id ? api.put<Chart>(`${BASE}/charts/${id}`, data) : api.post<Chart>(`${BASE}/charts`, data),
    onSuccess: invalidate,
  })
}
export function useDeleteChart() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/charts/${id}`), onSuccess: invalidate })
}
export const previewChart = (data: ChartForm) => api.post<QueryResult>(`${BASE}/charts/preview`, data)
export const queryChart = (id: string) => api.post<QueryResult>(`${BASE}/charts/${id}/query`)

// ---- 报表 ----
/** 本租户的报表列表（查看与管理共用，不含图表详情） */
export const useReportList = (q: PageQuery = {}) =>
  useQuery({
    queryKey: [...KEY, 'reports', q],
    queryFn: () => api.get<Page<Report>>(`${BASE}/view/reports`, q),
    placeholderData: keepPreviousData,
  })

export const useReport = (id?: string, scope: 'manage' | 'view' = 'view') =>
  useQuery({
    queryKey: [...KEY, 'report', scope, id],
    queryFn: () => api.get<Report>(scope === 'view' ? `${BASE}/view/reports/${id}` : `${BASE}/reports/${id}`),
    enabled: !!id,
  })

export function useSaveReport() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: ReportForm }) =>
      id ? api.put<Report>(`${BASE}/reports/${id}`, data) : api.post<Report>(`${BASE}/reports`, data),
    onSuccess: invalidate,
  })
}
export function useDeleteReport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/reports/${id}`),
    // 只失效列表：被删报表的详情/图表查询仍挂在查看器上，整域失效会让它们对已删 id 立刻重取并 404
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, 'reports'] }),
  })
}

export const queryReportChart = (reportId: string, chartId: string, filters: FilterValues) =>
  api.post<QueryResult>(`${BASE}/view/reports/${reportId}/charts/${chartId}/query`, { filters })

export const useFilterOptions = (reportId: string, key: string) =>
  useQuery({
    queryKey: [...KEY, 'filter-options', reportId, key],
    queryFn: () => api.post<(string | number)[]>(`${BASE}/view/reports/${reportId}/filters/${key}/options`, {}),
    staleTime: STALE,
  })

/**
 * 导出表格图表：真实后端返回 xlsx 文件(Blob)；mock 阶段没有二进制，返回结果集 JSON，这里退化成 CSV 文本。
 * labels 是前端按当前语言渲染好的表名与列标题：内置图表存的是 i18n 键、列名来自白名单元数据，
 * 服务端词典里没有这些词条，直接落文件就会把键名当表头导出。
 */
export async function exportReportChart(
  reportId: string,
  chartId: string,
  filters: FilterValues,
  labels?: { name?: string; headers?: string[] },
): Promise<{ blob: Blob; truncated: boolean; ext: 'xlsx' | 'csv' }> {
  const res = await http.post<unknown>(
    `${BASE}/view/reports/${reportId}/charts/${chartId}/export`,
    { filters, name: labels?.name ?? '', headers: labels?.headers ?? [] },
    { responseType: 'blob', timeout: 60_000 },
  )
  if (res.data instanceof Blob) return { blob: res.data, truncated: String(res.headers?.['x-report-truncated'] ?? '') === '1', ext: 'xlsx' }
  const body = res.data as QueryResult
  const esc = (v: unknown) => `"${String(v ?? '').replace(/"/g, '""')}"`
  const lines = [body.columns.map((c, i) => esc(labels?.headers?.[i] || c.label)).join(','), ...body.rows.map((r) => body.columns.map((c) => esc(r[c.key])).join(','))]
  return { blob: new Blob(['\uFEFF', lines.join('\n')], { type: 'text/csv;charset=utf-8' }), truncated: !!body.truncated, ext: 'csv' }
}

// ---- 大屏 ----
/** 当前账号可见的大屏 */
export const useScreenList = () =>
  useQuery({ queryKey: [...KEY, 'screens', 'view'], queryFn: () => api.get<ScreenBrief[]>(`${BASE}/view/screens`), placeholderData: keepPreviousData })

export const useScreen = (id?: string, scope: 'manage' | 'view' = 'view') =>
  useQuery({
    queryKey: [...KEY, 'screen', scope, id],
    queryFn: () => api.get<Screen>(scope === 'view' ? `${BASE}/view/screens/${id}` : `${BASE}/screens/${id}`),
    enabled: !!id,
  })

export const useRoles = (enabled = true) =>
  useQuery({ queryKey: [...KEY, 'roles'], queryFn: () => api.get<RoleOption[]>(`${BASE}/screen-options/roles`), enabled, staleTime: STALE })

export function useSaveScreen() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: ScreenForm }) =>
      id ? api.put<Screen>(`${BASE}/screens/${id}`, data) : api.post<Screen>(`${BASE}/screens`, data),
    onSuccess: invalidate,
  })
}
export function useDeleteScreen() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/screens/${id}`), onSuccess: invalidate })
}
export const queryScreenChart = (screenId: string, chartId: string) =>
  api.post<QueryResult>(`${BASE}/view/screens/${screenId}/charts/${chartId}/query`)

export const useOverview = (refetchMs: number | false) =>
  // silent：插件未挂载 / 被停用时接口返回 404「插件未启用」，工作台卡片直接隐藏，不弹全局错误
  useQuery({
    queryKey: [...KEY, 'overview'],
    queryFn: () => api.get<Overview>(`${BASE}/overview`, undefined, { silent: true }),
    refetchInterval: refetchMs,
  })
