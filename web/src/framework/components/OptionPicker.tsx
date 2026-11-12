import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { OptionList, type Option } from './pickers/OptionList'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'

export interface OptionPickerProps {
  value: string[]
  onChange: (v: string[]) => void
  options: Option[]
  /** 手机抽屉标题 / 无障碍名（必填：DrawerTitle 依赖它） */
  title: string
  placeholder?: string
  disabled?: boolean
  className?: string
}

/** 静态选项的弹出多选（本地枚举没有字典时的筛选选择器）：勾选不关弹层，触发器按选项顺序显示已选标签。 */
export function OptionPicker({ value, onChange, options, title, placeholder, disabled, className }: OptionPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const labelOf = useMemo(() => new Map(options.map((o) => [o.value, o.label])), [options])
  // 触发器按选项声明顺序显示已选，与清单视觉一致；选项外的存量值（老数据）原样显示
  const display = useMemo(
    () =>
      value
        .map((v) => labelOf.get(v) ?? v)
        .join('、'),
    [value, labelOf],
  )
  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={title}
      trigger={
        <PickerTrigger
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? title}
          hasValue={value.length > 0}
          allowClear={false}
          display={display}
        />
      }
    >
      <OptionList
        options={options}
        value={value}
        multiple
        onSelect={(v) => onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v])}
        emptyText={t('common.empty')}
      />
    </PickerShell>
  )
}
