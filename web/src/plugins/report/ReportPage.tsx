import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { ChartsTab } from './ChartsTab'
import { DatasetsTab } from './DatasetsTab'
import { ReportsTab } from './ReportsTab'
import { ScreensTab } from './ScreensTab'

export default function ReportPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const tabs = [
    { key: 'reports', perm: 'report:view', node: <ReportsTab /> },
    { key: 'screens', perm: 'report:view', node: <ScreensTab /> },
    { key: 'charts', perm: 'report:chart', node: <ChartsTab /> },
    { key: 'datasets', perm: 'report:dataset', node: <DatasetsTab /> },
  ].filter((x) => has(x.perm))
  const [tab, setTab] = useState(tabs[0]?.key ?? 'reports')

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
