import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { useUnreadCount } from './api'
import { MessageTab } from './MessageTab'
import { NoticeTab } from './NoticeTab'

function CountDot({ n }: { n: number }) {
  if (!n) return null
  return (
    <span className="num inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-destructive/10 px-1.5 text-[11px] font-semibold text-destructive">
      {n > 99 ? '99+' : n}
    </span>
  )
}

export default function MessagePage() {
  const { t } = useTranslation()
  const { data } = useUnreadCount()

  return (
    <PageContainer>
      <PageHeader title={t('message.title')} description={t('message.description')} />
      <Tabs defaultValue="messages" className="flex flex-col gap-4">
        <TabsList className="w-full justify-start md:w-fit">
          <TabsTrigger value="messages" className="flex-1 md:flex-none">
            {t('message.tabMessages')}
            <CountDot n={data?.messages ?? 0} />
          </TabsTrigger>
          <TabsTrigger value="notices" className="flex-1 md:flex-none">
            {t('message.tabNotices')}
            <CountDot n={data?.notices ?? 0} />
          </TabsTrigger>
        </TabsList>
        <TabsContent value="messages" className="mt-0">
          <MessageTab unread={data?.messages ?? 0} />
        </TabsContent>
        <TabsContent value="notices" className="mt-0">
          <NoticeTab />
        </TabsContent>
      </Tabs>
    </PageContainer>
  )
}
