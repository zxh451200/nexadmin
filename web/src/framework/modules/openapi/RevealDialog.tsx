import type { ReactNode } from 'react'
import { TriangleAlert } from 'lucide-react'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { CopyButton } from '../sso/CopyButton'

export interface RevealField {
  label: string
  value: string
}

/** 单行可复制的值 */
export function ValueRow({ label, value }: RevealField) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex items-center gap-1 rounded-md border border-border bg-muted/40 ps-3">
        <code className="min-w-0 flex-1 break-all py-2 font-mono text-sm text-foreground">{value}</code>
        <CopyButton text={value} label={label} />
      </div>
    </div>
  )
}

/** 多行代码块 + 复制按钮 */
export function CodeBlock({ code, label }: { code: string; label: string }) {
  return (
    <div className="relative rounded-md border border-border bg-muted/40">
      <div className="absolute end-1 top-1">
        <CopyButton text={code} label={label} />
      </div>
      <pre className="overflow-x-auto p-3 pe-12 text-xs leading-relaxed text-foreground">
        <code className="font-mono">{code}</code>
      </pre>
    </div>
  )
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  /** 有值时显示醒目的“只显示一次”提示 */
  warning?: ReactNode
  fields: RevealField[]
  closeText: ReactNode
  /** 附加内容（使用示例、签名说明等） */
  children?: ReactNode
}

/** 一次性展示密钥 / 令牌的对话框：关闭后不可再次查看 */
export function RevealDialog({ open, onOpenChange, title, description, warning, fields, closeText, children }: Props) {
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={title}
      description={description}
      footer={
        <Button type="button" onClick={() => onOpenChange(false)}>
          {closeText}
        </Button>
      }
    >
      <div className="flex flex-col gap-5">
        {warning ? (
          <div role="alert" className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-foreground">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
            <span>{warning}</span>
          </div>
        ) : null}
        {fields.map((f) => (
          <ValueRow key={f.label} {...f} />
        ))}
        {children}
      </div>
    </ResponsiveDialog>
  )
}
