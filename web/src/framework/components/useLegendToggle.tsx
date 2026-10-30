import { useCallback, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * 图例点一下切显隐。
 *
 * Recharts 的 Legend 默认不可交互，而"同一个系列在折线/面积/柱状/报表里各写一遍"很容易漂移，
 * 所以显隐状态与 formatter 收在这里，由各图表把 `hidden` 传给对应系列、把 `formatter` 传给自己的
 * `<Legend>`（Legend 必须是图表的直接子节点，不能包一层组件，否则 Recharts 认不出来）。
 *
 * 关闭态用「置灰」而不是删除线（ECharts/Highcharts 同一做法），并且不允许关掉最后一个系列：
 * 全关之后图表只剩一块空白，用户没有别的返回路径。
 */
export function useLegendToggle(
  keyOf?: (value: ReactNode, entry: { dataKey?: unknown }) => string,
  keys?: readonly string[],
) {
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set())

  const toggle = useCallback(
    (key: string) => {
      if (!key) return
      setHidden((prev) => {
        const next = new Set(prev)
        if (next.has(key)) {
          next.delete(key)
          return next
        }
        if (keys && keys.length > 0) {
          const visible = keys.reduce((n, k) => (next.has(k) ? n : n + 1), 0)
          if (visible <= 1) return prev
        }
        next.add(key)
        return next
      })
    },
    [keys],
  )

  const formatter = (value: ReactNode, entry: { dataKey?: unknown }, _index?: number) => {
    // 饼图的每个扇区共用 dataKey="value"，按 dataKey 切会把整张饼一起切掉，所以允许调用方换取值方式。
    const key = keyOf ? keyOf(value, entry) : String(entry?.dataKey ?? '')
    const off = hidden.has(key)
    return (
      <span
        role="button"
        tabIndex={0}
        aria-pressed={!off}
        title={key}
        onClick={() => toggle(key)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            toggle(key)
          }
        }}
        // 关掉态不能只压透明度：recharts 已把 inactive 项刷成 muted 色，再叠 opacity 才和图例里"隐身"区分开
        className={cn(
          'cursor-pointer select-none rounded-sm px-0.5 transition-opacity focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40',
          off ? 'opacity-40 hover:opacity-70' : 'hover:opacity-80',
        )}
      >
        {value}
      </span>
    )
  }

  return { hidden, toggle, formatter }
}
