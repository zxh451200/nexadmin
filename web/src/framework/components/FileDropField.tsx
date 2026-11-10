import { useRef, useState } from 'react'
import { FileSpreadsheet, FileText, Upload, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'

const SHEET_EXTS = ['.csv', '.xlsx', '.xls']

export interface FileDropFieldProps {
  files: File[]
  onChange: (files: File[]) => void
  /** 多选：虚线框常驻、可继续拖入，选中的文件按行列出；默认单选，选中后虚线框收成一张文件卡 */
  multiple?: boolean
  /** 原生 accept 串；其中的扩展名既用于拖拽校验，也用于框内提示 */
  accept?: string
  /** 外部 Label 的 id，用于把可见标签关到框上 */
  labelledBy?: string
  disabled?: boolean
  className?: string
}

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function fileIcon(name: string) {
  return SHEET_EXTS.some((e) => name.toLowerCase().endsWith(e)) ? FileSpreadsheet : FileText
}

/** 文件选择：空态是可点击/可拖放的虚线框；single 选好后收成一张卡片，multiple 则保留虚线框并在下方列出文件。 */
export function FileDropField({ files, onChange, multiple, accept, labelledBy, disabled, className }: FileDropFieldProps) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [rejected, setRejected] = useState(false)

  const exts = (accept ?? '')
    .split(',')
    .map((s) => s.trim().toLowerCase())
    .filter((s) => s.startsWith('.'))
  const extText = exts.join(' / ')
  const one = !multiple && files.length > 0

  // 拖放绕过了 input 的 accept 过滤，所以要在落盘前自己拦一次
  const take = (picked: File[]) => {
    if (!picked.length) return
    const ok: File[] = []
    let bad = false
    for (const f of picked) {
      const name = f.name.toLowerCase()
      if (exts.length && !exts.some((e) => name.endsWith(e))) bad = true
      else ok.push(f)
    }
    setRejected(bad)
    if (!ok.length) return
    if (!multiple) return onChange([ok[0]])
    const added = ok.filter((f) => !files.some((x) => x.name === f.name && x.size === f.size))
    if (added.length) onChange([...files, ...added])
  }

  const row = (f: File, actions: React.ReactNode) => {
    const Icon = fileIcon(f.name)
    return (
      <div
        key={`${f.name}-${f.size}`}
        className="flex items-center gap-3 rounded-lg border border-border bg-muted/40 px-3 py-2.5 dark:bg-black/20"
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary ring-1 ring-inset ring-primary/15">
          <Icon className="size-4" aria-hidden="true" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-foreground">{f.name}</p>
          <p className="text-xs text-muted-foreground">{formatFileSize(f.size)}</p>
        </div>
        {actions}
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple={multiple}
        disabled={disabled}
        className="hidden"
        onChange={(e) => {
          take([...(e.target.files ?? [])])
          e.target.value = ''
        }}
      />
      <div
        className={cn('flex flex-col gap-2', dragging && one && 'rounded-lg ring-2 ring-primary/20')}
        onDragOver={(e) => {
          if (disabled) return
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false)
        }}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          if (disabled) return
          take([...(e.dataTransfer.files ?? [])])
        }}
      >
        {one ? (
          row(
            files[0],
            <>
              <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={() => inputRef.current?.click()}>
                {t('common.replace')}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-8 text-muted-foreground"
                aria-label={t('common.remove')}
                disabled={disabled}
                onClick={() => {
                  setRejected(false)
                  onChange([])
                }}
              >
                <X aria-hidden="true" />
              </Button>
            </>,
          )
        ) : (
          <button
            type="button"
            disabled={disabled}
            aria-labelledby={labelledBy}
            onClick={() => inputRef.current?.click()}
            className={cn(
              'flex w-full items-center gap-3 rounded-lg border border-dashed px-4 py-4 transition-colors',
              multiple ? 'text-start' : 'flex-col justify-center gap-2 py-5 text-center',
              'focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
              rejected
                ? 'border-destructive/60 bg-destructive/5'
                : dragging
                  ? 'border-primary bg-accent/60'
                  : 'border-input bg-muted/40 hover:border-primary/60 hover:bg-accent/40 dark:bg-black/20',
              disabled && 'cursor-not-allowed opacity-60',
            )}
          >
            <span
              className={cn(
                'flex size-9 shrink-0 items-center justify-center rounded-full transition-colors',
                rejected
                  ? 'bg-destructive/10 text-destructive ring-1 ring-inset ring-destructive/20'
                  : 'bg-primary/10 text-primary ring-1 ring-inset ring-primary/15',
              )}
            >
              <Upload className="size-4" aria-hidden="true" />
            </span>
            <span className={cn('flex flex-col gap-0.5', multiple && 'min-w-0 flex-1')}>
              <span className="text-sm font-medium text-foreground">{t('common.dropFile')}</span>
              {extText ? <span className="text-xs text-muted-foreground">{t('common.supportedFormats', { ext: extText })}</span> : null}
            </span>
          </button>
        )}
        {rejected ? (
          <p role="alert" className="text-xs text-destructive">
            {t('common.fileTypeInvalid', { ext: extText })}
          </p>
        ) : null}
        {multiple
          ? files.map((f) =>
              row(
                f,
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground"
                  aria-label={t('common.remove')}
                  disabled={disabled}
                  onClick={() => onChange(files.filter((x) => x !== f))}
                >
                  <X aria-hidden="true" />
                </Button>,
              ),
            )
          : null}
      </div>
    </div>
  )
}
