import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Can } from '@/framework/auth/Can'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { usePermission } from '@/framework/auth/usePermission'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { JobsTab } from './JobsTab'
import { LogsTab } from './LogsTab'

export default function CronPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const [tab, setTab] = useState('jobs')
  const [logJobId, setLogJobId] = useState<string | undefined>()

  return (
    <PageContainer>
      <PageHeader title={t('plugin_cron.title')} description={t('plugin_cron.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="jobs">{t('plugin_cron.tabs.jobs')}</TabsTrigger>
          {has('cron:log') ? <TabsTrigger value="logs">{t('plugin_cron.tabs.logs')}</TabsTrigger> : null}
        </TabsList>
        <TabsContent value="jobs" className="m-0">
          <JobsTab
            onViewLogs={(j) => {
              setLogJobId(j.id)
              setTab('logs')
            }}
          />
        </TabsContent>
        <Can perm="cron:log">
          <TabsContent value="logs" className="m-0">
            <LogsTab jobId={logJobId} onJobChange={setLogJobId} />
          </TabsContent>
        </Can>
      </Tabs>
    </PageContainer>
  )
}
