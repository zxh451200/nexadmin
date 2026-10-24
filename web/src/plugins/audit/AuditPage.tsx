import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { LogsTab } from './LogsTab'
import { ScopeTab } from './ScopeTab'

export default function AuditPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  return (
    <PageContainer>
      <PageHeader title={t('plugin_audit.title')} description={t('plugin_audit.description')} />
      <Tabs defaultValue="logs" className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="logs">{t('plugin_audit.tabs.logs')}</TabsTrigger>
          {has('audit:config') ? <TabsTrigger value="scope">{t('plugin_audit.tabs.scope')}</TabsTrigger> : null}
        </TabsList>
        <TabsContent value="logs" className="m-0">
          <LogsTab />
        </TabsContent>
        {has('audit:config') ? (
          <TabsContent value="scope" className="m-0">
            <ScopeTab />
          </TabsContent>
        ) : null}
      </Tabs>
    </PageContainer>
  )
}
