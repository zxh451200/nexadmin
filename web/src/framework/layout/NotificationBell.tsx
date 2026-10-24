import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Bell } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import { useMessageStream } from '@/framework/hooks/use-message-stream'
import type { Page } from '@/framework/request/types'
import { Button } from '@/framework/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { useState } from 'react'

interface UnreadCount {
  messages: number
  notices: number
}

interface MessageItem {
  id: string
  title: string
  category?: string
  link?: string
  readAt?: string | null
  createdAt: string
}

function formatTime(iso: string, lang: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat(lang, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(d)
}

export function NotificationBell() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  // 优先用 SSE 推送；断线重连失败 3 次或 mock 模式下退化为 30 秒轮询（顶栏全局，不随标签页暂停）
  const refetchInterval = useMessageStream()
  const { data: unread } = useQuery({
    queryKey: ['notify', 'unread-count'],
    queryFn: () => api.get<UnreadCount>('/messages/unread-count', undefined, { silent: true }),
    refetchInterval,
    retry: false,
  })
  const { data: recent } = useQuery({
    queryKey: ['notify', 'recent'],
    queryFn: () => api.get<Page<MessageItem>>('/messages', { page: 1, pageSize: 5 }, { silent: true }),
    enabled: open,
    retry: false,
  })

  const total = (unread?.messages ?? 0) + (unread?.notices ?? 0)
  const go = (to: string) => {
    setOpen(false)
    navigate(to)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="relative size-11 md:size-9"
          aria-label={total > 0 ? t('layout.notificationsUnread', { count: total }) : t('layout.notifications')}
        >
          <Bell className="size-5 md:size-4" aria-hidden="true" />
          {total > 0 && (
            <span
              className="num absolute end-1.5 top-1.5 flex min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-medium leading-4 text-destructive-foreground md:end-0.5 md:top-0.5"
              aria-hidden="true"
            >
              {total > 99 ? '99+' : total}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[calc(100vw-2rem)] p-0 md:w-80">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-sm font-medium">{t('layout.messageCenter')}</span>
          <span className="num text-xs text-muted-foreground">
            {t('layout.unreadSummary', { messages: unread?.messages ?? 0, notices: unread?.notices ?? 0 })}
          </span>
        </div>
        <ul className="max-h-80 overflow-y-auto">
          {(recent?.list ?? []).length === 0 && (
            <li className="px-4 py-8 text-center text-sm text-muted-foreground">{t('layout.noMessages')}</li>
          )}
          {(recent?.list ?? []).map((m) => (
            <li key={m.id}>
              <button
                type="button"
                onClick={() => go(m.link || '/message')}
                className="flex min-h-11 w-full items-start gap-3 px-4 py-3 text-start hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
              >
                <span className={m.readAt ? 'mt-1.5 size-2 shrink-0' : 'mt-1.5 size-2 shrink-0 rounded-full bg-destructive'} aria-hidden="true" />
                <span className="min-w-0 flex-1">
                  <span className={m.readAt ? 'block truncate text-sm text-muted-foreground' : 'block truncate text-sm font-medium'}>{m.title}</span>
                  <span className="num block text-xs text-muted-foreground">{formatTime(m.createdAt, i18n.language)}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
        <div className="border-t border-border p-2">
          <Button variant="ghost" className="w-full" onClick={() => go('/message')}>
            {t('layout.viewAll')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
