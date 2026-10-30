export type JsonParseResult =
  | { ok: true; value: unknown }
  | { ok: false; line: number; column: number }

const LINE_COLUMN = /line (\d+) column (\d+)/
const POSITION = /position (\d+)/

/** 解析失败时尽量还原出错的行列（1 起算）；引擎只给 position 时按偏移量折算 */
export function parseJsonText(text: string): JsonParseResult {
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch (err) {
    const message = err instanceof Error ? err.message : ''
    const located = LINE_COLUMN.exec(message)
    if (located) return { ok: false, line: Number(located[1]), column: Number(located[2]) }
    const positioned = POSITION.exec(message)
    const index = positioned ? Math.min(Number(positioned[1]), text.length) : text.length
    const before = text.slice(0, index)
    return { ok: false, line: before.split('\n').length, column: index - before.lastIndexOf('\n') }
  }
}

export function isJsonObjectValue(value: unknown): boolean {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export type JsonCheck =
  | { ok: true }
  | { ok: false; kind: 'empty' }
  | { ok: false; kind: 'syntax'; line: number; column: number }
  | { ok: false; kind: 'object' }

export interface JsonCheckOptions {
  /** 必须是 JSON 对象，不接受数组与单个值 */
  objectOnly?: boolean
  /** 空串视为合法 */
  optional?: boolean
}

export function checkJsonText(text: string, { objectOnly = true, optional = true }: JsonCheckOptions = {}): JsonCheck {
  if (!text.trim()) return optional ? { ok: true } : { ok: false, kind: 'empty' }
  const parsed = parseJsonText(text)
  if (!parsed.ok) return { ok: false, kind: 'syntax', line: parsed.line, column: parsed.column }
  if (objectOnly && !isJsonObjectValue(parsed.value)) return { ok: false, kind: 'object' }
  return { ok: true }
}

export function isJsonObjectText(text: string): boolean {
  return checkJsonText(text).ok
}

/** 合法时返回重排后的文本，非法返回 null */
export function formatJsonText(text: string, space = 2): string | null {
  const parsed = parseJsonText(text)
  return parsed.ok ? JSON.stringify(parsed.value, null, space) : null
}
