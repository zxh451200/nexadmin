import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'

/**
 * 批量操作条：不占视觉层级地悬浮，勾选不再把表格下推。三种落点：
 * - viewport（默认）：portal 到 body，fixed 在视口底边居中。长短页面行为一致——
 *   sticky 只在宿主容器比视口高时才贴底，宫格这类短内容页会停在自然位置（文件管理踩过）。
 *   单表格页用它。
 * - sticky：渲染在调用处文档流里，bottom-3 粘性贴底。主从双栏页（字典管理）用——
 *   两个表格都往视口 portal 会叠在同一点互相遮挡；sticky 横向只占自己那栏，
 *   容器比视口长时也始终贴在可视区底边（absolute 会沉到容器末尾看不见）。
 * - absolute：钉在最近 relative 祖先的底边。给定高卡片（工单队列）用——
 *   卡片内部有滚动列表，sticky 的文档流位置会挤压列表，又回到「下推」老问题。
 */
export function BatchBar({
  count,
  onClear,
  children,
  mode = 'viewport',
}: {
  count: number
  onClear: () => void
  children: ReactNode
  mode?: 'viewport' | 'sticky' | 'absolute'
}) {
  const { t } = useTranslation()
  const bar = (
    <div
      className={cn(
        'z-30',
        mode === 'sticky' && 'sticky bottom-3 pointer-events-none',
        mode === 'absolute' && 'pointer-events-none absolute inset-x-0 bottom-3',
        mode === 'viewport' && 'pointer-events-none fixed inset-x-0 bottom-4',
      )}
      role="region"
      aria-label={t('dataTable.batchActions')}
    >
      <div className="flex justify-center px-2">
        <div className="pointer-events-auto flex max-w-full flex-wrap items-center gap-1 rounded-2xl border border-border bg-popover px-2 py-1.5 text-popover-foreground shadow-lg overlay-panel animate-in fade-in-0 slide-in-from-bottom-1 duration-(--motion-fast) ease-(--ease-standard) motion-reduce:animate-none">
          <span className="num ps-2 text-sm text-foreground" aria-live="polite">
            {t('dataTable.selectedCount', { count })}
          </span>
          <Button type="button" variant="ghost" size="sm" onClick={onClear}>
            {t('common.clear')}
          </Button>
          <div className="flex flex-wrap items-center gap-2 pe-0.5">{children}</div>
        </div>
      </div>
    </div>
  )
  return mode === 'viewport' ? createPortal(bar, document.body) : bar
}
