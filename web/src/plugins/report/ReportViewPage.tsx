import { ArrowLeft, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer } from '@/framework/components/PageHeader'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useReport } from './api'
import { ReportViewer } from './ReportViewer'
import { builtinText } from './builtinText'

/**
 * 报表查看页：从报表中心列表进入，给报表整页宽度（图表按 12 栅格铺开，塞进半栏会挤成一列），
 * 同时留一个可收藏、可分享、可挂到监控室大屏的稳定 URL —— refreshSec 的自动刷新才有落点。
 */
export default function ReportViewPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [params] = useSearchParams()
  const id = params.get('id') ?? undefined
  const { data: report, isLoading, isError } = useReport(id, 'view')
  const back = () => navigate('/monitor/report', { replace: true })

  if (!id || isError) return <EmptyState title={t('plugin_report.report.notFound')} action={<Button onClick={back}>{t('common.back')}</Button>} />
  if (isLoading || !report) return <Skeleton className="m-4 h-96" />

  const refreshSec = report.config.refreshSec ?? 0
  const name = builtinText(t, report.name)
  const group = builtinText(t, report.groupName)
  return (
    <PageContainer>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="icon" aria-label={t('common.back')} onClick={back}>
          <ArrowLeft aria-hidden="true" />
        </Button>
        <div className="min-w-0 flex-1">
          {group ? <p className="truncate text-xs text-muted-foreground">{group}</p> : null}
          <h1 className="truncate text-lg font-semibold tracking-tight text-foreground md:text-2xl">{name}</h1>
        </div>
        {refreshSec > 0 ? <span className="hidden text-xs text-muted-foreground md:inline">{t('plugin_report.report.autoRefresh', { sec: refreshSec })}</span> : null}
        <Button
          variant="ghost"
          size="icon"
          aria-label={t('common.refresh')}
          onClick={() => void qc.invalidateQueries({ queryKey: ['plugin_report', 'result', 'report', report.id] })}
        >
          <RefreshCw aria-hidden="true" />
        </Button>
      </div>
      <ReportViewer reportId={report.id} />
    </PageContainer>
  )
}
