export type FieldWidget = 'text' | 'textarea' | 'rich'

/** 一个可翻译字段：后端 i18nField 白名单的镜像，由实体 schema 接口下发。 */
export interface LocalizedFieldSpec {
  key: string
  /** i18n 键；后端只负责命名，文案仍在翻译文件里 */
  labelKey: string
  widget: FieldWidget
  /** 与后端裁剪长度同源，前端用它做 maxLength，避免静默截断 */
  max?: number
  rows?: number
}

/** 不随语言变化的字段（slug、封面图…）由消费方自己判断：它知道哪些输入框是自己的。 */
export interface EntitySchema {
  entity: string
  translatable: LocalizedFieldSpec[]
  /** 后端声明该内容是否有「按语言发布」；只有带 published 裸列的内容成立 */
  publishable: boolean
}

export interface LocaleOption {
  code: string
  label: string
  default?: boolean
  /** 公开站 URL 前缀（默认语言为空）：用来把共享的 slug 拼成该语言的完整站内路径 */
  prefix?: string
}

/** {语言: {字段: 译文}}，序列化后即 i18nJson；默认语言不入内。 */
export type LocaleDoc = Record<string, Record<string, string>>
/** {语言: 是否发布}，序列化后即 publishedJson；默认语言的发布态是行的裸列。 */
export type PublishDoc = Record<string, boolean>

/**
 * {语言: 'mt'}：该语言的译文是机器翻译、还没人工校对过。
 * 与 LocaleDoc 同存在 i18nJson 里，用保留键 `_review` 存放；已校对的语言直接不出现（不写 'ok'）。
 */
export type ReviewDoc = Record<string, 'mt'>

/**
 * 批量机翻的传输函数。同一批必须同一种格式（Format 是请求级的），
 * 所以纯文本字段与富文本字段分两批调用。
 */
export type TranslateBatch = (texts: string[], to: string, format: 'text' | 'html') => Promise<string[]>

export interface Completion {
  done: number
  total: number
  /** 未翻译的字段 key，供提示语点名 */
  missing: string[]
}
