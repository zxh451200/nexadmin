import { useState } from 'react'
import { Download, Eraser, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { pageAfterDelete } from '@/framework/hooks/page-fallback'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { DataTable } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { fmtDateTime } from '../oper-log/format'
import {
  LOGIN_TYPES,
  createLoginLogExportJob,
  useCleanLoginLogs,
  useDeleteLoginLogs,
  useLoginLogList,
  type LoginLog,
  type LoginLogQuery,
  type LoginType,
} from './api'
import { LoginStatsPanel } from './StatsPanel'

interface Filters {
  keyword: string
  status: string[]
  loginType: string[]
  range: DateRangeValue
}

const emptyFilters: Filters = { keyword: '', status: [], loginType: [], range: {} }

// 清理档位与文案映射，与操作日志页同口径
const CLEAN_OPTIONS = ['7', '30', '90', '0'] as const
const cleanKey = { '7': 'd7', '30': 'd30', '90': 'd90', '0': 'all' } as const

function toQuery(f: Filters): Omit<LoginLogQuery, 'page' | 'pageSize'> {
  return {
    keyword: f.keyword.trim() || undefined,
    status: f.status.length ? f.status.join(',') : undefined,
    loginType: f.loginType.length ? f.loginType.join(',') : undefined,
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
  ldap: 'outline',
  cas: 'outline',
  saml: 'outline',
  '2fa': 'info',
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
  const { start: startExportJob } = useAsyncJob()

  const { data, isLoading } = useLoginLogList({ page, pageSize, ...toQuery(applied) })
  const del = useDeleteLoginLogs()
  const clean = useCleanLoginLogs()
  const [cleanOpen, setCleanOpen] = useState(false)
  const [cleanDays, setCleanDays] = useState<string>('30')

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
      const rows = data?.list ?? []
      const removed = rows.filter((r) => ids.includes(r.id)).length
      await del.mutateAsync(ids)
      setPage(pageAfterDelete(page, rows.length, removed))
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onClean = async () => {
    if (!(await confirm({ destructive: true, description: t('loginLog.clean.confirm') }))) return
    try {
      const res = await clean.mutateAsync(Number(cleanDays))
      setCleanOpen(false)
      setSelected([])
      setPage(1)
      toast.success(t('loginLog.clean.done', { count: res.removed }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  // 统一异步导出：提交 core/job 任务进导出中心（需要 loginLog:export 权限），提交/完成提示由 useAsyncJob 统一给出
  const onExport = async () => {
    try {
      await startExportJob(() => createLoginLogExportJob(toQuery(applied)))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<LoginLog>[] = [
    { key: 'username', title: t('loginLog.col.username'), mobile: 'title' },
    {
      key: 'loginType',
      title: t('loginLog.col.type'),
      mobile: 'sub',
      render: (r) => <Badge variant={TYPE_VARIANT[r.loginType as LoginType]}>{t(`loginLog.type.${r.loginType}`, { defaultValue: r.loginType })}</Badge>,
    },
    { key: 'ip', title: t('loginLog.col.ip'), render: (r) => <span className="num">{r.ip}</span> },
    {
      key: 'location',
      title: t('loginLog.col.location'),
      render: (r) => (r.location ? <span className="block max-w-[10rem] truncate" title={r.location}>{r.location}</span> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'browser',
      title: t('loginLog.col.browser'),
      render: (r) => (r.browser ? <span className="block max-w-[10rem] truncate" title={r.browser}>{r.browser}</span> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'os',
      title: t('loginLog.col.os'),
      render: (r) => (r.os ? <span className="block max-w-[10rem] truncate" title={r.os}>{r.os}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'status', title: t('loginLog.col.status'), render: (r) => <DictTag dict="login_status" value={r.status} /> },
    {
      key: 'failReason',
      title: t('loginLog.col.reason'),
      // 未映射的原因会把原始错误串直接显示出来——截断 + 悬停看全文
      render: (r) => {
        if (!r.failReason) return <span className="text-muted-foreground">-</span>
        const text = t(`loginLog.reason.${r.failReason}`, { defaultValue: r.failReason })
        return (
          <span className="block max-w-xs truncate" title={text}>
            {text}
          </span>
        )
      },
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
        <OptionPicker
          title={t('loginLog.filter.type')}
          value={draft.loginType}
          onChange={(v) => setDraft({ ...draft, loginType: v })}
          options={LOGIN_TYPES.map((k) => ({ value: k, label: t(`loginLog.type.${k}`) }))}
          className="md:w-44"
          placeholder={t('loginLog.filter.type')}
        />
        <DictSelect dict="login_status" multiple value={draft.status} onChange={(v) => setDraft({ ...draft, status: v })} aria-label={t('loginLog.filter.status')} className="md:w-40" placeholder={t('loginLog.filter.status')} />
        <DateRangePicker value={draft.range} onChange={(v) => setDraft({ ...draft, range: v })} ariaLabel={t('loginLog.filter.time')} className="md:w-64" placeholder={t('loginLog.filter.time')} />
      </FilterBar>
      <DataTable<LoginLog>
        toolbar={
          <>
            <Can perm="loginLog:export">
              <Button type="button" variant="outline" onClick={() => void onExport()}>
                <Download className="me-2 size-4" aria-hidden="true" />
                {t('loginLog.action.export')}
              </Button>
            </Can>
            <Can perm="loginLog:delete">
              <Button type="button" variant="outline" onClick={() => setCleanOpen(true)}>
                <Eraser className="me-2 size-4" aria-hidden="true" />
                {t('loginLog.clean.title')}
              </Button>
            </Can>
          </>
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
      <ResponsiveDialog
        open={cleanOpen}
        onOpenChange={setCleanOpen}
        title={t('loginLog.clean.title')}
        description={t('loginLog.clean.description')}
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
          <Label>{t('loginLog.clean.range')}</Label>
          <Select value={cleanDays} onValueChange={setCleanDays}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CLEAN_OPTIONS.map((d) => (
                <SelectItem key={d} value={d}>
                  {t(`loginLog.clean.${cleanKey[d]}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  )
}
