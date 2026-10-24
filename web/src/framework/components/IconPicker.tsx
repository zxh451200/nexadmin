import { useMemo, useState } from 'react'
import { icons, type LucideProps } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { ICON_NAMES } from './pickers/constants'

const ALL = icons as Record<string, React.ComponentType<LucideProps>>
const AVAILABLE = ICON_NAMES.filter((n) => ALL[n])

export interface IconPickerProps {
  /** lucide 图标名 */
  value?: string | null
  onChange?: (value: string) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

export function IconPicker({ value, onChange, allowClear = true, placeholder, disabled, className, id }: IconPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const kw = search.trim().toLowerCase()
  const list = useMemo(() => (kw ? AVAILABLE.filter((n) => n.toLowerCase().includes(kw)) : AVAILABLE), [kw])
  const Current = value ? ALL[value] : undefined

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('picker.selectIcon')}
      contentClassName="min-w-80"
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? t('picker.selectIcon')}
          hasValue={!!value}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          icon={Current ? <Current className="size-4 shrink-0" aria-hidden="true" /> : undefined}
          display={value}
        />
      }
    >
      <div className="border-b border-border p-2">
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('picker.searchIcon')}
          aria-label={t('common.search')}
          className="h-11 w-full rounded-md border border-input bg-background px-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
        />
      </div>
      <div className="grid max-h-72 grid-cols-5 gap-1 overflow-y-auto p-2 md:grid-cols-6">
        {list.map((n) => {
          const Icon = ALL[n]
          return (
            <button
              key={n}
              type="button"
              title={n}
              aria-label={n}
              aria-pressed={value === n}
              onClick={() => {
                onChange?.(n)
                setOpen(false)
              }}
              className={cn(
                'flex size-11 items-center justify-center rounded-md hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:size-10',
                value === n && 'bg-primary text-primary-foreground hover:bg-primary',
              )}
            >
              <Icon className="size-5" aria-hidden="true" />
            </button>
          )
        })}
        {!list.length && <p className="col-span-full py-6 text-center text-sm text-muted-foreground">{t('common.empty')}</p>}
      </div>
    </PickerShell>
  )
}
