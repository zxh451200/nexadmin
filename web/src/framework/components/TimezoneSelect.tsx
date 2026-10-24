import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { OptionList, type Option } from './pickers/OptionList'

export interface TimezoneSelectProps {
  /** IANA 时区名，如 Asia/Shanghai */
  value?: string | null
  onChange?: (value: string) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

const FALLBACK = [
  'UTC',
  'Asia/Shanghai',
  'Asia/Hong_Kong',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Asia/Dubai',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Chicago',
  'America/Los_Angeles',
  'Australia/Sydney',
]

/** 全部 IANA 时区取自浏览器内置的 Intl，不增加数据文件；不支持时退化为常用列表 */
function allZones(): string[] {
  try {
    const list = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone')
    if (list && list.length) return list.includes('UTC') ? list : ['UTC', ...list]
  } catch {
    // 退化为常用列表
  }
  return FALLBACK
}

function offsetMinutes(zone: string, at: Date): number {
  try {
    const opts = { timeZone: zone, timeZoneName: 'longOffset' } as Intl.DateTimeFormatOptions
    const text = new Intl.DateTimeFormat('en-US', opts).formatToParts(at).find((p) => p.type === 'timeZoneName')?.value ?? 'GMT'
    const m = /GMT([+-])(\d{1,2})(?::?(\d{2}))?/.exec(text)
    if (!m) return 0
    return (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3] ?? 0))
  } catch {
    return 0
  }
}

function formatOffset(min: number) {
  const abs = Math.abs(min)
  return `UTC${min < 0 ? '-' : '+'}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`
}

/** 带搜索的时区选择：按 UTC 偏移排序，可输入城市名或偏移搜索；手机上是底部抽屉 */
export function TimezoneSelect({ value, onChange, allowClear = false, placeholder, disabled, className, id }: TimezoneSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const all = useMemo(() => {
    const now = new Date()
    return allZones()
      .map((zone) => {
        const min = offsetMinutes(zone, now)
        return { value: zone, label: `(${formatOffset(min)}) ${zone.replace(/_/g, ' ')}`, min }
      })
      .sort((a, b) => a.min - b.min || a.value.localeCompare(b.value))
  }, [])

  const labelOf = (zone: string) => all.find((o) => o.value === zone)?.label ?? zone
  const kw = search.trim().toLowerCase()
  const options: Option[] = (kw ? all.filter((o) => o.label.toLowerCase().includes(kw) || o.value.toLowerCase().includes(kw)) : all).map((o) => ({
    value: o.value,
    label: o.label,
  }))
  const shown = options.map((o) => ({ ...o, node: <span>{o.label}</span> }))
  const title = placeholder ?? t('timezone.select')

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={title}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={title}
          hasValue={!!value}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          display={value ? labelOf(value) : ''}
        />
      }
    >
      <OptionList
        options={shown}
        value={value ? [value] : []}
        onSelect={(v) => {
          onChange?.(v)
          setOpen(false)
          setSearch('')
        }}
        search={search}
        onSearchChange={setSearch}
      />
    </PickerShell>
  )
}
