import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { CallbacksTab } from './CallbacksTab'
import { ChannelsTab } from './ChannelsTab'
import { OrdersTab } from './OrdersTab'
import { RefundsTab } from './RefundsTab'

export default function PaymentPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('orders')

  return (
    <PageContainer>
      <PageHeader title={t('plugin_payment.title')} description={t('plugin_payment.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="orders">{t('plugin_payment.tabs.orders')}</TabsTrigger>
          <TabsTrigger value="refunds">{t('plugin_payment.tabs.refunds')}</TabsTrigger>
          <TabsTrigger value="callbacks">{t('plugin_payment.tabs.callbacks')}</TabsTrigger>
          <TabsTrigger value="channels">{t('plugin_payment.tabs.channels')}</TabsTrigger>
        </TabsList>
        <TabsContent value="orders" className="m-0">
          <OrdersTab />
        </TabsContent>
        <TabsContent value="refunds" className="m-0">
          <RefundsTab />
        </TabsContent>
        <TabsContent value="callbacks" className="m-0">
          <CallbacksTab />
        </TabsContent>
        <TabsContent value="channels" className="m-0">
          <ChannelsTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
