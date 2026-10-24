import type { ChartConfig, ChartType, Filter, QueryResult, ScreenItem, SourceCol } from './api'

/** 图表颜色只允许语义 token（与后端 colorTokens 一致），渲染时映射成 hsl(var(--token)) */
export const COLOR_TOKENS = ['chart-1', 'chart-2', 'chart-3', 'chart-4', 'chart-5', 'primary', 'success', 'warning', 'destructive', 'info'] as const
export const DEFAULT_PALETTE = ['chart-1', 'chart-2', 'chart-3', 'chart-4', 'chart-5']

export const colorVar = (token: string) => `hsl(var(--${token}))`
export const colorAt = (colors: string[] | undefined, i: number) => {
  const list = colors && colors.length > 0 ? colors : DEFAULT_PALETTE
  return colorVar(list[i % list.length])
}

export function emptyChartConfig(): ChartConfig {
  return { dimensions: [], measures: [{ agg: 'count' }], filters: [], orderBy: [], limit: 1000, colors: [] }
}

/** 按图表类型夹紧维度/度量个数（与后端 validateChartShape 一致） */
export function shapeError(type: ChartType, cfg: Pick<ChartConfig, 'dimensions' | 'measures'>): string | null {
  const nd = cfg.dimensions.length
  const nm = cfg.measures.length
  switch (type) {
    case 'bar':
    case 'line':
      return nd >= 1 && nm >= 1 ? null : 'shapeCartesian'
    case 'pie':
      return nd === 1 && nm === 1 ? null : 'shapePie'
    case 'metric':
      return nd === 0 && nm === 1 ? null : 'shapeMetric'
    default:
      return nd + nm >= 1 ? null : 'shapeTable'
  }
}

/** 度量可选的聚合：sum/avg 只对数值列，min/max 对数值与时间列，count 任意 */
export function aggsFor(col?: SourceCol): string[] {
  if (!col) return ['count']
  if (col.type === 'number') return ['count', 'sum', 'avg', 'min', 'max']
  if (col.type === 'time') return ['count', 'min', 'max']
  return ['count']
}

/** 过滤操作符按列类型的可用集合（与后端 filterSQL 一致） */
export function opsFor(type: SourceCol['type']): Filter['op'][] {
  switch (type) {
    case 'number':
    case 'time':
      return ['=', '!=', '>', '<', '>=', '<=', 'in', 'between']
    case 'string':
      return ['=', '!=', 'in', 'like']
    default:
      return ['=', '!=']
  }
}

/** 把表单里的文本值转成接口需要的形状：in 用逗号分隔，between 用 ~ 分隔 */
export function parseFilterValue(op: Filter['op'], type: SourceCol['type'], raw: string): unknown {
  const one = (s: string): unknown => {
    const v = s.trim()
    if (type === 'number') return v === '' || Number.isNaN(Number(v)) ? v : Number(v)
    if (type === 'bool') return v === 'true' || v === '1'
    return v
  }
  if (op === 'in') return raw.split(',').map(one).filter((v) => v !== '')
  if (op === 'between') return raw.split('~').map(one)
  return one(raw)
}
export function formatFilterValue(op: Filter['op'], v: unknown): string {
  if (Array.isArray(v)) return v.join(op === 'between' ? '~' : ',')
  return v == null ? '' : String(v)
}

export interface CartesianData {
  data: Record<string, string | number | null>[]
  series: { key: string; label: string }[]
}

/** 结果集 -> 笛卡尔图数据。1 个维度：每个度量一条系列；2 个维度 + 1 个度量：第二个维度展开成系列 */
export function toCartesian(res: QueryResult): CartesianData {
  const dims = res.columns.filter((c) => c.kind === 'dim')
  const meas = res.columns.filter((c) => c.kind === 'measure')
  const xKey = dims[0]?.key
  if (!xKey) return { data: [], series: [] }
  if (dims.length >= 2 && meas.length >= 1) {
    const sKey = dims[1].key
    const mKey = meas[0].key
    const names: string[] = []
    const byX = new Map<string, Record<string, string | number | null>>()
    for (const r of res.rows) {
      const x = String(r[xKey] ?? '')
      const s = String(r[sKey] ?? '')
      if (!names.includes(s)) names.push(s)
      const row = byX.get(x) ?? { x }
      row[`s:${s}`] = Number(r[mKey] ?? 0)
      byX.set(x, row)
    }
    return { data: [...byX.values()], series: names.map((n) => ({ key: `s:${n}`, label: n })) }
  }
  return {
    data: res.rows.map((r) => {
      const row: Record<string, string | number | null> = { x: String(r[xKey] ?? '') }
      for (const m of meas) row[m.key] = r[m.key] == null ? null : Number(r[m.key])
      return row
    }),
    series: meas.map((m) => ({ key: m.key, label: m.label })),
  }
}

export function toPie(res: QueryResult): { name: string; value: number }[] {
  const d = res.columns.find((c) => c.kind === 'dim')
  const m = res.columns.find((c) => c.kind === 'measure')
  if (!d || !m) return []
  return res.rows.map((r) => ({ name: String(r[d.key] ?? ''), value: Number(r[m.key] ?? 0) }))
}

export function toMetric(res: QueryResult): { label: string; value: number | null } {
  const m = res.columns.find((c) => c.kind === 'measure')
  if (!m || res.rows.length === 0) return { label: m?.label ?? '', value: null }
  const v = res.rows[0][m.key]
  return { label: m.label, value: v == null ? null : Number(v) }
}

// ---- 大屏画布栅格 ----
export const GRID_COLS = 24
export const GRID_ROWS = 60

export const overlaps = (a: ScreenItem, b: ScreenItem) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

export function inBounds(it: ScreenItem): boolean {
  return it.w >= 1 && it.h >= 1 && it.x >= 0 && it.y >= 0 && it.x + it.w <= GRID_COLS && it.y + it.h <= GRID_ROWS
}

/** 把 next 放到 items[idx] 的位置是否合法：在画布内且不与其他组件重叠 */
export function canPlace(items: ScreenItem[], idx: number, next: ScreenItem): boolean {
  return inBounds(next) && items.every((o, i) => i === idx || !overlaps(o, next))
}

/** 夹紧到画布内（拖拽中的临时位置） */
export function clampItem(it: ScreenItem): ScreenItem {
  const w = Math.min(Math.max(1, it.w), GRID_COLS)
  const h = Math.min(Math.max(1, it.h), GRID_ROWS)
  return { ...it, w, h, x: Math.min(Math.max(0, it.x), GRID_COLS - w), y: Math.min(Math.max(0, it.y), GRID_ROWS - h) }
}

/** 为新组件找第一个能放下的空位（行优先扫描），放不下返回 null */
export function firstFreeSlot(items: ScreenItem[], w: number, h: number): { x: number; y: number } | null {
  for (let y = 0; y + h <= GRID_ROWS; y++) {
    for (let x = 0; x + w <= GRID_COLS; x++) {
      if (items.every((o) => !overlaps(o, { chartId: '', x, y, w, h }))) return { x, y }
    }
  }
  return null
}

export function fmtCell(v: unknown, lang: string): string {
  if (v == null) return ''
  if (typeof v === 'number') return new Intl.NumberFormat(lang).format(v)
  if (typeof v === 'boolean') return v ? '✓' : '✗'
  const s = String(v)
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(s)) {
    const d = new Date(s)
    if (!Number.isNaN(d.getTime())) return new Intl.DateTimeFormat(lang, { dateStyle: 'medium', timeStyle: 'medium' }).format(d)
  }
  return s
}
