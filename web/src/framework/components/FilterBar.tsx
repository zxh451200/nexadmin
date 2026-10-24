import { useState, type ReactNode } from 'react'
import { Filter, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { Button } from '@/framework/ui/button'
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle } from '@/framework/ui/drawer'
import { cn } from '@/lib/utils'

export interface FilterBarProps {
  children: ReactNode
  onSearch: () => void
  onReset: () => void
  className?: string
}

/** 桌面：一行筛选 + 搜索/重置；手机：折叠为「筛选」按钮，底部 Drawer 内放 children */
export function FilterBar({ children, onSearch, onReset, className }: FilterBarProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const [open, setOpen] = useState(false)

  if (isMobile) {
    return (
      <div className={cn('flex items-center', className)}>
        <Button type="button" variant="outline" className="w-full" onClick={() => setOpen(true)}>
          <Filter className="me-2 h-4 w-4" aria-hidden="true" />
          {t('filterBar.filter')}
        </Button>
        <Drawer open={open} onOpenChange={setOpen}>
          <DrawerContent className="flex max-h-[92dvh] flex-col">
            <DrawerHeader className="text-start">
              <DrawerTitle>{t('filterBar.title')}</DrawerTitle>
              <DrawerDescription className="sr-only">{t('filterBar.title')}</DrawerDescription>
            </DrawerHeader>
            <form
              className="flex min-h-0 flex-1 flex-col"
              onSubmit={(e) => {
                e.preventDefault()
                onSearch()
                setOpen(false)
              }}
            >
              <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4 [&_input]:text-base">
                {children}
              </div>
              <div className="flex shrink-0 gap-2 border-t border-border bg-background px-4 pt-3 pb-[max(1rem,env(safe-area-inset-bottom))]">
                <Button type="button" variant="outline" className="flex-1" onClick={onReset}>
                  <RotateCcw className="me-2 h-4 w-4" aria-hidden="true" />
                  {t('common.reset')}
                </Button>
                <Button type="submit" className="flex-1">
                  <Search className="me-2 h-4 w-4" aria-hidden="true" />
                  {t('common.search')}
                </Button>
              </div>
            </form>
          </DrawerContent>
        </Drawer>
      </div>
    )
  }

  return (
    <form
      className={cn('flex flex-wrap items-end gap-3', className)}
      onSubmit={(e) => {
        e.preventDefault()
        onSearch()
      }}
    >
      {children}
      <div className="flex items-center gap-2">
        <Button type="submit">
          <Search className="me-2 h-4 w-4" aria-hidden="true" />
          {t('common.search')}
        </Button>
        <Button type="button" variant="outline" onClick={onReset}>
          <RotateCcw className="me-2 h-4 w-4" aria-hidden="true" />
          {t('common.reset')}
        </Button>
      </div>
    </form>
  )
}
