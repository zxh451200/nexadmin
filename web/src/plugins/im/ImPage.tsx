import { useEffect, useRef, useState } from 'react'
import { Ban, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { useWs } from '@/framework/hooks/useWs'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
import { useCloseConv, useConversations, useMessages, useSendReply, openImStream, type ImConversation, type ImMessage } from './api'
import { formatTime, statusVariant } from './format'

const ALL = '__all'

export default function ImPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<string | undefined>()
  const [keyword, setKeyword] = useState('')
  const [selected, setSelected] = useState<string | undefined>()
  const [live, setLive] = useState(false)

  const convs = useConversations({ status, keyword: keyword || undefined, page: 1, pageSize: 50 })
  const msgs = useMessages(selected, live ? 15000 : 2000)
  const send = useSendReply()
  const close = useCloseConv()

  // 实时推送双通道:优先 WebSocket(框架 useWs hub,主题 im:<convNo>),
  // WS 不可用时降级到 SSE 流;都断时退回慢轮询。
  const { status: wsStatus, subscribe } = useWs()
  useEffect(() => {
    setLive(false)
    if (!selected) return
    const invalidate = () => void queryClient.invalidateQueries({ queryKey: ['plugin_im', 'messages', selected] })

    // WS 在线:直接订阅主题
    if (wsStatus === 'open') {
      setLive(true)
      return subscribe('im:' + selected, invalidate)
    }
    // WS 未就绪:SSE 兜底
    let cancelled = false
    let closeStream: (() => void) | null = null
    void openImStream(
      selected,
      invalidate,
      () => !cancelled && setLive(false),
    ).then((close) => {
      if (cancelled) close()
      else {
        closeStream = close
        setLive(true)
      }
    })
    return () => {
      cancelled = true
      closeStream?.()
    }
  }, [selected, queryClient, wsStatus, subscribe])

  const onSend = async (content: string) => {
    if (!selected) return
    try {
      await send.mutateAsync({ convNo: selected, content })
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onClose = async (c: ImConversation) => {
    if (await confirm({ destructive: true, description: t('plugin_im.closeConfirm', { no: c.convNo }) })) {
      try {
        await close.mutateAsync(c.convNo)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_im.title')} description={t('plugin_im.description')} />
      <div className="grid h-[calc(100vh-14rem)] min-h-96 grid-cols-1 gap-4 md:grid-cols-[22rem_1fr]">
        <div className="flex min-h-0 flex-col gap-2 rounded-lg border bg-card p-2">
          <div className="flex items-center gap-2">
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('plugin_im.searchPlaceholder')}
              aria-label={t('common.keyword')}
              className="h-8"
            />
            <select
              aria-label={t('plugin_im.fields.status')}
              className="h-8 rounded-md border bg-background px-2 text-sm"
              value={status ?? ALL}
              onChange={(e) => setStatus(e.target.value === ALL ? undefined : e.target.value)}
            >
              <option value={ALL}>{t('plugin_im.all')}</option>
              <option value="open">{t('plugin_im.status.open')}</option>
              <option value="closed">{t('plugin_im.status.closed')}</option>
            </select>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {(convs.data?.list ?? []).map((c) => (
              <ConvListItem key={c.id} conv={c} active={c.convNo === selected} onSelect={() => setSelected(c.convNo)} />
            ))}
            {!convs.isLoading && (convs.data?.list ?? []).length === 0 ? (
              <div className="p-4 text-center text-sm text-muted-foreground">{t('plugin_im.empty')}</div>
            ) : null}
          </div>
        </div>

        <ChatPane
          conv={msgs.data?.conversation}
          messages={msgs.data?.messages ?? []}
          loading={msgs.isLoading}
          onSend={onSend}
          sending={send.isPending}
          onClose={(c) => void onClose(c)}
        />
      </div>
    </PageContainer>
  )
}

function ConvListItem({ conv, active, onSelect }: { conv: ImConversation; active: boolean; onSelect: () => void }) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'flex w-full flex-col gap-1 rounded-md px-3 py-2 text-start text-sm transition-colors hover:bg-accent',
        active && 'bg-accent',
      )}
    >
      <div className="flex w-full items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">
          {conv.visitorName || conv.visitorToken}
        </span>
        {conv.unread > 0 ? <Badge variant="destructive">{conv.unread}</Badge> : null}
        <Badge variant={statusVariant(conv.status)}>{t(`plugin_im.status.${conv.status}`)}</Badge>
      </div>
      <div className="truncate text-xs text-muted-foreground">
        {conv.bizNo ? `${conv.bizType}/${conv.bizNo} · ` : ''}
        {conv.lastPreview || t('plugin_im.noMessages')}
      </div>
    </button>
  )
}

function ChatPane({
  conv,
  messages,
  loading,
  sending,
  onSend,
  onClose,
}: {
  conv?: ImConversation
  messages: ImMessage[]
  loading: boolean
  sending: boolean
  onSend: (content: string) => void
  onClose: (conv: ImConversation) => void
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length, conv?.convNo])

  if (!conv) {
    return (
      <div className="flex items-center justify-center rounded-lg border bg-card text-sm text-muted-foreground">
        {t('plugin_im.pickConversation')}
      </div>
    )
  }

  const submit = () => {
    const content = draft.trim()
    if (!content) return
    onSend(content)
    setDraft('')
  }

  return (
    <div className="flex min-h-0 flex-col rounded-lg border bg-card">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium text-foreground">
            {conv.visitorName || conv.visitorToken}
            {conv.subject ? <span className="text-muted-foreground"> · {conv.subject}</span> : null}
          </div>
          <div className="truncate text-xs text-muted-foreground">{conv.convNo}</div>
        </div>
        {conv.status === 'open' ? (
          <Can perm="im:edit">
            <Button variant="outline" size="sm" onClick={() => onClose(conv)}>
              <Ban aria-hidden="true" />
              {t('plugin_im.close')}
            </Button>
          </Can>
        ) : (
          <Badge variant="secondary">{t('plugin_im.status.closed')}</Badge>
        )}
      </div>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
        {loading && messages.length === 0 ? (
          <div className="text-center text-sm text-muted-foreground">{t('plugin_im.loading')}</div>
        ) : null}
        {messages.map((m) => (
          <div key={m.id} className={cn('flex', m.senderType === 'agent' ? 'justify-end' : 'justify-start')}>
            <div
              className={cn(
                'max-w-[70%] rounded-lg px-3 py-2 text-sm',
                m.senderType === 'agent'
                  ? 'bg-primary text-primary-foreground'
                  : m.senderType === 'system'
                    ? 'bg-muted text-muted-foreground'
                    : 'bg-muted text-foreground',
              )}
            >
              <div className="mb-0.5 text-[11px] opacity-70">
                {m.senderName || t(`plugin_im.senders.${m.senderType}`)} · {formatTime(m.createdAt, i18n.language)}
              </div>
              <div className="whitespace-pre-wrap break-words">{m.content}</div>
            </div>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      {conv.status === 'open' ? (
        <Can perm="im:reply">
          <form
            className="flex items-center gap-2 border-t p-3"
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
          >
            <Input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={t('plugin_im.replyPlaceholder')}
              aria-label={t('plugin_im.replyPlaceholder')}
            />
            <Button type="submit" disabled={sending || !draft.trim()}>
              <Send aria-hidden="true" />
              {t('plugin_im.send')}
            </Button>
          </form>
        </Can>
      ) : null}
    </div>
  )
}
