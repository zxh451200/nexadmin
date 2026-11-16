import { useState } from 'react'
import { CheckCheck, Download, ExternalLink, Inbox, Paperclip, Trash2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
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
  createMessageExportJob,
  useDeleteMessages,
  useMessageSuggestions,
  useMessages,
  useSetMessageStatus,
  type MessageStatus,
  type SiteMessage,
} from './api'

/** 询盘附件大小：一眼能判断「这张图要不要下载」，>=1MB 给一位小数。 */
function formatSize(n: number): string {
  if (!n) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/** 官网「在线留言」收件箱：按状态跟进（未读 → 已读 → 已处理），支持批量。 */
export default function MessagePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { start: startExportJob } = useAsyncJob()
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
  // 导出沿用当前筛选（不是当前页）：统一走异步任务，提交后由 useAsyncJob
  // 提示「已提交到导出中心」，进度与下载都在导出中心看。
  const onExport = async () => {
    setExporting(true)
    try {
      await startExportJob(() => createMessageExportJob({ keyword: filters.keyword, status: filters.status }))
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
  // 推荐按需拉：打开详情才请求，超时未处理的线索在弹窗里直接给出可回推的商品。
  const { data: suggestData, isLoading: suggestLoading } = useMessageSuggestions(view?.id ?? null)

  const columns: Column<SiteMessage>[] = [
    {
      key: 'status',
      title: t('plugin_website.message.status'),
      render: (r) => (
        <span className="inline-flex flex-wrap items-center gap-1">
          {statusBadge(r.status)}
          {r.overdue ? (
            <Badge variant="destructive" title={t('plugin_website.message.overdueHint')}>
              {t('plugin_website.message.overdue')}
            </Badge>
          ) : null}
        </span>
      ),
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
      // 邮箱是不可断的长串：截断 + 悬停看全量
      render: (r) => {
        const text = `${r.email}${r.phone ? ` · ${r.phone}` : ''}`
        return (
          <span className="block max-w-[14rem] truncate text-muted-foreground" title={text}>
            {text}
          </span>
        )
      },
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
            {view.sourceProduct ? (
              <div className="flex gap-2">
                <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.sourceProduct')}</dt>
                <dd className="min-w-0">
                  <a
                    className="inline-flex items-center gap-1 underline"
                    href={view.sourceProduct.path}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <span className="wrap-break-word">{view.sourceProduct.title}</span>
                    <ExternalLink className="size-3 shrink-0" aria-hidden="true" />
                  </a>
                </dd>
              </div>
            ) : null}
            {typeof view.cardConsent === 'boolean' ? (
              <div className="flex gap-2">
                <dt className="w-20 shrink-0 text-muted-foreground">{t('plugin_website.message.cardConsent')}</dt>
                <dd>
                  <Badge variant={view.cardConsent ? 'success' : 'outline'}>
                    {view.cardConsent
                      ? t('plugin_website.message.cardConsentYes')
                      : t('plugin_website.message.cardConsentNo')}
                  </Badge>
                </dd>
              </div>
            ) : null}
            <div className="whitespace-pre-wrap wrap-break-word rounded-md border bg-muted/40 p-3">{view.content}</div>
            {view.attachments?.length ? (
              <div className="flex flex-col gap-2">
                <span className="text-muted-foreground">{t('plugin_website.message.attachments')}</span>
                <ul className="flex flex-col gap-1">
                  {view.attachments.map((f) => (
                    <li key={f.token}>
                      <a
                        className="inline-flex max-w-full items-center gap-1.5 underline"
                        href={`/website/inquiry/attachments/${f.token}`}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <Paperclip className="size-3.5 shrink-0" aria-hidden="true" />
                        <span className="truncate">{f.name}</span>
                        <span className="num shrink-0 text-xs text-muted-foreground">{formatSize(f.size)}</span>
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {view.overdue ? (
              <p className="flex items-start gap-1.5 rounded-md border border-dashed p-2.5 text-xs text-muted-foreground">
                <TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                <span>{t('plugin_website.message.overdueHint')}</span>
              </p>
            ) : null}
            {suggestData?.suggested?.length ? (
              <div className="flex flex-col gap-2">
                <span className="text-muted-foreground">{t('plugin_website.message.suggestTitle')}</span>
                <ul className="flex flex-col gap-1.5">
                  {suggestData.suggested.map((p) => (
                    <li key={p.id} className="flex items-baseline justify-between gap-2">
                      <a
                        className="min-w-0 inline-flex items-center gap-1 underline"
                        href={p.path}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <span className="truncate">{p.title}</span>
                        <ExternalLink className="size-3 shrink-0" aria-hidden="true" />
                      </a>
                      <span className="num shrink-0 text-xs text-muted-foreground">
                        {p.priceMin > 0 ? `${p.currency}${p.priceMin}` : t('plugin_website.message.suggestNoPrice')}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {suggestLoading ? (
              <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
            ) : null}
            <div className="text-xs text-muted-foreground">{[view.ip, view.ua].filter(Boolean).join(' · ')}</div>
          </dl>
        </ResponsiveDialog>
      ) : null}
    </PageContainer>
  )
}
