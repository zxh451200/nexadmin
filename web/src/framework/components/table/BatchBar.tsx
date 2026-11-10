import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'

/**
 * 批量操作条：浮在表格底部，不占文档流高度。
 * 之前它是一行插入的区块，勾选任意一行都会把表格整体下推一格；
 * 现在用 0 高 sticky 定位：表格比视口高时贴住视口底边，表格较短时落在分页那一行，布局始终不变。
 * `-mt-3` 抵消外层 flex 的 gap-3，否则这一行即使 0 高也会把分页推开 12px。
 */
export function BatchBar({ count, onClear, children }: { count: number; onClear: () => void; children: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="pointer-events-none sticky bottom-4 z-30 -mt-3 h-0" role="region" aria-label={t('dataTable.batchActions')}>
      <div className="absolute inset-x-0 bottom-0 flex justify-center px-2">
        <div className="pointer-events-auto flex max-w-full flex-wrap items-center gap-1 rounded-2xl border border-border bg-popover px-2 py-1.5 text-popover-foreground overlay-panel animate-in fade-in-0 slide-in-from-bottom-1 duration-150 motion-reduce:animate-none">
          <span className="num ps-2 text-sm text-foreground" aria-live="polite">
            {t('dataTable.selectedCount', { count })}
          </span>
          <Button type="button" variant="ghost" size="sm" onClick={onClear}>
            {t('common.clear')}
          </Button>
          <div className="flex flex-wrap items-center gap-2 pe-0.5">{children}</div>
        </div>
      </div>
    </div>
  )
}
