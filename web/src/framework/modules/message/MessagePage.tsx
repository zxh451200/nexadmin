import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { cn } from '@/lib/utils'
import { useUnreadCount } from './api'
import { MessageTab } from './MessageTab'
import { NoticeTab } from './NoticeTab'

// 选中态 tab 是品牌色胶囊，徽标必须跟着反色，否则深红字压在蓝底上看不清。
function CountDot({ n, active }: { n: number; active: boolean }) {
  if (!n) return null
  return (
    <span
      className={cn(
        'num inline-flex h-5 min-w-5 items-center justify-center rounded-full px-1.5 text-[11px] font-semibold',
        active ? 'bg-primary-foreground/20 text-primary-foreground' : 'bg-destructive/10 text-destructive',
      )}
    >
      {n > 99 ? '99+' : n}
    </span>
  )
}

export default function MessagePage() {
  const { t } = useTranslation()
  const { data } = useUnreadCount()
  // 顶栏铃铛点公告会带 ?tab=notices 进来；keep-alive 复用时也要跟着 URL 切。
  const [params] = useSearchParams()
  const fromUrl = params.get('tab') === 'notices' ? 'notices' : 'messages'
  const [tab, setTab] = useState(fromUrl)
  useEffect(() => setTab(fromUrl), [fromUrl])

  return (
    <PageContainer>
      <PageHeader title={t('message.title')} description={t('message.description')} />
      <Tabs value={tab} onValueChange={setTab} className="flex flex-col gap-4">
        <TabsList className="w-full justify-start md:w-fit">
          <TabsTrigger value="messages" className="flex-1 md:flex-none">
            {t('message.tabMessages')}
            <CountDot n={data?.messages ?? 0} active={tab === 'messages'} />
          </TabsTrigger>
          <TabsTrigger value="notices" className="flex-1 md:flex-none">
            {t('message.tabNotices')}
            <CountDot n={data?.notices ?? 0} active={tab === 'notices'} />
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
