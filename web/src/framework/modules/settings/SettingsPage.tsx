import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Skeleton } from '@/framework/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { useSettings } from './api'
import { AuthSection } from './AuthSection'
import { NotifySection } from './NotifySection'
import { AiSection, CacheSection, SecuritySection, SiteSection, SmsSection, StorageSection } from './Sections'

const TABS = ['site', 'security', 'cache', 'storage', 'ai', 'sms', 'notify', 'auth'] as const

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useSettings()

  return (
    <PageContainer>
      <PageHeader title={t('settings.title')} description={t('settings.description')} />
      {isLoading || !data ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <Tabs defaultValue="site" className="flex flex-col gap-4">
          <TabsList className="w-full flex-wrap overflow-visible sm:w-fit">
            {TABS.map((k) => (
              <TabsTrigger key={k} value={k} className="flex-1 sm:flex-none">
                {t(`settings.tabs.${k}`)}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="site">
            <SiteSection value={data.site} />
          </TabsContent>
          <TabsContent value="security">
            <SecuritySection value={data.security} />
          </TabsContent>
          <TabsContent value="cache">
            <CacheSection value={data.cache} />
          </TabsContent>
          <TabsContent value="storage">
            <StorageSection value={data.storage} />
          </TabsContent>
          <TabsContent value="ai">
            <AiSection value={data.ai} />
          </TabsContent>
          <TabsContent value="sms">
            <SmsSection value={data.sms} />
          </TabsContent>
          <TabsContent value="notify">
            <NotifySection value={data.notify} />
          </TabsContent>
          <TabsContent value="auth">
            <AuthSection value={data.auth} />
          </TabsContent>
        </Tabs>
      )}
    </PageContainer>
  )
}
