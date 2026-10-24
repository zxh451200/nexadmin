import { useMemo, useState } from 'react'
import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/framework/components/EmptyState'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { exportReportChart, queryReportChart, useReport, type Chart, type FilterValues } from './api'
import { ChartView } from './ChartView'
import { saveBlob } from './download'
import { ReportFilters } from './ReportFilters'
import { useChartResult } from './useChartResult'

const SPAN: Record<number, string> = {
  1: 'md:col-span-1', 2: 'md:col-span-2', 3: 'md:col-span-3', 4: 'md:col-span-4', 5: 'md:col-span-5', 6: 'md:col-span-6',
  7: 'md:col-span-7', 8: 'md:col-span-8', 9: 'md:col-span-9', 10: 'md:col-span-10', 11: 'md:col-span-11', 12: 'md:col-span-12',
}

function ChartCard({ reportId, chart, values }: { reportId: string; chart?: Chart; values: FilterValues }) {
  const { t } = useTranslation()
  const [exporting, setExporting] = useState(false)
  const q = useChartResult(['report', reportId, chart?.id, values], () => queryReportChart(reportId, chart!.id, values), { enabled: !!chart })
  const onExport = async () => {
    if (!chart) return
    setExporting(true)
    try {
      const { blob, truncated, ext } = await exportReportChart(reportId, chart.id, values)
      saveBlob(blob, `${chart.name}.${ext}`)
      if (truncated) toast.warning(t('plugin_report.report.exportTruncated'))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }
  return (
    <Card className="h-full">
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0 pb-2">
        <CardTitle className="truncate text-base font-medium">{chart?.name ?? t('plugin_report.report.chartDeleted')}</CardTitle>
        {chart?.type === 'table' ? (
          <Button variant="ghost" size="sm" disabled={exporting} onClick={() => void onExport()}>
            <Download aria-hidden="true" />
            {t('plugin_report.report.export')}
          </Button>
        ) : null}
      </CardHeader>
      <CardContent>
        {chart ? (
          <ChartView type={chart.type} result={q.data} config={chart.config} height={chart.type === 'table' ? 320 : 260} loading={q.isLoading} error={q.isError} />
        ) : (
          <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_report.report.chartDeleted')}</p>
        )}
      </CardContent>
    </Card>
  )
}

export function ReportViewer({ reportId }: { reportId: string }) {
  const { t } = useTranslation()
  const { data: report, isLoading } = useReport(reportId, 'view')
  const [values, setValues] = useState<FilterValues>({})
  const groups = useMemo(() => {
    const out: { name: string; items: { chartId: string; span: number }[] }[] = []
    for (const it of report?.config.items ?? []) {
      let g = out.find((x) => x.name === it.group)
      if (!g) out.push((g = { name: it.group, items: [] }))
      g.items.push({ chartId: it.chartId, span: it.span })
    }
    return out
  }, [report])

  if (isLoading) return <Skeleton className="h-64 w-full" />
  if (!report) return <EmptyState title={t('plugin_report.report.notFound')} />

  return (
    <div className="flex flex-col gap-4">
      <ReportFilters reportId={reportId} filters={report.config.filters} values={values} onChange={setValues} />
      {groups.length === 0 ? <EmptyState title={t('plugin_report.report.noCharts')} /> : null}
      {groups.map((g) => (
        <section key={g.name} className="flex flex-col gap-3" aria-label={g.name || undefined}>
          {g.name ? <h2 className="text-sm font-medium text-muted-foreground">{g.name}</h2> : null}
          <div className="grid grid-cols-1 gap-3 md:grid-cols-12">
            {g.items.map((it) => (
              <div key={it.chartId} className={cn('min-w-0', SPAN[it.span] ?? 'md:col-span-12')}>
                <ChartCard reportId={reportId} chart={report.charts?.find((c) => c.id === it.chartId)} values={values} />
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
