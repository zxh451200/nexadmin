import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { queryScreenChart, type Chart, type Screen, type ScreenItem } from './api'
import { ChartView } from './ChartView'
import { GRID_COLS } from './chartData'
import { useChartResult } from './useChartResult'
import { builtinText } from './builtinText'

export const ROW_PX = 40
export const GAP_PX = 12
const HEADER_PX = 36

const itemHeight = (h: number) => h * ROW_PX + (h - 1) * GAP_PX

function ScreenCell({ screenId, item, chart, refreshMs }: { screenId: string; item: ScreenItem; chart?: Chart; refreshMs: number }) {
  const { t } = useTranslation()
  const q = useChartResult(['screen', screenId, item.chartId], () => queryScreenChart(screenId, item.chartId), { enabled: !!chart, refetchMs: refreshMs })
  const title = builtinText(t, item.title || chart?.name) || t('plugin_report.report.chartDeleted')
  return (
    <section className="flex h-full min-w-0 flex-col overflow-hidden rounded-xl border border-border bg-card text-card-foreground" aria-label={title}>
      <h3 className="truncate px-3 text-sm font-medium text-muted-foreground" style={{ lineHeight: `${HEADER_PX}px` }}>
        {title}
      </h3>
      <div className="min-h-0 flex-1 px-2 pb-2">
        {chart ? (
          <ChartView type={chart.type} result={q.data} config={chart.config} height={Math.max(60, itemHeight(item.h) - HEADER_PX - 12)} loading={q.isLoading} error={q.isError} />
        ) : null}
      </div>
    </section>
  )
}

interface Props {
  screen: Screen
  /** 自动刷新间隔（毫秒），0 = 不刷新；页面不在前台时自动暂停 */
  refreshMs: number
}

/**
 * 大屏画布（只读）：24 列栅格，组件按 JSON 里的 x/y/w/h 摆放。
 * 手机上退化为按位置顺序的单列堆叠。颜色全部来自语义 token，亮/暗跟应用主题走。
 */
export function ScreenCanvas({ screen, refreshMs }: Props) {
  const isMobile = useIsMobile()
  const items = useMemo(() => [...screen.config.items].sort((a, b) => a.y - b.y || a.x - b.x), [screen])
  return (
    <div
      className="grid"
      style={{
        gap: GAP_PX,
        gridTemplateColumns: isMobile ? '1fr' : `repeat(${GRID_COLS}, minmax(0, 1fr))`,
        gridAutoRows: isMobile ? undefined : ROW_PX,
      }}
    >
      {items.map((it) => (
        <div
          key={it.chartId}
          className="min-w-0"
          style={isMobile ? { height: itemHeight(Math.max(it.h, 5)) } : { gridColumn: `${it.x + 1} / span ${it.w}`, gridRow: `${it.y + 1} / span ${it.h}` }}
        >
          <ScreenCell screenId={screen.id} item={isMobile ? { ...it, h: Math.max(it.h, 5) } : it} chart={screen.charts?.find((c) => c.id === it.chartId)} refreshMs={refreshMs} />
        </div>
      ))}
    </div>
  )
}
