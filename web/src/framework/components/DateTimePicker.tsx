import { CalendarClock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import { Button } from '@/framework/ui/button'
import { Calendar } from '@/framework/ui/calendar'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { TimeField } from './pickers/TimeField'
import { atTime, dateLocale, fmtDateTime, toDate } from './pickers/dates'
import { usePickerDraft } from './pickers/usePickerDraft'

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

/** 弹层内草稿：日期与时刻一起改、确认时合成一个值，避免半选状态落到表单 */
interface Draft {
  day?: Date
  /** 'HH:mm' */
  time: string
}

export function DateTimePicker({ value, onChange, allowClear = true, placeholder, disabled, className, id, ariaLabel }: DateTimePickerProps) {
  const { t, i18n } = useTranslation()
  const date = toDate(value)
  const { open, setOpen, draft, setDraft } = usePickerDraft<Draft>(() =>
    date ? { day: date, time: format(date, 'HH:mm') } : { time: '00:00' },
  )

  const confirm = () => {
    if (!draft?.day) return
    onChange?.(atTime(draft.day, draft.time).toISOString())
    setOpen(false)
  }

  const patch = (p: Partial<Draft>) => setDraft({ day: draft?.day, time: draft?.time ?? '00:00', ...p })

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
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
        <Calendar
          mode="single"
          selected={draft?.day}
          defaultMonth={draft?.day}
          onSelect={(day) => patch({ day: day ?? undefined })}
          locale={dateLocale(i18n.language)}
          fixedWeeks
          initialFocus
        />
      </div>
      <div className="flex items-center gap-2 border-t border-border p-3">
        <span className="shrink-0 text-sm text-muted-foreground">{t('picker.time')}</span>
        <TimeField value={draft?.time ?? '00:00'} onChange={(time) => patch({ time })} />
        <Button type="button" size="sm" className="ms-auto" disabled={!draft?.day} onClick={confirm}>
          {t('common.confirm')}
        </Button>
      </div>
    </PickerShell>
  )
}
