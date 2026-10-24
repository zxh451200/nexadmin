import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { Button } from '@/framework/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { cn } from '@/lib/utils'
import type { PaginationProps } from './types'

/** 生成页码序列，-1 表示省略号 */
function pageList(page: number, pages: number): number[] {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1)
  const set = new Set([1, pages, page - 1, page, page + 1])
  if (page <= 3) [2, 3, 4].forEach((n) => set.add(n))
  if (page >= pages - 2) [pages - 3, pages - 2, pages - 1].forEach((n) => set.add(n))
  const sorted = [...set].filter((n) => n >= 1 && n <= pages).sort((a, b) => a - b)
  const out: number[] = []
  sorted.forEach((n, i) => {
    if (i > 0 && n - sorted[i - 1] > 1) out.push(-1)
    out.push(n)
  })
  return out
}

export function TablePagination({ page, pageSize, total, onChange, pageSizeOptions = [10, 20, 50, 100] }: PaginationProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const go = (p: number) => onChange(Math.min(Math.max(1, p), pages), pageSize)

  if (isMobile) {
    return (
      <div className="flex items-center justify-between gap-2">
        <Button type="button" variant="outline" disabled={page <= 1} onClick={() => go(page - 1)}>
          <ChevronLeft className="me-1 h-4 w-4 rtl:rotate-180" aria-hidden="true" />
          {t('common.prev')}
        </Button>
        <span className="num text-center text-sm text-muted-foreground">
          {t('dataTable.pageInfo', { page, pages })}
        </span>
        <Button type="button" variant="outline" disabled={page >= pages} onClick={() => go(page + 1)}>
          {t('common.next')}
          <ChevronRight className="ms-1 h-4 w-4 rtl:rotate-180" aria-hidden="true" />
        </Button>
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <span className="num text-sm text-muted-foreground">{t('dataTable.totalCount', { count: total })}</span>
      <div className="flex items-center gap-2">
        <Select value={String(pageSize)} onValueChange={(v) => onChange(1, Number(v))}>
          <SelectTrigger className="w-28" aria-label={t('dataTable.pageSizeLabel')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {pageSizeOptions.map((s) => (
              <SelectItem key={s} value={String(s)}>
                {t('dataTable.pageSizeOption', { size: s })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <nav className="flex items-center gap-1" aria-label="pagination">
          <Button
            type="button"
            variant="outline"
            size="icon"
            disabled={page <= 1}
            onClick={() => go(page - 1)}
            aria-label={t('dataTable.prevPage')}
          >
            <ChevronLeft className="h-4 w-4 rtl:rotate-180" aria-hidden="true" />
          </Button>
          {pageList(page, pages).map((n, i) =>
            n === -1 ? (
              <span key={`e${i}`} className="px-1 text-muted-foreground" aria-hidden="true">
                …
              </span>
            ) : (
              <Button
                key={n}
                type="button"
                size="icon"
                variant={n === page ? 'default' : 'outline'}
                className={cn('num')}
                aria-current={n === page ? 'page' : undefined}
                aria-label={t('dataTable.goToPage', { page: n })}
                onClick={() => go(n)}
              >
                {n}
              </Button>
            ),
          )}
          <Button
            type="button"
            variant="outline"
            size="icon"
            disabled={page >= pages}
            onClick={() => go(page + 1)}
            aria-label={t('dataTable.nextPage')}
          >
            <ChevronRight className="h-4 w-4 rtl:rotate-180" aria-hidden="true" />
          </Button>
        </nav>
      </div>
    </div>
  )
}
