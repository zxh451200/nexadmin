/**
 * mock 用的服务端校验：逐条对应后端 plugins/formbuilder 的 validate.go（定义校验）、cond.go（显隐条件）、
 * check.go（提交校验）。语义与错误文案保持一致，所以在 mock 模式下设计器、填写页看到的行为和真实后端相同。
 * 唯一的近似：Go 用 RE2 编译正则，这里只能用浏览器的 RegExp（并保留长度上限）。
 */
import { mockError } from '@/framework/request/mock'
import type { Cond, FormDefinition, FormField, FormSettings } from './api'
import {
  MaxDefBytes,
  MaxFields,
  MaxFiles,
  MaxOptions,
  MaxPattern,
  MaxRatingStars,
  MaxSubmissionCap,
  MaxTextRunes,
  MaxTitle,
} from './limits'

const KEY_RE = /^[A-Za-z][A-Za-z0-9_]{0,31}$/
const DICT_RE = /^[A-Za-z0-9_.-]{1,64}$/
const MAX_NUMBER = 1e15
const MAX_COND_DEPTH = 4
const MAX_COND_NODES = 20
const MAX_COND_LIST = 50
const DEFAULT_TEXT = 500
const DEFAULT_AREA = 5000
const TYPES = ['text', 'textarea', 'number', 'radio', 'checkbox', 'select', 'date', 'switch', 'rating', 'file', 'section']

const runes = (s: string) => [...s].length
const nil = (v: unknown) => v === undefined || v === null
const hasValue = (f: FormField) => f.type !== 'section'
const usesOptions = (f: FormField) => f.type === 'radio' || f.type === 'checkbox' || f.type === 'select'
const bad = (msg: string): never => mockError(400, msg)

/** 把"字段级"的错误文案（普通 Error）统一抛成 400 */
class Msg extends Error {}
const fail = (msg: string): never => {
  throw new Msg(msg)
}

// ---- 定义校验（validate.go）----

function compilePattern(p: string): RegExp {
  if (new TextEncoder().encode(p).length > MaxPattern) return fail(`正则过长（上限 ${MaxPattern} 字节）`)
  try {
    return new RegExp(p)
  } catch {
    return fail('正则语法错误')
  }
}

export function validateDefinition(d: FormDefinition): void {
  const name = (d.name ?? '').trim()
  if (name === '' || runes(name) > 100) bad('表单名称必填，且不超过 100 个字符')
  if (runes(d.description ?? '') > 500) bad('表单说明不超过 500 个字符')
  if ((d.fields ?? []).length > MaxFields) bad(`字段数量不能超过 ${MaxFields}`)
  validateSettings(d.settings ?? ({} as FormSettings))
  const earlier = new Set<string>()
  const seen = new Set<string>()
  for (const f of d.fields ?? []) {
    if (!KEY_RE.test(f.key ?? '')) bad(`字段标识 "${f.key}" 不合法（字母开头，字母数字下划线，最长 32）`)
    if (seen.has(f.key)) bad(`字段标识 "${f.key}" 重复`)
    seen.add(f.key)
    try {
      validateFieldDef(f)
    } catch (e) {
      if (e instanceof Msg) bad(`字段 ${f.key}：${e.message}`)
      throw e
    }
    if (f.showIf) {
      try {
        validateCond(f.showIf, earlier)
      } catch (e) {
        if (e instanceof Msg) bad(`字段 ${f.key} 的显隐条件：${e.message}`)
        throw e
      }
    }
    if (hasValue(f)) earlier.add(f.key)
  }
  if (new TextEncoder().encode(JSON.stringify(d)).length > MaxDefBytes) bad('表单定义过大')
}

function validateSettings(s: FormSettings): void {
  const max = s.maxSubmissions ?? 0
  if (!Number.isInteger(max) || max < 0 || max > MaxSubmissionCap) bad('提交次数上限不合法')
  if (runes(s.successMessage ?? '') > 500) bad('提交后提示不超过 500 个字符')
}

function validateFieldDef(f: FormField): void {
  if (!TYPES.includes(f.type)) fail(`未知字段类型 "${f.type}"`)
  if ((f.title ?? '').trim() === '' || runes(f.title) > MaxTitle) fail(`标题必填，且不超过 ${MaxTitle} 个字符`)
  if (runes(f.description ?? '') > 1000 || runes(f.placeholder ?? '') > 200) fail('说明或占位文字过长')
  if (!hasValue(f)) return
  if (usesOptions(f)) validateOptions(f)
  validateRules(f)
  if (!nil(f.default) && (!usesOptions(f) || (f.options?.length ?? 0) > 0)) {
    try {
      checkValue(f, f.default)
    } catch (e) {
      if (e instanceof Msg) fail(`默认值不合法：${e.message}`)
      throw e
    }
  }
}

function validateOptions(f: FormField): void {
  if (f.dictType && !DICT_RE.test(f.dictType)) fail('字典类型不合法')
  const opts = f.options ?? []
  if (!f.dictType && opts.length === 0) fail('需要至少一个选项，或绑定字典类型')
  if (opts.length > MaxOptions) fail(`选项不能超过 ${MaxOptions} 个`)
  const seen = new Set<string>()
  for (const o of opts) {
    if (!o.value || runes(o.value) > 100 || (o.label ?? '').trim() === '' || runes(o.label) > 100) fail('选项的值与名称必填，且不超过 100 个字符')
    if (seen.has(o.value)) fail(`选项值 "${o.value}" 重复`)
    seen.add(o.value)
  }
}

function validateRules(f: FormField): void {
  const isText = f.type === 'text' || f.type === 'textarea'
  if ((!nil(f.minLen) || !nil(f.maxLen) || f.pattern) && !isText) fail('长度与正则规则只适用于文本字段')
  if ((!nil(f.minLen) && (f.minLen as number) < 0) || (!nil(f.maxLen) && ((f.maxLen as number) < 1 || (f.maxLen as number) > MaxTextRunes))) fail(`长度范围不合法（最大 ${MaxTextRunes}）`)
  if (!nil(f.minLen) && !nil(f.maxLen) && (f.minLen as number) > (f.maxLen as number)) fail('最小长度不能大于最大长度')
  if (f.pattern) compilePattern(f.pattern)
  if (!nil(f.min) && !nil(f.max) && (f.min as number) > (f.max as number)) fail('最小值不能大于最大值')
  for (const v of [f.min, f.max]) if (!nil(v) && (!Number.isFinite(v) || Math.abs(v as number) > MAX_NUMBER)) fail('范围值不合法')
  if (f.type === 'rating' && !nil(f.max) && ((f.max as number) < 1 || (f.max as number) > MaxRatingStars || !Number.isInteger(f.max))) fail(`评分最大值需为 1-${MaxRatingStars} 的整数`)
  if (f.type === 'file' && !nil(f.max) && ((f.max as number) < 1 || (f.max as number) > MaxFiles)) fail(`文件个数上限需为 1-${MaxFiles}`)
  for (const s of [f.minDate, f.maxDate]) {
    if (!s) continue
    if (f.type !== 'date') fail('日期范围只适用于日期字段')
    if (!validDate(s)) fail('日期范围格式应为 yyyy-MM-dd')
  }
  if (f.minDate && f.maxDate && f.minDate > f.maxDate) fail('最早日期不能晚于最晚日期')
  if (f.multiple && f.type !== 'select') fail('multiple 只适用于下拉')
}

/** yyyy-MM-dd 且是真实存在的日期（对应 Go 的 time.Parse("2006-01-02")） */
function validDate(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false
  const d = new Date(`${s}T00:00:00Z`)
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === s
}

// ---- 显隐条件（cond.go）----

const isScalar = (v: unknown) => typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean'

export function validateCond(c: Cond, earlier: Set<string>): void {
  const counter = { nodes: 0 }
  validateCondNode(c, earlier, 1, counter)
}

function validateCondNode(c: Cond, earlier: Set<string>, depth: number, counter: { nodes: number }): void {
  counter.nodes++
  if (depth > MAX_COND_DEPTH || counter.nodes > MAX_COND_NODES) fail('条件过于复杂')
  const kinds = (c.all?.length ? 1 : 0) + (c.any?.length ? 1 : 0) + (c.field || c.op ? 1 : 0)
  if (kinds !== 1) fail('条件必须是 all、any 或 field+op 三种形态之一')
  for (const x of c.all ?? []) validateCondNode(x, earlier, depth + 1, counter)
  for (const x of c.any ?? []) validateCondNode(x, earlier, depth + 1, counter)
  if (c.all?.length || c.any?.length) return
  if (!earlier.has(c.field ?? '')) fail(`条件引用的字段 "${c.field}" 不存在，或排在当前字段之后`)
  switch (c.op) {
    case 'empty':
    case 'notEmpty':
      return
    case 'eq':
    case 'ne':
    case 'contains':
      if (!isScalar(c.value)) fail(`条件 ${c.op} 的值必须是字符串、数字或布尔`)
      return
    case 'gt':
    case 'gte':
    case 'lt':
    case 'lte':
      if (typeof c.value !== 'number' && typeof c.value !== 'string') fail(`条件 ${c.op} 的值必须是数字或字符串`)
      return
    case 'in':
      if (!Array.isArray(c.value) || c.value.length === 0 || c.value.length > MAX_COND_LIST) fail(`条件 in 的值必须是 1-${MAX_COND_LIST} 个元素的数组`)
      if ((c.value as unknown[]).some((v) => !isScalar(v))) fail('条件 in 的元素必须是字符串、数字或布尔')
      return
    default:
      fail(`未知的条件运算符 "${c.op}"`)
  }
}

const isEmptyValue = (v: unknown) => nil(v) || v === '' || (Array.isArray(v) && v.length === 0)
const scalarEq = (a: unknown, b: unknown) => isScalar(a) && typeof a === typeof b && a === b

function containsValue(actual: unknown, want: unknown): boolean {
  if (Array.isArray(actual)) return actual.some((e) => scalarEq(e, want))
  return typeof actual === 'string' && typeof want === 'string' && want !== '' && actual.includes(want)
}

function compare(op: string, a: unknown, b: unknown): boolean {
  const ok = (typeof a === 'number' && typeof b === 'number' && !Number.isNaN(a) && !Number.isNaN(b)) || (typeof a === 'string' && typeof b === 'string')
  if (!ok) return false
  const x = a as number | string
  const y = b as number | string
  return op === 'gt' ? x > y : op === 'gte' ? x >= y : op === 'lt' ? x < y : x <= y
}

/** vals 只包含可见字段的已清洗值；引用到隐藏字段等价于该字段为空。类型不匹配一律得到 false。 */
export function evalCond(c: Cond, vals: Record<string, unknown>): boolean {
  if (c.all?.length) return c.all.every((x) => evalCond(x, vals))
  if (c.any?.length) return c.any.some((x) => evalCond(x, vals))
  const actual = vals[c.field ?? '']
  const empty = isEmptyValue(actual)
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
      return !empty && containsValue(actual, c.value)
    case 'in': {
      const list = Array.isArray(c.value) ? c.value : []
      if (empty) return false
      return Array.isArray(actual) ? actual.some((a) => list.some((x) => scalarEq(a, x))) : list.some((x) => scalarEq(actual, x))
    }
    case 'gt':
    case 'gte':
    case 'lt':
    case 'lte':
      return !empty && compare(c.op, actual, c.value)
    default:
      return false
  }
}

// ---- 提交校验（check.go）----

export interface FieldError {
  key: string
  msg: string
}

/** 按定义重新校验并清洗提交数据；返回清洗后的值或字段错误列表 */
export function validateSubmission(def: FormDefinition, raw: Record<string, unknown>): { clean?: Record<string, unknown>; errors?: FieldError[] } {
  const clean: Record<string, unknown> = {}
  const errors: FieldError[] = []
  if (Object.keys(raw ?? {}).length > MaxFields * 4) return { errors: [{ key: '', msg: '提交的字段过多' }] }
  for (const f of def.fields) {
    if (f.showIf && !evalCond(f.showIf, clean)) continue // 隐藏：不校验、不保存
    if (!hasValue(f)) continue
    const v = raw?.[f.key]
    if (v === undefined || isBlank(f, v)) {
      if (f.required) errors.push({ key: f.key, msg: '必填' })
      continue
    }
    try {
      const norm = checkValue(f, v)
      if (f.type === 'switch' && f.required && norm !== true) {
        errors.push({ key: f.key, msg: '必须开启' })
        continue
      }
      clean[f.key] = norm
    } catch (e) {
      if (!(e instanceof Msg)) throw e
      errors.push({ key: f.key, msg: e.message })
    }
  }
  return errors.length > 0 ? { errors } : { clean }
}

function isBlank(f: FormField, v: unknown): boolean {
  if (nil(v)) return true
  if (typeof v === 'string') return v.trim() === '' && ['text', 'textarea', 'date', 'radio', 'select'].includes(f.type)
  return Array.isArray(v) && v.length === 0
}

/** 把字段错误合成一条 400 消息：最多列 5 条，用字段标题而不是 key（与 service_fill.go 的 fieldErrors 一致） */
export function fieldErrorsMessage(def: FormDefinition, errs: FieldError[]): string {
  const titles = new Map(def.fields.map((f) => [f.key, f.title]))
  let msg = '提交内容校验未通过：'
  for (let i = 0; i < errs.length; i++) {
    if (i === 5) {
      msg += `……（共 ${errs.length} 项）`
      break
    }
    if (i > 0) msg += '；'
    msg += `${titles.get(errs[i].key) || errs[i].key} ${errs[i].msg}`
  }
  return msg
}

function checkValue(f: FormField, v: unknown): unknown {
  switch (f.type) {
    case 'text':
    case 'textarea':
      return checkString(f, v)
    case 'number':
      return checkNumber(f, v)
    case 'radio':
      return checkOne(f, v)
    case 'select':
      return f.multiple ? checkMany(f, v) : checkOne(f, v)
    case 'checkbox':
      return checkMany(f, v)
    case 'date':
      return checkDate(f, v)
    case 'switch':
      return typeof v === 'boolean' ? v : fail('需要布尔值')
    case 'rating':
      return checkRating(f, v)
    case 'file':
      return checkFiles(f, v)
    default:
      return fail('未知字段类型')
  }
}

// 控制字符：NUL、DEL，以及 0x20 以下（textarea 允许换行/回车/制表）
function hasBadControl(s: string, allowNewline: boolean): boolean {
  for (const ch of s) {
    const r = ch.codePointAt(0) ?? 0
    if (r === 0 || r === 0x7f || (r < 0x20 && !(allowNewline && (r === 10 || r === 13 || r === 9)))) return true
  }
  return false
}

function checkString(f: FormField, v: unknown): string {
  if (typeof v !== 'string') return fail('需要文本')
  if (hasBadControl(v, f.type === 'textarea')) return fail('包含非法字符')
  const limit = f.maxLen ?? (f.type === 'textarea' ? DEFAULT_AREA : DEFAULT_TEXT)
  const n = runes(v)
  if (n > limit) return fail(`长度不能超过 ${limit}`)
  if (!nil(f.minLen) && n < (f.minLen as number)) return fail(`长度不能少于 ${f.minLen}`)
  if (f.pattern) {
    let re: RegExp
    try {
      re = compilePattern(f.pattern)
    } catch {
      return fail('字段的正则规则不可用')
    }
    if (!re.test(v)) return fail('格式不正确')
  }
  return v
}

function checkNumber(f: FormField, v: unknown): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) return fail('需要数字')
  if (Math.abs(v) > MAX_NUMBER) return fail('数值超出范围')
  if (f.integer && !Number.isInteger(v)) return fail('需要整数')
  if (!nil(f.min) && v < (f.min as number)) return fail(`不能小于 ${f.min}`)
  if (!nil(f.max) && v > (f.max as number)) return fail(`不能大于 ${f.max}`)
  return v
}

function checkOne(f: FormField, v: unknown): string {
  if (typeof v !== 'string') return fail('需要选项值')
  if (!(f.options ?? []).some((o) => o.value === v)) return fail('不是有效选项')
  return v
}

function checkMany(f: FormField, v: unknown): string[] {
  if (!Array.isArray(v)) return fail('需要选项数组')
  const set = new Set((f.options ?? []).map((o) => o.value))
  if (v.length > set.size) return fail('选择的数量超出选项数')
  const seen = new Set<string>()
  for (const e of v) {
    if (typeof e !== 'string') return fail('需要选项值')
    if (!set.has(e)) return fail('不是有效选项')
    if (seen.has(e)) return fail('选项重复')
    seen.add(e)
  }
  if (!nil(f.min) && v.length < (f.min as number)) return fail(`至少选择 ${f.min} 项`)
  if (!nil(f.max) && v.length > (f.max as number)) return fail(`最多选择 ${f.max} 项`)
  return v as string[]
}

function checkDate(f: FormField, v: unknown): string {
  if (typeof v !== 'string' || v.length !== 10 || !validDate(v)) return fail('日期格式应为 yyyy-MM-dd')
  if (f.minDate && v < f.minDate) return fail(`不能早于 ${f.minDate}`)
  if (f.maxDate && v > f.maxDate) return fail(`不能晚于 ${f.maxDate}`)
  return v
}

function checkRating(f: FormField, v: unknown): number {
  if (typeof v !== 'number' || !Number.isInteger(v)) return fail('需要整数评分')
  const max = f.max ?? 5
  if (v < 1 || v > max) return fail(`评分范围 1-${max}`)
  return v
}

function checkFiles(f: FormField, v: unknown): unknown[] {
  if (!Array.isArray(v)) return fail('需要文件数组')
  const limit = f.max ?? 5
  if (v.length > limit) return fail(`最多 ${limit} 个文件`)
  if (!nil(f.min) && v.length < (f.min as number)) return fail(`至少 ${f.min} 个文件`)
  return v.map((e) => {
    if (!e || typeof e !== 'object' || Array.isArray(e)) return fail('文件格式不正确')
    const m = e as Record<string, unknown>
    const name = typeof m.name === 'string' ? m.name : ''
    const url = typeof m.url === 'string' ? m.url : ''
    if (name === '' || runes(name) > 200 || hasBadControl(name, false)) return fail('文件名不合法')
    if (!validFileURL(url)) return fail('文件地址不合法')
    const item: Record<string, unknown> = { name, url }
    if (typeof m.size === 'number' && m.size >= 0 && m.size < MAX_NUMBER) item.size = m.size
    if (typeof m.type === 'string' && m.type.length <= 100 && !hasBadControl(m.type, false)) item.type = m.type
    return item
  })
}

/** 只接受站内绝对路径或 http(s) 地址，拒绝 javascript:、data: 等可被当作脚本渲染的协议 */
function validFileURL(s: string): boolean {
  if (s === '' || s.length > 500 || hasBadControl(s, false) || /[\\ \t\r\n]/.test(s)) return false
  if (s.startsWith('/')) return !s.startsWith('//')
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== ''
  } catch {
    return false
  }
}
