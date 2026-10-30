import * as React from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { Textarea } from '@/framework/ui/textarea'
import { ResponsiveDialog } from './ResponsiveDialog'
import { checkJsonText, formatJsonText, type JsonCheck } from './json'

export interface JsonInputProps
  extends Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'value' | 'onChange' | 'children'> {
  value: string
  onChange: (value: string) => void
  /** 必须是 JSON 对象，不接受数组与单个值（默认 true） */
  objectOnly?: boolean
  /** 允许留空（默认 true） */
  optional?: boolean
}

function messageOf(check: JsonCheck, t: TFunction): string {
  if (check.ok) return ''
  if (check.kind === 'empty') return t('jsonInput.required')
  if (check.kind === 'object') return t('jsonInput.notObject')
  return t('jsonInput.syntax', { line: check.line, column: check.column })
}

/** JSON 文本输入：等宽字体、实时校验、格式化/压缩、全屏编辑 */
export const JsonInput = React.forwardRef<HTMLTextAreaElement, JsonInputProps>(function JsonInput(
  { value, onChange, objectOnly = true, optional = true, className, rows = 5, placeholder = '{}', id, disabled, onBlur, ...rest },
  ref,
) {
  const { t } = useTranslation()
  // 空值先不报错，停止输入 800ms 或失焦后才提示，避免边打边红
  const [touched, setTouched] = React.useState(false)
  const [idle, setIdle] = React.useState(false)
  const [expanded, setExpanded] = React.useState(false)

  React.useEffect(() => {
    setIdle(false)
    const timer = window.setTimeout(() => setIdle(true), 800)
    return () => window.clearTimeout(timer)
  }, [value])

  const check = React.useMemo(() => checkJsonText(value, { objectOnly, optional }), [value, objectOnly, optional])
  const forced = rest['aria-invalid'] === true || rest['aria-invalid'] === 'true'
  const error = (touched || idle || forced) && !check.ok ? messageOf(check, t) : ''
  const canTidy = !disabled && check.ok && value.trim().length > 0
  const tidy = (space: number) => {
    const next = formatJsonText(value, space)
    if (next !== null) onChange(next)
  }

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <Textarea
        {...rest}
        id={id}
        ref={ref}
        rows={rows}
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        autoComplete="off"
        disabled={disabled}
        aria-invalid={error ? true : rest['aria-invalid']}
        className={cn(
          'font-mono leading-relaxed',
          error && 'border-destructive focus-visible:border-destructive focus-visible:ring-destructive/30',
        )}
        onChange={(e) => onChange(e.target.value)}
        onBlur={(e) => {
          setTouched(true)
          onBlur?.(e)
        }}
      />
      <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-1">
        {error ? (
          <p role="alert" className="min-w-0 flex-1 text-xs leading-5 text-destructive">
            {error}
          </p>
        ) : (
          <span className="flex-1" />
        )}
        <div className="ms-auto flex shrink-0 items-center gap-0.5">
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={!canTidy} onClick={() => tidy(2)}>
            {t('jsonInput.format')}
          </Button>
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={!canTidy} onClick={() => tidy(0)}>
            {t('jsonInput.minify')}
          </Button>
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={disabled} onClick={() => setExpanded(true)}>
            {t('jsonInput.expand')}
          </Button>
        </div>
      </div>
      <ResponsiveDialog
        open={expanded}
        onOpenChange={setExpanded}
        title={t('jsonInput.expandTitle')}
        size="xl"
        footer={<Button onClick={() => setExpanded(false)}>{t('jsonInput.done')}</Button>}
      >
        <Textarea
          id={id ? `${id}-expanded` : undefined}
          rows={16}
          autoFocus
          spellCheck={false}
          autoComplete="off"
          value={value}
          placeholder={placeholder}
          aria-invalid={error ? true : undefined}
          className={cn('min-h-[55dvh] font-mono leading-relaxed', error && 'border-destructive')}
          onChange={(e) => onChange(e.target.value)}
          onBlur={() => setTouched(true)}
        />
        {error ? (
          <p role="alert" className="mt-2 text-xs text-destructive">
            {error}
          </p>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
})
