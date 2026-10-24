import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { OptionList, type Option } from './pickers/OptionList'
import { COUNTRY_CODES } from './pickers/constants'

export interface CountrySelectProps {
  /** ISO 3166-1 alpha-2 代码 */
  value?: string | null
  onChange?: (value: string) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

/** 把代码转成国旗 emoji（区域指示符），不支持的环境退化为代码 */
function flag(code: string) {
  return String.fromCodePoint(...code.split('').map((c) => 127397 + c.charCodeAt(0)))
}

/** 名称由 Intl.DisplayNames 按当前语言生成，不增加翻译数据 */
export function useCountryNames() {
  const { i18n } = useTranslation()
  return useMemo(() => {
    let dn: Intl.DisplayNames | undefined
    try {
      dn = new Intl.DisplayNames([i18n.language], { type: 'region' })
    } catch {
      dn = undefined
    }
    return (code: string) => dn?.of(code) ?? code
  }, [i18n.language])
}

export function CountrySelect({ value, onChange, allowClear = true, placeholder, disabled, className, id }: CountrySelectProps) {
  const { t } = useTranslation()
  const nameOf = useCountryNames()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const all = useMemo<Option[]>(
    () => COUNTRY_CODES.map((c) => ({ value: c, label: nameOf(c) })).sort((a, b) => a.label.localeCompare(b.label)),
    [nameOf],
  )
  const kw = search.trim().toLowerCase()
  const options = kw ? all.filter((o) => o.label.toLowerCase().includes(kw) || o.value.toLowerCase() === kw) : all
  const shown = options.map((o) => ({ ...o, node: <span>{`${flag(o.value)} ${o.label}`}</span> }))

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('picker.selectCountry')}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? t('picker.selectCountry')}
          hasValue={!!value}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          display={value ? `${flag(value)} ${nameOf(value)}` : ''}
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
