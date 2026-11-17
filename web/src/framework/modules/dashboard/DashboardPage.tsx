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
  const { isError } = useDashboardStats()
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
      {/* 页头照抄 shadcn 教育仪表盘：左侧标题一行 + muted 副行，右侧小按钮，无装饰底板 */}
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="min-w-0 space-y-1">
          <h1 className="text-2xl tracking-tight md:text-3xl">
            {t('dashboard.welcome', { greeting: t(`dashboard.greeting.${greetingKey(now.getHours())}`), name })}
          </h1>
          <p className="text-sm text-muted-foreground">{t('dashboard.welcomeSub', { date: dateText })}</p>
        </div>
        {!editing ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="lg:w-fit"
            aria-label={t('dashboard.layout.edit')}
            disabled={layoutQuery.isLoading}
            onClick={() => setEditing(true)}
          >
            <LayoutGrid className="size-4" aria-hidden="true" />
            {t('dashboard.layout.edit')}
          </Button>
        ) : null}
      </div>

      {isError ? (
        <p role="alert" className="text-sm text-destructive">
          {t('dashboard.loadFailed')}
        </p>
      ) : null}

      {editing ? (
        <LayoutEditor cards={cards} base={layout} saving={save.isPending || reset.isPending} onSave={onSave} onExit={() => setEditing(false)} onReset={onReset} />
      ) : (
        <>
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
