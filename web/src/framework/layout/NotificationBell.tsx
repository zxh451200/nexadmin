import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Bell, ChevronRight } from 'lucide-react'
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

/** 公告（/notices/mine）里弹层需要的字段；只读标题与已读态。 */
interface NoticeItem {
  id: string
  title: string
  read: boolean
  publishedAt?: string | null
}

/** 弹层里的合并条目：消息与公告同列，按时间倒序取前 5 条。 */
type BellItem = {
  id: string
  title: string
  createdAt: string
  unread: boolean
  kind: 'message' | 'notice'
  link?: string
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
  // 角标把公告也算进去了，弹层就必须一起列：否则「公告 2」配「暂无消息」，看起来像丢了数据。
  const { data: recentNotices } = useQuery({
    queryKey: ['notify', 'recent-notices'],
    queryFn: () => api.get<Page<NoticeItem>>('/notices/mine', { page: 1, pageSize: 5 }, { silent: true }),
    enabled: open,
    retry: false,
  })

  const unreadMessages = unread?.messages ?? 0
  const unreadNotices = unread?.notices ?? 0
  const total = unreadMessages + unreadNotices
  // 「查看全部」跟着未读走：只有公告有未读时落在公告页签，别点进去看到空的消息列表
  const viewAllPath = unreadMessages === 0 && unreadNotices > 0 ? '/message?tab=notices' : '/message'
  const items: BellItem[] = [
    ...(recent?.list ?? []).map((m) => ({
      id: `m-${m.id}`,
      title: m.title,
      createdAt: m.createdAt,
      unread: !m.readAt,
      kind: 'message' as const,
      link: m.link,
    })),
    ...(recentNotices?.list ?? []).map((n) => ({
      id: `n-${n.id}`,
      title: n.title,
      createdAt: n.publishedAt ?? '',
      unread: !n.read,
      kind: 'notice' as const,
    })),
  ]
    // 未读优先，其次按时间倒序：角标统计的是未读，弹层里却只看到已读会让人以为漏了东西。
    .sort((a, b) => Number(b.unread) - Number(a.unread) || (b.createdAt || '').localeCompare(a.createdAt || ''))
    .slice(0, 5)

  const go = (to: string) => {
    setOpen(false)
    navigate(to)
  }
  const openItem = (item: BellItem) => {
    // 公告在消息中心的「公告」页签里看；消息按自身 link 或消息中心。
    if (item.kind === 'notice') go('/message?tab=notices')
    else go(item.link || '/message')
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
            {t('layout.unreadSummary', { messages: unreadMessages, notices: unreadNotices })}
          </span>
        </div>
        {/* 公告和消息是两条数据线：公告条数不一定挤得进下面 5 条预览，单独给一条固定入口，
            避免「公告 2 未读」却只看到消息。 */}
        {unreadNotices > 0 ? (
          <button
            type="button"
            onClick={() => go('/message?tab=notices')}
            className="flex min-h-11 w-full items-center gap-2 border-b border-border px-4 py-2.5 text-start text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
          >
            <span className="size-2 shrink-0 rounded-full bg-destructive" aria-hidden="true" />
            <span className="min-w-0 flex-1 truncate">{t('layout.noticeUnread', { count: unreadNotices })}</span>
            <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>
        ) : null}
        <ul className="max-h-80 overflow-y-auto">
          {items.length === 0 && (
            <li className="px-4 py-8 text-center text-sm text-muted-foreground">{t('layout.noMessages')}</li>
          )}
          {items.map((m) => (
            <li key={m.id}>
              <button
                type="button"
                onClick={() => openItem(m)}
                className="flex min-h-11 w-full items-start gap-3 px-4 py-3 text-start hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
              >
                <span className={m.unread ? 'mt-1.5 size-2 shrink-0 rounded-full bg-destructive' : 'mt-1.5 size-2 shrink-0'} aria-hidden="true" />
                <span className="min-w-0 flex-1">
                  <span className={m.unread ? 'block truncate text-sm font-medium' : 'block truncate text-sm text-muted-foreground'}>{m.title}</span>
                  <span className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="rounded border border-border px-1 text-[10px]">{m.kind === 'notice' ? t('layout.kindNotice') : t('layout.kindMessage')}</span>
                    <span className="num">{formatTime(m.createdAt, i18n.language)}</span>
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
        <div className="border-t border-border p-2">
          <Button variant="ghost" className="w-full" onClick={() => go(viewAllPath)}>
            {t('layout.viewAll')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
