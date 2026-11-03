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

/**
 * 标题转 slug：小写、空白折叠成连字符、剔除非 [a-z0-9-] 字符。
 * 纯中文等无法转写的标题回退 page-<时间戳>——宁可给个能改的占位，也不让保存被卡住。
 */
export function slugify(title: string): string {
  const s = title
    .trim()
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
    .replace(/-{2,}/g, '-')
    .replace(/^-+|-+$/g, '')
  return s || `page-${Date.now().toString(36)}`
}
