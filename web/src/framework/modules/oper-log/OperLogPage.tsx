import { useState } from 'react'
import { Download, Eraser, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { AiFilterBox } from '@/framework/components/AiFilterBox'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
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
import {
  exportOperLogs,
  isExportTooMany,
  useCleanOperLogs,
  useDeleteOperLogs,
  useOperLogList,
  type OperLog,
  type OperLogQuery,
} from './api'
import { OperLogDetailDialog } from './OperLogDetailDialog'
import { blobExt } from '../user/api'
import { saveBlob } from '../user/csv'
import { fmtDateTime, stamp } from './format'

interface Filters {
  keyword: string
  status: string
  type: string
  range: DateRangeValue
}

const emptyFilters: Filters = { keyword: '', status: '', type: '', range: {} }

function toQuery(f: Filters): Omit<OperLogQuery, 'page' | 'pageSize'> {
  return {
    keyword: f.keyword.trim() || undefined,
    status: f.status || undefined,
    type: f.type || undefined,
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
  const [selected, setSelected] = useState<string[]>([])
  const [detail, setDetail] = useState<OperLog | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)
  const [cleanDays, setCleanDays] = useState<string>('30')
  const [exporting, setExporting] = useState(false)

  const { data, isLoading } = useOperLogList({ page, pageSize, ...toQuery(applied) })
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
      await del.mutateAsync(ids)
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
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
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onExport = async () => {
    setExporting(true)
    try {
      // 服务端按当前筛选条件与数据范围生成文件（需要 operLog:export 权限），列由服务端白名单决定
      const blob = await exportOperLogs(toQuery(applied))
      saveBlob(`${t('operLog.export.filename')}-${stamp()}.${blobExt(blob)}`, blob)
      toast.success(t('operLog.export.done'))
    } catch (e) {
      toast.error(isExportTooMany(e) ? t('operLog.export.tooMany') : e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  const columns: Column<OperLog>[] = [
    { key: 'module', title: t('operLog.col.module'), mobile: 'title' },
    { key: 'type', title: t('operLog.col.type'), render: (r) => <DictTag dict="oper_type" value={r.type} />, mobile: 'sub' },
    { key: 'operator', title: t('operLog.col.operator') },
    { key: 'ip', title: t('operLog.col.ip'), render: (r) => <span className="num">{r.ip}</span> },
    { key: 'status', title: t('operLog.col.status'), render: (r) => <DictTag dict="login_status" value={r.status} /> },
    { key: 'costMs', title: t('operLog.col.cost'), align: 'end', render: (r) => <span className="num">{r.costMs} ms</span> },
    { key: 'createdAt', title: t('operLog.col.time'), render: (r) => <span className="num">{fmtDateTime(r.createdAt, i18n.language)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('operLog.menu')}
        description={t('operLog.description')}
        actions={
          <>
            <Can perm="operLog:export">
              <Button type="button" variant="outline" disabled={exporting} onClick={onExport}>
                <Download className="me-2 size-4" aria-hidden="true" />
                {t('operLog.action.export')}
              </Button>
            </Can>
            <Can perm="operLog:delete">
              <Button type="button" variant="outline" onClick={() => setCleanOpen(true)}>
                <Eraser className="me-2 size-4" aria-hidden="true" />
                {t('operLog.action.clean')}
              </Button>
            </Can>
          </>
        }
      />
      <AiFilterBox
        scope="operLog"
        onApply={(f) => {
          const next: Filters = {
            keyword: f.keyword ?? '',
            status: f.status ?? '',
            type: f.type ?? '',
            range: f.startDate || f.endDate ? { from: f.startDate, to: f.endDate } : {},
          }
          setDraft(next)
          setApplied(next)
          setPage(1)
          setSelected([])
        }}
      />
      <FilterBar onSearch={search} onReset={reset}>
        <div className="flex w-full flex-col gap-1.5 md:w-60">
          <Label htmlFor="ol-keyword">{t('operLog.filter.keyword')}</Label>
          <Input
            id="ol-keyword"
            value={draft.keyword}
            placeholder={t('operLog.filter.keywordPlaceholder')}
            onChange={(e) => setDraft({ ...draft, keyword: e.target.value })}
          />
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-40">
          <Label>{t('operLog.filter.type')}</Label>
          <DictSelect dict="oper_type" value={draft.type} onChange={(v) => setDraft({ ...draft, type: v })} />
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-36">
          <Label>{t('operLog.filter.status')}</Label>
          <DictSelect dict="login_status" value={draft.status} onChange={(v) => setDraft({ ...draft, status: v })} />
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-64">
          <Label>{t('operLog.filter.time')}</Label>
          <DateRangePicker value={draft.range} onChange={(v) => setDraft({ ...draft, range: v })} />
        </div>
      </FilterBar>
      <DataTable<OperLog>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="operLog"
        onRowClick={setDetail}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="operLog:delete">
            <Button type="button" variant="destructive" size="sm" className="h-10 md:h-8" onClick={() => onDelete(ids)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
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
