import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { OptionList, toArray, type Option, type PickerChange } from './pickers/OptionList'
import { useDictItems } from './pickers/useDictItems'

export interface DictSelectProps {
  dict: string
  value?: string | string[] | null
  onChange?: PickerChange
  multiple?: boolean
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

export function DictSelect({ dict, value, onChange, multiple, allowClear = true, placeholder, disabled, className, id }: DictSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const { data = [], isLoading } = useDictItems(dict)
  const selected = toArray(value)

  // 下拉里用纯文本，彩色标签（DictTag）只用于列表展示
  const options: Option[] = data.map((d) => ({ value: String(d.value), label: d.label }))
  const labels = selected.map((v) => data.find((d) => String(d.value) === v)?.label ?? v)

  const pick = (v: string) => {
    if (multiple) {
      onChange?.(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v])
    } else {
      onChange?.(v)
      setOpen(false)
    }
  }

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('common.pleaseSelect')}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder}
          hasValue={selected.length > 0}
          allowClear={allowClear}
          onClear={() => onChange?.(multiple ? [] : '')}
          display={labels.join(', ')}
        />
      }
    >
      <OptionList options={options} value={selected} multiple={multiple} onSelect={pick} loading={isLoading} />
    </PickerShell>
  )
}
