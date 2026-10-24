import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/framework/ui/alert-dialog'
import { buttonVariants } from '@/framework/ui/button'
import { cn } from '@/lib/utils'

export interface ConfirmOptions {
  title?: ReactNode
  description?: ReactNode
  confirmText?: ReactNode
  cancelText?: ReactNode
  /** 危险操作（删除等），确认按钮使用 destructive 样式 */
  destructive?: boolean
}

export interface ConfirmDialogProps extends ConfirmOptions {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  onCancel?: () => void
}

/** 受控确认框；一般用 useConfirm() 命令式调用 */
export function ConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  onCancel,
  title,
  description,
  confirmText,
  cancelText,
  destructive,
}: ConfirmDialogProps) {
  const { t } = useTranslation()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader className="text-start">
          <AlertDialogTitle>{title ?? t('dialog.confirmTitle')}</AlertDialogTitle>
          <AlertDialogDescription>{description ?? t('common.deleteConfirm')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>{cancelText ?? t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            className={cn(destructive && buttonVariants({ variant: 'destructive' }))}
          >
            {confirmText ?? t('common.confirm')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface Request {
  options: ConfirmOptions
  resolve: (v: boolean) => void
}

let enqueue: ((req: Request) => void) | null = null

function ConfirmHost() {
  const [queue, setQueue] = useState<Request[]>([])
  const [open, setOpen] = useState(false)
  const current = queue[0]

  useEffect(() => {
    enqueue = (req) => {
      setQueue((q) => [...q, req])
      setOpen(true)
    }
    return () => {
      enqueue = null
    }
  }, [])

  const settle = useCallback((value: boolean) => {
    setOpen(false)
    setQueue((q) => {
      q[0]?.resolve(value)
      return q.slice(1)
    })
  }, [])

  // 队列里还有下一个时重新打开
  useEffect(() => {
    if (!open && queue.length > 0) setOpen(true)
  }, [open, queue.length])

  if (!current) return null
  return (
    <ConfirmDialog
      {...current.options}
      open={open}
      onOpenChange={(o) => {
        if (!o) settle(false)
      }}
      onConfirm={() => settle(true)}
    />
  )
}

let hostMounted = false
function ensureHost() {
  if (hostMounted || typeof document === 'undefined') return
  hostMounted = true
  const el = document.createElement('div')
  document.body.appendChild(el)
  createRoot(el).render(<ConfirmHost />)
}

/**
 * 命令式确认：`const confirm = useConfirm(); if (await confirm({ destructive: true })) ...`
 * 无需在应用根部挂载，首次调用时自动创建宿主。
 */
export function useConfirm() {
  return useCallback((options: ConfirmOptions | string = {}) => {
    ensureHost()
    const opts: ConfirmOptions = typeof options === 'string' ? { description: options } : options
    return new Promise<boolean>((resolve) => {
      const push = () => {
        if (enqueue) enqueue({ options: opts, resolve })
        else setTimeout(push, 0)
      }
      push()
    })
  }, [])
}
