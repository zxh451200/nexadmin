import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { api } from '@/framework/request/http'
import { RichTextView } from '@/framework/components/RichTextView'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { ScrollArea } from '@/framework/ui/scroll-area'

interface PopupNotice {
  id: string
  title: string
  content: string
  publishedAt?: string
}

/** 登录后拉取重要公告，合并为一个弹窗分页展示；「我知道了」/「全部已读」记为已读，关闭则本次会话不再弹 */
export function AnnouncementPopup() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [dismissed, setDismissed] = useState(false)
  const [index, setIndex] = useState(0)

  const { data = [] } = useQuery({
    queryKey: ['notify', 'popup'],
    queryFn: () => api.get<PopupNotice[]>('/notices/popup', undefined, { silent: true }),
    staleTime: Infinity,
    retry: false,
  })

  const readAll = useMutation({
    mutationFn: (ids: string[]) => Promise.all(ids.map((id) => api.post(`/notices/${id}/read`, undefined, { silent: true }))),
    onSuccess: () => {
      setDismissed(true)
      void qc.invalidateQueries({ queryKey: ['notify', 'unread-count'] })
    },
  })

  // Dialog 必须保持挂载、用 open 驱动关闭：若在 dismissed 时直接 return null，
  // Radix 来不及走关闭流程，body 上的 pointer-events:none 不会被恢复，
  // 整个页面从此点不动（实测复现过：Esc 后只剩一个残留按钮、内容区不可交互）。
  if (data.length === 0) return null

  const total = data.length
  const current = data[Math.min(index, total - 1)]

  return (
    <Dialog open={!dismissed} onOpenChange={(o) => !o && setDismissed(true)}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{current.title}</DialogTitle>
          <DialogDescription className="sr-only">{t('layout.announcement')}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[60dvh] pe-3">
          <RichTextView html={current.content} />
        </ScrollArea>
        <DialogFooter className="items-center sm:justify-between">
          {total > 1 ? (
            <div className="flex items-center gap-1">
              <Button variant="ghost" size="icon" aria-label={t('layout.prevNotice')} disabled={index === 0} onClick={() => setIndex(index - 1)}>
                <ChevronLeft className="size-4" />
              </Button>
              <span className="text-sm tabular-nums text-muted-foreground">
                {index + 1} / {total}
              </span>
              <Button variant="ghost" size="icon" aria-label={t('layout.nextNotice')} disabled={index === total - 1} onClick={() => setIndex(index + 1)}>
                <ChevronRight className="size-4" />
              </Button>
            </div>
          ) : (
            <span />
          )}
          <Button onClick={() => readAll.mutate(data.map((n) => n.id))} disabled={readAll.isPending}>
            {total > 1 ? t('layout.gotItAll', { count: total }) : t('layout.gotIt')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
