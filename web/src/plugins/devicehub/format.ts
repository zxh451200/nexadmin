// 时间格式化：与 ticket/im 的同款 Intl 短格式（三处重复是已知债，框架统一前各自维护）。
export function formatTime(iso: string | null | undefined, lang = 'zh-CN'): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat(lang.startsWith('zh') ? 'zh-CN' : 'en-US', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(d)
}

// 秒级采样（bucket 形如 5s/30s）需要精确到秒，否则同一分钟内的点挤成同一个 x 轴标签。
export function formatTimeSec(iso: string | null | undefined, lang = 'zh-CN'): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat(lang.startsWith('zh') ? 'zh-CN' : 'en-US', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(d)
}
