import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { Chart, ChartConfig, ChartForm, Dataset, Filter, FilterValues, Report, ReportConfig, ReportForm, Screen, ScreenForm, ScreenItem } from './api'
import { MOCK_SOURCES, runSpec } from './mockEngine'

const now = () => new Date().toISOString()
const stamp = { createdAt: now(), updatedAt: now() }
const id = (n: number) => String(7430000000000000000n + BigInt(n))
const cfg = (p: Partial<ChartConfig>): ChartConfig => ({ dimensions: [], measures: [], filters: [], orderBy: [], limit: 1000, colors: [], ...p })

const logCols = ['job_name', 'handler', 'status', 'attempts', 'cost_ms', 'started_at', 'trigger_type']
let datasets: Dataset[] = [
  { id: id(1), name: '任务执行日志', source: 'plg_cron_log', columns: logCols, filters: [], remark: '定时任务的每次执行', ...stamp },
  { id: id(2), name: '登录日志', source: 'sys_login_log', columns: ['login_type', 'status', 'browser', 'os', 'created_at'], filters: [], remark: '', ...stamp },
]
let charts: Chart[] = [
  { id: id(11), name: '执行结果占比', datasetId: id(1), type: 'pie', remark: '', ...stamp, config: cfg({ dimensions: [{ column: 'status' }], measures: [{ agg: 'count' }], colors: ['success', 'destructive'] }) },
  { id: id(12), name: '近 30 天执行趋势', datasetId: id(1), type: 'line', remark: '', ...stamp, config: cfg({ dimensions: [{ column: 'started_at', bucket: 'day' }], measures: [{ agg: 'count' }], colors: ['primary'] }) },
  { id: id(13), name: '各任务平均耗时', datasetId: id(1), type: 'bar', remark: '', ...stamp, config: cfg({ dimensions: [{ column: 'job_name' }], measures: [{ agg: 'avg', column: 'cost_ms' }], colors: ['chart-2'], unit: 'ms' }) },
  { id: id(14), name: '失败明细', datasetId: id(1), type: 'table', remark: '', ...stamp, config: cfg({ dimensions: [{ column: 'job_name' }, { column: 'handler' }, { column: 'started_at' }], filters: [{ column: 'status', op: '=', value: 'failed' }], orderBy: [{ ref: 'd2', desc: true }], limit: 500 }) },
  { id: id(15), name: '执行总数', datasetId: id(1), type: 'metric', remark: '', ...stamp, config: cfg({ measures: [{ agg: 'count' }], colors: ['chart-1'], unit: '次' }) },
  { id: id(16), name: '登录方式分布', datasetId: id(2), type: 'pie', remark: '', ...stamp, config: cfg({ dimensions: [{ column: 'login_type' }], measures: [{ agg: 'count' }] }) },
]
const reportCfg: ReportConfig = {
  items: [
    { chartId: id(15), group: '概览', span: 3 }, { chartId: id(11), group: '概览', span: 4 }, { chartId: id(12), group: '概览', span: 5 },
    { chartId: id(13), group: '明细', span: 6 }, { chartId: id(14), group: '明细', span: 12 },
  ],
  filters: [
    { key: 'f1', label: '执行时间', type: 'dateRange', datasetId: id(1), column: 'started_at' },
    { key: 'f2', label: '处理器', type: 'select', datasetId: id(1), column: 'handler', multiple: true },
  ],
}
let reports: Report[] = [{ id: id(21), name: '定时任务运行报表', groupName: '运维', remark: '执行趋势与失败明细', config: reportCfg, ...stamp }]
let screens: Screen[] = [
  {
    id: id(31), name: '运维大屏', remark: '定时任务与登录概览', roleIds: [], ...stamp,
    config: {
      refreshSec: 30, title: '运维态势',
      items: [
        { chartId: id(15), x: 0, y: 0, w: 6, h: 4 }, { chartId: id(11), x: 6, y: 0, w: 8, h: 8 }, { chartId: id(16), x: 14, y: 0, w: 10, h: 8 },
        { chartId: id(12), x: 0, y: 8, w: 14, h: 8 }, { chartId: id(13), x: 14, y: 8, w: 10, h: 8 },
      ],
    },
  },
]
const ROLES = [{ id: id(101), name: '管理员' }, { id: id(102), name: '运维' }, { id: id(103), name: '普通用户' }]

const need = <T extends { id: string }>(list: T[], key: string) => list.find((x) => x.id === key) ?? mockError(404, '记录不存在')
const fieldsOf = (d: Dataset) => (MOCK_SOURCES.find((s) => s.table === d.source)?.columns ?? []).filter((c) => d.columns.includes(c.name))
const withCharts = <T extends { config: { items: { chartId: string }[] } }>(x: T) => ({ ...x, charts: charts.filter((c) => x.config.items.some((i) => i.chartId === c.id)) })

/** 报表筛选器取值 -> 查询过滤（与后端 runtimeFilters 一致：key 只能命中报表配置的筛选器） */
function runtime(c: ReportConfig, datasetId: string, vals: FilterValues): Filter[] {
  const out: Filter[] = []
  for (const f of c.filters) {
    const v = vals?.[f.key]
    if (f.datasetId !== datasetId || v == null || v === '') continue
    if (f.type === 'dateRange' && Array.isArray(v)) {
      if (v[0]) out.push({ column: f.column, op: '>=', value: v[0] })
      if (v[1]) out.push({ column: f.column, op: '<=', value: `${v[1]}T23:59:59Z` })
    } else if (Array.isArray(v)) {
      if (v.length) out.push({ column: f.column, op: 'in', value: v })
    } else {
      out.push({ column: f.column, op: '=', value: v })
    }
  }
  return out
}
function exec(ch: Chart, extra: Filter[] = [], maxRows = 5000) {
  const ds = need(datasets, ch.datasetId)
  try {
    return runSpec(ds.source, ds.columns, ch.config, extra, ds.filters, maxRows)
  } catch {
    return mockError(400, '数据源或字段不可用')
  }
}
const page = <T,>(rows: T[], q: Record<string, unknown>) => {
  const p = Math.max(1, Number(q.page) || 1)
  const s = Math.max(1, Number(q.pageSize) || 10)
  return { list: rows.slice((p - 1) * s, p * s), total: rows.length, page: p, pageSize: s }
}
const kw = (q: Record<string, unknown>) => String(q.keyword ?? '').trim().toLowerCase()
const chartErr = (b: Partial<ChartForm>) => {
  if (!b?.name?.trim() || !b.config) mockError(400, '名称不能为空')
  const ds = datasets.find((d) => d.id === b.datasetId) ?? mockError(400, '请选择数据集')
  try {
    runSpec(ds.source, ds.columns, b.config as ChartConfig, [], ds.filters)
  } catch {
    mockError(400, '数据源或字段不可用')
  }
}

const reportErr = (b: Partial<ReportForm>) => {
  if (!b?.name?.trim()) mockError(400, '名称不能为空')
  for (const it of b.config?.items ?? []) if (!charts.some((c) => c.id === it.chartId)) mockError(400, '引用的图表不存在')
  for (const f of b.config?.filters ?? []) {
    const ds = datasets.find((d) => d.id === f.datasetId)
    if (!ds || !ds.columns.includes(f.column)) mockError(400, '数据源或字段不可用')
  }
}
const screenErr = (b: Partial<ScreenForm>) => {
  if (!b?.name?.trim()) mockError(400, '名称不能为空')
  const r = b.config?.refreshSec ?? 0
  if (r !== 0 && (r < 5 || r > 3600)) mockError(400, '刷新间隔需为 0（不刷新）或 5 到 3600 秒')
  const items = b.config?.items ?? []
  items.forEach((a: ScreenItem, i: number) => {
    if (a.w < 1 || a.h < 1 || a.x < 0 || a.y < 0 || a.x + a.w > 24) mockError(400, '大屏布局超出画布')
    if (!charts.some((c) => c.id === a.chartId)) mockError(400, '引用的图表不存在')
    for (const o of items.slice(0, i)) if (a.x < o.x + o.w && o.x < a.x + a.w && a.y < o.y + o.h && o.y < a.y + a.h) mockError(400, '大屏组件不能重叠')
  })
}

const B = '/plugins/report'
const upsert = <T extends { id: string }>(list: T[], item: T) => (list.some((x) => x.id === item.id) ? list.map((x) => (x.id === item.id ? item : x)) : [...list, item])

defineMock([
  // ---- 数据集 ----
  { method: 'GET', url: `${B}/sources`, handler: () => MOCK_SOURCES },
  { method: 'GET', url: `${B}/datasets`, handler: ({ query }) => page([...datasets].reverse().filter((d) => !kw(query) || d.name.toLowerCase().includes(kw(query))), query) },
  { method: 'GET', url: `${B}/chart-options/datasets`, handler: () => datasets.map((d) => ({ ...d, fields: fieldsOf(d) })) },
  { method: 'GET', url: `${B}/report-options/datasets`, handler: () => datasets.map((d) => ({ ...d, fields: fieldsOf(d) })) },
  { method: 'GET', url: `${B}/datasets/:id`, handler: ({ params }) => need(datasets, params.id) },
  {
    method: 'POST',
    url: `${B}/datasets/preview`,
    handler: ({ body }) => {
      try {
        const dims = (body.columns as string[]).map((column) => ({ column }))
        return runSpec(body.source, body.columns, cfg({ dimensions: dims, filters: body.filters ?? [], limit: 20 }))
      } catch {
        return mockError(400, '数据源或字段不可用')
      }
    },
  },
  {
    method: 'POST',
    url: `${B}/datasets`,
    handler: ({ body }) => {
      const src = MOCK_SOURCES.find((s) => s.table === body?.source)
      if (!body?.name?.trim() || !src || !body.columns?.length || body.columns.some((c: string) => !src.columns.some((x) => x.name === c))) mockError(400, '数据源或字段不可用')
      const d: Dataset = { ...body, id: genId(), filters: body.filters ?? [], createdAt: now(), updatedAt: now() }
      datasets = [...datasets, d]
      return d
    },
  },
  {
    method: 'PUT',
    url: `${B}/datasets/:id`,
    handler: ({ params, body }) => {
      const old = need(datasets, params.id)
      const src = MOCK_SOURCES.find((s) => s.table === body?.source)
      if (!body?.name?.trim() || !src || body.columns.some((c: string) => !src.columns.some((x) => x.name === c))) mockError(400, '数据源或字段不可用')
      const d = { ...old, ...body, updatedAt: now() }
      datasets = upsert(datasets, d)
      return d
    },
  },
  {
    method: 'DELETE',
    url: `${B}/datasets/:id`,
    handler: ({ params }) => {
      need(datasets, params.id)
      if (charts.some((c) => c.datasetId === params.id)) mockError(409, '数据集仍被图表使用，无法删除')
      datasets = datasets.filter((d) => d.id !== params.id)
      return null
    },
  },
  // ---- 图表 ----
  { method: 'GET', url: `${B}/charts`, handler: ({ query }) => page([...charts].reverse().filter((c) => !kw(query) || c.name.toLowerCase().includes(kw(query))), query) },
  { method: 'GET', url: `${B}/report-options/charts`, handler: () => charts },
  { method: 'GET', url: `${B}/screen-options/charts`, handler: () => charts },
  { method: 'GET', url: `${B}/screen-options/roles`, handler: () => ROLES },
  { method: 'GET', url: `${B}/charts/:id`, handler: ({ params }) => need(charts, params.id) },
  { method: 'POST', url: `${B}/charts/preview`, handler: ({ body }) => (chartErr(body), exec({ ...(body as Chart), id: 'preview' }, [], 200)) },
  { method: 'POST', url: `${B}/charts/:id/query`, handler: ({ params }) => exec(need(charts, params.id)) },
  {
    method: 'POST',
    url: `${B}/charts`,
    handler: ({ body }) => {
      chartErr(body)
      const c: Chart = { ...body, id: genId(), createdAt: now(), updatedAt: now() }
      charts = [...charts, c]
      return c
    },
  },
  {
    method: 'PUT',
    url: `${B}/charts/:id`,
    handler: ({ params, body }) => {
      const old = need(charts, params.id)
      chartErr(body)
      const c = { ...old, ...body, updatedAt: now() }
      charts = upsert(charts, c)
      return c
    },
  },
  {
    method: 'DELETE',
    url: `${B}/charts/:id`,
    handler: ({ params }) => {
      need(charts, params.id)
      charts = charts.filter((c) => c.id !== params.id)
      return null
    },
  },
  // ---- 报表 ----
  { method: 'GET', url: `${B}/reports`, handler: ({ query }) => page([...reports].reverse().filter((r) => !kw(query) || r.name.toLowerCase().includes(kw(query))), query) },
  { method: 'GET', url: `${B}/reports/:id`, handler: ({ params }) => withCharts(need(reports, params.id)) },
  { method: 'GET', url: `${B}/view/reports`, handler: () => reports.map((r) => ({ ...r, config: { items: [], filters: [] } })) },
  { method: 'GET', url: `${B}/view/reports/:id`, handler: ({ params }) => withCharts(need(reports, params.id)) },
  {
    method: 'POST',
    url: `${B}/reports`,
    handler: ({ body }) => {
      reportErr(body)
      const r: Report = { ...body, id: genId(), createdAt: now(), updatedAt: now() }
      reports = [...reports, r]
      return r
    },
  },
  {
    method: 'PUT',
    url: `${B}/reports/:id`,
    handler: ({ params, body }) => {
      const old = need(reports, params.id)
      reportErr(body)
      const r = { ...old, ...body, updatedAt: now() }
      reports = upsert(reports, r)
      return r
    },
  },
  {
    method: 'DELETE',
    url: `${B}/reports/:id`,
    handler: ({ params }) => {
      need(reports, params.id)
      reports = reports.filter((r) => r.id !== params.id)
      return null
    },
  },
  {
    // 只能跑报表自己引用的图表；筛选器取值经报表配置翻译成过滤条件
    method: 'POST',
    url: `${B}/view/reports/:id/charts/:chartId/query`,
    handler: ({ params, body }) => {
      const r = need(reports, params.id)
      if (!r.config.items.some((i) => i.chartId === params.chartId)) mockError(404, '记录不存在')
      const ch = need(charts, params.chartId)
      return exec(ch, runtime(r.config, ch.datasetId, body?.filters ?? {}))
    },
  },
  {
    method: 'POST',
    url: `${B}/view/reports/:id/charts/:chartId/export`,
    handler: ({ params, body }) => {
      const r = need(reports, params.id)
      const ch = need(charts, params.chartId)
      if (!r.config.items.some((i) => i.chartId === params.chartId)) mockError(404, '记录不存在')
      if (ch.type !== 'table') mockError(400, '只有表格图表可以导出')
      return exec(ch, runtime(r.config, ch.datasetId, body?.filters ?? {}), 5000)
    },
  },
  {
    method: 'POST',
    url: `${B}/view/reports/:id/filters/:key/options`,
    handler: ({ params }) => {
      const f = need(reports, params.id).config.filters.find((x) => x.key === params.key && x.type === 'select') ?? mockError(404, '记录不存在')
      const ds = need(datasets, f.datasetId)
      const res = runSpec(ds.source, ds.columns, cfg({ dimensions: [{ column: f.column }], limit: 200 }), [], ds.filters)
      return res.rows.map((r) => r.d0)
    },
  },
  // ---- 大屏 ----
  { method: 'GET', url: `${B}/screens`, handler: ({ query }) => page([...screens].reverse().filter((s) => !kw(query) || s.name.toLowerCase().includes(kw(query))), query) },
  { method: 'GET', url: `${B}/screens/:id`, handler: ({ params }) => need(screens, params.id) },
  { method: 'GET', url: `${B}/view/screens`, handler: () => screens.map(({ id: sid, name, remark }) => ({ id: sid, name, remark })) },
  { method: 'GET', url: `${B}/view/screens/:id`, handler: ({ params }) => withCharts(need(screens, params.id)) },
  {
    method: 'POST',
    url: `${B}/view/screens/:id/charts/:chartId/query`,
    handler: ({ params }) => {
      const s = need(screens, params.id)
      if (!s.config.items.some((i) => i.chartId === params.chartId)) mockError(404, '记录不存在')
      return exec(need(charts, params.chartId))
    },
  },
  {
    method: 'POST',
    url: `${B}/screens`,
    handler: ({ body }) => {
      screenErr(body)
      const s: Screen = { ...body, id: genId(), createdAt: now(), updatedAt: now() }
      screens = [...screens, s]
      return s
    },
  },
  {
    method: 'PUT',
    url: `${B}/screens/:id`,
    handler: ({ params, body }) => {
      const old = need(screens, params.id)
      screenErr(body)
      const s = { ...old, ...body, updatedAt: now() }
      screens = upsert(screens, s)
      return s
    },
  },
  {
    method: 'DELETE',
    url: `${B}/screens/:id`,
    handler: ({ params }) => {
      need(screens, params.id)
      screens = screens.filter((s) => s.id !== params.id)
      return null
    },
  },
  { method: 'GET', url: `${B}/overview`, handler: () => ({ reports: reports.length, screens: screens.length }) },
])
