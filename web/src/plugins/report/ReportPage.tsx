import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { ChartsTab } from './ChartsTab'
import { DatasetsTab } from './DatasetsTab'
import { ReportsTab } from './ReportsTab'
import { ScreensTab } from './ScreensTab'

/**
 * 页签进 URL（?tab=）：刷新、浏览器后退、把「图表」页签发给同事都落在正确的位置。
 * 无该页签权限的深链回退到第一个可见页签，与设置页同口径。
 */
export default function ReportPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const [params, setParams] = useSearchParams()
  const tabs = [
    { key: 'reports', perm: 'report:view', node: <ReportsTab /> },
    { key: 'screens', perm: 'report:view', node: <ScreensTab /> },
    { key: 'charts', perm: 'report:chart', node: <ChartsTab /> },
    { key: 'datasets', perm: 'report:dataset', node: <DatasetsTab /> },
  ].filter((x) => has(x.perm))
  const visible = tabs.map((x) => x.key)
  const wanted = params.get('tab')
  const tab = visible.find((k) => k === wanted) ?? visible[0] ?? 'reports'
  const setTab = (k: string) => setParams({ tab: k }, { replace: true })

  return (
    <PageContainer>
      <PageHeader title={t('plugin_report.title')} description={t('plugin_report.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList className="w-fit max-w-full overflow-x-auto">
          {tabs.map((x) => (
            <TabsTrigger key={x.key} value={x.key}>
              {t(`plugin_report.tabs.${x.key}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {tabs.map((x) => (
          <TabsContent key={x.key} value={x.key} className="m-0">
            {x.node}
          </TabsContent>
        ))}
      </Tabs>
    </PageContainer>
  )
}
