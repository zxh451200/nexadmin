import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'

const NEW = '__new__'

export interface GroupSelectProps {
  id?: string
  /** 字段名，用于读屏名称与新建态的占位提示 */
  label: string
  /** 存库的原始值（内置行是 i18n 键），显示文案由 display 负责 */
  value: string
  onChange: (value: string) => void
  /** 已有取值（原始值，未去重也行） */
  options: string[]
  display: (value: string) => string
  newLabel: string
  maxLength?: number
}

/**
 * 派生式的「选已有或新建」下拉：分组不建表（报表分组来自已用过的值，小节名来自本报表的图表行），
 * 所以没有独立的分组维护页，新建就在用到的地方当场建。
 */
export function GroupSelect({ id, label, value, onChange, options, display, newLabel, maxLength = 64 }: GroupSelectProps) {
  const { t } = useTranslation()
  const [creating, setCreating] = useState(false)
  // 当前值可能不在选项里（新建的、或选项列表还没刷到），补进去才不会显示成空
  const known = value && !options.includes(value) ? [value, ...options] : options

  if (creating) {
    return (
      <div className="flex items-center gap-2">
        <Input
          id={id}
          autoFocus
          value={display(value)}
          maxLength={maxLength}
          placeholder={label}
          aria-label={label}
          onChange={(e) => onChange(e.target.value)}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            setCreating(false)
            if (!value.trim()) onChange('')
          }}
        >
          {t('plugin_report.report.backToOptions')}
        </Button>
      </div>
    )
  }

  return (
    <Select
      value={value}
      onValueChange={(v) => {
        if (v === NEW) {
          setCreating(true)
          return
        }
        onChange(v)
      }}
    >
      <SelectTrigger id={id} className="w-full" aria-label={label}>
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        {known.map((o) => (
          <SelectItem key={o} value={o}>
            {display(o)}
          </SelectItem>
        ))}
        <SelectItem value={NEW}>{newLabel}</SelectItem>
      </SelectContent>
    </Select>
  )
}
