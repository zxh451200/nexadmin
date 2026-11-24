import { useState } from 'react'
import { CircleCheck, CircleX, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FormGrid, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import {
  useDeleteMiss,
  useIgnoreMisses,
  useMisses,
  useReopenMiss,
  useResolveMiss,
  type KbMiss,
} from './api'

/** 补答案弹窗：问题原文只读，管理员写答案；保存后走入库管线转成一篇 QA 文档。 */
function ResolveDialog({ miss, onOpenChange }: { miss: KbMiss; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const resolve = useResolveMiss()
  const [answer, setAnswer] = useState('')

  const submit = async () => {
    if (!answer.trim()) return
    try {
      const doc = await resolve.mutateAsync({ id: miss.id, answer: answer.trim() })
      toast.success(t('plugin_kb.missResolveDone', { title: doc.title }))
      onOpenChange(false)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog
      side="right"
      open
      onOpenChange={onOpenChange}
      title={t('plugin_kb.missResolveTitle')}
      description={t('plugin_kb.missResolveDesc')}
      size="md"
      footer={
        <>
          <Button variant="outline" disabled={resolve.isPending} onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={resolve.isPending || !answer.trim()} onClick={() => void submit()}>
            {t('plugin_kb.missResolve')}
          </Button>
        </>
      }
    >
      <FormLayout>
        <FormGrid cols={1}>
          <div className="flex flex-col gap-1.5">
            <Label>{t('plugin_kb.missQuestion')}</Label>
            <p className="whitespace-pre-wrap rounded-md bg-muted px-3 py-2 text-sm">{miss.question}</p>
            <p className="text-xs text-muted-foreground">
              {t('plugin_kb.missAskedTimes', { count: miss.askCount })}
            </p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kb-miss-answer">{t('plugin_kb.missAnswer')}</Label>
            <Textarea
              id="kb-miss-answer"
              rows={9}
              value={answer}
              onChange={(e) => setAnswer(e.target.value)}
              placeholder={t('plugin_kb.missAnswerHint')}
            />
            <p className="text-xs text-muted-foreground">{t('plugin_kb.missAnswerNote')}</p>
          </div>
        </FormGrid>
      </FormLayout>
    </ResponsiveDialog>
  )
}

/**
 * 未命中问题池：访客问了、知识库答不上来的问题按热度攒在这里。
 * 补一条答案就转成 QA 文档（复用文档入库管线），下次同类问题就能命中——
 * 这是知识库从「一次性导入」变成「持续生长」的入口。
 */
export default function MissPoolPanel() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [statusFilter, setStatusFilter] = useState<'all' | 'pending' | 'resolved' | 'ignored'>('all')
  const [selected, setSelected] = useState<string[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [resolveTarget, setResolveTarget] = useState<KbMiss | null>(null)
  const { sort, desc, sortProp } = useHeaderSort('askCount', true, () => setPage(1))

  const misses = useMisses(statusFilter === 'all' ? undefined : statusFilter, sort, desc, page, pageSize)
  const ignore = useIgnoreMisses()
  const reopen = useReopenMiss()
  const remove = useDeleteMiss()

  const rows = misses.data?.list ?? []
  const total = misses.data?.total ?? 0

  const onIgnore = async (ids: string[]) => {
    if (ids.length === 0) return
    const ok = await confirm({
      title: t('plugin_kb.missIgnore'),
      description: t('plugin_kb.missIgnoreConfirm', { count: ids.length }),
    })
    if (!ok) return
    try {
      const res = await ignore.mutateAsync(ids)
      toast.success(t('plugin_kb.missIgnoreDone', { count: res.count }))
      setSelected([])
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const onReopen = async (miss: KbMiss) => {
    try {
      await reopen.mutateAsync(miss.id)
      toast.success(t('plugin_kb.missReopenDone'))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const onDelete = async (miss: KbMiss) => {
    const ok = await confirm({
      title: t('common.delete'),
      description: t('plugin_kb.missDeleteConfirm'),
      destructive: true,
    })
    if (!ok) return
    try {
      await remove.mutateAsync(miss.id)
      toast.success(t('common.operateSuccess'))
      if (rows.length === 1 && page > 1) setPage(page - 1)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const columns: Column<KbMiss>[] = [
    {
      key: 'question',
      title: t('plugin_kb.missQuestion'),
      render: (m) => <span className="block max-w-[28rem] whitespace-pre-wrap break-words text-sm">{m.question}</span>,
    },
    {
      key: 'askCount',
      title: t('plugin_kb.missAskCount'),
      sortKey: 'askCount',
      align: 'end',
      render: (m) => <span className="num font-medium">{m.askCount}</span>,
    },
    {
      key: 'status',
      title: t('plugin_kb.fields.status'),
      render: (m) => (
        <Badge variant={m.status === 'pending' ? 'warning' : m.status === 'resolved' ? 'success' : 'outline'}>
          {t(`plugin_kb.missStatus.${m.status}`)}
        </Badge>
      ),
    },
    {
      key: 'lastAt',
      title: t('plugin_kb.missLastAt'),
      sortKey: 'lastAt',
      render: (m) => (
        <span className="num text-xs text-muted-foreground">
          {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(m.lastAt))}
        </span>
      ),
    },
    {
      key: 'actions',
      title: t('plugin_kb.fields.actions'),
      render: (m) => (
        <div className="flex items-center gap-1">
          <Can perm="kb:edit">
            {m.status !== 'resolved' ? (
              <Button variant="ghost" size="icon" title={t('plugin_kb.missResolve')} onClick={() => setResolveTarget(m)}>
                <CircleCheck aria-hidden="true" />
              </Button>
            ) : null}
            {m.status === 'pending' ? (
              <Button variant="ghost" size="icon" title={t('plugin_kb.missIgnore')} onClick={() => void onIgnore([m.id])}>
                <CircleX aria-hidden="true" />
              </Button>
            ) : (
              <Button variant="ghost" size="icon" title={t('plugin_kb.missReopen')} onClick={() => void onReopen(m)}>
                <RefreshCw aria-hidden="true" />
              </Button>
            )}
          </Can>
          <Can perm="kb:delete">
            <Button variant="ghost" size="icon" title={t('common.delete')} onClick={() => void onDelete(m)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        </div>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-foreground">{t('plugin_kb.missTitle')}</h2>
        <p className="text-xs text-muted-foreground">{t('plugin_kb.missDesc')}</p>
      </div>
      <DataTable
        columns={columns}
        data={rows}
        rowKey="id"
        loading={misses.isLoading}
        isError={misses.isError}
        onRetry={() => void misses.refetch()}
        retrying={misses.isFetching}
        hasFilters={statusFilter !== 'all'}
        sort={sortProp}
        pagination={{
          page,
          pageSize,
          total,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
            setSelected([])
          },
        }}
        columnSettingsKey="plugin-kb-misses"
        emptyText={t('plugin_kb.missEmpty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="kb:edit">
            <Button size="sm" variant="outline" disabled={ignore.isPending} onClick={() => void onIgnore(ids)}>
              <CircleX aria-hidden="true" />
              {t('plugin_kb.missIgnore')}
            </Button>
          </Can>
        )}
        toolbar={
          <SimpleSelect
            className="w-40"
            aria-label={t('plugin_kb.filterStatus')}
            value={statusFilter}
            onChange={(v) => {
              setStatusFilter(v as 'all' | 'pending' | 'resolved' | 'ignored')
              setPage(1)
              setSelected([])
            }}
            options={[
              { value: 'all', label: t('plugin_kb.filterAll') },
              { value: 'pending', label: t('plugin_kb.missStatus.pending') },
              { value: 'resolved', label: t('plugin_kb.missStatus.resolved') },
              { value: 'ignored', label: t('plugin_kb.missStatus.ignored') },
            ]}
          />
        }
      />
      {resolveTarget ? <ResolveDialog miss={resolveTarget} onOpenChange={(v) => !v && setResolveTarget(null)} /> : null}
    </div>
  )
}
