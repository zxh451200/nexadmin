import type { ChartConfig, ColType, Filter, QueryResult, Source } from './api'

// 前端 mock 用的内存数据与查询执行器：语义与后端一致（白名单列、操作符白名单、limit 上限、租户内数据）。

export const MOCK_SOURCES: Source[] = [
  {
    table: 'plg_cron_job',
    label: '定时任务',
    columns: [
      { name: 'name', label: '任务名称', type: 'string' },
      { name: 'handler', label: '处理器', type: 'string' },
      { name: 'enabled', label: '是否启用', type: 'bool' },
      { name: 'retry_count', label: '重试次数', type: 'number' },
      { name: 'last_status', label: '最近状态', type: 'string' },
      { name: 'last_run_at', label: '最近执行时间', type: 'time' },
      { name: 'created_at', label: '创建时间', type: 'time' },
    ],
  },
  {
    table: 'plg_cron_log',
    label: '定时任务执行日志',
    columns: [
      { name: 'job_name', label: '任务名称', type: 'string' },
      { name: 'handler', label: '处理器', type: 'string' },
      { name: 'trigger_type', label: '触发方式', type: 'string' },
      { name: 'status', label: '状态', type: 'string' },
      { name: 'attempts', label: '尝试次数', type: 'number' },
      { name: 'cost_ms', label: '耗时(ms)', type: 'number' },
      { name: 'started_at', label: '开始时间', type: 'time' },
      { name: 'finished_at', label: '结束时间', type: 'time' },
    ],
  },
  {
    table: 'sys_login_log',
    label: '登录日志',
    columns: [
      { name: 'login_type', label: '登录方式', type: 'string' },
      { name: 'status', label: '结果', type: 'string' },
      { name: 'fail_reason', label: '失败原因', type: 'string' },
      { name: 'browser', label: '浏览器', type: 'string' },
      { name: 'os', label: '操作系统', type: 'string' },
      { name: 'created_at', label: '登录时间', type: 'time' },
    ],
  },
]

type Row = Record<string, string | number | boolean | null>
const day = 86_400_000
const ago = (d: number, h = 0) => new Date(Date.now() - d * day - h * 3_600_000).toISOString().replace(/\.\d{3}Z$/, 'Z')
const pick = <T,>(list: T[], i: number) => list[i % list.length]

const jobs: Row[] = ['清理日志', '健康检查', '同步订单', '日报汇总', '备份数据库'].map((name, i) => ({
  name, handler: pick(['clean_logs', 'http', 'log'], i), enabled: i !== 4, retry_count: i % 3, last_status: pick(['success', 'failed', 'success'], i),
  last_run_at: ago(0, i + 1), created_at: ago(40 - i),
}))
const logs: Row[] = Array.from({ length: 180 }, (_, i) => {
  const failed = i % 7 === 0
  return {
    job_name: pick(['清理日志', '健康检查', '同步订单', '日报汇总'], i), handler: pick(['clean_logs', 'http', 'log', 'http'], i),
    trigger_type: i % 9 === 0 ? 'manual' : 'schedule', status: failed ? 'failed' : 'success', attempts: failed ? 3 : 1,
    cost_ms: 80 + ((i * 37) % 900), started_at: ago(i % 30, i % 24), finished_at: ago(i % 30, (i % 24) - 0.01),
  }
})
const logins: Row[] = Array.from({ length: 120 }, (_, i) => ({
  login_type: pick(['password', 'password', 'phone', 'sso'], i), status: i % 11 === 0 ? 'fail' : 'success', fail_reason: i % 11 === 0 ? 'bad_password' : '',
  browser: pick(['Chrome', 'Safari', 'Edge', 'Firefox'], i), os: pick(['Windows', 'macOS', 'Linux', 'iOS'], i), created_at: ago(i % 30, i % 24),
}))
const DATA: Record<string, Row[]> = { plg_cron_job: jobs, plg_cron_log: logs, sys_login_log: logins }

const BUCKET_LEN: Record<string, number> = { day: 10, month: 7, year: 4 }

function coerce(type: ColType, v: unknown): string | number | boolean {
  if (type === 'number') return Number(v)
  if (type === 'bool') return v === true || v === 'true' || v === '1' || v === 1
  return String(v)
}

function matches(row: Row, f: Filter, type: ColType): boolean {
  const cell = row[f.column]
  const cmp = (a: unknown, b: unknown) => (a === b ? 0 : (a as string | number) < (b as string | number) ? -1 : 1)
  const norm = (v: unknown) => (type === 'time' ? new Date(String(v)).getTime() : coerce(type, v))
  const c = type === 'time' ? new Date(String(cell)).getTime() : cell
  switch (f.op) {
    case '=': return c === norm(f.value)
    case '!=': return c !== norm(f.value)
    case '>': return cmp(c, norm(f.value)) > 0
    case '<': return cmp(c, norm(f.value)) < 0
    case '>=': return cmp(c, norm(f.value)) >= 0
    case '<=': return cmp(c, norm(f.value)) <= 0
    case 'in': return (f.value as unknown[]).map(norm).includes(c as never)
    case 'like': return String(cell ?? '').includes(String(f.value))
    case 'between': {
      const [lo, hi] = f.value as unknown[]
      return cmp(c, norm(lo)) >= 0 && cmp(c, norm(hi)) <= 0
    }
  }
  return false
}

export function runSpec(table: string, allowed: string[], cfg: ChartConfig, extra: Filter[] = [], fixed: Filter[] = [], maxRows = 5000): QueryResult {
  const src = MOCK_SOURCES.find((s) => s.table === table)
  if (!src) throw new Error('source')
  const col = (n: string) => {
    const c = src.columns.find((x) => x.name === n)
    if (!c || !allowed.includes(n)) throw new Error('column')
    return c
  }
  let rows = DATA[table] ?? []
  for (const f of [...fixed, ...cfg.filters, ...extra]) {
    const c = col(f.column)
    rows = rows.filter((r) => matches(r, f, c.type))
  }
  const dimVal = (r: Row, i: number) => {
    const d = cfg.dimensions[i]
    const v = r[d.column]
    return d.bucket && typeof v === 'string' ? v.slice(0, BUCKET_LEN[d.bucket]) : v
  }
  const columns: QueryResult['columns'] = [
    ...cfg.dimensions.map((d, i) => ({ key: `d${i}`, label: col(d.column).label, kind: 'dim' as const, type: d.bucket ? ('string' as const) : col(d.column).type })),
    ...cfg.measures.map((m, i) => ({ key: `m${i}`, label: m.label || (m.column ? col(m.column).label : 'count'), kind: 'measure' as const, type: 'number' as const })),
  ]
  const out: Row[] = []
  if (cfg.measures.length === 0) {
    const seen = new Set<string>()
    for (const r of rows) {
      const o: Row = {}
      cfg.dimensions.forEach((_, i) => (o[`d${i}`] = dimVal(r, i) as Row[string]))
      const k = JSON.stringify(o)
      if (cfg.dimensions.length && !seen.has(k)) out.push(o)
      seen.add(k)
    }
  } else {
    const groups = new Map<string, Row[]>()
    for (const r of rows) {
      const k = JSON.stringify(cfg.dimensions.map((_, i) => dimVal(r, i)))
      groups.set(k, [...(groups.get(k) ?? []), r])
    }
    if (cfg.dimensions.length === 0 && groups.size === 0) groups.set('[]', [])
    for (const [k, g] of groups) {
      const o: Row = {}
      ;(JSON.parse(k) as Row[string][]).forEach((v, i) => (o[`d${i}`] = v))
      cfg.measures.forEach((m, i) => {
        const vals = g.map((r) => (m.column ? r[m.column] : 1)).filter((v) => v != null) as (number | string)[]
        const nums = vals.map(Number)
        o[`m${i}`] =
          m.agg === 'count' ? vals.length : m.agg === 'sum' ? nums.reduce((a, b) => a + b, 0) : m.agg === 'avg' ? Math.round((nums.reduce((a, b) => a + b, 0) / (nums.length || 1)) * 100) / 100
          : m.agg === 'min' ? vals.sort()[0] ?? null : vals.sort().at(-1) ?? null
      })
      out.push(o)
    }
  }
  const order = cfg.orderBy[0] ?? (cfg.dimensions.length ? { ref: 'd0', desc: false } : null)
  if (order) out.sort((a, b) => (a[order.ref]! < b[order.ref]! ? -1 : a[order.ref]! > b[order.ref]! ? 1 : 0) * (order.desc ? -1 : 1))
  const limit = Math.min(cfg.limit > 0 ? cfg.limit : 1000, maxRows, 5000)
  return { columns, rows: out.slice(0, limit), truncated: out.length > limit }
}
