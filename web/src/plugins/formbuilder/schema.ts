import type { Cond, FieldType, FormDefinition, FormField, Scalar } from './api'

export const FIELD_TYPES: FieldType[] = [
  'text',
  'textarea',
  'number',
  'radio',
  'checkbox',
  'select',
  'date',
  'switch',
  'rating',
  'file',
  'section',
]

export const hasValue = (f: FormField) => f.type !== 'section'
/** 与服务端 isBlank 一致：这几种类型的纯空白字符串视为未填 */
const TRIMMED_TYPES: FieldType[] = ['text', 'textarea', 'date', 'radio', 'select']
const isBlankString = (f: FormField, v: unknown) => typeof v === 'string' && v.trim() === '' && TRIMMED_TYPES.includes(f.type)
export const usesOptions = (f: FormField) => f.type === 'radio' || f.type === 'checkbox' || f.type === 'select'
export const isMulti = (f: FormField) => f.type === 'checkbox' || f.type === 'file' || (f.type === 'select' && !!f.multiple)

export function nextKey(fields: FormField[]): string {
  const used = new Set(fields.map((f) => f.key))
  for (let i = fields.length + 1; ; i++) {
    const k = `field${i}`
    if (!used.has(k)) return k
  }
}

export function newField(type: FieldType, fields: FormField[], title: string): FormField {
  const f: FormField = { key: nextKey(fields), type, title }
  if (usesOptions(f)) {
    f.options = [
      { label: '1', value: '1' },
      { label: '2', value: '2' },
    ]
  }
  if (type === 'rating') f.max = 5
  return f
}

// ---- 显隐条件（与服务端 cond.go 语义一致；仅数据比较）----

const isEmpty = (v: unknown) => v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0)

function scalarEq(a: unknown, b: unknown) {
  return typeof a === typeof b && (typeof a === 'string' || typeof a === 'number' || typeof a === 'boolean') && a === b
}

export function evalCond(c: Cond, values: Record<string, unknown>): boolean {
  if (c.all?.length) return c.all.every((x) => evalCond(x, values))
  if (c.any?.length) return c.any.some((x) => evalCond(x, values))
  const actual = c.field ? values[c.field] : undefined
  const empty = isEmpty(actual)
  switch (c.op) {
    case 'empty':
      return empty
    case 'notEmpty':
      return !empty
    case 'eq':
      return !empty && scalarEq(actual, c.value)
    case 'ne':
      return empty || !scalarEq(actual, c.value)
    case 'contains':
      if (empty) return false
      if (Array.isArray(actual)) return actual.some((e) => scalarEq(e, c.value))
      return typeof actual === 'string' && typeof c.value === 'string' && c.value !== '' && actual.includes(c.value)
    case 'in': {
      if (empty || !Array.isArray(c.value)) return false
      const list = c.value
      return Array.isArray(actual) ? actual.some((a) => list.some((x) => scalarEq(a, x))) : list.some((x) => scalarEq(actual, x))
    }
    case 'gt':
    case 'gte':
    case 'lt':
    case 'lte': {
      if (empty || typeof actual !== typeof c.value || (typeof actual !== 'number' && typeof actual !== 'string')) return false
      const a = actual as number | string
      const b = c.value as number | string
      return c.op === 'gt' ? a > b : c.op === 'gte' ? a >= b : c.op === 'lt' ? a < b : a <= b
    }
    default:
      return false
  }
}

/** 按字段顺序计算可见性：隐藏字段的值视为空，所以依赖它的字段同样被隐藏。 */
export function visibleValues(def: FormDefinition, values: Record<string, unknown>) {
  const clean: Record<string, unknown> = {}
  const visible = new Set<string>()
  for (const f of def.fields) {
    if (f.showIf && !evalCond(f.showIf, clean)) continue
    visible.add(f.key)
    if (hasValue(f) && !isEmpty(values[f.key]) && !isBlankString(f, values[f.key])) clean[f.key] = values[f.key]
  }
  return { clean, visible }
}

export function initialValues(def: FormDefinition): Record<string, unknown> {
  const v: Record<string, unknown> = {}
  for (const f of def.fields) if (hasValue(f) && f.default !== undefined && f.default !== null) v[f.key] = f.default
  return v
}

/** 条件里可引用的字段：排在当前字段之前、产生数据的字段 */
export function referenceable(fields: FormField[], index: number): FormField[] {
  return fields.slice(0, index).filter(hasValue)
}

// ---- 客户端校验（服务端会再校验一遍，这里只为即时反馈）----

type T = (key: string, opts?: Record<string, unknown>) => string

export function validateValues(def: FormDefinition, values: Record<string, unknown>, t: T): Record<string, string> {
  const errors: Record<string, string> = {}
  const { clean, visible } = visibleValues(def, values)
  for (const f of def.fields) {
    if (!visible.has(f.key) || !hasValue(f)) continue
    const v = clean[f.key]
    if (isEmpty(v) || (typeof v === 'string' && v.trim() === '' && f.type !== 'switch')) {
      if (f.required) errors[f.key] = t('plugin_formbuilder.errors.required')
      continue
    }
    const msg = checkOne(f, v, t)
    if (msg) errors[f.key] = msg
  }
  return errors
}

function checkOne(f: FormField, v: unknown, t: T): string | null {
  const E = (k: string, o?: Record<string, unknown>) => t(`plugin_formbuilder.errors.${k}`, o)
  switch (f.type) {
    case 'text':
    case 'textarea': {
      const s = String(v)
      const len = [...s].length
      const max = f.maxLen ?? (f.type === 'textarea' ? 5000 : 500)
      if (len > max) return E('maxLen', { n: max })
      if (f.minLen !== undefined && len < f.minLen) return E('minLen', { n: f.minLen })
      if (f.pattern) {
        try {
          if (!new RegExp(f.pattern).test(s)) return E('pattern')
        } catch {
          return null // 浏览器正则方言不同，交给服务端判定
        }
      }
      return null
    }
    case 'number': {
      if (typeof v !== 'number' || Number.isNaN(v)) return E('number')
      if (f.integer && !Number.isInteger(v)) return E('integer')
      if (f.min !== undefined && v < f.min) return E('min', { n: f.min })
      if (f.max !== undefined && v > f.max) return E('max', { n: f.max })
      return null
    }
    case 'checkbox':
    case 'select':
      if (Array.isArray(v)) {
        if (f.min !== undefined && v.length < f.min) return E('minCount', { n: f.min })
        if (f.max !== undefined && v.length > f.max) return E('maxCount', { n: f.max })
      }
      return null
    case 'file':
      if (Array.isArray(v)) {
        if (f.min !== undefined && v.length < f.min) return E('minFiles', { n: f.min })
        if (v.length > (f.max ?? 5)) return E('maxFiles', { n: f.max ?? 5 })
      }
      return null
    case 'date':
      if (typeof v === 'string') {
        if (f.minDate && v < f.minDate) return E('minDate', { d: f.minDate })
        if (f.maxDate && v > f.maxDate) return E('maxDate', { d: f.maxDate })
      }
      return null
    case 'rating': {
      const max = f.max ?? 5
      return typeof v !== 'number' || !Number.isInteger(v) || v < 1 || v > max ? E('rating', { n: max }) : null
    }
    case 'switch':
      return f.required && v !== true ? E('mustOn') : null
    default:
      return null
  }
}

const SERVER_PREFIX = '提交内容校验未通过：'

/**
 * 服务端把字段错误合成一条 400 消息（"提交内容校验未通过：标题 原因；标题 原因"，最多 5 条），没有结构化字段。
 * 这里按字段标题还原成 {key: 原因}；标题重复或对不上的片段放进 rest，由页面整体展示。
 */
export function fieldErrorsFromMessage(def: FormDefinition, message: string): { fields: Record<string, string>; rest: string } {
  if (!message.startsWith(SERVER_PREFIX)) return { fields: {}, rest: message }
  const body = message.slice(SERVER_PREFIX.length).replace(/……（共 \d+ 项）$/, '')
  const counts = new Map<string, number>()
  for (const f of def.fields) counts.set(f.title, (counts.get(f.title) ?? 0) + 1)
  const fields = def.fields.filter((f) => hasValue(f) && counts.get(f.title) === 1).sort((a, b) => b.title.length - a.title.length)
  const out: Record<string, string> = {}
  const rest: string[] = []
  for (const seg of body.split('；')) {
    const hit = fields.find((f) => seg.startsWith(`${f.title} `))
    if (hit) out[hit.key] = seg.slice(hit.title.length + 1)
    else if (seg) rest.push(seg)
  }
  return { fields: out, rest: rest.join('；') }
}

/** 幂等 key：服务端要求 8-64 位字母数字下划线连字符 */
export function newIdemKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `k${Date.now().toString(36)}${Math.random().toString(36).slice(2, 12)}`
}

/** 把提交值转成可展示的文本（选项换成名称） */
export function displayValue(f: FormField | undefined, v: unknown, yes: string, no: string): string {
  if (v === undefined || v === null || v === '') return ''
  const label = (x: unknown) => f?.options?.find((o) => o.value === x)?.label ?? String(x)
  if (typeof v === 'boolean') return v ? yes : no
  if (Array.isArray(v)) return v.map((e) => (e && typeof e === 'object' ? String((e as { name?: string }).name ?? '') : label(e))).join(', ')
  return label(v)
}

export const OPS_BY_TYPE: Record<FieldType, Array<Cond['op']>> = {
  text: ['eq', 'ne', 'contains', 'empty', 'notEmpty'],
  textarea: ['contains', 'empty', 'notEmpty'],
  number: ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'empty', 'notEmpty'],
  radio: ['eq', 'ne', 'in', 'empty', 'notEmpty'],
  checkbox: ['contains', 'in', 'empty', 'notEmpty'],
  select: ['eq', 'ne', 'in', 'contains', 'empty', 'notEmpty'],
  date: ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'empty', 'notEmpty'],
  switch: ['eq', 'ne'],
  rating: ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'empty', 'notEmpty'],
  file: ['empty', 'notEmpty'],
  section: [],
}

export type { Scalar }

// ---- 设计器辅助 ----

export const KEY_RE = /^[A-Za-z][A-Za-z0-9_]{0,31}$/

function pruneCond(c: Cond, allowed: Set<string>): Cond | undefined {
  if (c.all || c.any) {
    const kind = c.all ? 'all' : 'any'
    const list = (c.all ?? c.any ?? []).map((x) => pruneCond(x, allowed)).filter((x): x is Cond => !!x)
    if (list.length === 0) return undefined
    return list.length === 1 ? list[0] : ({ [kind]: list } as Cond)
  }
  return c.field && allowed.has(c.field) ? c : undefined
}

/**
 * 条件只能引用排在当前字段之前、产生数据的字段（服务端 validateCond 的规则）。
 * 排序、删除字段之后调用：去掉失效的条件叶子，返回新字段数组与被清理的字段数。
 */
export function pruneConds(fields: FormField[]): { fields: FormField[]; cleaned: number } {
  const earlier = new Set<string>()
  let cleaned = 0
  const out = fields.map((f) => {
    let next = f
    if (f.showIf) {
      const pruned = pruneCond(f.showIf, earlier)
      if (JSON.stringify(pruned) !== JSON.stringify(f.showIf)) {
        cleaned++
        next = { ...f, showIf: pruned }
        if (!pruned) delete next.showIf
      }
    }
    if (hasValue(f)) earlier.add(f.key)
    return next
  })
  return { fields: out, cleaned }
}

function renameInCond(c: Cond, from: string, to: string): Cond {
  if (c.all) return { ...c, all: c.all.map((x) => renameInCond(x, from, to)) }
  if (c.any) return { ...c, any: c.any.map((x) => renameInCond(x, from, to)) }
  return c.field === from ? { ...c, field: to } : c
}

/** 修改字段标识，并同步其他字段条件里的引用 */
export function renameFieldKey(fields: FormField[], index: number, to: string): FormField[] {
  const from = fields[index].key
  return fields.map((f, i) => {
    if (i === index) return { ...f, key: to }
    return f.showIf ? { ...f, showIf: renameInCond(f.showIf, from, to) } : f
  })
}

/** 设计器画布上的即时提示：返回问题的 i18n 子键（plugin_formbuilder.problem.*） */
export function fieldProblems(f: FormField): string[] {
  const out: string[] = []
  if (!f.title.trim()) out.push('title')
  if (usesOptions(f) && !f.dictType && !(f.options?.length)) out.push('options')
  if (f.pattern) {
    try {
      new RegExp(f.pattern)
    } catch {
      out.push('pattern')
    }
  }
  return out
}
