import { useState } from 'react'
import { Download, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { isExportTooMany } from '../oper-log/api'
import { fmtDateTime, stamp } from '../oper-log/format'
import { blobExt } from '../user/api'
import { saveBlob } from '../user/csv'
import {
  LOGIN_TYPES,
  exportLoginLogs,
  useDeleteLoginLogs,
  useLoginLogList,
  type LoginLog,
  type LoginLogQuery,
  type LoginType,
} from './api'
import { LoginStatsPanel } from './StatsPanel'

interface Filters {
  keyword: string
  status: string
  loginType: string
  range: DateRangeValue
}

const emptyFilters: Filters = { keyword: '', status: '', loginType: 'all', range: {} }

function toQuery(f: Filters): Omit<LoginLogQuery, 'page' | 'pageSize'> {
  return {
    keyword: f.keyword.trim() || undefined,
    status: f.status || undefined,
    loginType: f.loginType === 'all' ? undefined : f.loginType,
    startDate: f.range.from,
    endDate: f.range.to,
  }
}

const TYPE_VARIANT: Record<LoginType, BadgeProps['variant']> = {
  password: 'secondary',
  phone: 'info',
  wecom: 'success',
  dingtalk: 'info',
  feishu: 'warning',
  google: 'outline',
  github: 'default',
  oidc: 'secondary',
}

export default function LoginLogPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 视图写进 URL：?view=stats 可以直接分享给同事
  const [params, setParams] = useSearchParams()
  const view = params.get('view') === 'stats' ? 'stats' : 'logs'
  const setView = (v: string) => setParams(v === 'stats' ? { view: v } : {}, { replace: true })
  const [selected, setSelected] = useState<string[]>([])
  const [exporting, setExporting] = useState(false)

  const { data, isLoading } = useLoginLogList({ page, pageSize, ...toQuery(applied) })
  const del = useDeleteLoginLogs()

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
    if (!(await confirm({ destructive: true, description: t('loginLog.confirm.delete', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onExport = async () => {
    setExporting(true)
    try {
      // 服务端按当前筛选条件与数据范围生成文件（需要 loginLog:export 权限），列由服务端白名单决定
      const blob = await exportLoginLogs(toQuery(applied))
      saveBlob(`${t('loginLog.export.filename')}-${stamp()}.${blobExt(blob)}`, blob)
      toast.success(t('loginLog.export.done'))
    } catch (e) {
      toast.error(isExportTooMany(e) ? t('loginLog.export.tooMany') : e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  const columns: Column<LoginLog>[] = [
    { key: 'username', title: t('loginLog.col.username'), mobile: 'title' },
    {
      key: 'loginType',
      title: t('loginLog.col.type'),
      mobile: 'sub',
      render: (r) => <Badge variant={TYPE_VARIANT[r.loginType]}>{t(`loginLog.type.${r.loginType}`)}</Badge>,
    },
    { key: 'ip', title: t('loginLog.col.ip'), render: (r) => <span className="num">{r.ip}</span> },
    { key: 'location', title: t('loginLog.col.location') },
    { key: 'browser', title: t('loginLog.col.browser') },
    { key: 'os', title: t('loginLog.col.os') },
    { key: 'status', title: t('loginLog.col.status'), render: (r) => <DictTag dict="login_status" value={r.status} /> },
    {
      key: 'failReason',
      title: t('loginLog.col.reason'),
      render: (r) => (r.failReason ? t(`loginLog.reason.${r.failReason}`) : <span className="text-muted-foreground">-</span>),
    },
    { key: 'createdAt', title: t('loginLog.col.time'), render: (r) => <span className="num">{fmtDateTime(r.createdAt, i18n.language)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('loginLog.menu')} description={t('loginLog.description')} />
      {/* 列表与统计分屏：查日志的人不该先滚过九百像素的图表，做复盘的也不该被表格挤走 */}
      <Tabs value={view} onValueChange={setView}>
        <TabsList className="w-fit">
          <TabsTrigger value="logs">{t('loginLog.view.logs')}</TabsTrigger>
          <TabsTrigger value="stats">{t('loginLog.view.stats')}</TabsTrigger>
        </TabsList>
        <TabsContent value="logs">
          <div className="flex flex-col gap-4">
      <FilterBar onSearch={search} onReset={reset}>
        <Input aria-label={t('loginLog.filter.keyword')} className="md:w-56"
          id="ll-keyword"
          value={draft.keyword}
          placeholder={t('loginLog.filter.keywordPlaceholder')}
          onChange={(e) => setDraft({ ...draft, keyword: e.target.value })}
        />
        <Select value={draft.loginType} onValueChange={(v) => setDraft({ ...draft, loginType: v })}>
          <SelectTrigger className="md:w-40" aria-label={t('loginLog.filter.type')}>
            <SelectValue placeholder={t('loginLog.filter.type')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('loginLog.filter.allTypes')}</SelectItem>
            {LOGIN_TYPES.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`loginLog.type.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <DictSelect dict="login_status" value={draft.status} allLabel={t('loginLog.filter.allStatus')} onChange={(v) => setDraft({ ...draft, status: v })} aria-label={t('loginLog.filter.status')} className="md:w-36" placeholder={t('loginLog.filter.status')} />
        <DateRangePicker value={draft.range} onChange={(v) => setDraft({ ...draft, range: v })} aria-label={t('loginLog.filter.time')} className="md:w-64" placeholder={t('loginLog.filter.time')} />
      </FilterBar>
      <DataTable<LoginLog>
        toolbar={
          <Can perm="loginLog:export">
            <Button type="button" variant="outline" disabled={exporting} onClick={onExport}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('loginLog.action.export')}
            </Button>
          </Can>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="loginLog"
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="loginLog:delete">
            <Button type="button" variant="destructive" size="sm" className="h-10 md:h-8" onClick={() => onDelete(ids)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('loginLog.action.deleteSelected')}
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
          </div>
        </TabsContent>
        <TabsContent value="stats">
          <LoginStatsPanel />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
