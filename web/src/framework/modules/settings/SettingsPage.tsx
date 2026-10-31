import { Suspense, useEffect, useMemo } from 'react'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { usePermission } from '@/framework/auth/usePermission'
import { usePersistedSet } from '@/framework/hooks/use-persisted-set'
import { allRoutes } from '@/framework/router/discover'
import type { SettingsGroupKey } from '@/framework/router/types'
import { useActivePlugins } from '@/framework/modules/plugin/active'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { useSettings } from './api'
import { AuthSection } from './AuthSection'
import { NotifySection } from './NotifySection'
import { AiSection, CacheSection, SecuritySection, SiteSection, SmsSection, StorageSection } from './Sections'
import { ParamsSection } from '@/framework/modules/config/SysConfigPage'
import { LanguageSection } from '@/framework/modules/language/LanguagePage'

/**
 * 内置分区：分组即左侧导航的层级，key 同时是 ?tab= 的取值；perm 用于跨模块分区。
 * 路由里声明了 settings 元数据的页面（LDAP、单点登录、地图等）会自动追加到对应分组，
 * 组件与权限由路由提供，这里不逐项硬编码。
 */
const GROUPS: ReadonlyArray<{ key: SettingsGroupKey; items: ReadonlyArray<{ key: string; perm?: string }> }> = [
  { key: 'site', items: [{ key: 'site' }] },
  { key: 'security', items: [{ key: 'security' }, { key: 'auth' }] },
  { key: 'integration', items: [{ key: 'storage' }, { key: 'ai' }, { key: 'sms' }, { key: 'notify' }] },
  {
    key: 'advanced',
    items: [
      { key: 'cache' },
      { key: 'language', perm: 'language:list' },
      { key: 'params', perm: 'config:list' },
    ],
  },
]

export default function SettingsPage() {
  const { t } = useTranslation()
  const { has } = usePermission()
  const { data, isLoading } = useSettings()
  const [params, setParams] = useSearchParams()
  const { data: activePlugins } = useActivePlugins()
  const activeSet = useMemo(() => (activePlugins ? new Set(activePlugins) : null), [activePlugins])

  // 没有该分区权限的项直接不出现；深链指向无权分区时回退到第一个可见分区。
  const groups = useMemo(() => {
    // 路由声明的分区：插件未启用（授权失效 / 被停用）时不出现，旧后端问不到时保持原样。
    const extra = allRoutes.filter((r) => r.settings && (!r.plugin || !activeSet || activeSet.has(r.plugin)))
    return GROUPS.map((g) => ({
      key: g.key,
      items: [
        ...g.items.map((it) => ({ key: it.key, title: `settings.tabs.${it.key}`, perm: it.perm, order: 0 })),
        ...extra
          .filter((r) => r.settings!.group === g.key)
          .map((r) => ({ key: r.settings!.key, title: r.settings!.title, perm: r.perm, order: r.settings!.order ?? 100 }))
          .sort((a, b) => a.order - b.order),
      ].filter((it) => has(it.perm)),
    })).filter((g) => g.items.length > 0)
  }, [activeSet, has])
  const visible = groups.flatMap((g) => g.items.map((it) => it.key))
  const wanted = params.get('tab')
  const active = visible.find((k) => k === wanted) ?? visible[0] ?? 'site'
  const go = (k: string) => setParams({ tab: k }, { replace: true })
  const Extra = allRoutes.find((r) => r.settings?.key === active)?.settings?.component

  // 分组折叠：存的是「收起的组」，默认全展开，刷新后保持；
  // 点到某个分区时自动展开它所在的分组（并写回记忆），收起的操作留给用户自己撤销。
  const { has: isFolded, toggle: toggleGroup, remove: unfold } = usePersistedSet('nex-settings-groups-folded')
  const isOpen = (key: string) => !isFolded(key)
  useEffect(() => {
    const owner = groups.find((g) => g.items.some((it) => it.key === active))
    if (owner) unfold(owner.key)
    // 分区之间切换时回到顶部：路由没变，AppLayout 不会重置滚动位置
    document.getElementById('main-content')?.scrollTo({ top: 0 })
  }, [active, groups, unfold])

  return (
    <PageContainer>
      <PageHeader title={t('settings.title')} description={t('settings.description')} />
      {isLoading || !data ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <div className="grid gap-6 md:grid-cols-[180px_minmax(0,1fr)]">
          {/* 窄屏用下拉，宽屏用左侧分组导航：分区再多也只是导航变长，内容区不动 */}
          <div className="md:hidden">
            <Select value={active} onValueChange={go}>
              <SelectTrigger aria-label={t('settings.sectionNav')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {groups.map((g) =>
                  g.items.map((it) => (
                    <SelectItem key={it.key} value={it.key}>
                      {t(`settings.groups.${g.key}`)} · {t(it.title)}
                    </SelectItem>
                  )),
                )}
              </SelectContent>
            </Select>
          </div>
          <nav aria-label={t('settings.sectionNav')} className="hidden flex-col gap-4 md:flex">
            {groups.map((g) =>
              g.items.length ? (
                <div key={g.key} className="flex flex-col gap-1">
                  {/* 组标题可折叠：分区多了以后左栏只会变长，用户能自己收起来；层级靠缩进 + 竖线表达 */}
                  <button
                    type="button"
                    aria-expanded={isOpen(g.key)}
                    aria-controls={`settings-group-${g.key}`}
                    onClick={() => toggleGroup(g.key)}
                    className="flex items-center justify-between gap-2 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
                  >
                    <span>{t(`settings.groups.${g.key}`)}</span>
                    <ChevronDown
                      aria-hidden="true"
                      className={cn('size-3.5 transition-transform', !isOpen(g.key) && '-rotate-90')}
                    />
                  </button>
                  {isOpen(g.key) ? (
                    <div id={`settings-group-${g.key}`} className="ms-3 flex flex-col gap-1 border-s border-border ps-2">
                      {g.items.map((it) => (
                        <button
                          key={it.key}
                          type="button"
                          aria-current={active === it.key ? 'page' : undefined}
                          onClick={() => go(it.key)}
                          className={cn(
                            'rounded-md px-2 py-1.5 text-start text-sm transition-colors',
                            // 选中态跟全站 tab 同一套语言（品牌色系），但左导航不填满，用淡底 + 品牌色字
                            active === it.key
                              ? 'bg-primary/10 font-medium text-primary'
                              : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
                          )}
                        >
                          {t(it.title)}
                        </button>
                      ))}
                    </div>
                  ) : null}
                </div>
              ) : null,
            )}
          </nav>
          <div className="min-w-0">
            {Extra ? (
              <Suspense fallback={<Skeleton className="h-64 w-full" />}>
                <Extra />
              </Suspense>
            ) : null}
            {active === 'site' ? <SiteSection value={data.site} /> : null}
            {active === 'security' ? <SecuritySection value={data.security} /> : null}
            {active === 'cache' ? <CacheSection value={data.cache} /> : null}
            {active === 'storage' ? <StorageSection value={data.storage} /> : null}
            {active === 'ai' ? <AiSection value={data.ai} /> : null}
            {active === 'sms' ? <SmsSection value={data.sms} /> : null}
            {active === 'notify' ? <NotifySection value={data.notify} /> : null}
            {active === 'auth' ? <AuthSection value={data.auth} /> : null}
            {active === 'language' ? <LanguageSection /> : null}
            {active === 'params' ? <ParamsSection /> : null}
          </div>
        </div>
      )}
    </PageContainer>
  )
}
