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

/** 只得到偏移或缩写（GMT+8 / ET）的名字不能单独上屏，继续尝试下一种写法 */
const OFFSET_ONLY = /^(?:GMT|UTC)(?:[+-]?\d{1,2}(?::?\d{2})?)?$/i
const ABBR_ONLY = /^[A-Z]{2,6}$/
const LATIN = /[A-Za-z]/

const nameCache = new Map<string, string>()

/**
 * 本地化时区名：按通用名 → 长名 → long 依次尝试。
 * CLDR 缺译时通用名会留下英文城市（zh-CN 的“Honolulu时间”），此时退到全译的长名
 * （“夏威夷-阿留申标准时间”）；拉丁语系语言的候选本身都带字母，取首个可用候选。
 */
function zoneName(zone: string, locale: string, at: Date, override?: string): string {
  if (override) return override
  const key = `${locale}|${zone}`
  const hit = nameCache.get(key)
  if (hit) return hit
  let fallback = ''
  for (const style of ['shortGeneric', 'longGeneric', 'long'] as const) {
    try {
      const opts = { timeZone: zone, timeZoneName: style } as Intl.DateTimeFormatOptions
      const name = new Intl.DateTimeFormat(locale, opts).formatToParts(at).find((p) => p.type === 'timeZoneName')?.value ?? ''
      if (!name || OFFSET_ONLY.test(name) || ABBR_ONLY.test(name)) continue
      if (!LATIN.test(name)) {
        nameCache.set(key, name)
        return name
      }
      fallback ||= name
    } catch {
      // 该写法不被支持，继续尝试下一种
    }
  }
  const name = fallback || zone.replace(/_/g, ' ')
  nameCache.set(key, name)
  return name
}

/** 语言包里的时区补充：CLDR 没译到的名字、重名区的城市名、以及城市别名（只用于搜索） */
interface ZoneText {
  names: Record<string, string>
  cities: Record<string, string>
  aliases: Record<string, string>
}

type I18nInstance = ReturnType<typeof useTranslation>['i18n']

function zoneText(i18n: I18nInstance, locale: string): ZoneText {
  const tz = (i18n.getDataByLanguage(locale)?.translation as { timezone?: Partial<ZoneText> } | undefined)?.timezone
  return { names: tz?.names ?? {}, cities: tz?.cities ?? {}, aliases: tz?.aliases ?? {} }
}

interface ZoneOption {
  value: string
  label: string
  /** 搜索用：标签 + IANA 名 + 城市别名，已转小写 */
  search: string
  min: number
}

function zoneLabel(zone: string, locale: string, at: Date, text: ZoneText): string {
  const min = offsetMinutes(zone, at)
  return `(${formatOffset(min)}) ${zoneName(zone, locale, at, text.names[zone])}`
}

let cachedKey = ''
let cachedList: ZoneOption[] = []

function zoneOptions(locale: string, text: ZoneText): ZoneOption[] {
  const at = new Date()
  // 偏移随夏令时变化，按天失效；名字与语言绑定，单独缓存
  const key = `${locale}|${at.toISOString().slice(0, 10)}`
  if (key === cachedKey) return cachedList
  cachedKey = key
  const list = allZones().map((zone) => {
    const min = offsetMinutes(zone, at)
    return { value: zone, label: `(${formatOffset(min)}) ${zoneName(zone, locale, at, text.names[zone])}`, search: '', min }
  })
  // CLDR 的通用名会把同一规则的多个地区并成一行（6 个阿拉斯加城市都叫“阿拉斯加时间”），重名时补城市名
  const counts = new Map<string, number>()
  for (const o of list) counts.set(o.label, (counts.get(o.label) ?? 0) + 1)
  for (const o of list) {
    if ((counts.get(o.label) ?? 0) > 1) o.label += ` · ${text.cities[o.value] ?? o.value.split('/').pop()!.replace(/_/g, ' ')}`
  }
  for (const o of list) o.search = `${o.label} ${o.value.replace(/_/g, ' ')} ${text.aliases[o.value] ?? ''}`.toLowerCase()
  cachedList = list.sort((a, b) => a.min - b.min || a.value.localeCompare(b.value))
  return cachedList
}

/** 带搜索的时区选择：按 UTC 偏移排序，可输入城市名或偏移搜索；手机上是底部抽屉 */
export function TimezoneSelect({ value, onChange, allowClear = false, placeholder, disabled, className, id }: TimezoneSelectProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage || i18n.language
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const text = useMemo(() => zoneText(i18n, locale), [i18n, locale])
  const all = useMemo(() => zoneOptions(locale, text), [locale, text])

  const labelOf = (zone: string) => all.find((o) => o.value === zone)?.label ?? zoneLabel(zone, locale, new Date(), text)
  const kw = search.trim().toLowerCase()
  const options: Option[] = (kw ? all.filter((o) => o.search.includes(kw)) : all).map((o) => ({
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
