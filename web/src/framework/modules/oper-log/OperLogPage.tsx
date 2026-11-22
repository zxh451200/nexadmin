import { useState } from 'react'
import { Download, Eraser, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { AiFilterBox } from '@/framework/components/AiFilterBox'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { pageAfterDelete } from '@/framework/hooks/page-fallback'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { DataTable } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import {
  createOperLogExportJob,
  useCleanOperLogs,
  useDeleteOperLogs,
  useOperLogList,
  type OperLog,
  type OperLogQuery,
} from './api'
import { OperLogDetailDialog } from './OperLogDetailDialog'
import { fmtDateTime } from './format'
import { OperStatsPanel } from './StatsPanel'

interface Filters {
  keyword: string
  status: string[]
  type: string[]
  range: DateRangeValue
}

const emptyFilters: Filters = { keyword: '', status: [], type: [], range: {} }

function toQuery(f: Filters): Omit<OperLogQuery, 'page' | 'pageSize'> {
  return {
    keyword: f.keyword.trim() || undefined,
    status: f.status.length ? f.status.join(',') : undefined,
    type: f.type.length ? f.type.join(',') : undefined,
    startDate: f.range.from,
    endDate: f.range.to,
  }
}

const CLEAN_OPTIONS = ['7', '30', '90', '0'] as const
const cleanKey = { '7': 'd7', '30': 'd30', '90': 'd90', '0': 'all' } as const

export default function OperLogPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 表头排序：时间最新的在前是默认，点列头切换
  const [sort, setSort] = useState('created_at')
  const [desc, setDesc] = useState(true)
  const [selected, setSelected] = useState<string[]>([])
  const [detail, setDetail] = useState<OperLog | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)
  // 视图页签用本地 state 做真源（与 TicketPage/LoginLogPage 同一模式）：URL 仅挂载读一次
  // 作深链（?view=stats 可直接分享）、之后尽力同步——完全从 URL 派生时点击会静默失效
  const [params, setParams] = useSearchParams()
  const [view, setViewState] = useState<'logs' | 'stats'>(() => (params.get('view') === 'stats' ? 'stats' : 'logs'))
  const setView = (v: string) => {
    setViewState(v === 'stats' ? 'stats' : 'logs')
    setParams(v === 'stats' ? { view: v } : {}, { replace: true })
  }
  const [cleanDays, setCleanDays] = useState<string>('30')
  const { start: startExportJob } = useAsyncJob()

  const { data, isLoading } = useOperLogList({ page, pageSize, sort, desc, ...toQuery(applied) })
  const del = useDeleteOperLogs()
  const clean = useCleanOperLogs()

  const search = () => {
    setApplied(draft)
    setPage(1)
    setSelected([])
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
    setSelected([])
  }

  const onDelete = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('operLog.confirm.delete', { count: ids.length }) }))) return
    try {
      const rows = data?.list ?? []
      const removed = rows.filter((r) => ids.includes(r.id)).length
      await del.mutateAsync(ids)
      setPage(pageAfterDelete(page, rows.length, removed))
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const onClean = async () => {
    if (!(await confirm({ destructive: true, description: t('operLog.clean.confirm') }))) return
    try {
      const res = await clean.mutateAsync(Number(cleanDays))
      setCleanOpen(false)
      setSelected([])
      setPage(1)
      toast.success(t('operLog.clean.done', { count: res.removed }))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  // 统一异步导出：提交 core/job 任务进导出中心（需要 operLog:export 权限），提交/完成提示由 useAsyncJob 统一给出
  const onExport = async () => {
    try {
      await startExportJob(() => createOperLogExportJob(toQuery(applied)))
    } catch {
      // 创建失败的原因由 http 拦截器统一弹出
    }
  }

  const columns: Column<OperLog>[] = [
    { key: 'module', title: t('operLog.col.module'), sortKey: 'module', mobile: 'title' },
    { key: 'type', title: t('operLog.col.type'), render: (r) => <DictTag dict="oper_type" value={r.type} />, mobile: 'sub' },
    { key: 'operator', title: t('operLog.col.operator'), sortKey: 'operator' },
    { key: 'ip', title: t('operLog.col.ip'), render: (r) => <span className="num">{r.ip}</span> },
    { key: 'status', title: t('operLog.col.status'), render: (r) => <DictTag dict="login_status" value={r.status} /> },
    { key: 'costMs', title: t('operLog.col.cost'), sortKey: 'cost_ms', align: 'end', render: (r) => <span className="num">{r.costMs} ms</span> },
    { key: 'createdAt', title: t('operLog.col.time'), sortKey: 'created_at', render: (r) => <span className="num">{fmtDateTime(r.createdAt, i18n.language)}</span> },
  ]

  return (
    // 满高布局：logs 页签整页撑满主区，表格内部滚动、分页钉底；stats 页签保持文档流
    <PageContainer className="h-full min-h-0">
      <PageHeader title={t('operLog.menu')} description={t('operLog.description')} />
      {/* 列表与统计分屏：查操作记录的人不该先滚过一屏图表。
          TabsList 必须 shrink-0：全高链（h-full → flex-1 → min-h-0）下统计内容超出视口时，
          flex 会把首行页签压扁，而 TabsList 自带 overflow-y-hidden，标签直接被裁掉半行。
          不能加 items-start：纵向 flex 里它让 TabsContent 收缩成 fit-content，统计网格缩成一窄条 */}
      <Tabs value={view} onValueChange={setView} className="flex min-h-0 flex-1 flex-col gap-4">
        <TabsList className="w-fit shrink-0">
          <TabsTrigger value="logs">{t('operLog.view.logs')}</TabsTrigger>
          <TabsTrigger value="stats">{t('operLog.view.stats')}</TabsTrigger>
        </TabsList>
        <TabsContent value="logs" className="flex min-h-0 w-full flex-1 flex-col gap-4">
      <AiFilterBox
        scope="operLog"
        onApply={(f) => {
          // AI 筛选产出单值，多选字段统一收进数组
          const next: Filters = {
            keyword: f.keyword ?? '',
            status: f.status ? [f.status] : [],
            type: f.type ? [f.type] : [],
            range: f.startDate || f.endDate ? { from: f.startDate, to: f.endDate } : {},
          }
          setDraft(next)
          setApplied(next)
          setPage(1)
          setSelected([])
        }}
      />
      <FilterBar onSearch={search} onReset={reset}>
        <Input aria-label={t('operLog.filter.keyword')} className="md:w-60"
          id="ol-keyword"
          value={draft.keyword}
          placeholder={t('operLog.filter.keywordPlaceholder')}
          onChange={(e) => setDraft({ ...draft, keyword: e.target.value })}
        />
        <DictSelect dict="oper_type" multiple value={draft.type} onChange={(v) => setDraft({ ...draft, type: v })} aria-label={t('operLog.filter.type')} className="md:w-44" placeholder={t('operLog.filter.type')} />
        <DictSelect dict="login_status" multiple value={draft.status} onChange={(v) => setDraft({ ...draft, status: v })} aria-label={t('operLog.filter.status')} className="md:w-40" placeholder={t('operLog.filter.status')} />
        <DateRangePicker value={draft.range} onChange={(v) => setDraft({ ...draft, range: v })} ariaLabel={t('operLog.filter.time')} className="md:w-64" placeholder={t('operLog.filter.time')} />
      </FilterBar>
      <DataTable<OperLog>
        fill
        className="min-h-0 flex-1"
        toolbar={
          <>
            <Can perm="operLog:export">
              <Button type="button" variant="outline" onClick={() => void onExport()}>
                <Download aria-hidden="true" />
                {t('operLog.action.export')}
              </Button>
            </Can>
            <Can perm="operLog:delete">
              <Button type="button" variant="outline" onClick={() => setCleanOpen(true)}>
                <Eraser aria-hidden="true" />
                {t('operLog.action.clean')}
              </Button>
            </Can>
          </>
        }
        columns={columns}
        sort={{
          key: sort,
          desc,
          onChange: (key, nextDesc) => {
            setSort(key)
            setDesc(nextDesc)
            setPage(1)
            setSelected([])
          },
        }}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="operLog"
        onRowClick={setDetail}
        rowActions={(l) => <DetailAction onClick={() => setDetail(l)} />}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="operLog:delete">
            <Button type="button" variant="destructive" size="sm" className="h-10 md:h-8" onClick={() => onDelete(ids)}>
              <Trash2 aria-hidden="true" />
              {t('operLog.action.deleteSelected')}
            </Button>
          </Can>
        )}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />
        </TabsContent>
        <TabsContent value="stats">
          <OperStatsPanel />
        </TabsContent>
      </Tabs>
      <OperLogDetailDialog log={detail} onClose={() => setDetail(null)} />
      <ResponsiveDialog
        open={cleanOpen}
        onOpenChange={setCleanOpen}
        title={t('operLog.clean.title')}
        description={t('operLog.clean.description')}
        size="sm"
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setCleanOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" variant="destructive" disabled={clean.isPending} onClick={onClean}>
              {t('common.confirm')}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-1.5">
          <Label>{t('operLog.clean.range')}</Label>
          <Select value={cleanDays} onValueChange={setCleanDays}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CLEAN_OPTIONS.map((d) => (
                <SelectItem key={d} value={d}>
                  {t(`operLog.clean.${cleanKey[d]}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  )
}
