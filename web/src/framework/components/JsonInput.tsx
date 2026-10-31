import * as React from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
  const key = check.reason ? `jsonInput.syntaxReason.${check.reason}` : 'jsonInput.syntax'
  return t(key, { line: check.line, column: check.column })
}

interface EditorProps {
  id?: string
  rows: number
  value: string
  error: boolean
  invalidLine?: number
  placeholder: string
  disabled?: boolean
  autoFocus?: boolean
  boxClassName?: string
  textareaProps?: React.TextareaHTMLAttributes<HTMLTextAreaElement>
  onChange: (value: string) => void
  onBlur?: (event: React.FocusEvent<HTMLTextAreaElement>) => void
}

/** 带行号的文本域：行号与内容、滚动同步，出错行标红；不折行以保证行号对齐 */
const Editor = React.forwardRef<HTMLTextAreaElement, EditorProps>(function Editor(
  {
    id,
    rows,
    value,
    error,
    invalidLine,
    placeholder,
    disabled,
    autoFocus,
    boxClassName,
    textareaProps,
    onChange,
    onBlur,
  },
  ref,
) {
  const gutterRef = React.useRef<HTMLDivElement>(null)
  const lineCount = Math.max(value.split('\n').length, 1)
  const numbers = React.useMemo(() => Array.from({ length: lineCount }, (_, i) => i + 1), [lineCount])
  const digits = String(lineCount).length

  const syncScroll = (el: HTMLTextAreaElement) => {
    if (gutterRef.current) gutterRef.current.scrollTop = el.scrollTop
  }

  return (
    <div
      className={cn(
        'relative flex rounded-md border border-input bg-background shadow-sm transition-[color,box-shadow,border-color] duration-150 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/40',
        error && 'border-destructive focus-within:border-destructive focus-within:ring-destructive/30',
        boxClassName,
      )}
      style={{ '--json-gutter-w': `calc(${digits}ch + 1.25rem)` } as React.CSSProperties}
    >
      <div
        ref={gutterRef}
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-0 start-0 w-[var(--json-gutter-w)] select-none overflow-hidden border-e border-border/60 bg-muted py-2 ps-2 pe-1.5 text-end font-mono text-base leading-relaxed text-muted-foreground/70 tabular-nums md:text-sm"
      >
        {numbers.map((n) => (
          <div key={n} className={cn(n === invalidLine && 'font-medium text-destructive')}>
            {n}
          </div>
        ))}
      </div>
      <Textarea
        {...textareaProps}
        ref={ref}
        id={id}
        rows={rows}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoFocus={autoFocus}
        wrap="off"
        spellCheck={false}
        autoComplete="off"
        aria-invalid={error ? true : textareaProps?.['aria-invalid']}
        className={cn(
          'min-h-0 flex-1 resize-none rounded-none border-0 bg-transparent py-2 ps-[calc(var(--json-gutter-w)+0.5rem)] pe-2 font-mono leading-relaxed shadow-none focus-visible:border-0 focus-visible:ring-0',
          error && 'border-0 focus-visible:border-0 focus-visible:ring-0',
        )}
        onChange={(e) => {
          onChange(e.target.value)
          syncScroll(e.target)
        }}
        onScroll={(e) => syncScroll(e.currentTarget)}
        onBlur={onBlur}
      />
    </div>
  )
})

/** JSON 文本输入：行号、等宽字体、实时校验、格式化/压缩、全屏编辑 */
export const JsonInput = React.forwardRef<HTMLTextAreaElement, JsonInputProps>(function JsonInput(
  { value, onChange, objectOnly = true, optional = true, className, rows = 5, placeholder = '{}', id, disabled, onBlur, ...rest },
  ref,
) {
  const { t } = useTranslation()
  // 空值先不报错，停止输入 800ms 或失焦后才提示，避免边打边红
  const [touched, setTouched] = React.useState(false)
  const [idle, setIdle] = React.useState(false)
  const [expanded, setExpanded] = React.useState(false)
  const [repairing, setRepairing] = React.useState(false)

  React.useEffect(() => {
    setIdle(false)
    const timer = window.setTimeout(() => setIdle(true), 800)
    return () => window.clearTimeout(timer)
  }, [value])

  const check = React.useMemo(() => checkJsonText(value, { objectOnly, optional }), [value, objectOnly, optional])
  const forced = rest['aria-invalid'] === true || rest['aria-invalid'] === 'true'
  const error = (touched || idle || forced) && !check.ok ? messageOf(check, t) : ''
  const invalidLine = !check.ok && check.kind === 'syntax' ? check.line : undefined
  const canTidy = !disabled && check.ok && value.trim().length > 0
  const tidy = (space: number) => {
    const next = formatJsonText(value, space)
    if (next !== null) onChange(next)
  }
  // 只在语法错误时提供修复：合法内容不需要，空值 / 非对象也不是「笔误」
  const canRepair = !disabled && value.trim().length > 0 && !check.ok && check.kind === 'syntax'
  const repair = async () => {
    setRepairing(true)
    try {
      const { repairJsonText } = await import('./json-repair')
      const fixed = repairJsonText(value)
      if (!fixed.ok) {
        toast.error(t('jsonInput.repairFailed'))
        return
      }
      onChange(fixed.text)
      setTouched(true)
      const after = checkJsonText(fixed.text, { objectOnly, optional })
      if (after.ok) toast.success(t('jsonInput.repaired'))
      else toast.warning(t('jsonInput.repairPartial'))
    } catch {
      toast.error(t('jsonInput.repairFailed'))
    } finally {
      setRepairing(false)
    }
  }

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <Editor
        textareaProps={rest}
        id={id}
        ref={ref}
        rows={rows}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        error={!!error}
        invalidLine={invalidLine}
        onChange={onChange}
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
          {canRepair ? (
            <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={repairing} onClick={() => void repair()}>
              {repairing ? t('jsonInput.repairing') : t('jsonInput.repair')}
            </Button>
          ) : null}
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
        footer={
          <div className="flex items-center gap-2">
            {canRepair ? (
              <Button variant="outline" disabled={repairing} onClick={() => void repair()}>
                {repairing ? t('jsonInput.repairing') : t('jsonInput.repair')}
              </Button>
            ) : null}
            <Button onClick={() => setExpanded(false)}>{t('jsonInput.done')}</Button>
          </div>
        }
      >
        <Editor
          id={id ? `${id}-expanded` : undefined}
          rows={16}
          boxClassName="min-h-[55dvh]"
          value={value}
          placeholder={placeholder}
          autoFocus
          error={!!error}
          invalidLine={invalidLine}
          onChange={onChange}
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
