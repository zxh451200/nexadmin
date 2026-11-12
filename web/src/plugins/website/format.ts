export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      }).format(d)
}

/** slug 列宽，与后端 pkg/slug.MaxLen 同值。 */
const SLUG_MAX = 64

/** 兜底随机串：8 位小写 base36，与后端 pkg/slug.Random 同形态。 */
function randomSlug(): string {
  const alphabet = '0123456789abcdefghijklmnopqrstuvwxyz'
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return [...bytes].map((b) => alphabet[b % alphabet.length]).join('')
}

/**
 * 标题转 slug，与后端 pkg/slug 同一套行业标准：
 * NFKD 去变音符（é→e）→ 小写 → 连续的非 [a-z0-9] 压成单个连字符 → 去首尾连字符 → 限长 64。
 * 纯中文等无法转写的标题回退随机 base36 串——后端生成口径一致，前端预填只是省一次输入。
 */
export function slugify(title: string): string {
  const s = title
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, SLUG_MAX)
    .replace(/-+$/g, '')
  return s || randomSlug()
}
