import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ArrowRightLeft, Ban, Bell, BellOff, ChevronLeft, ChevronRight, ChevronUp, Download, Ellipsis, FileText, Lock, Merge, MessageSquare, MessagesSquare, Paperclip, Pencil, Send, Settings2, SlidersHorizontal, Star, StickyNote, Tag, Trash2, Undo2, UserMinus, UserPlus, UserRound, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAuth } from '@/framework/auth/useAuth'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { QueueAvatar, QueueRow, QueueStatus } from '@/framework/components/QueueRow'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { ChatBubble, SystemPill } from '@/framework/components/chat/MessageBubble'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Skeleton } from '@/framework/ui/skeleton'
import { TimeField } from '@/framework/components/pickers/TimeField'
import { Lightbox, type LightboxSlide } from '@/framework/upload/Lightbox'
import { useWs } from '@/framework/hooks/useWs'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Avatar, AvatarFallback } from '@/framework/ui/avatar'
import { Checkbox } from '@/framework/ui/checkbox'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Input } from '@/framework/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { Switch } from '@/framework/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import {
  exportConversations,
  heartbeatPresence,
  imAttachmentUrl,
  messageAttachment,
  openImStream,
  sendTyping,
  uploadAttachment,
  useAgents,
  useChatSession,
  useImSettings,
  useImStats,
  useMergeConv,
  useSaveImSettings,
  useClaimConv,
  useCloseConv,
  useAttachmentLimits,
  useConversations,
  useDeleteConv,
  useDeleteConvsBatch,
  useDeleteQuickReply,
  useQuickReplies,
  useRecallMessage,
  useSaveQuickReply,
  useSendReply,
  useSetNote,
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
import { formatSize } from '@/framework/upload/precheck'
import { daySeparator, formatClock, formatTime, initials, relativeStamp } from './format'

const ALL = '__all'
/** 会话队列每页条数：与工单列表同一档 */
const CONV_PAGE_SIZE = 50
const NOTIFY_KEY = 'nex_im_notify'

type StreamEvent = { type?: string; senderType?: SenderType; seq?: number }

function asStreamEvent(v: unknown): StreamEvent | null {
  return typeof v === 'object' && v !== null ? (v as StreamEvent) : null
}

function isMessage(v: unknown): v is ImMessage {
  return typeof v === 'object' && v !== null && typeof (v as ImMessage).seq === 'number'
}

/** 本地日历日：跨天判定用，别用 UTC 的 toISOString（东八区凌晨会被算到前一天）。 */
function dayOf(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
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
  const { isSuper } = usePermission()
  const [params, setParams] = useSearchParams()
  // 站内信深链：/im/conversations?conv=IMxxx 直接打开对应会话。
  const linkedConv = params.get('conv') || undefined
  // 列表与统计分屏：与工单中心同款——处理会话的人先看到队列，做复盘的人不被双栏挤走
  const view = params.get('view') === 'stats' ? 'stats' : 'workbench'
  const setView = (v: string) => {
    const next = new URLSearchParams(params)
    if (v === 'stats') next.set('view', 'stats')
    else next.delete('view')
    setParams(next, { replace: true })
  }
  const [status, setStatus] = useState<string | undefined>()
  const [assignee, setAssignee] = useState<'all' | 'me' | 'unassigned'>('all')
  const [keyword, setKeyword] = useState('')
  const [filtersOpen, setFiltersOpen] = useState(false)
  // 生效中的筛选 chips + 一键重置：与工单中心同一交互（点 chip 撤销对应维度）。
  // 关键词不算 chip——它本身就在搜索框里看得见，再列一枚等于同一件事说两遍。
  const activeFilters: { key: string; label: string; value: string; clear: () => void }[] = [
    ...(status ? [{ key: 'status', label: t('plugin_im.fields.status'), value: t(`plugin_im.status.${status}`), clear: () => setStatus(undefined) }] : []),
    ...(assignee !== 'all'
      ? [{ key: 'assignee', label: t('plugin_im.fields.assignee'), value: t(`plugin_im.assignee.${assignee}`), clear: () => setAssignee('all') }]
      : []),
  ]
  const resetFilters = () => {
    setKeyword('')
    setStatus(undefined)
    setAssignee('all')
  }
  const [selected, setSelected] = useState<string | undefined>(linkedConv)
  const [live, setLive] = useState(false)
  const [typing, setTyping] = useState(false)
  const [page, setPage] = useState(1)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const [notifyOn, setNotifyOn] = useState(() => localStorage.getItem(NOTIFY_KEY) === '1')

  const convs = useConversations({
    status,
    keyword: keyword || undefined,
    assignee: assignee === 'all' ? undefined : assignee,
    page,
    pageSize: CONV_PAGE_SIZE,
  })
  // 筛选变了回第一页：旧页码在新筛选下经常越界，空列表会被读成「没有会话」
  useEffect(() => setPage(1), [status, assignee, keyword])
  const chat = useChatSession(selected, live ? 15000 : 2000)
  const send = useSendReply()
  const close = useCloseConv()
  const claim = useClaimConv()
  const setTags = useSetTags()
  const setNote = useSetNote()
  const deleteConv = useDeleteConv()
  const deleteConvs = useDeleteConvsBatch()
  const recall = useRecallMessage()
  const merge = useMergeConv()
  const transfer = useTransferConv()
  const [uploading, setUploading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const refresh = chat.refresh

  useEffect(() => {
    if (linkedConv) setSelected(linkedConv)
  }, [linkedConv])

  // 坐席在线心跳：60 秒一次；开启自动分配时后端会顺手把最早的排队会话分给我。
  // refetch 放进 ref：心跳定时器不需要随列表对象每次渲染重建。
  const refetchRef = useRef(convs.refetch)
  refetchRef.current = convs.refetch
  useEffect(() => {
    let stopped = false
    const beat = () => {
      heartbeatPresence()
        .then((r) => {
          if (!stopped && r?.assigned) void refetchRef.current()
        })
        .catch(() => undefined)
    }
    beat()
    const timer = window.setInterval(beat, 60_000)
    return () => {
      stopped = true
      window.clearInterval(timer)
    }
  }, [])

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

  const onSaveNote = async (c: ImConversation, note: string) => {
    try {
      await setNote.mutateAsync({ convNo: c.convNo, note })
      await refresh()
      toast.success(t('common.operateSuccess'))
      return true
    } catch {
      toast.error(t('common.operateFailed'))
      return false
    }
  }

  // 删除是超管专属、且只对已结束的会话开放：消息与附件会一并删除，不可恢复。
  const onDeleteConv = async (c: ImConversation) => {
    if (!(await confirm({ destructive: true, title: t('plugin_im.delete.title'), description: t('plugin_im.delete.confirm', { no: c.convNo }) }))) return
    try {
      await deleteConv.mutateAsync(c.convNo)
      if (selected === c.convNo) setSelected(undefined)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onExport = async () => {
    setExporting(true)
    try {
      await exportConversations({
        status,
        keyword: keyword || undefined,
        assignee: assignee === 'all' ? undefined : assignee,
        page: 1,
        pageSize: 0,
      })
    } catch {
      toast.error(t('plugin_im.exportFailed'))
    } finally {
      setExporting(false)
    }
  }

  const onRecall = async (c: ImConversation, m: ImMessage) => {
    try {
      await recall.mutateAsync({ convNo: c.convNo, seq: m.seq })
      await refresh()
    } catch {
      toast.error(t('plugin_im.recallFailed'))
    }
  }

  const onMerge = async (c: ImConversation, target: string) => {
    try {
      const res = await merge.mutateAsync({ convNo: c.convNo, target })
      await convs.refetch()
      setSelected(res.target)
      toast.success(t('common.operateSuccess'))
      return true
    } catch {
      toast.error(t('common.operateFailed'))
      return false
    }
  }

  // useMemo：勾选清理那个 effect 依赖 list，裸 `?? []` 会让它每次渲染都重跑。
  const list = useMemo(() => convs.data?.list ?? [], [convs.data])
  const total = convs.data?.total ?? 0
  // 与工单队列同一套页码分页（50/页）：会话队列也是要「知道自己一共管多少」的工作台，
  // 追加式加载更多让总数与位置永远不可见。
  const pages = Math.max(1, Math.ceil(total / CONV_PAGE_SIZE))

  // 批量删除沿用单条删除的口径：超管、且只有已结束的会话可删（进行中的后端会跳过，勾上也是白勾）。
  const canPick = isSuper
  const deletable = list.filter((c) => c.status === 'closed').map((c) => c.convNo)
  const allPicked = deletable.length > 0 && deletable.every((no) => picked.includes(no))

  // 会话被删掉或翻页移出视野时同步收掉勾选项，别让看不见的会话留在待删清单里。
  useEffect(() => {
    setPicked((p) => {
      const kept = p.filter((no) => list.some((c) => c.convNo === no))
      return kept.length === p.length ? p : kept
    })
  }, [list])

  const togglePicked = (convNo: string, on: boolean) =>
    setPicked((p) => (on ? [...new Set([...p, convNo])] : p.filter((x) => x !== convNo)))

  const onDeletePicked = async () => {
    const nos = [...picked]
    if (nos.length === 0) return
    if (
      !(await confirm({
        destructive: true,
        title: t('plugin_im.batch.deleteTitle'),
        description: t('plugin_im.batch.deleteConfirm', { count: nos.length }),
      }))
    )
      return
    try {
      const res = await deleteConvs.mutateAsync(nos)
      if (selected && nos.includes(selected)) setSelected(undefined)
      setPicked([])
      toast.success(t('plugin_im.batch.deleted', { count: res.deleted }))
      if (res.skipped.length > 0) toast.warning(t('plugin_im.batch.skipped', { count: res.skipped.length }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_im.title')}
        description={t('plugin_im.description')}
        actions={
          <>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download aria-hidden="true" />
              {t('plugin_im.export')}
            </Button>
            {/* 打开弹窗读的是 GET /settings（im:list），按 im:edit 门控会让只读角色点开就转圈；
                能不能保存由弹窗内部按 im:edit 决定 */}
            <Can perm="im:list">
              <Button variant="outline" onClick={() => setSettingsOpen(true)}>
                <Settings2 aria-hidden="true" />
                {t('plugin_im.settings.action')}
              </Button>
            </Can>
          </>
        }
      />
      {/* 列表与统计分屏：处理会话的人先看到队列，做复盘的人不被双栏挤走（分屏结构对齐工单中心） */}
      <Tabs value={view} onValueChange={setView}>
        <TabsList className="w-fit">
          <TabsTrigger value="workbench">{t('plugin_im.view.workbench')}</TabsTrigger>
          <TabsTrigger value="stats">{t('plugin_im.view.stats')}</TabsTrigger>
        </TabsList>
        <TabsContent value="workbench">
      {/* 分栏几何与工单中心同款（24rem 列 + 桌面定高 + 移动端左栏限高）：两套坐席工作台并排打开不会互相打架。
          17rem = 顶部栏+页签条+页头+「会话/统计」页签行+间距；两处常量必须同步改 */}
      <div className="grid h-auto min-h-96 grid-cols-1 gap-4 md:h-[calc(100vh-17rem)] md:min-h-144 md:grid-cols-[24rem_1fr]">
        <Card className="flex max-h-128 min-h-0 flex-col gap-2 p-2 md:max-h-none">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('plugin_im.searchPlaceholder')}
              aria-label={t('common.keyword')}
              className="min-w-32 flex-1"
            />
            {/* 筛选收进弹层、选中的显示成 chips：与工单中心同一套（24rem 窄栏铺不下几个常驻控件） */}
            <Popover open={filtersOpen} onOpenChange={setFiltersOpen}>
              <PopoverTrigger asChild>
                <Button variant="outline" className="h-10 shrink-0 md:h-9">
                  <SlidersHorizontal aria-hidden="true" />
                  {t('plugin_im.filter.title')}
                  {activeFilters.length > 0 ? (
                    <>
                      <Badge variant="secondary" aria-hidden="true" className="ms-1 min-w-5 justify-center">
                        {activeFilters.length}
                      </Badge>
                      {/* 屏下只念「筛选 1」听不出 1 是什么，补一句可读的计数 */}
                      <span className="sr-only">{t('plugin_im.filter.activeCount', { count: activeFilters.length })}</span>
                    </>
                  ) : null}
                </Button>
              </PopoverTrigger>
              <PopoverContent align="end" className="w-72 space-y-2">
                <SimpleSelect
                  aria-label={t('plugin_im.fields.status')}
                  label={t('plugin_im.fields.status')}
                  className="w-full"
                  value={status ?? ALL}
                  onChange={(v) => setStatus(v === ALL ? undefined : v)}
                  options={[
                    { value: ALL, label: t('plugin_im.all') },
                    { value: 'open', label: t('plugin_im.status.open') },
                    { value: 'closed', label: t('plugin_im.status.closed') },
                  ]}
                />
                <SimpleSelect
                  aria-label={t('plugin_im.fields.assignee')}
                  label={t('plugin_im.fields.assignee')}
                  className="w-full"
                  value={assignee}
                  onChange={(v) => setAssignee(v as 'all' | 'me' | 'unassigned')}
                  options={[
                    { value: 'all', label: t('plugin_im.assignee.all') },
                    { value: 'me', label: t('plugin_im.assignee.me') },
                    { value: 'unassigned', label: t('plugin_im.assignee.unassigned') },
                  ]}
                />
                <div className="flex items-center justify-end border-t pt-2">
                  <Button variant="ghost" size="sm" onClick={resetFilters} disabled={activeFilters.length === 0}>
                    {t('common.reset')}
                  </Button>
                </div>
              </PopoverContent>
            </Popover>
            <Button
              type="button"
              variant={notifyOn ? 'secondary' : 'outline'}
              size="icon"
              className="shrink-0"
              onClick={() => void toggleNotify()}
              aria-pressed={notifyOn}
              aria-label={notifyOn ? t('plugin_im.notify.on') : t('plugin_im.notify.off')}
              title={notifyOn ? t('plugin_im.notify.on') : t('plugin_im.notify.off')}
            >
              {notifyOn ? <Bell aria-hidden="true" /> : <BellOff aria-hidden="true" />}
            </Button>
            {/* WS hub 已连接 = 新消息帧直达；断线退回增量轮询。与工单中心同一枚指示。 */}
            <Badge
              variant={wsStatus === 'open' ? 'success' : 'secondary'}
              className="shrink-0"
              title={wsStatus === 'open' ? t('common.liveRealtime') : t('common.livePolling')}
            >
              {wsStatus === 'open' ? t('common.liveRealtime') : t('common.livePolling')}
            </Badge>
          </div>
          {activeFilters.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              {activeFilters.map((f) => (
                <button
                  key={f.key}
                  type="button"
                  onClick={f.clear}
                  aria-label={`${t('plugin_im.filter.remove')} ${f.label}${f.value ? ` ${f.value}` : ''}`}
                  className="flex h-7 max-w-full items-center gap-1 rounded-full border border-input bg-muted/60 px-2.5 text-xs hover:bg-muted"
                >
                  <span className="shrink-0 text-muted-foreground">{f.label}</span>
                  <span className="truncate">{f.value}</span>
                  <X aria-hidden="true" className="size-3 shrink-0" />
                </button>
              ))}
            </div>
          ) : null}
          {canPick ? (
            <div className="flex items-center gap-2 border-b px-1 pb-1.5">
              <Checkbox
                id="im-pick-all"
                checked={allPicked ? true : picked.length > 0 ? 'indeterminate' : false}
                disabled={deletable.length === 0}
                onCheckedChange={(v) => setPicked(v === true ? deletable : [])}
              />
              <label htmlFor="im-pick-all" className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                {picked.length > 0 ? t('common.selected', { count: picked.length }) : t('plugin_im.batch.selectAllClosed')}
              </label>
              {picked.length > 0 ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="shrink-0 text-destructive"
                  disabled={deleteConvs.isPending}
                  onClick={() => void onDeletePicked()}
                >
                  <Trash2 aria-hidden="true" />
                  {t('plugin_im.batch.delete')}
                </Button>
              ) : null}
            </div>
          ) : null}
          <div className="min-h-0 flex-1 overflow-y-auto">
            {list.map((c) => (
              <ConvListItem
                key={c.id}
                conv={c}
                active={c.convNo === selected}
                canPick={canPick}
                picked={picked.includes(c.convNo)}
                onTogglePick={(on) => togglePicked(c.convNo, on)}
                onSelect={() => setSelected(c.convNo)}
              />
            ))}
            {!convs.isLoading && list.length === 0 ? <EmptyState title={t('plugin_im.empty')} className="py-8" /> : null}
          </div>
          <div className="flex items-center justify-between gap-2 border-t pt-2 text-xs text-muted-foreground">
            <span>{t('dataTable.totalCount', { count: total })}</span>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('dataTable.prevPage')}
                disabled={page <= 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft aria-hidden="true" />
              </Button>
              <span className="whitespace-nowrap">{t('dataTable.pageInfo', { page, pages })}</span>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('dataTable.nextPage')}
                disabled={page >= pages}
                onClick={() => setPage((p) => Math.min(pages, p + 1))}
              >
                <ChevronRight aria-hidden="true" />
              </Button>
            </div>
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
          onSaveNote={onSaveNote}
          onDelete={(c) => void onDeleteConv(c)}
          onRecall={onRecall}
          onMerge={onMerge}
          canDelete={isSuper}
        />
      </div>
        </TabsContent>
        <TabsContent value="stats">
          <StatsPanel />
        </TabsContent>
      </Tabs>

      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
    </PageContainer>
  )
}

function ConvListItem({
  conv,
  active,
  canPick,
  picked,
  onTogglePick,
  onSelect,
}: {
  conv: ImConversation
  active: boolean
  /** 只有超管看得到勾选框 */
  canPick: boolean
  picked: boolean
  onTogglePick: (on: boolean) => void
  onSelect: () => void
}) {
  const { t, i18n } = useTranslation()
  const tags = (conv.tags || '').split(',').filter(Boolean)
  const visitor = conv.visitorName || conv.visitorToken
  const stamp = relativeStamp(conv.lastMsgAt, i18n.language)
  // 一行最多三枚元信息：归属、业务单号优先，标签按余位截断（标签在会话头部仍全量可见）
  const tagSlots = Math.max(0, 3 - (conv.assigneeName ? 1 : 0) - (conv.bizNo ? 1 : 0))
  const meta = [
    conv.assigneeName ? { key: 'assignee', node: <><UserRound aria-hidden="true" className="size-3" />{conv.assigneeName}</> } : null,
    conv.bizNo
      ? {
          key: 'biz',
          node: (
            <span className="font-mono" title={conv.bizType ? conv.bizType.replace(/_/g, ' ') : undefined} aria-label={t('plugin_im.bizRef', { no: conv.bizNo })}>
              {conv.bizNo}
            </span>
          ),
        }
      : null,
    ...tags.slice(0, tagSlots).map((tag) => ({ key: `tag-${tag}`, node: <>{tag}</> })),
  ].filter(Boolean) as { key: string; node: React.ReactNode }[]

  const row = (
    <>
      <span className="flex w-full items-center gap-2">
        <QueueAvatar>{initials(visitor)}</QueueAvatar>
        <span className={cn('min-w-0 flex-1 truncate text-sm text-foreground', conv.unread > 0 ? 'font-semibold' : 'font-medium')}>
          {visitor}
        </span>
        {conv.unread > 0 ? (
          <span className="shrink-0 rounded-full bg-destructive px-1.5 text-[10px] font-medium leading-4 text-destructive-foreground tabular-nums">
            {conv.unread}
          </span>
        ) : null}
        {stamp ? (
          <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
            {stamp.kind === 'yesterday' ? t('plugin_im.yesterday') : stamp.text}
          </span>
        ) : null}
      </span>
      <span className="mt-1 flex w-full items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{conv.lastPreview || t('plugin_im.noMessages')}</span>
        <QueueStatus tone={conv.status === 'open' ? 'success' : 'neutral'}>{t(`plugin_im.status.${conv.status}`)}</QueueStatus>
      </span>
      {meta.length > 0 ? (
        <span className="mt-1 flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground">
          {meta.map((m) => (
            <span key={m.key} className="inline-flex min-w-0 items-center gap-1">
              {m.node}
            </span>
          ))}
        </span>
      ) : null}
    </>
  )
  const checkbox = canPick ? (
    <Checkbox
      checked={picked}
      disabled={conv.status !== 'closed'}
      title={conv.status === 'closed' ? undefined : t('plugin_im.batch.onlyClosed')}
      aria-label={visitor}
      onCheckedChange={(v) => onTogglePick(v === true)}
    />
  ) : null
  return <QueueRow selected={active} onSelect={onSelect} checkbox={checkbox}>{row}</QueueRow>
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

/** 消息流的日期分隔：细线夹日期，与系统事件胶囊区分开。 */
function DayDivider({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 py-2 text-[11px] text-muted-foreground">
      <span className="h-px flex-1 bg-border" aria-hidden="true" />
      <span>{label}</span>
      <span className="h-px flex-1 bg-border" aria-hidden="true" />
    </div>
  )
}

function RatingStars({ value, className }: { value: number; className?: string }) {  return (
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
function ImageAttachment({ att, url, onZoom }: { att: ImAttachment; url: string; onZoom: () => void }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <FileAttachment att={att} url={url} />
  return (
    <button
      type="button"
      onClick={onZoom}
      aria-label={att.name}
      title={att.name}
      className="block max-w-full cursor-zoom-in rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
    >
      <img
        src={url}
        alt={att.name}
        loading="lazy"
        onError={() => setFailed(true)}
        className="max-h-60 max-w-full min-w-20 rounded-md object-contain"
      />
    </button>
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
  onSaveNote,
  onDelete,
  onRecall,
  onMerge,
  canDelete,
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
  onSaveNote: (conv: ImConversation, note: string) => Promise<boolean>
  onDelete: (conv: ImConversation) => void
  onRecall: (conv: ImConversation, msg: ImMessage) => void
  onMerge: (conv: ImConversation, target: string) => Promise<boolean>
  /** 只有超管能删除会话（已结束的） */
  canDelete: boolean
}) {
  const { t, i18n } = useTranslation()
  const isMobile = useIsMobile()
  const userId = useAuth((s) => s.user?.id)
  const myName = useAuth((s) => s.user?.nickname || s.user?.username)
  const { has } = usePermission()
  // 「更多」只在真有可收项时出现，别给没权限的坐席摆一个空菜单
  const showMoreMenu = has('im:edit') || (canDelete && conv?.status === 'closed')
  const [draft, setDraft] = useState('')
  const [tagDraft, setTagDraft] = useState<string | null>(null)
  const [noteDraft, setNoteDraft] = useState<string | null>(null)
  const [mergeOpen, setMergeOpen] = useState(false)
  const [transferOpen, setTransferOpen] = useState(false)
  const [zoomImageId, setZoomImageId] = useState<string | null>(null)
  // 会话里已加载的图片消息：点开任意一张，左右就能翻到其余的
  const imageSlides = useMemo<(LightboxSlide & { id: string })[]>(() => {
    const out: (LightboxSlide & { id: string })[] = []
    for (const m of messages) {
      if (m.kind === 'system') continue
      const att = messageAttachment(m)
      if (att?.mime.startsWith('image/')) out.push({ id: m.id, src: imAttachmentUrl(att.token), alt: att.name })
    }
    return out
  }, [messages])
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const fileRef = useRef<HTMLInputElement | null>(null)
  // 可传的类型与大小由后端下发：服务端白名单改了，这里的文件选择器跟着变
  const { data: attLimits } = useAttachmentLimits()
  const lastTyping = useRef(0)

  useEffect(() => {
    setTagDraft(null)
    setNoteDraft(null)
    setDraft('')
    setTransferOpen(false)
    setMergeOpen(false)
  }, [conv?.convNo])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length, conv?.convNo])

  if (!conv) {
    // 移动端会话列表在上方，空态文案不提「左侧」
    return (
      <Card className="flex items-center justify-center">
        <EmptyState
          icon={<MessagesSquare aria-hidden="true" className="size-6" />}
          title={t('plugin_im.pickConversationTitle')}
          description={t(isMobile ? 'plugin_im.pickConversationMobile' : 'plugin_im.pickConversation')}
          className="py-16"
        />
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
        <Avatar className="size-9 shrink-0">
          <AvatarFallback className="text-xs">{initials(conv.visitorName || conv.visitorToken)}</AvatarFallback>
        </Avatar>
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
        <div className="flex flex-wrap items-center gap-x-1 gap-y-2">
          <Can perm="im:reply">
            {conv.assigneeId ? (
              <Button variant="outline" size="sm" onClick={() => onClaim(conv, true)}>
                <UserMinus aria-hidden="true" />
                {t('plugin_im.release')}
              </Button>
            ) : (
              <Button variant="outline" size="sm" onClick={() => onClaim(conv, false)}>
                <UserPlus aria-hidden="true" />
                {t('plugin_im.claim')}
              </Button>
            )}
          </Can>
          {/* 管理类动作收进「更多」：铺开成六七个纯图标按钮既没人看得懂，也把会话信息挤没了 */}
          {showMoreMenu ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Ellipsis aria-hidden="true" />
                  {t('common.more')}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="min-w-44">
                <Can perm="im:edit">
                  <DropdownMenuItem onClick={() => setTransferOpen(true)}>
                    <ArrowRightLeft aria-hidden="true" />
                    {t('plugin_im.transfer')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setTagDraft(conv.tags || '')}>
                    <Tag aria-hidden="true" />
                    {t('plugin_im.tags.action')}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setNoteDraft(conv.note || '')}>
                    <StickyNote aria-hidden="true" />
                    {t('plugin_im.note.action')}
                  </DropdownMenuItem>
                  {conv.status === 'open' ? (
                    <DropdownMenuItem onClick={() => setMergeOpen(true)}>
                      <Merge aria-hidden="true" />
                      {t('plugin_im.merge.action')}
                    </DropdownMenuItem>
                  ) : null}
                </Can>
                {canDelete && conv.status === 'closed' ? (
                  <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => onDelete(conv)}>
                    <Trash2 aria-hidden="true" />
                    {t('plugin_im.delete.action')}
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
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

      {conv.note && noteDraft === null ? (
        <div className="border-b px-4 py-1.5 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{t('plugin_im.note.label')}</span>：{conv.note}
        </div>
      ) : null}

      {noteDraft !== null ? (
        <div className="flex items-center gap-2 border-b px-4 py-2">
          <Input
            value={noteDraft}
            maxLength={500}
            onChange={(e) => setNoteDraft(e.target.value)}
            placeholder={t('plugin_im.note.placeholder')}
            aria-label={t('plugin_im.note.placeholder')}
            autoFocus
          />
          <Button
            size="sm"
            onClick={() => {
              void onSaveNote(conv, noteDraft).then((ok) => ok && setNoteDraft(null))
            }}
          >
            {t('common.save')}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setNoteDraft(null)}>
            {t('common.cancel')}
          </Button>
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
        {messages.map((m, i) => {
          const prev = i > 0 ? messages[i - 1] : undefined
          const newDay = !prev || dayOf(prev.createdAt) !== dayOf(m.createdAt)
          const day = newDay ? daySeparator(m.createdAt, i18n.language) : null
          const separator = day ? (
            <DayDivider
              label={day.kind === 'today' ? t('plugin_im.today') : day.kind === 'yesterday' ? t('plugin_im.yesterday') : day.text}
            />
          ) : null
          if (m.kind === 'system') {
            return (
              <Fragment key={m.id}>
                {separator}
                <SystemPill>
                  {systemLabel(m, (key) => t(key))} · {formatTime(m.createdAt, i18n.language)}
                </SystemPill>
              </Fragment>
            )
          }
          const mine = m.senderType === 'agent'
          const att = messageAttachment(m)
          const attUrl = att ? imAttachmentUrl(att.token) : ''
          const recallable =
            mine && !m.recalledAt && m.senderId && m.senderId === userId &&
            Date.now() - new Date(m.createdAt).getTime() < 120_000
          // 同一人连发只在一组的第一条露头像与姓名，否则气泡上方全是重复信息
          const groupStart =
            newDay || !prev || prev.kind === 'system' || prev.senderType !== m.senderType || prev.senderId !== m.senderId
          const who = mine ? myName || t('plugin_im.senders.agent') : conv.visitorName || conv.visitorToken
          return (
            <Fragment key={m.id}>
              {separator}
              <div className={cn('flex items-end gap-2', mine ? 'flex-row-reverse' : 'flex-row')}>
                {groupStart ? (
                  <Avatar className="mb-0.5 size-7 shrink-0" aria-hidden="true">
                    <AvatarFallback className="text-[10px]">{initials(who)}</AvatarFallback>
                  </Avatar>
                ) : (
                  <span className="size-7 shrink-0" aria-hidden="true" />
                )}
                <div className="min-w-0 flex-1">
                  <ChatBubble
                    side={mine ? 'end' : 'start'}
                    tone={mine ? 'primary' : 'default'}
                    meta={
                      <>
                        {groupStart ? <span>{m.senderName || t(`plugin_im.senders.${m.senderType}`)}</span> : null}
                        <span title={formatTime(m.createdAt, i18n.language)}>{formatClock(m.createdAt, i18n.language)}</span>
                        {mine ? (
                          <span>{conv.visitorReadSeq >= m.seq ? t('plugin_im.read') : t('plugin_im.unread')}</span>
                        ) : null}
                      </>
                    }
                    footer={
                      recallable ? (
                        <button
                          type="button"
                          onClick={() => onRecall(conv, m)}
                          className="mt-0.5 flex items-center gap-0.5 text-[11px] opacity-70 hover:opacity-100"
                        >
                          <Undo2 aria-hidden="true" className="size-3" />
                          {t('plugin_im.recall.action')}
                        </button>
                      ) : undefined
                    }
                  >
                    {att ? (
                      att.mime.startsWith('image/') ? (
                        <ImageAttachment att={att} url={attUrl} onZoom={() => setZoomImageId(m.id)} />
                      ) : (
                        <FileAttachment att={att} url={attUrl} />
                      )
                    ) : null}
                    {m.content ? <div className="whitespace-pre-wrap wrap-break-word">{m.content}</div> : null}
                  </ChatBubble>
                </div>
              </div>
            </Fragment>
          )
        })}
        <div ref={bottomRef} />
      </div>

      {zoomImageId !== null && imageSlides.some((s) => s.id === zoomImageId) ? (
        <Lightbox
          open
          onOpenChange={(open) => {
            if (!open) setZoomImageId(null)
          }}
          slides={imageSlides}
          index={imageSlides.findIndex((s) => s.id === zoomImageId)}
        />
      ) : null}

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
              accept={attLimits?.accept}
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
              {t('plugin_im.send')}
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
      <MergeDialog open={mergeOpen} onOpenChange={setMergeOpen} conv={conv} onConfirm={onMerge} />
    </Card>
  )
}

/** 合并会话：填写目标会话号，把当前会话的消息并进去，当前会话随之结束。 */
function MergeDialog({
  open,
  onOpenChange,
  conv,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  conv: ImConversation
  onConfirm: (conv: ImConversation, target: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [target, setTarget] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (open) setTarget('')
  }, [open])
  const submit = async () => {
    const to = target.trim()
    if (!to) return
    setBusy(true)
    const ok = await onConfirm(conv, to)
    setBusy(false)
    if (ok) onOpenChange(false)
  }
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_im.merge.title')}
      description={t('plugin_im.merge.hint', { no: conv.convNo })}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={busy || !target.trim()}>
            {t('common.confirm')}
          </Button>
        </>
      }
    >
      <label className="flex flex-col gap-1 text-sm">
        <span>
          {t('plugin_im.merge.target')}
          <RequiredMark />
        </span>
        <Input
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          placeholder="IM..."
          aria-label={t('plugin_im.merge.target')}
          className="font-mono"
          autoFocus
        />
      </label>
      <p className="mt-2 text-xs text-muted-foreground">{t('plugin_im.merge.warning')}</p>
    </ResponsiveDialog>
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
            <span className="flex min-w-0 items-center gap-2">
              <span
                className={cn('size-2 shrink-0 rounded-full', agent.online ? 'bg-success' : 'bg-muted-foreground/40')}
                aria-hidden="true"
              />
              <span className="truncate">{agent.nickname || agent.username}</span>
              <span className="sr-only">{agent.online ? t('plugin_im.online') : t('plugin_im.offline')}</span>
            </span>
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
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted-foreground">
                {t('plugin_im.quickReply.name')}
                <RequiredMark />
              </span>
              <Input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t('plugin_im.quickReply.name')}
                aria-label={t('plugin_im.quickReply.name')}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted-foreground">
                {t('plugin_im.quickReply.content')}
                <RequiredMark />
              </span>
              <Textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                placeholder={t('plugin_im.quickReply.content')}
                aria-label={t('plugin_im.quickReply.content')}
                rows={3}
              />
            </label>
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
  const { has } = usePermission()
  const settings = useImSettings(open)
  const save = useSaveImSettings()
  // 读设置只要 im:list，写要 im:edit：只读角色也该看得到当前配置，但保存按钮不给
  const canEdit = has('im:edit')
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
        <div className="flex items-center justify-end gap-2">
          {canEdit ? null : (
            <span className="me-auto text-xs text-muted-foreground">{t('plugin_im.settings.readonlyHint')}</span>
          )}
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          {canEdit ? (
            <Button onClick={() => void submit()} disabled={!form || save.isPending}>
              {t('common.save')}
            </Button>
          ) : null}
        </div>
      }
    >
      {!form ? (
        settings.isError ? (
          <div className="flex flex-col items-center gap-3 py-6 text-center">
            <p className="text-sm text-destructive">{t('common.loadFailed')}</p>
            <Button variant="outline" size="sm" onClick={() => void settings.refetch()} disabled={settings.isFetching}>
              {t('common.retry')}
            </Button>
          </div>
        ) : (
          <div className="py-6 text-center text-sm text-muted-foreground">{t('plugin_im.loading')}</div>
        )
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
          <label className="flex items-center justify-between gap-3 text-sm">
            <span>{t('plugin_im.settings.autoAssign')}</span>
            <Switch
              checked={form.autoAssign}
              onCheckedChange={(checked) => setForm({ ...form, autoAssign: checked })}
              aria-label={t('plugin_im.settings.autoAssign')}
            />
          </label>
          <p className="-mt-2 text-xs text-muted-foreground">{t('plugin_im.settings.autoAssignHint')}</p>
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
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.welcomeText')}
            <Textarea
              rows={2}
              maxLength={500}
              value={form.welcomeText}
              onChange={(e) => setForm({ ...form, welcomeText: e.target.value })}
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.bannedWords')}
            <Textarea
              rows={3}
              value={(form.bannedWords ?? []).join('\n')}
              onChange={(e) =>
                setForm({
                  ...form,
                  bannedWords: e.target.value
                    .split('\n')
                    .map((w) => w.trim())
                    .filter(Boolean),
                })
              }
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('plugin_im.settings.retentionDays')}
            <Input
              type="number"
              min={0}
              max={3650}
              value={form.retentionDays}
              onChange={(e) => setForm({ ...form, retentionDays: Number(e.target.value) || 0 })}
            />
            <span>{t('plugin_im.settings.retentionHint')}</span>
          </label>
        </div>
      )}
    </ResponsiveDialog>
  )
}

/** 秒数转「1m 20s」；0 显示占位符。 */
function fmtDuration(sec: number): string {
  if (!sec) return '—'
  if (sec < 60) return `${sec}s`
  const m = Math.floor(sec / 60)
  if (m < 60) return `${m}m ${sec % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

/** 统计面板：窗口内总量、首响、满意度、按天趋势与坐席工作量（独立 Tab，不再是弹窗）。 */
function StatsPanel() {
  const { t } = useTranslation()
  const [days, setDays] = useState(7)
  const stats = useImStats(days, true)
  const s = stats.data
  return (
    <div className="flex flex-col gap-4">
      {!s ? (
        // 与工单统计面板同一加载形态：骨架占位，不再是一行「加载中」文本
        <div className="flex flex-col gap-4 py-4">
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-24 w-full" />
            ))}
          </div>
          <Skeleton className="h-64 w-full" />
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex gap-2">
            {[7, 30, 90].map((d) => (
              <Button
                key={d}
                type="button"
                size="sm"
                variant={days === d ? 'secondary' : 'outline'}
                aria-pressed={days === d}
                onClick={() => setDays(d)}
              >
                {t('plugin_im.stats.days', { count: d })}
              </Button>
            ))}
          </div>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatCard title={t('plugin_im.stats.total')} value={s.total} hint={t('plugin_im.stats.openHint', { count: s.open })} />
            <StatCard title={t('plugin_im.stats.closed')} value={s.closed} />
            <StatCard
              title={t('plugin_im.stats.firstResponse')}
              value={fmtDuration(s.avgFirstResponseSec)}
              hint={t('plugin_im.stats.samples', { count: s.firstResponseCount })}
            />
            <StatCard
              title={t('plugin_im.stats.rating')}
              value={s.ratingCount ? s.avgRating.toFixed(1) : '—'}
              hint={t('plugin_im.stats.samples', { count: s.ratingCount })}
            />
          </div>
          <Card className="p-3">
            <TrendChart
              data={s.daily}
              xKey="date"
              type="bar"
              switchable={false}
              height={220}
              series={[
                { key: 'created', label: t('plugin_im.stats.createdSeries') },
                { key: 'closed', label: t('plugin_im.stats.closedSeries') },
              ]}
            />
          </Card>
          <section className="flex flex-col gap-2">
            <h3 className="text-sm font-semibold text-foreground">{t('plugin_im.stats.agents')}</h3>
            <div className="divide-y divide-border rounded-md border border-border">
              {s.agents.map((a) => (
                <div key={a.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
                  <span
                    className={cn('size-2 shrink-0 rounded-full', a.online ? 'bg-success' : 'bg-muted-foreground/40')}
                    aria-hidden="true"
                  />
                  <span className="min-w-0 flex-1 truncate">{a.name || a.id}</span>
                  <span className="text-xs text-muted-foreground">{t('plugin_im.stats.assignedShort', { count: a.assigned })}</span>
                  <span className="text-xs text-muted-foreground">{t('plugin_im.stats.closedShort', { count: a.closed })}</span>
                  <span className="text-xs text-muted-foreground">
                    ★ {a.avgRating ? a.avgRating.toFixed(1) : '—'}
                  </span>
                </div>
              ))}
              {s.agents.length === 0 ? (
                <div className="px-3 py-4 text-center text-xs text-muted-foreground">{t('plugin_im.stats.noAgents')}</div>
              ) : null}
            </div>
          </section>
          {s.truncated ? <p className="text-xs text-warning">{t('plugin_im.stats.truncated', { count: 5000 })}</p> : null}
        </div>
      )}
    </div>
  )
}
