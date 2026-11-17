import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { OptionList, type Option } from '@/framework/components/pickers/OptionList'
import { PickerShell } from '@/framework/components/pickers/PickerShell'
import { PickerTrigger } from '@/framework/components/pickers/PickerTrigger'

/**
 * 常量字段下拉（可搜）：币种/港口/付款方式/枚举档位这类「有惯用值但允许例外」的字段，
 * 收起时只占一行，展开可搜可多选。value 与多选值按「逗号+空格」拼接回原字符串，
 * 与存量数据的 "T/T, L/C" 存储格式互通；存量手写值不在选项里时补进选项，编辑老数据不丢显示。
 * allowCustom 打开后，搜索框里打什么就能存什么（列表末尾出「使用自定义值」行）。
 */
export function ConstantPicker({
  value,
  onChange,
  options,
  multiple,
  placeholder,
  disabled,
  className,
  allowCustom,
}: {
  value: string
  onChange: (v: string) => void
  options: Option[]
  multiple?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  allowCustom?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const selected = useMemo(() => value.split(',').map((s) => s.trim()).filter(Boolean), [value])
  const all = useMemo(() => {
    const list = [...options]
    for (const v of selected) {
      if (!list.some((o) => o.value === v)) list.push({ value: v, label: v })
    }
    return list
  }, [options, selected])
  const kw = search.trim().toLowerCase()
  const shown = kw ? all.filter((o) => o.label.toLowerCase().includes(kw) || o.value.toLowerCase().includes(kw)) : all
  // 允许自定义时，列表里没有的写法在末尾补一行可点的「使用自定义值」，
  // 点完就把原文存进去（展示侧对认不出的值原样显示，不会瞎翻译）。
  const typed = search.trim()
  const custom = allowCustom && typed && !all.some((o) => o.value.toLowerCase() === typed.toLowerCase())
    ? [{ value: typed, label: t('constantPicker.useCustom', { value: typed }) }]
    : []
  const pick = (v: string) => {
    if (multiple) {
      onChange((selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]).join(', '))
    } else {
      onChange(v)
      setOpen(false)
    }
  }
  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('constantPicker.pleaseSelect')}
      trigger={
        <PickerTrigger
          className={className}
          disabled={disabled}
          placeholder={placeholder}
          hasValue={selected.length > 0}
          allowClear={false}
          display={selected.map((v) => all.find((o) => o.value === v)?.label ?? v).join(', ')}
        />
      }
    >
      <OptionList options={custom.length ? [...shown, ...custom] : shown} value={selected} multiple={multiple} onSelect={pick} search={search} onSearchChange={setSearch} />
    </PickerShell>
  )
}
