import { useState } from 'react'
import { CheckCheck, Download, ExternalLink, Inbox, Paperclip, Trash2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { DetailAction } from '@/framework/components/DetailAction'
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
  // 表头排序：与筛选分开存；默认最新留言在前（与后端回退一致）
  const { sort, desc, sortProp } = useHeaderSort('created_at', true, () => setPage(1))
  const [draft, setDraft] = useState<{ keyword?: string; status?: string[] }>({})
  const [filters, setFilters] = useState<{ keyword?: string; status?: string }>({})
  const [selected, setSelected] = useState<string[]>([])
  const [detail, setDetail] = useState<SiteMessage | null>(null)
  const { data, isLoading } = useMessages({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    status: filters.status,
    sort,
    desc,
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

  // 按 id 删，不按「当前页的行」删：勾选可以跨页，拿 data.list 反查只会删到当前页那几条，
  // 界面却按总数提示成功。单条确认文案里的名字从当前页取，取不到就退回按条数提示。
  const onDelete = async (ids: string[]) => {
    const name = ids.length === 1 ? (data?.list.find((r) => r.id === ids[0])?.name ?? '') : ''
    const ok = await confirm({
      destructive: true,
      description: name
        ? t('plugin_website.message.deleteConfirm', { name })
        : t('plugin_website.message.deleteBatchConfirm', { count: ids.length }),
    })
    if (!ok) return
    try {
      await remove.mutateAsync(ids)
      if (detail && ids.includes(detail.id)) setDetail(null)
      setSelected((s) => s.filter((id) => !ids.includes(id)))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

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
      sortKey: 'created_at',
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
        sort={sortProp}
        toolbar={
          <Can perm="website:message">
            <Button type="button" variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download aria-hidden="true" />
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
              <Inbox aria-hidden="true" />
              {t('plugin_website.message.markRead')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => void applyStatus(ids, 'handled')}>
              <CheckCheck aria-hidden="true" />
              {t('plugin_website.message.markHandled')}
            </Button>
            <Button type="button" variant="destructive" size="sm" onClick={() => void onDelete(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:message">
            <DetailAction onClick={() => open(row)} />
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
            <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => void onDelete([row.id])}>
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
          <div className="flex flex-col gap-4 text-sm">
            <dl className="flex flex-col gap-3">
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
            </dl>

            {/* 留言正文单独成块：贴边的一整段字，比塞进「标签: 值」的网格里更像来信本身 */}
            <div className="whitespace-pre-wrap wrap-break-word rounded-md border bg-muted/40 p-3">{view.content}</div>

            {view.attachments?.length ? (
              <section className="flex flex-col gap-2 border-t border-border pt-3">
                <h3 className="text-xs font-medium text-muted-foreground">
                  {t('plugin_website.message.attachments')}
                </h3>
                <ul className="flex flex-wrap gap-2">
                  {view.attachments.map((f) => {
                    // 图片服务端回 inline，开新标签页预览；其余回的是 Content-Disposition: attachment，
                    // 再配 target=_blank 只会留下一个空白标签页（浏览器把响应当下载，页面永远空白），
                    // 所以非图片一律原地下载，弹窗也不跳走。
                    const preview = f.mime.startsWith('image/')
                    return (
                      <li key={f.token} className="min-w-0">
                        <a
                          className="inline-flex max-w-full items-center gap-2 rounded-md border bg-muted/30 px-2.5 py-1.5 transition-colors hover:bg-muted"
                          href={`/website/inquiry/attachments/${f.token}`}
                          {...(preview ? { target: '_blank', rel: 'noopener noreferrer' } : { download: f.name })}
                        >
                          <Paperclip className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                          <span className="min-w-0 truncate">{f.name}</span>
                          <span className="num shrink-0 text-xs text-muted-foreground">{formatSize(f.size)}</span>
                          {preview ? (
                            <ExternalLink className="size-3.5 shrink-0 text-muted-foreground/70" aria-hidden="true" />
                          ) : (
                            <Download className="size-3.5 shrink-0 text-muted-foreground/70" aria-hidden="true" />
                          )}
                        </a>
                      </li>
                    )
                  })}
                </ul>
              </section>
            ) : null}
            {view.overdue ? (
              <div className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 p-2.5 text-xs text-foreground">
                <TriangleAlert className="mt-px size-3.5 shrink-0 text-warning" aria-hidden="true" />
                <span className="leading-relaxed">{t('plugin_website.message.overdueHint')}</span>
              </div>
            ) : null}
            {suggestData?.suggested?.length ? (
              <section className="flex flex-col gap-1 border-t border-border pt-3">
                <h3 className="text-xs font-medium text-muted-foreground">
                  {t('plugin_website.message.suggestTitle')}
                </h3>
                <ul className="-mx-2 flex flex-col">
                  {suggestData.suggested.map((p) => (
                    <li key={p.id}>
                      <a
                        className="flex items-center gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-muted"
                        href={p.path}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        <span className="min-w-0 flex-1 truncate">{p.title}</span>
                        <span className="num shrink-0 text-xs text-muted-foreground">
                          {p.priceMin > 0 ? `${p.currency}${p.priceMin}` : t('plugin_website.message.suggestNoPrice')}
                        </span>
                        <ExternalLink className="size-3 shrink-0 text-muted-foreground/70" aria-hidden="true" />
                      </a>
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
            {suggestLoading ? (
              <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
            ) : null}
            {/* 访客来源：IP + UA。以前裸着一行小字贴在列表尾部，读起来像第 5 条商品，
                现在给它标签、加分隔线并降到脚注层级——排查灌水/群发时它是唯一线索，不能省。 */}
            {view.ip || view.ua ? (
              <div className="flex gap-2 border-t border-border pt-3 text-xs text-muted-foreground">
                <span className="w-20 shrink-0">{t('plugin_website.message.source')}</span>
                <span className="min-w-0 break-words font-mono">{[view.ip, view.ua].filter(Boolean).join(' · ')}</span>
              </div>
            ) : null}
          </div>
        </ResponsiveDialog>
      ) : null}
    </PageContainer>
  )
}
