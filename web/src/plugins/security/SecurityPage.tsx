import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { IpRulesTab } from './IpRulesTab'
import { LogsTab } from './LogsTab'
import { PolicyTab } from './PolicyTab'
import { TwoFactorTab } from './TwoFactorTab'
import { UsersTab } from './UsersTab'

export default function SecurityPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const tabs = [
    { key: 'twofa', perm: 'security:self', node: <TwoFactorTab /> },
    { key: 'policy', perm: 'security:policy', node: <PolicyTab /> },
    { key: 'users', perm: 'security:user', node: <UsersTab /> },
    { key: 'ip', perm: 'security:ip', node: <IpRulesTab /> },
    { key: 'logs', perm: 'security:log', node: <LogsTab /> },
  ].filter((x) => has(x.perm))

  return (
    <PageContainer>
      <PageHeader title={t('plugin_security.title')} description={t('plugin_security.description')} />
      <Tabs defaultValue="twofa" className="flex flex-col gap-4">
        <TabsList className="h-auto flex-wrap justify-start">
          {tabs.map((x) => (
            <TabsTrigger key={x.key} value={x.key}>
              {t(`plugin_security.tabs.${x.key}`)}
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
