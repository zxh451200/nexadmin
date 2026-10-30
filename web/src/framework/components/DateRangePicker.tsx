import { useState } from 'react'
import { CalendarRange } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { DateRange } from 'react-day-picker'
import { Calendar } from '@/framework/ui/calendar'
import { Button } from '@/framework/ui/button'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { dateLocale, fmtDate, toDate } from './pickers/dates'

export interface DateRangeValue {
  from?: string
  to?: string
}

export interface DateRangePickerProps {
  value?: DateRangeValue | null
  /** 输出 'yyyy-MM-dd'；清空时输出 {} */
  onChange?: (value: DateRangeValue) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  /** 无可见标签的场景（如筛选栏）给读屏用的名称 */
  ariaLabel?: string
}

export function DateRangePicker({ value, onChange, allowClear = true, placeholder, disabled, className, id, ariaLabel }: DateRangePickerProps) {
  const { t, i18n } = useTranslation()
  const isMobile = useIsMobile()
  const [open, setOpen] = useState(false)
  const from = toDate(value?.from)
  const to = toDate(value?.to)
  const [draft, setDraft] = useState<DateRange | undefined>()

  const text = from ? `${fmtDate(from)} ~ ${to ? fmtDate(to) : ''}` : ''

  return (
    <PickerShell
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) setDraft(from ? { from, to } : undefined)
      }}
      disabled={disabled}
      title={placeholder ?? t('picker.selectDateRange')}
      contentClassName="w-auto min-w-0"
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          aria-label={ariaLabel}
          placeholder={placeholder ?? t('picker.selectDateRange')}
          hasValue={!!from}
          allowClear={allowClear}
          onClear={() => onChange?.({})}
          icon={<CalendarRange className="size-4 shrink-0 opacity-60" aria-hidden="true" />}
          display={<span className="num">{text}</span>}
        />
      }
    >
      <div className="flex justify-center">
        <Calendar
          mode="range"
          numberOfMonths={isMobile ? 1 : 2}
          selected={draft}
          defaultMonth={draft?.from}
          onSelect={setDraft}
          locale={dateLocale(i18n.language)}
          initialFocus
        />
      </div>
      <div className="flex justify-end gap-2 border-t border-border p-3">
        <Button type="button" variant="outline" size="sm" onClick={() => setOpen(false)}>
          {t('common.cancel')}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!draft?.from}
          onClick={() => {
            if (!draft?.from) return
            onChange?.({ from: fmtDate(draft.from), to: fmtDate(draft.to ?? draft.from) })
            setOpen(false)
          }}
        >
          {t('common.confirm')}
        </Button>
      </div>
    </PickerShell>
  )
}
