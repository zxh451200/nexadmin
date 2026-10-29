import { Suspense, useMemo, useState } from 'react'
import { LayoutGrid } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuth } from '@/framework/auth/useAuth'
import { usePermission } from '@/framework/auth/usePermission'
import { PageContainer } from '@/framework/components/PageHeader'
import { useActivePlugins } from '@/framework/modules/plugin/active'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useDashboardLayout, useDashboardStats, useResetDashboardLayout, useSaveDashboardLayout } from './api'
import { CARD_GRID, CARD_GRID_LOADED, CARD_SPAN } from './card-size'
import { CardBoundary } from './CardBoundary'
import { discoverCards, filterCards } from './discover'
import { LayoutEditor } from './LayoutEditor'
import { mergeLayout } from './layout'
import type { SavedLayout } from './types'

function greetingKey(hour: number) {
  if (hour < 11) return 'morning'
  if (hour < 13) return 'noon'
  if (hour < 18) return 'afternoon'
  return 'evening'
}

export default function DashboardPage() {
  const { t, i18n } = useTranslation()
  const { perms } = usePermission()
  const user = useAuth((s) => s.user)
  const { data, isError } = useDashboardStats()
  const layoutQuery = useDashboardLayout()
  const save = useSaveDashboardLayout()
  const reset = useResetDashboardLayout()
  const [editing, setEditing] = useState(false)
  const activePlugins = useActivePlugins()

  // 当前用户有权限的卡片，按保存的布局（没有则默认）排序、区分显隐；未启用插件的卡片不参与
  const cards = useMemo(
    () => filterCards(perms, discoverCards(activePlugins.isPending ? undefined : (activePlugins.data ?? null))),
    [perms, activePlugins.isPending, activePlugins.data],
  )
  const layout = useMemo(() => mergeLayout(cards, layoutQuery.data), [cards, layoutQuery.data])
  const byId = useMemo(() => new Map(cards.map((c) => [c.id, c])), [cards])
  const hidden = useMemo(() => new Set(layout.hidden), [layout.hidden])
  const visible = layout.order.filter((id) => !hidden.has(id)).flatMap((id) => byId.get(id) ?? [])

  const onSave = async (next: SavedLayout) => {
    try {
      await save.mutateAsync(next)
      toast.success(t('dashboard.layout.saved'))
      setEditing(false)
      return true
    } catch {
      toast.error(t('dashboard.layout.saveFailed')) // 失败保留编辑状态，草稿不丢
      return false
    }
  }

  const onReset = async () => {
    try {
      await reset.mutateAsync()
      toast.success(t('dashboard.layout.resetDone'))
      setEditing(false)
    } catch {
      toast.error(t('dashboard.layout.resetFailed'))
    }
  }

  const now = new Date()
  const name = user?.nickname || user?.username || ''
  const dateText = now.toLocaleDateString(i18n.language, { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <PageContainer>
      <section className="relative overflow-hidden rounded-2xl border bg-card p-5 shadow-sm md:p-8">
        {/* 装饰：点阵 + 一团品牌柔光，亮暗主题自适应，不再整块涂品牌渐变 */}
        <div aria-hidden="true" className="tech-grid pointer-events-none absolute inset-0 opacity-70 [mask-image:linear-gradient(to_bottom_left,black,transparent_70%)]" />
        <div aria-hidden="true" className="pointer-events-none absolute -end-16 -top-24 size-72 rounded-full bg-primary/10 blur-3xl" />
        <div aria-hidden="true" className="pointer-events-none absolute -bottom-24 start-1/3 size-64 rounded-full bg-primary/5 blur-3xl" />
        <div className="relative flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="min-w-0">
            <h1 className="text-xl font-semibold tracking-tight md:text-3xl">
              {t('dashboard.welcome', { greeting: t(`dashboard.greeting.${greetingKey(now.getHours())}`), name })}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground md:text-base">{t('dashboard.welcomeSub', { date: dateText })}</p>
          </div>
          {data ? (
            <div className="flex flex-wrap gap-2">
              <div className="rounded-xl border bg-muted/50 px-4 py-2">
                <p className="text-xs text-muted-foreground">{t('dashboard.stat.todayLogin')}</p>
                <p className="num text-xl font-semibold">{data.todayLogin.value}</p>
              </div>
              <div className="rounded-xl border bg-muted/50 px-4 py-2">
                <p className="text-xs text-muted-foreground">{t('dashboard.stat.unreadMessages')}</p>
                <p className="num text-xl font-semibold">{data.unreadMessages.value}</p>
              </div>
            </div>
          ) : null}
        </div>
      </section>

      {isError ? (
        <p role="alert" className="text-sm text-destructive">
          {t('dashboard.loadFailed')}
        </p>
      ) : null}

      {editing ? (
        <LayoutEditor cards={cards} base={layout} saving={save.isPending || reset.isPending} onSave={onSave} onExit={() => setEditing(false)} onReset={onReset} />
      ) : (
        <>
          <div className="flex justify-end">
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={t('dashboard.layout.edit')}
              title={t('dashboard.layout.edit')}
              disabled={layoutQuery.isLoading}
              onClick={() => setEditing(true)}
            >
              <LayoutGrid className="size-4" aria-hidden="true" />
            </Button>
          </div>
          {layoutQuery.isLoading ? (
            <div className={CARD_GRID}>
              <Skeleton className="h-28 w-full md:col-span-4" />
              <Skeleton className="h-72 w-full md:col-span-2" />
              <Skeleton className="h-72 w-full md:col-span-2" />
            </div>
          ) : (
            <div className={CARD_GRID_LOADED}>
              {visible.map((c) => (
                <div key={c.id} className={`min-w-0 ${CARD_SPAN[c.size]}`}>
                  <CardBoundary>
                    <Suspense fallback={<Skeleton className="h-40 w-full" />}>
                      <c.component />
                    </Suspense>
                  </CardBoundary>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </PageContainer>
  )
}
