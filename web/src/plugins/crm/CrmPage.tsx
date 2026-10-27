import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { CustomersTab } from './CustomersTab'
import { OpportunitiesTab } from './OpportunitiesTab'

export default function CrmPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('customers')

  return (
    <PageContainer>
      <PageHeader title={t('plugin_crm.title')} description={t('plugin_crm.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="customers">{t('plugin_crm.tabs.customers')}</TabsTrigger>
          <TabsTrigger value="opportunities">{t('plugin_crm.tabs.opportunities')}</TabsTrigger>
        </TabsList>
        <TabsContent value="customers" className="m-0">
          <CustomersTab />
        </TabsContent>
        <TabsContent value="opportunities" className="m-0">
          <OpportunitiesTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
