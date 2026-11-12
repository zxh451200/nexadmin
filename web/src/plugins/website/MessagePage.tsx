import { useState } from 'react'
import { CheckCheck, Download, Inbox, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { downloadCsv } from '@/framework/components/export/csv'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { formatTime } from './format'
import {
  MESSAGE_STATUSES,
  fetchMessagesForExport,
  useDeleteMessages,
  useMessages,
  useSetMessageStatus,
  type MessageStatus,
  type SiteMessage,
} from './api'

/** 官网「在线留言」收件箱：按状态跟进（未读 → 已读 → 已处理），支持批量。 */
export default function MessagePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [draft, setDraft] = useState<{ keyword?: string; status?: string[] }>({})
  const [filters, setFilters] = useState<{ keyword?: string; status?: string }>({})
  const [selected, setSelected] = useState<string[]>([])
  const [detail, setDetail] = useState<SiteMessage | null>(null)
  const { data, isLoading } = useMessages({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    status: filters.status,
  })
  const setStatus = useSetMessageStatus()
  const remove = useDeleteMessages()

  const statusLabel = (s: MessageStatus) =>
    s === 'new' ? t('plugin_website.message.statusNew')
      : s === 'read' ? t('plugin_website.message.statusRead')
        : t('plugin_website.message.statusHandled')

  const statusBadge = (s: MessageStatus) =>
    s === 'new' ? <Badge variant="info">{statusLabel(s)}</Badge>
      : s === 'handled' ? <Badge variant="success">{statusLabel(s)}</Badge>
        : <Badge variant="outline">{statusLabel(s)}</Badge>

  // 批量改状态后要清选择：列表一刷新，选中的行可能已经不在了。
  const applyStatus = async (ids: string[], status: MessageStatus) => {
    try {
      await setStatus.mutateAsync({ ids, status })
      setSelected([])
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const open = (row: SiteMessage) => {
    setDetail(row)
    // 打开详情即算已读；已处理的不回退，避免看一次就把跟进状态冲掉。
    if (row.status === 'new') void applyStatus([row.id], 'read')
  }

  const [exporting, setExporting] = useState(false)
  // 导出沿用当前筛选（不是当前页）：后端按同一套条件取全量，前端生成带 BOM 的 CSV。
  const onExport = async () => {
    setExporting(true)
    try {
      const rows = await fetchMessagesForExport({ keyword: filters.keyword, status: filters.status })
      const stamp = new Date().toISOString().slice(0, 10).replace(/-/g, '')
      downloadCsv(`messages-${stamp}.csv`, [
        [
          t('plugin_website.message.status'),
          t('plugin_website.message.name'),
          t('plugin_website.message.email'),
          t('plugin_website.message.phone'),
          t('plugin_website.message.content'),
          t('plugin_website.message.ip'),
          t('plugin_website.message.createdAt'),
        ],
        ...rows.map((r) => [
          statusLabel(r.status),
          r.name,
          r.email,
          r.phone,
          r.content,
          r.ip,
          formatTime(r.createdAt, i18n.language),
        ]),
      ])
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  const onDelete = async (rows: SiteMessage[]) => {
    const ok = await confirm({
      destructive: true,
      description: rows.length === 1
        ? t('plugin_website.message.deleteConfirm', { name: rows[0].name })
        : t('plugin_website.message.deleteBatchConfirm', { count: rows.length }),
    })
    if (!ok) return
    try {
      await remove.mutateAsync(rows.map((r) => r.id))
      if (detail && rows.some((r) => r.id === detail.id)) setDetail(null)
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))
  // 详情弹窗读实时那一行：标已读后列表刷新，弹窗里的状态徽标跟着变，而不是停在你点开的旧值上。
  const view = detail ? (data?.list.find((r) => r.id === detail.id) ?? detail) : null

  const columns: Column<SiteMessage>[] = [
    {
      key: 'status',
      title: t('plugin_website.message.status'),
      render: (r) => statusBadge(r.status),
    },
    {
      key: 'name',
      title: t('plugin_website.message.name'),
      mobile: 'title',
      render: (r) => (
        <button type="button" className="font-medium text-foreground hover:underline" onClick={() => open(r)}>
          {r.name}
        </button>
      ),
    },
    {
      key: 'contact',
      title: t('plugin_website.message.contact'),
      mobile: 'sub',
      render: (r) => (
        <span className="text-muted-foreground">
          {r.email}
          {r.phone ? ` · ${r.phone}` : ''}
        </span>
      ),
    },
    {
      key: 'content',
      title: t('plugin_website.message.content'),
      render: (r) => <span className="line-clamp-1 text-muted-foreground">{r.content}</span>,
    },
    {
      key: 'createdAt',
      title: t('plugin_website.message.createdAt'),
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.message.title')} description={t('plugin_website.message.desc')} />
      <FilterBar
        onSearch={() => {
          setFilters({ keyword: draft.keyword, status: draft.status?.length ? draft.status.join(',') : undefined })
          setPage(1)
          setSelected([])
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
          setSelected([])
        }}
      >
        <Input
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_website.message.searchPlaceholder')}
          aria-label={t('plugin_website.message.searchPlaceholder')}
          className="md:w-64"
        />
        <OptionPicker
          title={t('plugin_website.message.status')}
          value={draft.status ?? []}
          onChange={(v) => setDraft((d) => ({ ...d, status: v.length ? v : undefined }))}
          options={MESSAGE_STATUSES.map((s) => ({ value: s, label: statusLabel(s) }))}
          className="md:w-44"
          placeholder={t('plugin_website.message.status')}
        />
      </FilterBar>
      <DataTable
        toolbar={
          <Can perm="website:message">
            <Button type="button" variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('common.export')}
            </Button>
          </Can>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-messages"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_website.message.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="website:message">
            <Button type="button" variant="outline" size="sm" onClick={() => void applyStatus(ids, 'read')}>
              <Inbox className="me-2 size-4" aria-hidden="true" />
              {t('plugin_website.message.markRead')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => void applyStatus(ids, 'handled')}>
              <CheckCheck className="me-2 size-4" aria-hidden="true" />
              {t('plugin_website.message.markHandled')}
            </Button>
            <Button type="button" variant="destructive" size="sm" onClick={() => void onDelete(selectedRows)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:message">
            {row.status !== 'handled' ? (
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('plugin_website.message.markHandled')}
                onClick={() => void applyStatus([row.id], 'handled')}
              >
                <CheckCheck className="size-4" aria-hidden="true" />
              </Button>
            ) : (
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('plugin_website.message.markUnread')}
                onClick={() => void applyStatus([row.id], 'new')}
              >
                <Inbox className="size-4" aria-hidden="true" />
              </Button>
            )}
            <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => void onDelete([row])}>
              <Trash2 className="size-4" aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {view ? (
        <ResponsiveDialog
          open
          onOpenChange={(v) => {
            if (!v) setDetail(null)
          }}
          title={t('plugin_website.message.detailTitle')}
          footer={
            <Button variant="outline" onClick={() => setDetail(null)}>
              {t('common.close')}
            </Button>
          }
        >
          <dl className="flex flex-col gap-3 text-sm">
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.status')}</dt>
              <dd>{statusBadge(view.status)}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.name')}</dt>
              <dd className="min-w-0 wrap-break-word">{view.name}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.contact')}</dt>
              <dd className="min-w-0 wrap-break-word">
                <a className="underline" href={`mailto:${view.email}`}>{view.email}</a>
                {view.phone ? <span className="text-muted-foreground"> · {view.phone}</span> : null}
              </dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.createdAt')}</dt>
              <dd className="num">{formatTime(view.createdAt, i18n.language)}</dd>
            </div>
            <div className="whitespace-pre-wrap wrap-break-word rounded-md border bg-muted/40 p-3">{view.content}</div>
            <div className="text-xs text-muted-foreground">{[view.ip, view.ua].filter(Boolean).join(' · ')}</div>
          </dl>
        </ResponsiveDialog>
      ) : null}
    </PageContainer>
  )
}
