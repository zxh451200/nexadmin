// 5 段 cron（分 时 日 月 周）的前端解析，用于表单校验与「下次执行时间」预览。
// 语义与后端 backend/cronexpr.go 保持一致：支持 * , - /，周 0-7（0 和 7 都是周日），
// 日与周都被限制时取"或"。按浏览器本地时区计算（后端按服务器本地时区执行，预览仅供参考）。

export type CronErrorCode = 'fields' | 'value' | 'range' | 'step' | 'empty'

export interface CronSchedule {
  next: (from: Date) => Date | null
  nextRuns: (from: Date, count: number) => Date[]
}

export type CronParseResult = { ok: true; schedule: CronSchedule } | { ok: false; code: CronErrorCode }

interface Spec {
  min: number
  max: number
}

const SPECS: Spec[] = [
  { min: 0, max: 59 }, // 分
  { min: 0, max: 23 }, // 时
  { min: 1, max: 31 }, // 日
  { min: 1, max: 12 }, // 月
  { min: 0, max: 7 }, // 周
]

function parseField(part: string, spec: Spec): Set<number> | CronErrorCode {
  const out = new Set<number>()
  for (const item of part.split(',')) {
    if (!item) return 'empty'
    const [rng, stepStr, extra] = item.split('/')
    if (extra !== undefined) return 'step'
    let step = 1
    if (stepStr !== undefined) {
      if (!/^\d+$/.test(stepStr) || Number(stepStr) < 1) return 'step'
      step = Number(stepStr)
    }
    let lo = spec.min
    let hi = spec.max
    if (rng === '*') {
      // 全范围
    } else if (rng.includes('-')) {
      const [a, b, more] = rng.split('-')
      if (more !== undefined || !/^\d+$/.test(a) || !/^\d+$/.test(b)) return 'value'
      lo = Number(a)
      hi = Number(b)
      if (lo < spec.min || hi > spec.max) return 'range'
      if (lo > hi) return 'range'
    } else {
      if (!/^\d+$/.test(rng)) return 'value'
      lo = Number(rng)
      if (lo < spec.min || lo > spec.max) return 'range'
      hi = stepStr !== undefined ? spec.max : lo
    }
    for (let v = lo; v <= hi; v += step) out.add(v)
  }
  return out
}

export function parseCron(expr: string): CronParseResult {
  const parts = expr.trim().split(/\s+/).filter(Boolean)
  if (parts.length !== 5) return { ok: false, code: 'fields' }
  const sets: Set<number>[] = []
  for (let i = 0; i < 5; i++) {
    const r = parseField(parts[i], SPECS[i])
    if (typeof r === 'string') return { ok: false, code: r }
    sets.push(r)
  }
  const [minute, hour, dom, month, dow] = sets
  if (dow.has(7)) {
    dow.add(0)
    dow.delete(7)
  }
  const star = (p: string) => p === '*' || p === '*/1'
  const domStar = star(parts[2])
  const dowStar = star(parts[4])

  const dayOk = (d: Date) => {
    const a = dom.has(d.getDate())
    const b = dow.has(d.getDay())
    if (domStar && dowStar) return true
    if (domStar) return b
    if (dowStar) return a
    return a || b
  }

  const next = (from: Date): Date | null => {
    const limit = from.getFullYear() + 5
    let t = new Date(from.getFullYear(), from.getMonth(), from.getDate(), from.getHours(), from.getMinutes() + 1, 0, 0)
    while (t.getFullYear() <= limit) {
      if (!month.has(t.getMonth() + 1)) t = new Date(t.getFullYear(), t.getMonth() + 1, 1, 0, 0)
      else if (!dayOk(t)) t = new Date(t.getFullYear(), t.getMonth(), t.getDate() + 1, 0, 0)
      else if (!hour.has(t.getHours())) t = new Date(t.getFullYear(), t.getMonth(), t.getDate(), t.getHours() + 1, 0)
      else if (!minute.has(t.getMinutes())) t = new Date(t.getTime() + 60_000)
      else return t
    }
    return null
  }

  const nextRuns = (from: Date, count: number) => {
    const list: Date[] = []
    let cur = from
    while (list.length < count) {
      const n = next(cur)
      if (!n) break
      list.push(n)
      cur = n
    }
    return list
  }

  return { ok: true, schedule: { next, nextRuns } }
}

/** 常用预设；文案在 locales 的 plugin_cron.presets.<key>。 */
export const CRON_PRESETS = [
  { key: 'everyMinute', expr: '* * * * *' },
  { key: 'every5Minutes', expr: '*/5 * * * *' },
  { key: 'everyHour', expr: '0 * * * *' },
  { key: 'dailyMidnight', expr: '0 0 * * *' },
  { key: 'dailyNoon', expr: '0 12 * * *' },
  { key: 'weekdays9', expr: '0 9 * * 1-5' },
  { key: 'mondayMorning', expr: '0 0 * * 1' },
  { key: 'monthlyFirst', expr: '0 0 1 * *' },
] as const

export const CUSTOM_PRESET = 'custom'

export function presetKeyOf(expr: string): string {
  const norm = expr.trim().split(/\s+/).join(' ')
  return CRON_PRESETS.find((p) => p.expr === norm)?.key ?? CUSTOM_PRESET
}

export const WEEKDAYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const

/**
 * describeCron 把表达式翻译成人类可读的一句话（如「每 5 分钟」「每天 09:00」）。
 * 只覆盖常见形态，覆盖不到时返回空串（表单里留空即可，不硬凑）。中文文案由调用方组织，
 * 这里返回结构化的描述零件：mode（频率形态）与细节。
 */
export interface CronDescription {
  /** everyMinutes | hourly | daily | weekly | monthly | other */
  mode: 'everyMinutes' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'other'
  /** 分钟步长（everyMinutes） */
  minutes?: number
  /** 小时（daily/monthly，0-23） */
  hour?: number
  /** 周几（weekly，0-6，0=周日） */
  weekdays?: number[]
  /** 几号（monthly，1-31） */
  day?: number
}

/** 解析单字段为「全部」或值集合；解析失败返回 undefined。内部用，不校验合法性（parseCron 已验）。 */
function fieldValues(part: string, min: number, max: number): Set<number> | undefined {
  const r = parseField(part, { min, max })
  if (typeof r === 'string') return undefined
  return r
}

export function describeCron(expr: string): CronDescription | null {
  const parts = expr.trim().split(/\s+/).filter(Boolean)
  if (parts.length !== 5) return null
  const [min, hour, dom, mon, dow] = parts
  if (mon !== '*') return { mode: 'other' }
  const star = (p: string) => p === '*' || p === '*/1'

  // 每 N 分钟：*/5 * * * *
  const minStep = min.match(/^\*\/(\d+)$/)
  if (minStep && star(hour) && star(dom) && star(dow)) {
    return { mode: 'everyMinutes', minutes: Number(minStep[1]) }
  }
  // 每小时：0 * * * *（分钟固定）
  if (hour === '*' && star(dom) && star(dow) && /^\d+$/.test(min)) {
    return { mode: 'hourly' }
  }
  // 每天：0 9 * * *
  if (star(dom) && star(dow) && /^\d+$/.test(min) && /^\d+$/.test(hour)) {
    return { mode: 'daily', hour: Number(hour) }
  }
  // 每周：0 9 * * 1-5 / 0 0 * * 1
  if (star(dom) && star(mon) && /^\d+$/.test(min) && /^\d+$/.test(hour)) {
    const days = fieldValues(dow, 0, 7)
    if (days) {
      const list = [...days].map((d) => (d === 7 ? 0 : d)).sort((a, b) => a - b)
      return { mode: 'weekly', hour: Number(hour), weekdays: list }
    }
  }
  // 每月：0 0 1 * *
  if (star(dow) && star(mon) && /^\d+$/.test(min) && /^\d+$/.test(hour) && /^\d+$/.test(dom)) {
    return { mode: 'monthly', hour: Number(hour), day: Number(dom) }
  }
  return { mode: 'other' }
}
