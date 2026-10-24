import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { BasicTab } from './BasicTab'
import { BindingsTab } from './BindingsTab'
import { PasswordTab } from './PasswordTab'
import { PreferencesTab } from './PreferencesTab'
import { SessionsTab } from './SessionsTab'
import { TokensTab } from './TokensTab'

const TABS = [
  { key: 'basic', Comp: BasicTab },
  { key: 'password', Comp: PasswordTab },
  { key: 'bindings', Comp: BindingsTab },
  { key: 'sessions', Comp: SessionsTab },
  { key: 'tokens', Comp: TokensTab },
  { key: 'preferences', Comp: PreferencesTab },
] as const

export default function ProfilePage() {
  const { t } = useTranslation()
  const [params] = useSearchParams() // 第三方绑定完成后以 ?tab=bindings 回到「账号绑定」
  const tab = TABS.find((x) => x.key === params.get('tab'))?.key ?? 'basic'
  return (
    <PageContainer>
      <PageHeader title={t('profile.menu')} description={t('profile.description')} />
      <Tabs defaultValue={tab} className="w-full">
        <TabsList className="h-auto w-full flex-wrap overflow-visible md:w-fit">
          {TABS.map(({ key }) => (
            <TabsTrigger key={key} value={key} className="flex-1 md:flex-none">
              {t(`profile.tab.${key}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {TABS.map(({ key, Comp }) => (
          <TabsContent key={key} value={key} className="mt-4 rounded-lg border border-border bg-card p-4 md:p-6">
            <Comp />
          </TabsContent>
        ))}
      </Tabs>
    </PageContainer>
  )
}
