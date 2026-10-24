import { useState } from 'react'
import { CalendarIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Calendar } from '@/framework/ui/calendar'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { dateLocale, fmtDate, toDate } from './pickers/dates'

export interface DatePickerProps {
  /** 'yyyy-MM-dd' 或 ISO 字符串或 Date */
  value?: string | Date | null
  /** 输出 'yyyy-MM-dd'；清空时输出空字符串 */
  onChange?: (value: string) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  fromDate?: Date
  toDate?: Date
}

export function DatePicker({ value, onChange, allowClear = true, placeholder, disabled, className, id, fromDate, toDate: to }: DatePickerProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const date = toDate(value)

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('picker.selectDate')}
      contentClassName="w-auto min-w-0"
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? t('picker.selectDate')}
          hasValue={!!date}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          icon={<CalendarIcon className="size-4 shrink-0 opacity-60" aria-hidden="true" />}
          display={date ? <span className="num">{fmtDate(date)}</span> : ''}
        />
      }
    >
      <div className="flex justify-center">
        <Calendar
          mode="single"
          selected={date}
          defaultMonth={date}
          locale={dateLocale(i18n.language)}
          fromDate={fromDate}
          toDate={to}
          onSelect={(d) => {
            if (!d) return
            onChange?.(fmtDate(d))
            setOpen(false)
          }}
          initialFocus
        />
      </div>
    </PickerShell>
  )
}
