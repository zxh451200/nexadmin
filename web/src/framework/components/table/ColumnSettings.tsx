import { Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import type { Column } from './types'

interface Props<T> {
  columns: Column<T>[]
  hidden: string[]
  onToggle: (key: string) => void
  onReset: () => void
}

export function ColumnSettings<T>({ columns, hidden, onToggle, onReset }: Props<T>) {
  const { t } = useTranslation()
  const items = columns.filter((c) => c.hideable !== false)
  if (items.length === 0) return null
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="icon" aria-label={t('dataTable.columnSettings')}>
          <Settings2 className="h-4 w-4" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-60 p-2">
        <p className="px-2 pb-1 pt-1 text-xs text-muted-foreground">{t('dataTable.columnSettingsHint')}</p>
        <ul className="max-h-72 overflow-y-auto">
          {items.map((c) => {
            const id = `col-${c.key}`
            return (
              <li key={c.key}>
                <label
                  htmlFor={id}
                  className="flex min-h-11 cursor-pointer items-center gap-3 rounded-md px-2 text-sm hover:bg-muted md:min-h-9"
                >
                  <Checkbox id={id} checked={!hidden.includes(c.key)} onCheckedChange={() => onToggle(c.key)} />
                  <span className="min-w-0 flex-1 truncate">{c.title}</span>
                </label>
              </li>
            )
          })}
        </ul>
        <div className="mt-1 border-t border-border pt-1">
          <Button type="button" variant="ghost" size="sm" className="w-full" onClick={onReset}>
            {t('dataTable.resetColumns')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
