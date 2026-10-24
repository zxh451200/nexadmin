import * as React from 'react'
import { Check, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

/** 选择器 onChange：单选为 string，多选为 string[]；调用方各自按场景收窄，此处保持宽松以免逐处断言 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type PickerChange = (value: any) => void

export interface Option {
  value: string
  label: string
  /** 额外渲染内容（如 Badge） */
  node?: React.ReactNode
  disabled?: boolean
}

export interface OptionListProps {
  options: Option[]
  value: string[]
  multiple?: boolean
  onSelect: (value: string) => void
  /** 提供则显示搜索框 */
  search?: string
  onSearchChange?: (s: string) => void
  loading?: boolean
  footer?: React.ReactNode
  emptyText?: string
  className?: string
}

/** 通用单/多选列表（带可选搜索框），用于 DictSelect / RemoteSelect / CountrySelect */
export function OptionList({
  options,
  value,
  multiple,
  onSelect,
  search,
  onSearchChange,
  loading,
  footer,
  emptyText,
  className,
}: OptionListProps) {
  const { t } = useTranslation()
  return (
    <div className={cn('flex min-h-0 flex-col', className)}>
      {onSearchChange && (
        <div className="relative border-b border-border p-2">
          <Search className="pointer-events-none absolute start-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <input
            value={search ?? ''}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t('picker.searchPlaceholder')}
            aria-label={t('common.search')}
            className="h-11 w-full rounded-md border border-input bg-background ps-9 pe-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
          />
        </div>
      )}
      <ul role="listbox" aria-multiselectable={multiple} className="max-h-72 overflow-y-auto p-1">
        {options.map((o) => {
          const selected = value.includes(o.value)
          return (
            <li key={o.value} role="option" aria-selected={selected}>
              <button
                type="button"
                disabled={o.disabled}
                onClick={() => onSelect(o.value)}
                className={cn(
                  'flex min-h-11 w-full items-center gap-2 rounded-sm px-2 text-start text-base hover:bg-muted focus-visible:bg-muted focus-visible:outline-none md:min-h-8 md:text-sm',
                  'disabled:pointer-events-none disabled:opacity-50',
                  selected && 'bg-muted/60',
                )}
              >
                <Check className={cn('size-4 shrink-0 text-primary', selected ? 'opacity-100' : 'opacity-0')} aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate">{o.node ?? o.label}</span>
              </button>
            </li>
          )
        })}
        {!options.length && (
          <li className="px-2 py-6 text-center text-sm text-muted-foreground">
            {loading ? t('common.loading') : emptyText ?? t('common.empty')}
          </li>
        )}
      </ul>
      {footer}
    </div>
  )
}

export function toArray(v: string | string[] | null | undefined): string[] {
  if (v == null || v === '') return []
  return Array.isArray(v) ? v : [v]
}
