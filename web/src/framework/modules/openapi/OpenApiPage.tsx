import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { AppsTab } from './AppsTab'
import { DeliveriesTab } from './DeliveriesTab'
import { DocsTab } from './DocsTab'
import { WebhooksTab } from './WebhooksTab'

const TABS = [
  { key: 'apps', Comp: AppsTab },
  { key: 'webhooks', Comp: WebhooksTab },
  { key: 'deliveries', Comp: DeliveriesTab },
  { key: 'docs', Comp: DocsTab },
] as const

export default function OpenApiPage() {
  const { t } = useTranslation()
  return (
    <PageContainer>
      <PageHeader title={t('openapi.title')} description={t('openapi.description')} />
      <Tabs defaultValue="apps" className="w-full">
        <TabsList className="h-auto w-full flex-wrap overflow-visible md:w-fit">
          {TABS.map(({ key }) => (
            <TabsTrigger key={key} value={key} className="flex-1 md:flex-none">
              {t(`openapi.tab.${key}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {TABS.map(({ key, Comp }) => (
          <TabsContent key={key} value={key} className="mt-4">
            <Comp />
          </TabsContent>
        ))}
      </Tabs>
    </PageContainer>
  )
}
