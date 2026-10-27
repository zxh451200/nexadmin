import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { ModelsTab } from './ModelsTab'
import { UsageTab } from './UsageTab'

export default function AiGatewayPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('models')

  return (
    <PageContainer>
      <PageHeader title={t('plugin_aigateway.title')} description={t('plugin_aigateway.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="models">{t('plugin_aigateway.tabs.models')}</TabsTrigger>
          <TabsTrigger value="usage">{t('plugin_aigateway.tabs.usage')}</TabsTrigger>
        </TabsList>
        <TabsContent value="models" className="m-0">
          <ModelsTab />
        </TabsContent>
        <TabsContent value="usage" className="m-0">
          <UsageTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
