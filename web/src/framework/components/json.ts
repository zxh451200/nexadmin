export type JsonParseResult =
  | { ok: true; value: unknown }
  | { ok: false; line: number; column: number }

const LINE_COLUMN = /line (\d+) column (\d+)/
const POSITION = /position (\d+)/
const TOKEN = /Unexpected token '([^']*)'/

function lineColumnAt(text: string, index: number): { line: number; column: number } {
  const clamped = Math.min(Math.max(index, 0), text.length)
  const before = text.slice(0, clamped)
  return { line: before.split('\n').length, column: clamped - before.lastIndexOf('\n') }
}

/** 解析失败时尽量还原出错的行列（1 起算）；引擎只给位置或只给字符时按偏移量折算 */
export function parseJsonText(text: string): JsonParseResult {
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch (err) {
    const message = err instanceof Error ? err.message : ''
    const located = LINE_COLUMN.exec(message)
    if (located) return { ok: false, line: Number(located[1]), column: Number(located[2]) }
    const positioned = POSITION.exec(message)
    if (positioned) return { ok: false, ...lineColumnAt(text, Number(positioned[1])) }
    // 部分引擎只报出错的字符：定位到它的首次出现处，找不到再看首个非空白字符，避免指到文本末尾
    const token = TOKEN.exec(message)?.[1]
    const tokenAt = token ? text.indexOf(token) : -1
    const firstChar = text.search(/\S/)
    return { ok: false, ...lineColumnAt(text, tokenAt >= 0 ? tokenAt : Math.max(firstChar, 0)) }
  }
}

export function isJsonObjectValue(value: unknown): boolean {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export type JsonCheck =
  | { ok: true }
  | { ok: false; kind: 'empty' }
  | { ok: false; kind: 'syntax'; line: number; column: number; reason?: JsonSyntaxReason }
  | { ok: false; kind: 'object' }

/** 常见语法错误的原因：用于给出「具体怎么改」，而不是只说引号逗号括号不配对 */
export type JsonSyntaxReason = 'fullWidth' | 'singleQuote' | 'comment' | 'trailingComma' | 'unclosed'

/** 把双引号字符串的内容替换成空格（保留换行）：只看字符串外部，避免把内容里的撇号 / 引号误判成语法错误 */
function maskStrings(text: string): string {
  let out = ''
  let inString = false
  let escaped = false
  for (const ch of text) {
    if (inString) {
      if (escaped) {
        escaped = false
        out += ch === '\n' ? '\n' : ' '
      } else if (ch === '\\') {
        escaped = true
        out += ' '
      } else if (ch === '"') {
        inString = false
        out += '"'
      } else {
        out += ch === '\n' ? '\n' : ' '
      }
    } else if (ch === '"') {
      inString = true
      out += '"'
    } else {
      out += ch
    }
  }
  return out
}

function bracketsPaired(text: string): boolean {
  const stack: string[] = []
  for (const ch of text) {
    if (ch === '{' || ch === '[') stack.push(ch)
    else if (ch === '}' || ch === ']') {
      const open = stack.pop()
      if ((ch === '}' && open !== '{') || (ch === ']' && open !== '[')) return false
    }
  }
  return stack.length === 0
}

export function diagnoseJsonSyntax(text: string): JsonSyntaxReason | undefined {
  const masked = maskStrings(text)
  if (/[“”‘’，：]/.test(masked)) return 'fullWidth'
  if (/'/.test(masked)) return 'singleQuote'
  if (/\/[/*]/.test(masked)) return 'comment'
  if (/,\s*[}\]]/.test(masked)) return 'trailingComma'
  if (!bracketsPaired(masked)) return 'unclosed'
  return undefined
}

export interface JsonCheckOptions {
  /** 必须是 JSON 对象，不接受数组与单个值 */
  objectOnly?: boolean
  /** 空串视为合法 */
  optional?: boolean
}

export function checkJsonText(text: string, { objectOnly = true, optional = true }: JsonCheckOptions = {}): JsonCheck {
  if (!text.trim()) return optional ? { ok: true } : { ok: false, kind: 'empty' }
  const parsed = parseJsonText(text)
  if (!parsed.ok) {
    return { ok: false, kind: 'syntax', line: parsed.line, column: parsed.column, reason: diagnoseJsonSyntax(text) }
  }
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
