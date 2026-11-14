import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { cn } from '@/lib/utils'

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'))
const MINUTES = Array.from({ length: 60 }, (_, i) => String(i).padStart(2, '0'))

export interface TimeFieldProps {
  /** 'HH:mm'（24 小时制） */
  value?: string
  onChange: (value: string) => void
  disabled?: boolean
  className?: string
}

/** 触发按钮：对齐 SelectTrigger 的规格，让它在表单里像同一个构件 */
function TimeTrigger({ label, text, open, disabled }: { label: string; text: string; open: boolean; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="combobox"
      aria-expanded={open}
      aria-label={label}
      disabled={disabled}
      className={cn(
        'num flex h-10 w-20 items-center justify-between gap-2 rounded-md border border-input bg-background px-3 shadow-xs transition-[color,box-shadow,border-color] duration-(--motion-fast) ease-(--ease-standard) focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 md:h-8 md:text-sm',
        open && 'ring-2 ring-ring/40',
      )}
    >
      <span>{text}</span>
      <ChevronDown aria-hidden="true" className="h-4 w-4 shrink-0 opacity-50" />
    </button>
  )
}

/** 6 列数字网格：全部候选一屏平铺，没有滚动列表，选中即关闭 */
function TimeGrid({
  values,
  current,
  onPick,
  label,
}: {
  values: string[]
  current: string
  onPick: (v: string) => void
  label: string
}) {
  return (
    <div role="listbox" aria-label={label} className="grid grid-cols-6 gap-1 p-1.5">
      {values.map((v) => (
        <button
          key={v}
          type="button"
          role="option"
          aria-selected={v === current}
          onClick={() => onPick(v)}
          className={cn(
            'num h-7 rounded-md text-xs leading-4 tabular-nums transition-colors',
            v === current
              ? 'bg-primary text-primary-foreground'
              : 'text-foreground hover:bg-accent hover:text-accent-foreground',
          )}
        >
          {v}
        </button>
      ))}
    </div>
  )
}

/** 时/分两个网格弹层。不用原生 input[type=time]：浏览器原生弹层样式与站点无关，且各浏览器不一致。
    不用 Select 长列表：60 个分钟项滚起来又快又晕，平铺网格一次看全、点一下就中。 */
export function TimeField({ value, onChange, disabled, className }: TimeFieldProps) {
  const { t } = useTranslation()
  const [hh = '00', mm = '00'] = (value ?? '').split(':')
  const [hourOpen, setHourOpen] = useState(false)
  const [minuteOpen, setMinuteOpen] = useState(false)
  const set = (part: 'h' | 'm', v: string) => onChange(part === 'h' ? `${v}:${mm}` : `${hh}:${v}`)

  return (
    <div className={cn('flex items-center gap-1', className)}>
      <Popover open={hourOpen} onOpenChange={setHourOpen}>
        <PopoverTrigger asChild>
          <TimeTrigger label={t('picker.hour')} text={hh} open={hourOpen} disabled={disabled} />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-auto p-1">
          <TimeGrid
            values={HOURS}
            current={hh}
            label={t('picker.hour')}
            onPick={(v) => {
              set('h', v)
              setHourOpen(false)
            }}
          />
        </PopoverContent>
      </Popover>
      <span aria-hidden="true" className="text-muted-foreground">
        :
      </span>
      <Popover open={minuteOpen} onOpenChange={setMinuteOpen}>
        <PopoverTrigger asChild>
          <TimeTrigger label={t('picker.minute')} text={mm} open={minuteOpen} disabled={disabled} />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-auto p-1">
          <TimeGrid
            values={MINUTES}
            current={mm}
            label={t('picker.minute')}
            onPick={(v) => {
              set('m', v)
              setMinuteOpen(false)
            }}
          />
        </PopoverContent>
      </Popover>
    </div>
  )
}
