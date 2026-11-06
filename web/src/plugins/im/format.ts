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

// open（进行中）映射 info：与工单的 processing 同语义（蓝）；closed（已结束）与工单的
// closed/resolved 收尾档一致走 secondary。文件大小格式化直接用框架的 formatSize（upload/precheck）。
export function statusVariant(s: string): 'success' | 'destructive' | 'info' | 'warning' | 'secondary' {
  if (s === 'open') return 'info'
  return 'secondary'
}
