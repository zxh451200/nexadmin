import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { FALLBACK_COLOR, PRESET_COLORS } from './pickers/constants'

export interface ColorPickerProps {
  /** 十六进制色值，如 #3b82f6 */
  value?: string | null
  onChange?: (value: string) => void
  presets?: string[]
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

const HEX = /^#[0-9a-fA-F]{6}$/

export function ColorPicker({ value, onChange, presets = PRESET_COLORS, allowClear = true, placeholder, disabled, className, id }: ColorPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [hex, setHex] = useState('')

  return (
    <PickerShell
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) setHex(value ?? '')
      }}
      disabled={disabled}
      title={placeholder ?? t('picker.selectColor')}
      contentClassName="min-w-72"
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? t('picker.selectColor')}
          hasValue={!!value}
          allowClear={allowClear}
          onClear={() => onChange?.('')}
          icon={<span className="size-4 shrink-0 rounded border border-border" style={{ backgroundColor: value ?? undefined }} aria-hidden="true" />}
          display={<span className="num">{value}</span>}
        />
      }
    >
      <div className="grid grid-cols-7 gap-2 p-3">
        {presets.map((c) => (
          <button
            key={c}
            type="button"
            aria-label={c}
            aria-pressed={value?.toLowerCase() === c.toLowerCase()}
            onClick={() => {
              onChange?.(c)
              setOpen(false)
            }}
            style={{ backgroundColor: c }}
            className={cn(
              'flex size-11 items-center justify-center rounded-md border border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:size-8',
            )}
          >
            {value?.toLowerCase() === c.toLowerCase() && <Check className="size-4 mix-blend-difference" aria-hidden="true" />}
          </button>
        ))}
      </div>
      <div className="flex items-center gap-2 border-t border-border p-3">
        <input
          type="color"
          aria-label={t('picker.customColor')}
          value={HEX.test(hex) ? hex : FALLBACK_COLOR}
          onChange={(e) => {
            setHex(e.target.value)
            onChange?.(e.target.value)
          }}
          className="size-11 shrink-0 cursor-pointer rounded-md border border-input bg-background p-1 md:size-9"
        />
        <input
          value={hex}
          onChange={(e) => {
            const v = e.target.value
            setHex(v)
            if (HEX.test(v)) onChange?.(v)
          }}
          placeholder="#RRGGBB"
          maxLength={7}
          aria-label={t('picker.customColor')}
          className="num h-11 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
        />
      </div>
    </PickerShell>
  )
}
