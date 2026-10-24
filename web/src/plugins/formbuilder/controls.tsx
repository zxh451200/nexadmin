import type { ReactNode } from 'react'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

/** 属性面板里的一行：label 在上，控件在下 */
export function Field({ id, label, hint, children }: { id: string; label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

interface NumProps {
  id: string
  value: number | undefined
  onChange: (v: number | undefined) => void
  min?: number
  max?: number
  step?: number | 'any'
  placeholder?: string
}

/** 数字输入：清空 = undefined（该规则不生效） */
export function NumInput({ id, value, onChange, min, max, step = 'any', placeholder }: NumProps) {
  return (
    <Input
      id={id}
      type="number"
      inputMode="decimal"
      value={value ?? ''}
      min={min}
      max={max}
      step={step}
      placeholder={placeholder}
      onChange={(e) => {
        const raw = e.target.value
        if (raw === '') return onChange(undefined)
        const n = Number(raw)
        if (Number.isFinite(n)) onChange(n)
      }}
    />
  )
}
