// 与 plugins/ticket/format.ts 同口径：两个坐席工作台的时间格式与状态色语义保持一致，
// 并排打开时不会出现「同一份进行中，一边红一边绿」的色彩语言冲突。
export function formatTime(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }).format(d)
}

/** 只有时分：日期交给消息流的日期分隔，气泡里不再重复年月日。 */
export function formatClock(iso: string | null | undefined, lang: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit', hour12: false }).format(d)
}

/** 把原始 UA 归纳成「系统 · 浏览器」；认不出时退回截断的原文，别让坐席看一整串 token。 */
export function uaSummary(ua: string | undefined | null): string {
  const s = ua || ''
  if (!s) return ''
  const browser = /Edg\//.test(s)
    ? 'Edge'
    : /OPR\//.test(s)
      ? 'Opera'
      : /Firefox\//.test(s)
        ? 'Firefox'
        : /Chrome\//.test(s)
          ? 'Chrome'
          : /Safari\//.test(s)
            ? 'Safari'
            : ''
  const os = /Windows NT/.test(s)
    ? 'Windows'
    : /Android/.test(s)
      ? 'Android'
      : /iPhone|iPad|iPod/.test(s)
        ? 'iOS'
        : /Mac OS X/.test(s)
          ? 'macOS'
          : /Linux/.test(s)
            ? 'Linux'
            : ''
  return [os, browser].filter(Boolean).join(' · ') || s.slice(0, 60)
}

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()

const daysAgo = (d: Date) => Math.round((startOfDay(new Date()) - startOfDay(d)) / 86_400_000)

/** 会话列表右上角的时间：今天给时分、昨天给文案、更早给日期。 */
export function relativeStamp(
  iso: string | null | undefined,
  lang: string,
): { kind: 'time' | 'yesterday' | 'date'; text: string } | null {
  if (!iso) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  const days = daysAgo(d)
  if (days <= 0) return { kind: 'time', text: formatClock(iso, lang) }
  if (days === 1) return { kind: 'yesterday', text: '' }
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return {
    kind: 'date',
    text: new Intl.DateTimeFormat(lang, {
      ...(sameYear ? {} : { year: '2-digit' }),
      month: '2-digit',
      day: '2-digit',
    }).format(d),
  }
}

/** 消息流的日期分隔：今天/昨天走词典，更早用本地化日期。 */
export function daySeparator(
  iso: string | null | undefined,
  lang: string,
): { kind: 'today' | 'yesterday' | 'date'; text: string } | null {
  if (!iso) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  const days = daysAgo(d)
  if (days <= 0) return { kind: 'today', text: '' }
  if (days === 1) return { kind: 'yesterday', text: '' }
  return { kind: 'date', text: new Intl.DateTimeFormat(lang, { dateStyle: 'medium' }).format(d) }
}

/** 头像占位：中文取首个汉字，西文取两段首字母。 */
export function initials(name: string | null | undefined): string {
  const s = (name ?? '').trim()
  if (!s) return '?'
  const cjk = s.match(/[\u3400-\u9fff]/)
  if (cjk) return cjk[0]
  const parts = s.split(/\s+/)
  return (parts.length > 1 ? parts[0][0] + parts[1][0] : s[0]).toUpperCase()
}

// open（进行中）映射 info：与工单的 processing 同语义（蓝）；closed（已结束）与工单的
// closed/resolved 收尾档一致走 secondary。文件大小格式化直接用框架的 formatSize（upload/precheck）。
export function statusVariant(s: string): 'success' | 'destructive' | 'info' | 'warning' | 'secondary' {
  if (s === 'open') return 'info'
  return 'secondary'
}
