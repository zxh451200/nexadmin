import { useState } from 'react'
import { BarChart3, Pencil, Plus, Send, Trash2, Undo2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { AiFilterBox } from '@/framework/components/AiFilterBox'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Progress } from '@/framework/ui/progress'
import {
  useDeleteNotices,
  useNoticeList,
  usePublishNotice,
  useRevokeNotice,
  type Notice,
  type NoticeQuery,
} from './api'
import { NoticeFormDialog } from './NoticeFormDialog'
import { ReadStatsDialog } from './ReadStatsDialog'

const PAGE_SIZE = 10

function fmt(iso: string | null | undefined, lang: string) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ''
    : new Intl.DateTimeFormat(lang, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(d)
}

export default function NoticePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<NoticeQuery>({})
  const [filters, setFilters] = useState<NoticeQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZE)
  const [selected, setSelected] = useState<string[]>([])
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Notice | null>(null)
  const [statsFor, setStatsFor] = useState<Notice | null>(null)

  const { data, isLoading } = useNoticeList({ ...filters, page, pageSize })
  const publish = usePublishNotice()
  const revoke = useRevokeNotice()
  const remove = useDeleteNotices()

  const openForm = (n: Notice | null) => {
    setEditing(n)
    setFormOpen(true)
  }

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onPublish = async (n: Notice) => {
    if (await confirm({ title: t('notice.publish'), description: t('notice.publishConfirm', { title: n.title }) })) {
      await run(() => publish.mutateAsync(n.id))
    }
  }
  const onRevoke = async (n: Notice) => {
    if (await confirm({ title: t('notice.revoke'), description: t('notice.revokeConfirm', { title: n.title }), destructive: true })) {
      await run(() => revoke.mutateAsync(n.id))
    }
  }
  const onDelete = async (ids: string[]) => {
    if (await confirm({ destructive: true, description: t('common.deleteConfirm') })) {
      await run(async () => {
        await remove.mutateAsync(ids)
        setSelected((s) => s.filter((x) => !ids.includes(x)))
      })
    }
  }

  const columns: Column<Notice>[] = [
    {
      key: 'title',
      title: t('notice.fields.title'),
      mobile: 'title',
      render: (n) => (
        <div className="flex min-w-0 items-center gap-2">
          {n.pinned ? <Badge variant="outline">{t('notice.pinnedTag')}</Badge> : null}
          {n.platform ? <Badge variant="info">{t('notice.platformTag')}</Badge> : null}
          <span className="min-w-0 max-w-[18rem] truncate font-medium text-foreground">{n.title}</span>
        </div>
      ),
    },
    { key: 'type', title: t('notice.fields.type'), mobile: 'sub', render: (n) => <DictTag dict="notice_type" value={n.type} /> },
    { key: 'level', title: t('notice.fields.level'), render: (n) => <DictTag dict="notice_level" value={n.level} /> },
    { key: 'status', title: t('notice.fields.status'), render: (n) => <DictTag dict="notice_status" value={n.status} /> },
    { key: 'scope', title: t('notice.fields.scope'), render: (n) => t(`notice.scope.${n.scope}`) },
    {
      key: 'readStats',
      title: t('notice.fields.readStats'),
      render: (n) =>
        n.status === 'draft' || !n.totalCount ? (
          <span className="text-muted-foreground">-</span>
        ) : (
          <div className="flex min-w-32 items-center gap-2">
            <Progress className="w-20" value={Math.round((n.readCount / n.totalCount) * 100)} aria-label={t('notice.stats.readRate')} />
            <span className="num text-xs text-muted-foreground">
              {n.readCount}/{n.totalCount}
            </span>
          </div>
        ),
    },
    {
      key: 'publishedAt',
      title: t('notice.fields.publishedAt'),
      render: (n) => {
        const v = n.publishedAt ?? n.publishAt
        return v ? <span className="num">{fmt(v, i18n.language)}</span> : <span className="text-muted-foreground">-</span>
      },
    },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('notice.title')}
        description={t('notice.description')}
        actions={
          <Can perm="notice:create">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('notice.create')}
            </Button>
          </Can>
        }
      />

      <AiFilterBox
        scope="notice"
        onApply={(f) => {
          const next: NoticeQuery = { keyword: f.keyword, type: f.type, level: f.level, status: f.status }
          setDraft(next)
          setFilters(next)
          setPage(1)
          setSelected([])
        }}
      />

      <FilterBar
        onSearch={() => {
          setFilters(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
        }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('notice.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <DictSelect className="md:w-36" dict="notice_type" placeholder={t('notice.fields.type')} value={draft.type} onChange={(v) => setDraft((d) => ({ ...d, type: v }))} />
        <DictSelect className="md:w-36" dict="notice_level" placeholder={t('notice.fields.level')} value={draft.level} onChange={(v) => setDraft((d) => ({ ...d, level: v }))} />
        <DictSelect className="md:w-36" dict="notice_status" placeholder={t('notice.fields.status')} value={draft.status} onChange={(v) => setDraft((d) => ({ ...d, status: v }))} />
      </FilterBar>

      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="notice-list"
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
          <Can perm="notice:delete">
            <Button variant="destructive" size="sm" onClick={() => onDelete(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        emptyText={t('notice.empty')}
        rowActions={(n) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('notice.stats.action')} disabled={n.status === 'draft'} onClick={() => setStatsFor(n)}>
              <BarChart3 aria-hidden="true" />
            </Button>
            <Can perm="notice:update">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(n)}>
                <Pencil aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="notice:publish">
              {n.status === 'published' ? (
                <Button variant="ghost" size="icon" aria-label={t('notice.revoke')} onClick={() => onRevoke(n)}>
                  <Undo2 aria-hidden="true" />
                </Button>
              ) : (
                <Button variant="ghost" size="icon" aria-label={t('notice.publish')} onClick={() => onPublish(n)}>
                  <Send aria-hidden="true" />
                </Button>
              )}
            </Can>
            <Can perm="notice:delete">
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => onDelete([n.id])}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />

      <NoticeFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} />
      <ReadStatsDialog notice={statsFor} onOpenChange={(o) => !o && setStatsFor(null)} />
    </PageContainer>
  )
}
