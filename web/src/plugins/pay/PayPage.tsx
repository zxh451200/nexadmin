import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { AuthorizationsTab } from './AuthorizationsTab'
import { CallbacksTab } from './CallbacksTab'
import { ChannelsTab } from './ChannelsTab'
import { OrdersTab } from './OrdersTab'
import { ReconciliationTab } from './ReconciliationTab'
import { RefundsTab } from './RefundsTab'
import { SettlementTab } from './SettlementTab'

const TABS = ['orders', 'refunds', 'callbacks', 'channels', 'authorizations', 'reconcile', 'settle'] as const
type Tab = (typeof TABS)[number]

export default function PayPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const [params, setParams] = useSearchParams()
  const canSettle = has('pay:settle')
  const raw = params.get('tab')
  const parsed = TABS.includes(raw as Tab) ? (raw as Tab) : 'orders'
  const tab: Tab = parsed === 'reconcile' || parsed === 'settle' ? (canSettle ? parsed : 'orders') : parsed

  const setTab = (v: string) => {
    const next = new URLSearchParams(params)
    if (v === 'orders') next.delete('tab')
    else next.set('tab', v)
    if (v !== 'orders') next.delete('view')
    setParams(next, { replace: true })
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_pay.title')} description={t('plugin_pay.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList className="w-fit max-w-full overflow-x-auto">
          <TabsTrigger value="orders">{t('plugin_pay.tabs.orders')}</TabsTrigger>
          <TabsTrigger value="refunds">{t('plugin_pay.tabs.refunds')}</TabsTrigger>
          <TabsTrigger value="callbacks">{t('plugin_pay.tabs.callbacks')}</TabsTrigger>
          <TabsTrigger value="channels">{t('plugin_pay.tabs.channels')}</TabsTrigger>
          <TabsTrigger value="authorizations">{t('plugin_pay.tabs.authorizations')}</TabsTrigger>
          {canSettle ? <TabsTrigger value="reconcile">{t('plugin_pay.tabs.reconcile')}</TabsTrigger> : null}
          {canSettle ? <TabsTrigger value="settle">{t('plugin_pay.tabs.settle')}</TabsTrigger> : null}
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
        <TabsContent value="authorizations" className="m-0">
          <AuthorizationsTab />
        </TabsContent>
        {canSettle ? (
          <TabsContent value="reconcile" className="m-0">
            <ReconciliationTab />
          </TabsContent>
        ) : null}
        {canSettle ? (
          <TabsContent value="settle" className="m-0">
            <SettlementTab />
          </TabsContent>
        ) : null}
      </Tabs>
    </PageContainer>
  )
}
