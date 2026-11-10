import { useState } from 'react'
import { Download, Paperclip, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { ExportCsvButton } from '@/framework/components/export/ExportCsvButton'
import { dictText, useDictItems } from '@/framework/components/pickers/useDictItems'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { RichTextView } from '@/framework/components/RichTextView'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import { fetchNoticesForExport, useClearNotices, useDeleteNotice, useDismissNotices, useMyNotices, useReadAllNotices, useReadNotice, type MyNotice } from './api'
import { formatTime } from './format'

interface Filters {
  keyword: string
  type: string
  onlyUnread: boolean
}

const emptyFilters: Filters = { keyword: '', type: '', onlyUnread: false }

export function NoticeTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<MyNotice | null>(null)
  const [selected, setSelected] = useState<string[]>([])

  const search = () => {
    setApplied(draft)
    setPage(1)
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
  }

  const { data, isLoading } = useMyNotices({
    page,
    pageSize,
    keyword: applied.keyword.trim() || undefined,
    type: applied.type || undefined,
    unread: applied.onlyUnread ? '1' : undefined,
  })
  const readNotice = useReadNotice()
  const readAll = useReadAllNotices()
  const clear = useClearNotices()
  const remove = useDeleteNotice()
  const dismissBatch = useDismissNotices()

  const onReadAll = async () => {
    try {
      await readAll.mutateAsync()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  // 导出的类型/级别列要和页面上的 DictTag 用同一份字典，否则中英文对不上
  const { data: typeItems } = useDictItems('notice_type')
  const { data: levelItems } = useDictItems('notice_level')

  // 批量移除：语义与单条一致——只从「我的」消息中心摘掉，公告本身与其他人的列表不动。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('message.deleteBatchNoticeConfirm', { count: ids.length }) }))) return
    try {
      const { dismissed } = await dismissBatch.mutateAsync(ids)
      if (detail && ids.includes(detail.id)) setDetail(null)
      setSelected([])
      toast.success(dismissed < ids.length ? t('message.dismissedPartial', { dismissed, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDelete = async (n: MyNotice) => {
    if (!(await confirm({ destructive: true, description: t('message.deleteNoticeConfirm') }))) return
    try {
      await remove.mutateAsync(n.id)
      if (detail?.id === n.id) setDetail(null)
      setSelected((s) => s.filter((x) => x !== n.id))
      if (data?.list.length === 1 && page > 1) setPage(page - 1)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onClear = async () => {
    if (!(await confirm({ destructive: true, description: t('message.clearNoticesConfirm') }))) return
    try {
      await clear.mutateAsync()
      setDetail(null)
      setSelected([])
      setPage(1)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const open = (n: MyNotice) => {
    setDetail(n)
    if (!n.read) {
      readNotice.mutate(n.id)
      setDetail({ ...n, read: true })
    }
  }

  const columns: Column<MyNotice>[] = [
    {
      key: 'title',
      title: t('message.fields.title'),
      mobile: 'title',
      render: (n) => (
        <div className="flex min-w-0 items-center gap-2">
          {!n.read ? (
            <span className="size-2 shrink-0 rounded-full bg-primary" role="img" aria-label={t('message.unread')} />
          ) : null}
          {n.pinned ? <Badge variant="outline">{t('message.pinned')}</Badge> : null}
          <span className={n.read ? 'truncate text-muted-foreground' : 'truncate font-medium text-foreground'}>{n.title}</span>
        </div>
      ),
    },
    { key: 'type', title: t('message.fields.type'), mobile: 'sub', render: (n) => <DictTag dict="notice_type" value={n.type} /> },
    { key: 'level', title: t('message.fields.level'), render: (n) => <DictTag dict="notice_level" value={n.level} /> },
    {
      key: 'publishedAt',
      title: t('message.fields.publishedAt'),
      render: (n) => <span className="num text-muted-foreground">{formatTime(n.publishedAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar onSearch={search} onReset={reset}>
        <Input
          className="md:w-56"
          value={draft.keyword}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('message.noticeKeywordPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <DictSelect
          className="md:w-44"
          dict="notice_type"
          placeholder={t('message.fields.type')}
          value={draft.type}
          onChange={(v) => setDraft((d) => ({ ...d, type: v ?? '' }))}
        />
        <div className="flex min-h-11 items-center gap-2">
          <Switch id="notice-only-unread" checked={draft.onlyUnread} onCheckedChange={(v) => setDraft((d) => ({ ...d, onlyUnread: v }))} />
          <Label htmlFor="notice-only-unread">{t('message.onlyUnread')}</Label>
        </div>
      </FilterBar>
      <div className="flex flex-wrap gap-2 md:justify-end">
        <Button variant="outline" onClick={onReadAll} disabled={readAll.isPending || !data?.total}>
          {t('message.readAll')}
        </Button>
        <ExportCsvButton
          filename="notices"
          columns={[
            { header: t('message.fields.title'), value: (n) => n.title },
            { header: t('message.fields.type'), value: (n) => dictText(typeItems, n.type) },
            { header: t('message.fields.level'), value: (n) => dictText(levelItems, n.level) },
            { header: t('message.fields.status'), value: (n) => (n.read ? t('message.read') : t('message.unread')) },
            { header: t('message.fields.publishedAt'), value: (n) => formatTime(n.publishedAt ?? '', i18n.language) },
            { header: t('message.fields.content'), value: (n) => n.content },
          ]}
          getRows={() => fetchNoticesForExport({
            keyword: applied.keyword.trim() || undefined,
            type: applied.type || undefined,
            unread: applied.onlyUnread ? '1' : undefined,
          })}
        />
        <Button variant="outline" className="text-destructive" onClick={onClear} disabled={!data?.total || clear.isPending}>
          <Trash2 aria-hidden="true" />
          {t('message.clearNotices')}
        </Button>
      </div>

      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        onRowClick={open}
        emptyText={applied.keyword || applied.type || applied.onlyUnread ? t('message.emptyFiltered') : t('message.emptyNotices')}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
            <Trash2 aria-hidden="true" />
            {t('common.delete')}
          </Button>
        )}
        rowActions={(n) => (
          <Button variant="ghost" size="sm" className="text-destructive" onClick={() => onDelete(n)}>
            <Trash2 aria-hidden="true" />
            {t('common.delete')}
          </Button>
        )}
      />

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(o) => !o && setDetail(null)}
        size="lg"
        title={detail?.title}
        footer={
          <Button type="button" variant="outline" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <DictTag dict="notice_type" value={detail.type} />
              <DictTag dict="notice_level" value={detail.level} />
              <span className="num">{formatTime(detail.publishedAt, i18n.language)}</span>
            </div>
            <RichTextView html={detail.content} />
            {detail.attachments?.length ? (
              <div className="flex flex-col gap-2">
                <h3 className="flex items-center gap-1 text-sm font-semibold text-foreground">
                  <Paperclip className="size-4" aria-hidden="true" />
                  {t('message.attachments')}
                </h3>
                <ul className="flex flex-col gap-1">
                  {detail.attachments.map((f) => (
                    <li key={f.id}>
                      <a
                        href={f.url}
                        download={f.name}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex min-h-11 items-center gap-2 text-sm text-primary hover:underline md:min-h-8"
                      >
                        <Download className="size-4" aria-hidden="true" />
                        <span className="truncate">{f.name}</span>
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
