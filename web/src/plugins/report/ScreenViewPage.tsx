import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Maximize2, Minimize2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { EmptyState } from '@/framework/components/EmptyState'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { useScreen } from './api'
import { ScreenCanvas } from './ScreenCanvas'
import { builtinText } from './builtinText'

/**
 * 数据大屏展示页：跟随应用当前主题 + 全屏。
 * 颜色全部走语义 token，不写硬编码颜色，也不在容器上强制 dark，亮/暗与后台设置一致。
 * 全屏 = 浏览器 Fullscreen API（尽力而为）+ 固定铺满的覆盖层兜底（API 不可用或被拒绝时仍然铺满视口）。
 * 自动刷新：间隔来自大屏配置，页面不在前台（切走标签 / 浏览器标签页隐藏）时暂停，回到前台立即补刷一次。
 */
export default function ScreenViewPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [params] = useSearchParams()
  const id = params.get('id') ?? undefined
  const { data: screen, isLoading, isError } = useScreen(id, 'view')
  const boxRef = useRef<HTMLDivElement>(null)
  const [full, setFull] = useState(false)

  useEffect(() => {
    const onChange = () => setFull(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])
  useEffect(() => {
    if (!full) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && void exit()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const enter = useCallback(async () => {
    setFull(true)
    try {
      await boxRef.current?.requestFullscreen?.()
    } catch {
      // 浏览器拒绝时保留覆盖层模式
    }
  }, [])
  async function exit() {
    setFull(false)
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => undefined)
  }

  if (!id || isError) return <EmptyState title={t('plugin_report.screen.notFound')} action={<Button onClick={() => navigate(-1)}>{t('common.back')}</Button>} />
  if (isLoading || !screen) return <Skeleton className="m-4 h-96" />

  const refreshSec = screen.config.refreshSec
  return (
    <div
      ref={boxRef}
      className={cn('bg-background text-foreground', full ? 'fixed inset-0 z-50 overflow-auto p-4' : 'min-h-full p-3 md:p-5')}
      data-testid="screen-root"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {!full ? (
          <Button variant="ghost" size="icon" aria-label={t('common.back')} onClick={() => navigate(-1)}>
            <ArrowLeft aria-hidden="true" />
          </Button>
        ) : null}
        <h1 className="min-w-0 flex-1 truncate text-lg font-semibold tracking-tight md:text-2xl">{builtinText(t, screen.config.title || screen.name)}</h1>
        <span className="hidden text-xs text-muted-foreground md:inline">
          {refreshSec > 0 ? t('plugin_report.screen.autoRefresh', { sec: refreshSec }) : t('plugin_report.screen.noRefresh')}
        </span>
        <Button variant="ghost" size="icon" aria-label={t('common.refresh')} onClick={() => void qc.invalidateQueries({ queryKey: ['plugin_report', 'result', 'screen', screen.id] })}>
          <RefreshCw aria-hidden="true" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => void (full ? exit() : enter())}>
          {full ? <Minimize2 aria-hidden="true" /> : <Maximize2 aria-hidden="true" />}
          {full ? t('plugin_report.screen.exitFull') : t('plugin_report.screen.enterFull')}
        </Button>
      </div>
      {screen.config.items.length === 0 ? <EmptyState title={t('plugin_report.screen.emptyCanvas')} /> : <ScreenCanvas screen={screen} refreshMs={refreshSec * 1000} />}
    </div>
  )
}
