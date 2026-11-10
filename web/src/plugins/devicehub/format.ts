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

// 相对时间（「3 分钟前」）：监控页判断数据新不新鲜靠它，绝对时间只用于追溯。
// 走 Intl.RelativeTimeFormat，不自己拼词，中英和后续语种都不用加字典。
export function formatAgo(iso: string | null | undefined, lang = 'zh-CN'): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const sec = Math.round((d.getTime() - Date.now()) / 1000) // 负数 = 过去
  const abs = Math.abs(sec)
  const rtf = new Intl.RelativeTimeFormat(lang.startsWith('zh') ? 'zh-CN' : 'en-US', { numeric: 'auto' })
  if (abs < 60) return rtf.format(sec, 'second')
  if (abs < 3600) return rtf.format(Math.round(sec / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(sec / 3600), 'hour')
  return rtf.format(Math.round(sec / 86400), 'day')
}
