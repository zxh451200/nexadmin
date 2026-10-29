import { useCallback, useState, type ReactNode } from 'react'

/**
 * 图例点一下切显隐。
 *
 * Recharts 的 Legend 默认不可交互，而"同一个系列在折线/面积/柱状/报表里各写一遍"很容易漂移，
 * 所以显隐状态与 formatter 收在这里，由各图表把 `hidden` 传给对应系列、把 `formatter` 传给自己的
 * `<Legend>`（Legend 必须是图表的直接子节点，不能包一层组件，否则 Recharts 认不出来）。
 */
export function useLegendToggle() {
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set())

  const toggle = useCallback((key: string) => {
    if (!key) return
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  const formatter = (value: ReactNode, entry: { dataKey?: unknown }, _index?: number) => {
    const key = String(entry?.dataKey ?? '')
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
        className={off ? 'cursor-pointer select-none opacity-40 line-through' : 'cursor-pointer select-none'}
      >
        {value}
      </span>
    )
  }

  return { hidden, toggle, formatter }
}
