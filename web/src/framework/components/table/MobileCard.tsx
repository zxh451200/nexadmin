import { useState, type ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { renderCell, type Column } from './types'

interface Props<T> {
  columns: Column<T>[]
  row: T
  index: number
  /** 卡片左侧：多选框 / 拖动手柄 / 展开按钮 */
  leading?: ReactNode
  actions?: ReactNode
  /** 点击卡片主体区域 */
  onClick?: () => void
  /** 桌面窄栏用的紧凑行：标题/副标题 + 右侧操作，一行搞定，不显示其余字段 */
  dense?: boolean
}

/** 默认只显示前几个字段，其余点「更多」展开，避免一张卡片拉得很长 */
const COLLAPSED_FIELDS = 4

/**
 * 手机卡片主体：title 加粗做标题，sub 做副标题，normal 字段排成两列（小标签在上、值在下），
 * hidden 不显示；字段多时折叠；操作按钮收在底部一行。
 */
export function MobileCardContent<T>({ columns, row, index, leading, actions, onClick, dense }: Props<T>) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)

  const visible = columns.filter((c) => c.mobile !== 'hidden')
  const titleCol = visible.find((c) => c.mobile === 'title') ?? visible.find((c) => c.mobile !== 'sub')
  const subCols = visible.filter((c) => c.mobile === 'sub' && c !== titleCol)
  const normalCols = visible.filter((c) => c !== titleCol && c.mobile !== 'sub')
  const foldable = normalCols.length > COLLAPSED_FIELDS
  const shownCols = foldable && !expanded ? normalCols.slice(0, COLLAPSED_FIELDS) : normalCols

  if (dense) {
    return (
      <div className="flex items-center gap-1">
        {leading ? <div className="flex shrink-0 items-center">{leading}</div> : null}
        <div
          className={cn('min-w-0 flex-1 py-0.5', onClick && 'cursor-pointer')}
          onClick={onClick}
          role={onClick ? 'button' : undefined}
          tabIndex={onClick ? 0 : undefined}
          onKeyDown={
            onClick
              ? (e) => {
                  if (e.key === 'Enter' && e.target === e.currentTarget) onClick()
                }
              : undefined
          }
        >
          {titleCol ? <div className="truncate text-sm font-medium leading-snug text-foreground">{renderCell(titleCol, row, index)}</div> : null}
          {subCols.map((c) => (
            <div key={c.key} className="truncate text-xs text-muted-foreground">
              {renderCell(c, row, index)}
            </div>
          ))}
        </div>
        {actions ? <div className="flex shrink-0 items-center">{actions}</div> : null}
      </div>
    )
  }

  return (
    <>
      <div className="flex items-start gap-1">
        {leading ? <div className="-ms-1 flex shrink-0 items-center">{leading}</div> : null}
        <div
          className={cn('min-w-0 flex-1', leading ? 'py-1' : 'py-0.5', onClick && 'cursor-pointer')}
          onClick={onClick}
          role={onClick ? 'button' : undefined}
          tabIndex={onClick ? 0 : undefined}
          onKeyDown={
            onClick
              ? (e) => {
                  if (e.key === 'Enter' && e.target === e.currentTarget) onClick()
                }
              : undefined
          }
        >
          {titleCol ? (
            <div className="break-words text-[15px] font-semibold leading-snug text-foreground">{renderCell(titleCol, row, index)}</div>
          ) : null}
          {subCols.map((c) => (
            <div key={c.key} className="mt-0.5 break-words text-xs text-muted-foreground">
              {renderCell(c, row, index)}
            </div>
          ))}
          {shownCols.length > 0 ? (
            <dl className="mt-2.5 grid grid-cols-2 gap-x-4 gap-y-2.5">
              {shownCols.map((c) => (
                <div key={c.key} className="min-w-0">
                  <dt className="text-xs text-muted-foreground">{c.title}</dt>
                  <dd className="mt-0.5 min-w-0 break-words text-sm text-foreground">{renderCell(c, row, index)}</dd>
                </div>
              ))}
            </dl>
          ) : null}
        </div>
      </div>

      {foldable || actions ? (
        <div className="mt-2 flex items-center justify-between gap-2 border-t border-border/60 pt-1.5">
          {foldable ? (
            <button
              type="button"
              aria-expanded={expanded}
              onClick={() => setExpanded((v) => !v)}
              className="inline-flex h-9 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {expanded ? t('dataTable.collapse') : t('common.more')}
              <ChevronDown className={cn('size-3.5 transition-transform motion-reduce:transition-none', expanded && 'rotate-180')} aria-hidden="true" />
            </button>
          ) : (
            <span />
          )}
          <div className="flex flex-wrap items-center justify-end gap-0.5">{actions}</div>
        </div>
      ) : null}
    </>
  )
}
