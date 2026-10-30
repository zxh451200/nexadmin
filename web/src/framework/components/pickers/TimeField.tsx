import { useTranslation } from 'react-i18next'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
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

/** 时/分两个下拉。不用原生 input[type=time]：浏览器原生弹层样式与站点无关，且各浏览器不一致。 */
export function TimeField({ value, onChange, disabled, className }: TimeFieldProps) {
  const { t } = useTranslation()
  const [hh = '00', mm = '00'] = (value ?? '').split(':')
  const set = (part: 'h' | 'm', v: string) => onChange(part === 'h' ? `${v}:${mm}` : `${hh}:${v}`)

  return (
    <div className={cn('flex items-center gap-1', className)}>
      <Select value={hh} onValueChange={(v) => set('h', v)} disabled={disabled}>
        <SelectTrigger aria-label={t('picker.hour')} className="num w-20">
          <SelectValue />
        </SelectTrigger>
        <SelectContent className="max-h-60">
          {HOURS.map((h) => (
            <SelectItem key={h} value={h} className="num">
              {h}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <span aria-hidden="true" className="text-muted-foreground">
        :
      </span>
      <Select value={mm} onValueChange={(v) => set('m', v)} disabled={disabled}>
        <SelectTrigger aria-label={t('picker.minute')} className="num w-20">
          <SelectValue />
        </SelectTrigger>
        <SelectContent className="max-h-60">
          {MINUTES.map((m) => (
            <SelectItem key={m} value={m} className="num">
              {m}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
