import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * 坐席队列的一行：勾选框、选中底色、悬停底色统一收在这里，IM 会话与工单队列共用一套观感。
 * 两条约束来自踩过的坑：
 * - 勾选框必须是按钮的兄弟节点（button 里套 checkbox 既是无效 HTML，点勾选还会连带选中整行）；
 * - 底色作用域要包住勾选框，否则选中行看着像「勾选框旁边飘着一块色块」。
 */
export function QueueRow({
  selected,
  onSelect,
  checkbox,
  children,
  footer,
  accent,
}: {
  selected: boolean
  onSelect: () => void
  /** 左侧勾选框；没有批量选择能力时传 null */
  checkbox?: ReactNode
  children: ReactNode
  /** 行内按钮之外的内容（如「已并入…」跳转），不参与点击选中 */
  footer?: ReactNode
  /** 左缘主题色竖条（如未读标记）：绝对定位于行左缘，不参与布局 */
  accent?: boolean
}) {
  return (
    // 选中态用 primary 色底 + 内描边：accent/60 在浅色下和悬停几乎一个色，扫一眼分不出选没选中
    <div
      className={cn(
        'relative rounded-md transition-colors',
        selected ? 'bg-primary/10 ring-1 ring-inset ring-primary/30 hover:bg-primary/15' : 'hover:bg-accent/40',
      )}
    >
      {accent ? <span aria-hidden="true" className="absolute inset-y-1 start-0 w-[3px] rounded-full bg-primary" /> : null}
      {/* 勾选列整行等高：勾选框在列内垂直居中，热区由调用方的 label 纵向撑满，
          不再用负边距外扩——负边距会把不可见点击区压到相邻行和行按钮上，又重叠又误触 */}
      <div className="flex items-stretch gap-1.5">
        {checkbox ? <div className="flex shrink-0 items-stretch ps-2">{checkbox}</div> : null}
        <button
          type="button"
          onClick={onSelect}
          aria-pressed={selected}
          className={cn('min-w-0 flex-1 py-1.5 text-start', checkbox ? 'pe-2.5' : 'ps-0.5 pe-2.5')}
        >
          {children}
        </button>
      </div>
      {footer ? <div className={cn('pb-1', checkbox ? 'ps-9 pe-2.5' : 'px-2.5')}>{footer}</div> : null}
    </div>
  )
}

const TONE: Record<'neutral' | 'success' | 'warning' | 'destructive' | 'info', string> = {
  neutral: 'bg-muted-foreground/50',
  success: 'bg-success',
  warning: 'bg-warning',
  destructive: 'bg-destructive',
  info: 'bg-primary',
}

/**
 * 状态用「圆点 + 灰字」，不再用实心药丸：一行里同时出现状态、优先级、超时三枚药丸时，
 * 颜色互相竞争，眼睛没有落点。颜色交给圆点，文字保持中性，扫读时看点的颜色就够。
 */
export function QueueStatus({ tone = 'neutral', children }: { tone?: keyof typeof TONE; children: ReactNode }) {
  return (
    // 外层不 shrink-0：长状态词（字典驱动）会把标题/摘要挤没了；圆点仍固定
    <span className="inline-flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
      <span className={cn('size-1.5 shrink-0 rounded-full', TONE[tone])} aria-hidden="true" />
      <span className="min-w-0 max-w-24 truncate">{children}</span>
    </span>
  )
}

/** 会话头像的渐变色板：按 seed 稳定取色，同一会话永远同一个颜色，扫一眼能区分不同会话 */
const AVATAR_GRADIENTS = [
  'from-violet-500 to-indigo-500',
  'from-sky-500 to-cyan-400',
  'from-emerald-500 to-teal-400',
  'from-amber-500 to-orange-400',
  'from-rose-500 to-pink-400',
  'from-fuchsia-500 to-purple-500',
] as const

/** 行首头像位：品牌渐变小方块，和主按钮、Logo 同一套形状与配色语言；给 seed 时按会话稳定着色 */
export function QueueAvatar({ children, seed }: { children: ReactNode; seed?: string }) {
  let h = 0
  if (seed) {
    for (let i = 0; i < seed.length; i++) {
      h = (h * 31 + seed.charCodeAt(i)) >>> 0
    }
  }
  const g = seed ? AVATAR_GRADIENTS[h % AVATAR_GRADIENTS.length] : null
  return (
    <span
      className={cn(
        'flex size-7 shrink-0 items-center justify-center rounded-md bg-gradient-to-br text-[11px] font-semibold text-white',
        g ?? 'bg-brand-gradient text-primary-foreground',
      )}
    >
      {children}
    </span>
  )
}
