import { useState } from 'react'
import { CheckCheck, ExternalLink, Trash2 } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import { useClearMessages, useDeleteMessage, useMarkRead, useMessageList, useReadAll, type Message } from './api'
import { formatTime } from './format'

interface Filters {
  keyword: string
  category: string
  onlyUnread: boolean
  range: DateRangeValue
}

const emptyFilters: Filters = { keyword: '', category: '', onlyUnread: false, range: {} }

export function MessageTab({ unread }: { unread: number }) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<Message | null>(null)

  const search = () => {
    setApplied(draft)
    setPage(1)
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
  }

  const { data, isLoading } = useMessageList({
    page,
    pageSize,
    keyword: applied.keyword.trim() || undefined,
    category: applied.category || undefined,
    unread: applied.onlyUnread ? '1' : undefined,
    from: applied.range.from,
    to: applied.range.to,
  })
  const markRead = useMarkRead()
  const readAll = useReadAll()
  const remove = useDeleteMessage()
  const clear = useClearMessages()

  const open = (m: Message) => {
    setDetail(m)
    if (!m.readAt) markRead.mutate(m.id)
  }

  const onReadAll = async () => {
    try {
      await readAll.mutateAsync()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDelete = async (m: Message) => {
    if (!(await confirm({ destructive: true, description: t('message.deleteConfirm') }))) return
    try {
      await remove.mutateAsync(m.id)
      if (detail?.id === m.id) setDetail(null)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onClear = async () => {
    if (!(await confirm({ destructive: true, description: t('message.clearMessagesConfirm') }))) return
    try {
      await clear.mutateAsync()
      setDetail(null)
      setPage(1)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const goLink = (link: string) => {
    setDetail(null)
    if (/^https?:\/\//i.test(link)) window.open(link, '_blank', 'noopener,noreferrer')
    else navigate(link)
  }

  const columns: Column<Message>[] = [
    {
      key: 'title',
      title: t('message.fields.title'),
      mobile: 'title',
      render: (m) => (
        <div className="flex min-w-0 items-center gap-2">
          {!m.readAt ? (
            <span className="size-2 shrink-0 rounded-full bg-primary" role="img" aria-label={t('message.unread')} />
          ) : null}
          <span className={m.readAt ? 'truncate text-muted-foreground' : 'truncate font-medium text-foreground'}>
            {m.title}
          </span>
        </div>
      ),
    },
    { key: 'category', title: t('message.fields.category'), mobile: 'sub', render: (m) => <DictTag dict="message_category" value={m.category} /> },
    {
      key: 'createdAt',
      title: t('message.fields.time'),
      render: (m) => <span className="num text-muted-foreground">{formatTime(m.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar onSearch={search} onReset={reset}>
        <Input
          className="md:w-56"
          value={draft.keyword}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('message.keywordPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <DictSelect
          className="md:w-44"
          dict="message_category"
          placeholder={t('message.fields.category')}
          value={draft.category}
          onChange={(v) => setDraft((d) => ({ ...d, category: v ?? '' }))}
        />
        <DateRangePicker
          className="md:w-64"
          value={draft.range}
          onChange={(range) => setDraft((d) => ({ ...d, range }))}
          placeholder={t('message.timeRange')}
          ariaLabel={t('message.timeRange')}
        />
        <div className="flex min-h-11 items-center gap-2">
          <Switch id="message-only-unread" checked={draft.onlyUnread} onCheckedChange={(v) => setDraft((d) => ({ ...d, onlyUnread: v }))} />
          <Label htmlFor="message-only-unread">{t('message.onlyUnread')}</Label>
        </div>
      </FilterBar>
      <div className="flex flex-wrap gap-2 md:justify-end">
        <Button variant="outline" onClick={onReadAll} disabled={unread === 0 || readAll.isPending}>
          <CheckCheck aria-hidden="true" />
          {t('message.readAll')}
        </Button>
        <Button variant="outline" className="text-destructive" onClick={onClear} disabled={!data?.total || clear.isPending}>
          <Trash2 aria-hidden="true" />
          {t('message.clearMessages')}
        </Button>
      </div>

      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        onRowClick={open}
        emptyText={applied.keyword || applied.category || applied.onlyUnread || applied.range.from || applied.range.to ? t('message.emptyFiltered') : t('message.emptyMessages')}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        rowActions={(m) => (
          <Button variant="ghost" size="sm" className="text-destructive" onClick={() => onDelete(m)}>
            <Trash2 aria-hidden="true" />
            {t('common.delete')}
          </Button>
        )}
      />

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(o) => !o && setDetail(null)}
        title={detail?.title}
        footer={
          <>
            {detail?.link ? (
              <Button type="button" onClick={() => goLink(detail.link as string)}>
                <ExternalLink aria-hidden="true" />
                {t('message.goLink')}
              </Button>
            ) : null}
            <Button type="button" variant="outline" onClick={() => setDetail(null)}>
              {t('common.close')}
            </Button>
          </>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <DictTag dict="message_category" value={detail.category} />
              <span className="num">{formatTime(detail.createdAt, i18n.language)}</span>
            </div>
            <p className="whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">{detail.content}</p>
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
