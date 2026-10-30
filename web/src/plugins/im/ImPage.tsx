import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ArrowRightLeft, Ban, Bell, BellOff, ChevronUp, FileText, Lock, MessageSquare, Paperclip, Pencil, Send, Settings2, Star, Tag, Trash2, UserMinus, UserPlus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { TimeField } from '@/framework/components/pickers/TimeField'
import { useWs } from '@/framework/hooks/useWs'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import {
  imAttachmentUrl,
  messageAttachment,
  openImStream,
  sendTyping,
  uploadAttachment,
  useAgents,
  useChatSession,
  useImSettings,
  useSaveImSettings,
  useClaimConv,
  useCloseConv,
  useConversations,
  useDeleteQuickReply,
  useQuickReplies,
  useSaveQuickReply,
  useSendReply,
  useSetTags,
  useTransferConv,
  type ImAgent,
  type ImAttachment,
  type ImSettings,
  type ImConversation,
  type ImMessage,
  type ImQuickReply,
  type SenderType,
} from './api'
import { formatSize, formatTime, statusVariant } from './format'

const ALL = '__all'
const NOTIFY_KEY = 'nex_im_notify'

type StreamEvent = { type?: string; senderType?: SenderType; seq?: number }

function asStreamEvent(v: unknown): StreamEvent | null {
  return typeof v === 'object' && v !== null ? (v as StreamEvent) : null
}

function isMessage(v: unknown): v is ImMessage {
  return typeof v === 'object' && v !== null && typeof (v as ImMessage).seq === 'number'
}

/** 提示音：短促双音，不依赖音频资源。 */
function playBeep() {
  try {
    const Ctx = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctx) return
    const ctx = new Ctx()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'sine'
    osc.frequency.value = 880
    gain.gain.value = 0.04
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.start()
    osc.stop(ctx.currentTime + 0.12)
    window.setTimeout(() => void ctx.close(), 400)
  } catch {
    // 浏览器策略可能拒绝自动播放，静默
  }
}

function notifyDesktop(title: string, body: string) {
  if (!('Notification' in window) || Notification.permission !== 'granted') return
  try {
    new Notification(title, { body })
  } catch {
    // 某些环境（如桌面壳）不支持构造，忽略
  }
}

export default function ImPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [params] = useSearchParams()
  // 站内信深链：/im/conversations?conv=IMxxx 直接打开对应会话。
  const linkedConv = params.get('conv') || undefined
  const [status, setStatus] = useState<string | undefined>()
  const [assignee, setAssignee] = useState<'all' | 'me' | 'unassigned'>('all')
  const [keyword, setKeyword] = useState('')
  const [selected, setSelected] = useState<string | undefined>(linkedConv)
  const [live, setLive] = useState(false)
  const [typing, setTyping] = useState(false)
  const [pageSize, setPageSize] = useState(50)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [notifyOn, setNotifyOn] = useState(() => localStorage.getItem(NOTIFY_KEY) === '1')

  const convs = useConversations({
    status,
    keyword: keyword || undefined,
    assignee: assignee === 'all' ? undefined : assignee,
    page: 1,
    pageSize,
  })
  const chat = useChatSession(selected, live ? 15000 : 2000)
  const send = useSendReply()
  const close = useCloseConv()
  const claim = useClaimConv()
  const setTags = useSetTags()
  const transfer = useTransferConv()
  const [uploading, setUploading] = useState(false)
  const refresh = chat.refresh

  useEffect(() => {
    if (linkedConv) setSelected(linkedConv)
  }, [linkedConv])

  // 实时推送双通道：优先 WebSocket（框架 hub，主题 im:<convNo>），
  // WS 不可用降级 SSE，都断时退回慢轮询。
  const { status: wsStatus, subscribe } = useWs()
  useEffect(() => {
    setLive(false)
    setTyping(false)
    if (!selected) return
    let typingTimer = 0
    const onEvent = (raw: unknown) => {
      const evt = asStreamEvent(raw)
      if (evt && evt.type === 'typing') {
        if (evt.senderType !== 'visitor') return
        setTyping(true)
        window.clearTimeout(typingTimer)
        typingTimer = window.setTimeout(() => setTyping(false), 4000)
        return
      }
      if (isMessage(raw) && raw.senderType === 'visitor' && notifyOn && (document.hidden || !document.hasFocus())) {
        playBeep()
        notifyDesktop(t('plugin_im.title'), raw.content || t('plugin_im.newVisitorMessage'))
      }
      void refresh()
    }
    if (wsStatus === 'open') {
      setLive(true)
      const unsub = subscribe('im:' + selected, onEvent)
      return () => {
        window.clearTimeout(typingTimer)
        unsub()
      }
    }
    let cancelled = false
    let closeStream: (() => void) | null = null
    void openImStream(
      selected,
      onEvent,
      () => !cancelled && setLive(false),
    ).then((closeFn) => {
      if (cancelled) closeFn()
      else {
        closeStream = closeFn
        setLive(true)
      }
    })
    return () => {
      cancelled = true
      window.clearTimeout(typingTimer)
      closeStream?.()
    }
  }, [selected, wsStatus, subscribe, refresh, notifyOn, t])

  const toggleNotify = async () => {
    if (!('Notification' in window)) {
      toast.error(t('plugin_im.notify.unsupported'))
      return
    }
    if (Notification.permission !== 'granted') {
      const perm = await Notification.requestPermission()
      if (perm !== 'granted') {
        toast.error(t('plugin_im.notify.denied'))
        return
      }
    }
    setNotifyOn((prev) => {
      const next = !prev
      localStorage.setItem(NOTIFY_KEY, next ? '1' : '0')
      return next
    })
  }

  const onSend = async (content: string) => {
    if (!selected) return
    try {
      await send.mutateAsync({ convNo: selected, content })
      await refresh()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onUploadFile = async (file: File) => {
    if (!selected) return
    setUploading(true)
    try {
      const att = await uploadAttachment(selected, file)
      await send.mutateAsync({ convNo: selected, content: '', attachmentToken: att.token })
      await refresh()
    } catch {
      toast.error(t('plugin_im.uploadFailed'))
    } finally {
      setUploading(false)
    }
  }

  const onTransfer = async (c: ImConversation, agent: ImAgent) => {
    try {
      await transfer.mutateAsync({ convNo: c.convNo, assigneeId: agent.id, assigneeName: agent.nickname || agent.username })
      await refresh()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onClose = async (c: ImConversation) => {
    if (await confirm({ destructive: true, description: t('plugin_im.closeConfirm', { no: c.convNo }) })) {
      try {
        await close.mutateAsync(c.convNo)
        await refresh()
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const onClaim = async (c: ImConversation, release = false) => {
    try {
      await claim.mutateAsync({ convNo: c.convNo, release })
      await refresh()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onSaveTags = async (c: ImConversation, tags: string) => {
    try {
      await setTags.mutateAsync({ convNo: c.convNo, tags })
      await refresh()
      toast.success(t('common.operateSuccess'))
      return true
    } catch {
      toast.error(t('common.operateFailed'))
      return false
    }
  }

  const list = convs.data?.list ?? []
  const total = convs.data?.total ?? 0

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_im.title')}
        description={t('plugin_im.description')}
        actions={
          <Can perm="im:edit">
            <Button variant="outline" size="sm" onClick={() => setSettingsOpen(true)}>
              <Settings2 aria-hidden="true" />
              <span className="max-sm:sr-only">{t('plugin_im.settings.action')}</span>
            </Button>
          </Can>
        }
      />
      <div className="grid h-[calc(100vh-14rem)] min-h-96 grid-cols-1 gap-4 md:grid-cols-[22rem_1fr]">
        <Card className="flex min-h-0 flex-col gap-2 p-2">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('plugin_im.searchPlaceholder')}
              aria-label={t('common.keyword')}
              className="min-w-32 flex-1"
            />
            <SimpleSelect
              aria-label={t('plugin_im.fields.status')}
              className="w-20"
              value={status ?? ALL}
              onChange={(v) => setStatus(v === ALL ? undefined : v)}
              options={[
                { value: ALL, label: t('plugin_im.all') },
                { value: 'open', label: t('plugin_im.status.open') },
                { value: 'closed', label: t('plugin_im.status.closed') },
              ]}
            />
          </div>
          <div className="flex items-center gap-2">
            <SimpleSelect
              aria-label={t('plugin_im.fields.assignee')}
              className="flex-1"
              value={assignee}
              onChange={(v) => setAssignee(v as 'all' | 'me' | 'unassigned')}
              options={[
                { value: 'all', label: t('plugin_im.assignee.all') },
                { value: 'me', label: t('plugin_im.assignee.me') },
                { value: 'unassigned', label: t('plugin_im.assignee.unassigned') },
              ]}
            />
            <Button
              type="button"
              variant={notifyOn ? 'secondary' : 'outline'}
              size="icon"
              onClick={() => void toggleNotify()}
              aria-pressed={notifyOn}
              aria-label={notifyOn ? t('plugin_im.notify.on') : t('plugin_im.notify.off')}
              title={notifyOn ? t('plugin_im.notify.on') : t('plugin_im.notify.off')}
            >
              {notifyOn ? <Bell aria-hidden="true" /> : <BellOff aria-hidden="true" />}
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {list.map((c) => (
              <ConvListItem key={c.id} conv={c} active={c.convNo === selected} onSelect={() => setSelected(c.convNo)} />
            ))}
            {list.length < total ? (
              <Button variant="ghost" size="sm" className="mt-1 w-full" onClick={() => setPageSize((n) => n + 50)}>
                {t('plugin_im.loadMore')}
              </Button>
            ) : null}
            {!convs.isLoading && list.length === 0 ? <EmptyState title={t('plugin_im.empty')} className="py-8" /> : null}
          </div>
        </Card>

        <ChatPane
          conv={chat.conv}
          messages={chat.messages}
          loading={chat.loading}
          hasMore={chat.hasMore}
          typing={typing}
          onLoadOlder={() => void chat.loadOlder()}
          onSend={onSend}
          onTyping={() => selected && sendTyping(selected)}
          onUploadFile={(file) => void onUploadFile(file)}
          uploading={uploading}
          sending={send.isPending}
          onClose={(c) => void onClose(c)}
          onClaim={(c, release) => void onClaim(c, release)}
          onTransfer={(c, agent) => void onTransfer(c, agent)}
          onSaveTags={onSaveTags}
        />
      </div>

      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
    </PageContainer>
  )
}

function ConvListItem({ conv, active, onSelect }: { conv: ImConversation; active: boolean; onSelect: () => void }) {
  const { t } = useTranslation()
  const tags = (conv.tags || '').split(',').filter(Boolean)
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
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">{conv.visitorName || conv.visitorToken}</span>
        {conv.unread > 0 ? <Badge variant="destructive">{conv.unread}</Badge> : null}
        <Badge variant={statusVariant(conv.status)}>{t(`plugin_im.status.${conv.status}`)}</Badge>
      </div>
      <div className="truncate text-xs text-muted-foreground">
        {conv.assigneeName ? `${conv.assigneeName} · ` : ''}
        {conv.bizNo ? `${conv.bizType}/${conv.bizNo} · ` : ''}
        {conv.lastPreview || t('plugin_im.noMessages')}
      </div>
      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {tags.slice(0, 3).map((tag) => (
            <Badge key={tag} variant="outline" className="px-1.5 py-0 text-[10px] font-normal">
              {tag}
            </Badge>
          ))}
        </div>
      ) : null}
    </button>
  )
}

function systemLabel(m: ImMessage, t: (key: string) => string): string {
  if (m.meta) {
    try {
      const code = (JSON.parse(m.meta) as { code?: string }).code
      if (code) return t(`plugin_im.system.${code}`)
    } catch {
      // meta 不是 JSON 时回落到 content
    }
  }
  return m.content
}

function RatingStars({ value, className }: { value: number; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-0.5', className)} aria-label={`${value}/5`}>
      {[1, 2, 3, 4, 5].map((n) => (
        <Star
          key={n}
          aria-hidden="true"
          className={cn('size-3.5', n <= value ? 'fill-warning text-warning' : 'text-muted-foreground/40')}
        />
      ))}
    </span>
  )
}

function FileAttachment({ att, url }: { att: ImAttachment; url: string }) {
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      className="flex max-w-full items-center gap-2 underline underline-offset-2"
    >
      <FileText aria-hidden="true" className="size-4 shrink-0" />
      <span className="truncate">{att.name}</span>
      <span className="shrink-0 text-[11px] opacity-70">{formatSize(att.size)}</span>
    </a>
  )
}

// 图片取不到时退回文件名链接，气泡里不会剩一块空白。
function ImageAttachment({ att, url }: { att: ImAttachment; url: string }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <FileAttachment att={att} url={url} />
  return (
    <a href={url} target="_blank" rel="noreferrer" className="block">
      <img
        src={url}
        alt={att.name}
        loading="lazy"
        onError={() => setFailed(true)}
        className="max-h-60 max-w-full min-w-20 rounded-md object-contain"
      />
    </a>
  )
}

function ChatPane({
  conv,
  messages,
  loading,
  hasMore,
  typing,
  onLoadOlder,
  onSend,
  onTyping,
  onUploadFile,
  uploading,
  sending,
  onClose,
  onClaim,
  onTransfer,
  onSaveTags,
}: {
  conv?: ImConversation
  messages: ImMessage[]
  loading: boolean
  hasMore: boolean
  typing: boolean
  onLoadOlder: () => void
  onSend: (content: string) => void
  onTyping: () => void
  onUploadFile: (file: File) => void
  uploading: boolean
  sending: boolean
  onClose: (conv: ImConversation) => void
  onClaim: (conv: ImConversation, release: boolean) => void
  onTransfer: (conv: ImConversation, agent: ImAgent) => void
  onSaveTags: (conv: ImConversation, tags: string) => Promise<boolean>
}) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState('')
  const [tagDraft, setTagDraft] = useState<string | null>(null)
  const [transferOpen, setTransferOpen] = useState(false)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const fileRef = useRef<HTMLInputElement | null>(null)
  const lastTyping = useRef(0)

  useEffect(() => {
    setTagDraft(null)
    setDraft('')
    setTransferOpen(false)
  }, [conv?.convNo])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length, conv?.convNo])

  if (!conv) {
    return (
      <Card className="flex items-center justify-center text-sm text-muted-foreground">
        {t('plugin_im.pickConversation')}
      </Card>
    )
  }

  const submit = () => {
    const content = draft.trim()
    if (!content) return
    onSend(content)
    setDraft('')
  }

  const onChangeDraft = (value: string) => {
    setDraft(value)
    const now = Date.now()
    if (value && now - lastTyping.current > 2000) {
      lastTyping.current = now
      onTyping()
    }
  }

  return (
    <Card className="flex min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 truncate text-sm font-medium text-foreground">
            {conv.visitorName || conv.visitorToken}
            {conv.subject ? <span className="truncate text-muted-foreground"> · {conv.subject}</span> : null}
            {conv.rating > 0 ? <RatingStars value={conv.rating} /> : null}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {conv.convNo}
            {conv.assigneeName ? ` · ${t('plugin_im.assignee.ownedBy', { name: conv.assigneeName })}` : ''}
            {conv.visitorIp ? ` · ${conv.visitorIp}` : ''}
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Can perm="im:reply">
            {conv.assigneeId ? (
              <Button variant="ghost" size="sm" onClick={() => onClaim(conv, true)}>
                <UserMinus aria-hidden="true" />
                <span className="max-sm:sr-only">{t('plugin_im.release')}</span>
              </Button>
            ) : (
              <Button variant="ghost" size="sm" onClick={() => onClaim(conv, false)}>
                <UserPlus aria-hidden="true" />
                <span className="max-sm:sr-only">{t('plugin_im.claim')}</span>
              </Button>
            )}
          </Can>
          <Can perm="im:edit">
            <Button variant="ghost" size="sm" onClick={() => setTransferOpen(true)}>
              <ArrowRightLeft aria-hidden="true" />
              <span className="max-sm:sr-only">{t('plugin_im.transfer')}</span>
            </Button>
          </Can>
          <Can perm="im:edit">
            <Button variant="ghost" size="sm" onClick={() => setTagDraft(conv.tags || '')}>
              <Tag aria-hidden="true" />
              <span className="max-sm:sr-only">{t('plugin_im.tags.action')}</span>
            </Button>
          </Can>
          {conv.status === 'open' ? (
            <Can perm="im:edit">
              <Button variant="outline" size="sm" onClick={() => onClose(conv)}>
                <Ban aria-hidden="true" />
                <span className="max-sm:sr-only">{t('plugin_im.close')}</span>
              </Button>
            </Can>
          ) : (
            <Badge variant="secondary">{t('plugin_im.status.closed')}</Badge>
          )}
        </div>
      </div>

      {(conv.tags || tagDraft !== null) && tagDraft === null ? (
        <div className="flex flex-wrap gap-1 border-b px-4 py-1.5">
          {(conv.tags || '')
            .split(',')
            .filter(Boolean)
            .map((tag) => (
              <Badge key={tag} variant="outline" className="text-[11px] font-normal">
                {tag}
              </Badge>
            ))}
        </div>
      ) : null}

      {tagDraft !== null ? (
        <div className="flex items-center gap-2 border-b px-4 py-2">
          <Input
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            placeholder={t('plugin_im.tags.placeholder')}
            aria-label={t('plugin_im.tags.placeholder')}
            autoFocus
          />
          <Button
            size="sm"
            onClick={() => {
              void onSaveTags(conv, tagDraft).then((ok) => ok && setTagDraft(null))
            }}
          >
            {t('common.save')}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setTagDraft(null)}>
            {t('common.cancel')}
          </Button>
        </div>
      ) : null}

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
        {hasMore ? (
          <div className="text-center">
            <Button variant="ghost" size="sm" onClick={onLoadOlder}>
              <ChevronUp aria-hidden="true" />
              {t('plugin_im.loadOlder')}
            </Button>
          </div>
        ) : null}
        {loading && messages.length === 0 ? (
          <div className="text-center text-sm text-muted-foreground">{t('plugin_im.loading')}</div>
        ) : null}
        {messages.map((m) => {
          if (m.kind === 'system') {
            return (
              <div key={m.id} className="flex justify-center py-1">
                <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs text-muted-foreground">
                  {systemLabel(m, (key) => t(key))} · {formatTime(m.createdAt, i18n.language)}
                </span>
              </div>
            )
          }
          const mine = m.senderType === 'agent'
          const att = messageAttachment(m)
          const attUrl = att ? imAttachmentUrl(att.token) : ''
          return (
            <div key={m.id} className={cn('flex', mine ? 'justify-end' : 'justify-start')}>
              <div
                className={cn(
                  'max-w-[70%] rounded-lg px-3 py-2 text-sm',
                  mine ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground',
                )}
              >
                <div className="mb-0.5 flex items-center gap-2 text-[11px] opacity-70">
                  <span>{m.senderName || t(`plugin_im.senders.${m.senderType}`)}</span>
                  <span>{formatTime(m.createdAt, i18n.language)}</span>
                  {mine ? (
                    <span>{conv.visitorReadSeq >= m.seq ? t('plugin_im.read') : t('plugin_im.unread')}</span>
                  ) : null}
                </div>
                {att ? (
                  att.mime.startsWith('image/') ? (
                    <ImageAttachment att={att} url={attUrl} />
                  ) : (
                    <FileAttachment att={att} url={attUrl} />
                  )
                ) : null}
                {m.content ? <div className="whitespace-pre-wrap break-words">{m.content}</div> : null}
              </div>
            </div>
          )
        })}
        <div ref={bottomRef} />
      </div>

      {typing ? (
        <div className="px-4 pb-1 text-xs text-muted-foreground">{t('plugin_im.visitorTyping')}</div>
      ) : null}

      {conv.status === 'open' ? (
        <Can perm="im:reply">
          <form
            className="flex items-end gap-2 border-t p-3"
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
          >
            <input
              ref={fileRef}
              type="file"
              className="hidden"
              accept="image/png,image/jpeg,image/gif,image/webp,application/pdf,text/plain,.zip,.docx,.xlsx,.pptx"
              onChange={(e) => {
                const file = e.target.files?.[0]
                e.target.value = ''
                if (file) onUploadFile(file)
              }}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={uploading}
              onClick={() => fileRef.current?.click()}
              aria-label={t('plugin_im.attach')}
              title={t('plugin_im.attach')}
            >
              <Paperclip aria-hidden="true" />
            </Button>
            <QuickReplyPicker
              onPick={(content) => {
                setDraft((prev) => (prev ? `${prev}\n${content}` : content))
                inputRef.current?.focus()
              }}
            />
            <Textarea
              ref={inputRef}
              value={draft}
              onChange={(e) => onChangeDraft(e.target.value)}
              placeholder={t('plugin_im.replyPlaceholder')}
              aria-label={t('plugin_im.replyPlaceholder')}
              rows={1}
              className="max-h-32 min-h-10 flex-1 resize-none py-2"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault()
                  submit()
                }
              }}
            />
            <Button type="submit" disabled={sending || !draft.trim()}>
              <Send aria-hidden="true" />
              <span className="max-sm:sr-only">{t('plugin_im.send')}</span>
            </Button>
          </form>
        </Can>
      ) : (
        <div className="flex flex-wrap items-center gap-2 border-t bg-muted/40 px-4 py-3 text-sm">
          <Lock aria-hidden="true" className="size-4 text-muted-foreground" />
          <span className="text-muted-foreground">
            {conv.closedBy === 'auto' ? t('plugin_im.closedByAuto') : t('plugin_im.closedByAgent')}
          </span>
          {conv.rating > 0 ? (
            <span className="flex items-center gap-1 text-muted-foreground">
              <RatingStars value={conv.rating} />
              {conv.ratingComment ? <span className="truncate">“{conv.ratingComment}”</span> : null}
            </span>
          ) : (
            <span className="text-xs text-muted-foreground">{t('plugin_im.ratingPending')}</span>
          )}
        </div>
      )}

      <TransferDialog
        open={transferOpen}
        onOpenChange={setTransferOpen}
        conv={conv}
        onPick={(agent) => {
          setTransferOpen(false)
          onTransfer(conv, agent)
        }}
      />
    </Card>
  )
}

function TransferDialog({
  open,
  onOpenChange,
  conv,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  conv: ImConversation
  onPick: (agent: ImAgent) => void
}) {
  const { t } = useTranslation()
  const agents = useAgents(open)
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_im.transferTitle')}
      description={t('plugin_im.transferHint')}
      size="sm"
    >
      <div className="max-h-72 overflow-y-auto">
        {(agents.data ?? []).map((agent) => (
          <button
            key={agent.id}
            type="button"
            disabled={agent.id === conv.assigneeId}
            className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-2 text-start text-sm transition-colors hover:bg-accent disabled:opacity-50"
            onClick={() => onPick(agent)}
          >
            <span className="truncate">{agent.nickname || agent.username}</span>
            <span className="shrink-0 text-xs text-muted-foreground">{agent.username}</span>
          </button>
        ))}
        {agents.isLoading ? <div className="py-4 text-center text-sm text-muted-foreground">{t('plugin_im.loading')}</div> : null}
        {!agents.isLoading && (agents.data ?? []).length === 0 ? (
          <div className="py-6 text-center text-sm text-muted-foreground">{t('plugin_im.transferEmpty')}</div>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}

function QuickReplyPicker({ onPick }: { onPick: (content: string) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const replies = useQuickReplies(open)
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button type="button" variant="outline" size="icon" aria-label={t('plugin_im.quickReply.title')} title={t('plugin_im.quickReply.title')}>
            <MessageSquare aria-hidden="true" />
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-1">
          <div className="max-h-64 overflow-y-auto">
            {(replies.data ?? []).map((q) => (
              <button
                key={q.id}
                type="button"
                className="block w-full rounded-sm px-2 py-1.5 text-start text-sm hover:bg-accent"
                onClick={() => {
                  onPick(q.content)
                  setOpen(false)
                }}
              >
                <div className="font-medium text-foreground">{q.title}</div>
                <div className="truncate text-xs text-muted-foreground">{q.content}</div>
              </button>
            ))}
            {!replies.isLoading && (replies.data ?? []).length === 0 ? (
              <div className="px-2 py-3 text-center text-xs text-muted-foreground">{t('plugin_im.quickReply.empty')}</div>
            ) : null}
          </div>
        </PopoverContent>
      </Popover>
      <Can perm="im:edit">
        <QuickReplyManager />
      </Can>
    </>
  )
}

function QuickReplyManager() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const replies = useQuickReplies(open)
  const save = useSaveQuickReply()
  const del = useDeleteQuickReply()
  const [editing, setEditing] = useState<string | undefined>()
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')

  const reset = () => {
    setEditing(undefined)
    setTitle('')
    setContent('')
  }
  const submit = async () => {
    if (!title.trim() || !content.trim()) return
    try {
      await save.mutateAsync({ id: editing, title: title.trim(), content: content.trim() })
      reset()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        onClick={() => setOpen(true)}
        aria-label={t('plugin_im.quickReply.manage')}
        title={t('plugin_im.quickReply.manage')}
      >
        <Pencil aria-hidden="true" />
      </Button>
      <ResponsiveDialog open={open} onOpenChange={setOpen} title={t('plugin_im.quickReply.manage')} size="md">
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t('plugin_im.quickReply.name')}
              aria-label={t('plugin_im.quickReply.name')}
            />
            <Textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={t('plugin_im.quickReply.content')}
              aria-label={t('plugin_im.quickReply.content')}
              rows={3}
            />
            <div className="flex justify-end gap-2">
              {editing ? (
                <Button variant="ghost" size="sm" onClick={reset}>
                  {t('common.cancel')}
                </Button>
              ) : null}
              <Button size="sm" onClick={() => void submit()} disabled={!title.trim() || !content.trim()}>
                {editing ? t('common.save') : t('common.add')}
              </Button>
            </div>
          </div>
          <div className="max-h-64 overflow-y-auto">
            {(replies.data ?? []).map((q: ImQuickReply) => (
              <div key={q.id} className="flex items-start gap-2 border-b px-1 py-2 last:border-b-0">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-foreground">{q.title}</div>
                  <div className="line-clamp-2 text-xs text-muted-foreground">{q.content}</div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('common.edit')}
                  onClick={() => {
                    setEditing(q.id)
                    setTitle(q.title)
                    setContent(q.content)
                  }}
                >
                  <Pencil aria-hidden="true" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('common.delete')}
                  onClick={() => {
                    void del.mutateAsync(q.id).then(() => toast.success(t('common.operateSuccess'))).catch(() => toast.error(t('common.operateFailed')))
                  }}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              </div>
            ))}
            {!replies.isLoading && (replies.data ?? []).length === 0 ? (
              <div className="py-6 text-center text-sm text-muted-foreground">{t('plugin_im.quickReply.empty')}</div>
            ) : null}
          </div>
        </div>
      </ResponsiveDialog>
    </>
  )
}

function SettingsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation()
  const settings = useImSettings(open)
  const save = useSaveImSettings()
  const [form, setForm] = useState<ImSettings | null>(null)

  useEffect(() => {
    if (settings.data) setForm(settings.data)
  }, [settings.data])
  useEffect(() => {
    if (!open) setForm(null)
  }, [open])

  const days = t('plugin_im.settings.daysShort').split(',')
  const toggleDay = (day: number) => {
    setForm((f) =>
      f
        ? {
            ...f,
            workdays: f.workdays.includes(day)
              ? f.workdays.filter((x) => x !== day)
              : [...f.workdays, day].sort((a, b) => a - b),
          }
        : f,
    )
  }
  const submit = async () => {
    if (!form) return
    try {
      await save.mutateAsync(form)
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_im.settings.title')}
      description={t('plugin_im.settings.hint')}
      size="md"
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={!form || save.isPending}>
            {t('common.save')}
          </Button>
        </div>
      }
    >
      {!form ? (
        <div className="py-6 text-center text-sm text-muted-foreground">{t('plugin_im.loading')}</div>
      ) : (
        <div className="flex flex-col gap-4">
          <label className="flex items-center justify-between gap-3 text-sm">
            <span>{t('plugin_im.settings.businessHours')}</span>
            <Switch
              checked={form.businessHours}
              onCheckedChange={(checked) => setForm({ ...form, businessHours: checked })}
              aria-label={t('plugin_im.settings.businessHours')}
            />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-xs text-muted-foreground">
              {t('plugin_im.settings.start')}
              <TimeField value={form.hoursStart} onChange={(v) => setForm({ ...form, hoursStart: v })} />
            </label>
            <label className="flex flex-col gap-1 text-xs text-muted-foreground">
              {t('plugin_im.settings.end')}
              <TimeField value={form.hoursEnd} onChange={(v) => setForm({ ...form, hoursEnd: v })} />
            </label>
          </div>
          <div className="flex flex-col gap-2">
            <span className="text-xs text-muted-foreground">{t('plugin_im.settings.workdays')}</span>
            <div className="flex flex-wrap gap-1">
              {days.map((label, i) => {
                const day = i + 1
                const on = form.workdays.includes(day)
                return (
                  <Button
                    key={day}
                    type="button"
                    size="sm"
                    variant={on ? 'secondary' : 'outline'}
                    aria-pressed={on}
                    onClick={() => toggleDay(day)}
                  >
                    {label}
                  </Button>
                )
              })}
            </div>
          </div>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.timezone')}
            <Input value={form.timezone} onChange={(e) => setForm({ ...form, timezone: e.target.value })} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.offlineText')}
            <Textarea rows={2} value={form.offlineText} onChange={(e) => setForm({ ...form, offlineText: e.target.value })} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.inviteDelay')}
            <Input
              type="number"
              min={0}
              max={600}
              value={form.inviteDelay}
              onChange={(e) => setForm({ ...form, inviteDelay: Number(e.target.value) || 0 })}
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.inviteText')}
            <Textarea rows={2} value={form.inviteText} onChange={(e) => setForm({ ...form, inviteText: e.target.value })} />
          </label>
        </div>
      )}
    </ResponsiveDialog>
  )
}
