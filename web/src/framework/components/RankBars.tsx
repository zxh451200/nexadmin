export interface RankItem {
  name: string
  value: number
}

/**
 * Top-N 排行条：发丝行 + 同色进度条，颜色由外层 `[data-tone]` 决定（与 StatCard 同一套令牌）。
 * 用于日志统计这类「少量类目比大小」的场景，避免为几行数据起一张图表。
 * 传了 `onSelect` 时整行可点（下钻到带该条件的列表）。
 */
export function RankBars({
  items,
  emptyText,
  onSelect,
}: {
  items: RankItem[]
  emptyText?: string
  onSelect?: (name: string) => void
}) {
  const max = Math.max(1, ...items.map((i) => i.value))
  if (!items.length) {
    return <p className="py-6 text-center text-sm text-muted-foreground">{emptyText ?? '—'}</p>
  }
  return (
    <ul className="space-y-3 py-2">
      {items.map((i) => {
        const body = (
          <>
            <div className="min-w-0">
              <p className="truncate">{i.name}</p>
              <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
                <div className="tone-bar h-full rounded-full" style={{ width: `${(i.value / max) * 100}%` }} />
              </div>
            </div>
            <span className="num text-right text-muted-foreground">{i.value.toLocaleString()}</span>
          </>
        )
        return (
          <li key={i.name} className="text-xs">
            {onSelect ? (
              <button
                type="button"
                onClick={() => onSelect(i.name)}
                className="grid w-full cursor-pointer grid-cols-[minmax(0,1fr)_4rem] items-center gap-3 rounded-md px-1 py-0.5 text-start transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
              >
                {body}
              </button>
            ) : (
              <div className="grid grid-cols-[minmax(0,1fr)_4rem] items-center gap-3 px-1">{body}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}
