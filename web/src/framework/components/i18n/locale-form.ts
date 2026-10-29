import type { Completion, EntitySchema, LocaleDoc, LocaleOption, PublishDoc, ReviewDoc } from './types'

/**
 * 语言表单的纯逻辑：reducer、完成度、序列化。
 * 单独成文件是因为这部分决定了「缺译能不能发布」「切语言会不会丢输入」，
 * 而仓库没有组件测试运行器——把算术剥出来才能用 node --test 上闸。
 */

export function readRecord<T>(raw: string | null | undefined): Record<string, T> {
  if (!raw) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, T>) : {}
  } catch {
    return {}
  }
}

/** 保留键：机翻校对状态与译文存在同一个 i18nJson 里，不占迁移。 */
export const REVIEW_KEY = '_review'

/** 把 i18nJson 拆成「语言→字段」与「语言→机翻未校对」，互不污染。 */
export function splitReview(raw: string | null | undefined): { doc: LocaleDoc; review: ReviewDoc } {
  const all = readRecord<Record<string, string>>(raw)
  const review: ReviewDoc = {}
  const marked = all[REVIEW_KEY]
  if (marked && typeof marked === 'object') {
    for (const [code, v] of Object.entries(marked)) {
      if (v === 'mt') review[code] = 'mt'
    }
  }
  const doc: LocaleDoc = {}
  for (const [code, fields] of Object.entries(all)) {
    if (code === REVIEW_KEY) continue
    if (fields && typeof fields === 'object') doc[code] = fields
  }
  return { doc, review }
}

/** 默认语言的正文就是原文，发布态另有裸列，两者都不进 JSON；空白值不落盘。 */
export function marshalDoc(doc: LocaleDoc, omit: string, review: ReviewDoc = {}): string {
  const out: LocaleDoc = {}
  for (const [code, fields] of Object.entries(doc)) {
    if (code === omit) continue
    // 用 trim 判空，和 blank() 同一口径：否则「只敲了空格」的译文会被当成已翻译，
    // 后端 pick 也认为它有值，页面就渲染出一片空白。
    const kept = Object.fromEntries(Object.entries(fields ?? {}).filter(([, v]) => (v ?? '').trim() !== ''))
    if (Object.keys(kept).length) out[code] = kept
  }
  // 只有译文才谈得上机翻校对；空文档但有标记时也要落盘，否则「填完又清空」会丢掉状态。
  const marks = Object.entries(review).filter(([code]) => code !== omit)
  if (marks.length) out[REVIEW_KEY] = Object.fromEntries(marks)
  return Object.keys(out).length ? JSON.stringify(out) : ''
}

/** false 要显式落盘：「没点名」和「明确不发」在后端是两种语义。 */
export function marshalFlags(flags: PublishDoc, omit: string): string {
  const out: PublishDoc = {}
  for (const [code, on] of Object.entries(flags)) {
    if (code !== omit) out[code] = Boolean(on)
  }
  return Object.keys(out).length ? JSON.stringify(out) : ''
}

/**
 * 老数据从未按语言设置过发布：所有语言沿用整行 published，与后端 localePublished
 * 对空 publishedJson 的口径一致——否则打开弹窗再保存一次，就会把已上线的语言打掉。
 */
export function initialFlags(
  publishedJson: string | undefined,
  locales: LocaleOption[],
  defaultLocale: string,
  sourcePublished: boolean,
): PublishDoc {
  const stored = readRecord<boolean>(publishedJson)
  if (Object.keys(stored).length) return stored
  if (!sourcePublished) return {}
  return Object.fromEntries(locales.filter((l) => l.code !== defaultLocale).map((l) => [l.code, true]))
}

export interface LocaleFormState {
  base: Record<string, string>
  doc: LocaleDoc
  published: PublishDoc
  /** 机翻未校对的语言；人工确认后移除该项 */
  review: ReviewDoc
  sourcePublished: boolean
  active: string
  dirty: boolean
}

export type LocaleFormAction =
  | { type: 'activate'; code: string }
  /** code=null 表示写原文（默认语言）字段 */
  | { type: 'write'; code: string | null; key: string; value: string }
  | { type: 'fillBlanks'; code: string | null; values: Record<string, string> }
  | { type: 'publish'; code: string | null; on: boolean }
  /** on=true 记为该语言机翻未校对；on=false 表示人工已确认 */
  | { type: 'review'; code: string; on: boolean }

export function blank(target: Record<string, string> | undefined, key: string): boolean {
  return (target?.[key] ?? '').trim() === ''
}

export function initialState(args: {
  base: Record<string, string>
  i18nJson?: string
  publishedJson?: string
  sourcePublished: boolean
  defaultLocale: string
  locales: LocaleOption[]
}): LocaleFormState {
  const { doc, review } = splitReview(args.i18nJson)
  return {
    base: { ...args.base },
    doc,
    published: initialFlags(args.publishedJson, args.locales, args.defaultLocale, args.sourcePublished),
    review,
    sourcePublished: args.sourcePublished,
    active: args.defaultLocale,
    dirty: false,
  }
}

export function localeFormReducer(s: LocaleFormState, a: LocaleFormAction): LocaleFormState {
  switch (a.type) {
    case 'activate':
      return a.code === s.active ? s : { ...s, active: a.code }

    case 'write': {
      if (a.code === null) {
        if (s.base[a.key] === a.value) return s
        return { ...s, dirty: true, base: { ...s.base, [a.key]: a.value } }
      }
      const cur = s.doc[a.code] ?? {}
      if (cur[a.key] === a.value) return s
      return { ...s, dirty: true, doc: { ...s.doc, [a.code]: { ...cur, [a.key]: a.value } } }
    }

    case 'fillBlanks': {
      const target = a.code === null ? s.base : s.doc[a.code]
      const keys = Object.keys(a.values).filter((k) => blank(target, k) && (a.values[k] ?? '').trim() !== '')
      if (!keys.length) return s
      const patch = Object.fromEntries(keys.map((k) => [k, a.values[k]]))
      if (a.code === null) return { ...s, dirty: true, base: { ...s.base, ...patch } }
      return { ...s, dirty: true, doc: { ...s.doc, [a.code]: { ...(s.doc[a.code] ?? {}), ...patch } } }
    }

    case 'publish': {
      if (a.code === null) {
        return s.sourcePublished === a.on ? s : { ...s, dirty: true, sourcePublished: a.on }
      }
      if (Boolean(s.published[a.code]) === a.on) return s
      return { ...s, dirty: true, published: { ...s.published, [a.code]: a.on } }
    }

    case 'review': {
      if (a.on) {
        return s.review[a.code] === 'mt' ? s : { ...s, dirty: true, review: { ...s.review, [a.code]: 'mt' } }
      }
      if (!s.review[a.code]) return s
      const next = { ...s.review }
      delete next[a.code]
      return { ...s, dirty: true, review: next }
    }
  }
}

/** 某语言的字段值：原文取裸列，其余取译文文档。 */
export function valuesOf(s: LocaleFormState, code: string, defaultLocale: string): Record<string, string> {
  return code === defaultLocale ? s.base : (s.doc[code] ?? {})
}

/**
 * 完成度只统计「原文非空」的可翻译字段：原文本身为空时各语言渲染一致，
 * 没有「伪装成外语的中文页」要防，不该挡住发布。与 Go 侧 localeTranslatable 同口径。
 */
export function completionOf(
  schema: EntitySchema,
  s: LocaleFormState,
  code: string,
  defaultLocale: string,
): Completion {
  const vals = valuesOf(s, code, defaultLocale)
  const required = schema.translatable.filter((f) => !blank(s.base, f.key))
  const missing = required.filter((f) => blank(vals, f.key)).map((f) => f.key)
  return { done: required.length - missing.length, total: required.length, missing }
}

/** 原文的发布态就是行的裸列，永远可发布；外语要看完成度。 */
export function publishableOf(
  schema: EntitySchema,
  s: LocaleFormState,
  code: string,
  defaultLocale: string,
): { ok: boolean; missing: string[] } {
  if (code === defaultLocale) return { ok: true, missing: [] }
  const missing = completionOf(schema, s, code, defaultLocale).missing
  return { ok: missing.length === 0, missing }
}
