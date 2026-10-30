import { useState } from 'react'
import { CalendarClock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import { Button } from '@/framework/ui/button'
import { Calendar } from '@/framework/ui/calendar'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { TimeField } from './pickers/TimeField'
import { dateLocale, fmtDateTime, toDate } from './pickers/dates'

export interface DateTimePickerProps {
  /** ISO 字符串或 Date */
  value?: string | Date | null
  /** 输出 ISO 字符串（UTC）；清空时输出空字符串 */
  onChange?: (value: string) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  /** 无可见标签的场景（如筛选栏）给读屏用的名称 */
  ariaLabel?: string
}

export function DateTimePicker({ value, onChange, allowClear = true, placeholder, disabled, className, id, ariaLabel }: DateTimePickerProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const date = toDate(value)
  const [draftDay, setDraftDay] = useState<Date | undefined>(date)
  const [time, setTime] = useState('00:00')

  const confirm = () => {
    if (!draftDay) return
    const [h, m] = time.split(':').map(Number)
    const d = new Date(draftDay)
    d.setHours(h || 0, m || 0, 0, 0)
    onChange?.(d.toISOString())
    setOpen(false)
  }

  return (
    <PickerShell
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) {
          setDraftDay(date)
          setTime(date ? format(date, 'HH:mm') : '00:00')
        }
      }}
      disabled={disabled}
      title={placeholder ?? t('picker.selectDateTime')}
      contentClassName="w-auto min-w-0"
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          aria-label={ariaLabel}
          placeholder={placeholder ?? t('picker.selectDateTime')}
          hasValue={!!date}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          icon={<CalendarClock className="size-4 shrink-0 opacity-60" aria-hidden="true" />}
          display={date ? <span className="num">{fmtDateTime(date)}</span> : ''}
        />
      }
    >
      <div className="flex justify-center">
        <Calendar mode="single" selected={draftDay} defaultMonth={draftDay} onSelect={setDraftDay} locale={dateLocale(i18n.language)} initialFocus />
      </div>
      <div className="flex items-center gap-2 border-t border-border p-3">
        <span className="shrink-0 text-sm text-muted-foreground">{t('picker.time')}</span>
        <TimeField value={time} onChange={setTime} />
        <Button type="button" size="sm" className="ms-auto" disabled={!draftDay} onClick={confirm}>
          {t('common.confirm')}
        </Button>
      </div>
    </PickerShell>
  )
}
