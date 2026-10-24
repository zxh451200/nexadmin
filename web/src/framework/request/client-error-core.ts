// 前端错误上报的纯逻辑（无 DOM / 无网络依赖，便于单独验证）：脱敏、字段裁剪、指纹、节流与去重。
// 服务端 modules/clienterror 对同样的规则再做一遍（客户端的限制不可信）。

export const MAX_MESSAGE = 500
export const MAX_STACK_LINES = 20
export const MAX_STACK = 4000
export const MAX_ROUTE = 255
export const MAX_UA = 255
export const MAX_VERSION = 32
/** 序列化后的请求体上限；服务端硬上限是 8KB，这里留出余量 */
export const MAX_BODY_BYTES = 6 * 1024

const REDACTED = '[REDACTED]'

// key=value / "key":"value" 形式的敏感项：只抹值，保留键名方便排查
const SECRET_KV =
  /((?:"|')?(?:proxy-authorization|authorization|access[_-]?token|refresh[_-]?token|id[_-]?token|token|password|passwd|pwd|secret|api[_-]?key|set-cookie|cookie|credential)s?(?:"|')?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:Bearer|Basic)\s+[^\s,;&"'}]+|[^\s,;&"'}]+)/gi
const BEARER = /\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{8,}/gi
const JWT = /\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*/g
const USERINFO = /(\b[a-z][a-z0-9+.-]*:\/\/)[^/\s:@]+:[^/\s@]+@/gi

/** 抹掉令牌、密码、Authorization 等敏感值 */
export function scrubSensitive(s: string): string {
  if (!s) return s
  return s
    .replace(SECRET_KV, `$1${REDACTED}`)
    .replace(BEARER, `Bearer ${REDACTED}`)
    .replace(JWT, REDACTED)
    .replace(USERINFO, `$1${REDACTED}@`)
}

const CONTROL = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g

function clip(s: string, n: number): string {
  const chars = Array.from(s)
  return chars.length <= n ? s : chars.slice(0, n).join('')
}

export interface ErrorPayload {
  route: string
  message: string
  stack: string
  ua: string
  version: string
}

export interface RawError {
  message: string
  stack?: string
  route: string
  ua: string
  version: string
}

const byteLength = (s: string) => new TextEncoder().encode(s).length

/** 脱敏 + 裁剪成上报内容；消息为空返回 null；整体超出 MAX_BODY_BYTES 时逐步缩短堆栈，仍超限则放弃 */
export function buildPayload(raw: RawError): ErrorPayload | null {
  const message = clip(scrubSensitive(raw.message ?? '').replace(CONTROL, ' ').trim(), MAX_MESSAGE)
  if (!message) return null
  const stackLines = (raw.stack ?? '').split(/\r?\n/).slice(0, MAX_STACK_LINES).join('\n')
  const route = (raw.route ?? '').split(/[?#]/)[0]
  const p: ErrorPayload = {
    route: clip(scrubSensitive(route), MAX_ROUTE),
    message,
    stack: clip(scrubSensitive(stackLines).replace(CONTROL, ' '), MAX_STACK),
    ua: clip(raw.ua ?? '', MAX_UA),
    version: clip(raw.version ?? '', MAX_VERSION),
  }
  while (byteLength(JSON.stringify(p)) > MAX_BODY_BYTES && p.stack.length > 0) {
    p.stack = p.stack.slice(0, Math.floor(p.stack.length / 2))
  }
  return byteLength(JSON.stringify(p)) <= MAX_BODY_BYTES ? p : null
}

/** 同类错误的指纹：数字（ID、行列号）和构建产物的哈希文件名不参与，所以同一个 bug 只算一类 */
export function fingerprint(p: Pick<ErrorPayload, 'message' | 'stack'>): string {
  const norm = (s: string) =>
    s
      .replace(/[-.][A-Za-z0-9_]{8,}\.(m?js|css)\b/g, '.$1')
      .replace(/\d+/g, '#')
      .replace(/\s+/g, ' ')
      .trim()
  return `${norm(p.message)}|${norm(p.stack.split('\n').slice(0, 3).join('\n'))}`
}

export interface ThrottleOptions {
  /** 每分钟最多上报几条 */
  maxPerMinute: number
  /** 同一指纹在这个时间窗内只报一次 */
  dedupeWindowMs: number
  now?: () => number
}

const MAX_SEEN = 100

/** 节流 + 去重：allow() 返回 true 才应该真正发送 */
export function createThrottle(opts: ThrottleOptions) {
  const now = opts.now ?? Date.now
  const sent: number[] = []
  const seen = new Map<string, number>()
  return {
    allow(fp: string): boolean {
      const t = now()
      while (sent.length > 0 && t - sent[0] >= 60_000) sent.shift()
      const last = seen.get(fp)
      if (last !== undefined && t - last < opts.dedupeWindowMs) return false
      if (sent.length >= opts.maxPerMinute) return false
      sent.push(t)
      seen.delete(fp)
      seen.set(fp, t)
      if (seen.size > MAX_SEEN) seen.delete(seen.keys().next().value as string)
      return true
    },
  }
}

/** 浏览器里噪声大、没有排查价值的错误 */
export function isIgnorable(message: string): boolean {
  return /ResizeObserver loop (limit exceeded|completed with undelivered notifications)/i.test(message)
}
