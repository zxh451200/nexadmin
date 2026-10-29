import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChartCard } from './ChartCard'
import { TrendChart } from './TrendChart'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { api } from '@/framework/request/http'
import { cn } from '@/lib/utils'

export interface MetricPoint {
  ts: number
  metrics: Record<string, number>
}

interface MetricHistoryView {
  from: string
  to: string
  stepSeconds: number
  points: MetricPoint[]
}

/** 可切换的时间窗口（小时）。168 = 保留期上限。 */
const WINDOWS = [1, 6, 24, 168] as const

const windowKey: Record<number, string> = { 1: 'w1', 6: 'w6', 24: 'w24', 168: 'w168' }

const round = (v: number) => Math.round(v * 100) / 100

/**
 * 监控历史趋势：读 /monitor/<kind>/history（由 monitorsnap 周期落库），
 * 与页面顶部的实时曲线不同，这里是真·历史，最长覆盖整个保留期。
 * labels 由页面给出（键名 → 已翻译的显示名），只有实际落库过的指标才会出现在下拉里，
 * 所以非 Linux 主机不会看到一条假的 CPU 平线。
 */
export function MetricHistory({
  kind,
  title,
  labels,
  defaultHours = 6,
}: {
  kind: 'server' | 'cache' | 'api'
  title: string
  labels: Record<string, string>
  defaultHours?: number
}) {
  const { t, i18n } = useTranslation()
  const [hours, setHours] = useState<number>(defaultHours)
  const [picked, setPicked] = useState<string | undefined>(undefined)
  const { data, isPending } = useQuery({
    queryKey: ['monitor', kind, 'history', hours],
    queryFn: () => api.get<MetricHistoryView>(`/monitor/${kind}/history`, { hours }, { silent: true }),
    refetchInterval: 60_000,
  })

  const keys = useMemo(() => {
    const seen = new Set<string>()
    for (const p of data?.points ?? []) {
      for (const k of Object.keys(p.metrics)) seen.add(k)
    }
    return Object.keys(labels).filter((k) => seen.has(k))
  }, [data, labels])

  const active = picked && keys.includes(picked) ? picked : keys[0]

  const rows = useMemo(() => {
    if (!active) return [] as Record<string, unknown>[]
    const wide = hours >= 48
    const fmt = (ts: number) =>
      new Date(ts).toLocaleString(
        i18n.language,
        wide
          ? { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }
          : { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false },
      )
    return (data?.points ?? []).map((p) => ({ time: fmt(p.ts), value: round(p.metrics[active] ?? 0) }))
  }, [data, active, hours, i18n.language])

  const step = data?.stepSeconds
    ? ` · ${t('monitorHistory.step')} ${data.stepSeconds < 120 ? `${data.stepSeconds}${t('monitorHistory.stepSecond')}` : `${Math.round(data.stepSeconds / 60)}${t('monitorHistory.stepMinute')}`}`
    : ''

  return (
    <ChartCard
      title={title + step}
      actions={
        <div className="flex flex-wrap items-center justify-end gap-2">
          <div className="flex items-center gap-1" role="group" aria-label={t('monitorHistory.window')}>
            {WINDOWS.map((w) => (
              <button
                key={w}
                type="button"
                onClick={() => setHours(w)}
                aria-pressed={hours === w}
                className={cn(
                  'inline-flex h-8 items-center rounded-full px-2.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none md:h-7',
                  hours === w ? 'bg-background font-medium text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {t(`monitorHistory.${windowKey[w]}`)}
              </button>
            ))}
          </div>
          {keys.length > 0 ? (
            <Select value={active} onValueChange={(v) => setPicked(String(v))}>
              <SelectTrigger className="h-8 w-[9.5rem] text-xs md:h-7" aria-label={t('monitorHistory.metric')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {keys.map((k) => (
                  <SelectItem key={k} value={k}>
                    {labels[k]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
        </div>
      }
    >
      {isPending ? (
        <Skeleton className="h-[280px] w-full" />
      ) : rows.length === 0 ? (
        <div className="flex h-[280px] flex-col items-center justify-center gap-1 text-center">
          <p className="text-sm font-medium">{t('monitorHistory.empty')}</p>
          <p className="max-w-sm text-xs text-muted-foreground">{t('monitorHistory.emptyHint')}</p>
        </div>
      ) : (
        <TrendChart data={rows} xKey="time" series={[{ key: 'value', label: active ? labels[active] : '' }]} type="area" switchable={false} height={280} />
      )}
    </ChartCard>
  )
}
