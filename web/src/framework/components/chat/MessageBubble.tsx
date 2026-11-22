import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface ChatBubbleProps {
  /** 气泡靠哪一侧：end = 自己（右侧），start = 对方（左侧） */
  side: 'start' | 'end'
  /** 配色：default=对方（muted）；primary=自己（品牌色）；warning=内部备注；system=系统回复（灰字） */
  tone?: 'default' | 'primary' | 'warning' | 'system'
  /** meta 行内容：时间 · 已读等轻信息，渲染在气泡下方（muted 小字），不占气泡内部高度 */
  meta?: ReactNode
  /** meta 行的前置徽标（如「内部备注」） */
  badge?: ReactNode
  /** 气泡正文（文本 / 附件 / 结构化卡片） */
  children?: ReactNode
  className?: string
}

/**
 * 会话气泡：工单中心与在线客服的对话流共用。
 * 工单中心在用它的地方：客服回复（primary）、客户回复（default）、内部备注（warning）、系统回复（system）；
 * 在线客服在用它的地方：坐席消息（primary）、访客消息（default）。meta/正文等差异都从 props 进来。
 *
 * meta 渲染在气泡下方而不是气泡内部：塞进 primary 气泡会变成「紫底白字的时间戳」，
 * 读着像正文还把气泡撑高一截；外部 muted 小字才是行业常态（微信/Intercom 同款）。
 * 传 className 覆盖底色与内边距可把结构化卡片做成「卡片即气泡」（无外层灰盒）。
 */
export function ChatBubble({ side, tone = 'default', meta, badge, children, className }: ChatBubbleProps) {
  return (
    <div className={cn('flex flex-col', side === 'end' ? 'items-end' : 'items-start')}>
      <div
        className={cn(
          // overflow-wrap:anywhere：长 URL/无空格长串在气泡内折行，不顶破 75% 气泡。
          // rounded-2xl：大气泡圆角是 Studio Chat 一类客服台的观感基准，工单对话流同步受益
          'max-w-[75%] rounded-2xl px-3.5 py-2.5 text-sm [overflow-wrap:anywhere]',
          tone === 'primary'
            ? 'bg-primary text-primary-foreground'
            : tone === 'warning'
              ? 'border border-warning/40 bg-warning/10 text-foreground'
              : tone === 'system'
                ? 'bg-muted text-muted-foreground'
                : 'bg-muted text-foreground',
          className,
        )}
      >
        {children}
      </div>
      {meta || badge ? (
        <div className="mt-0.5 flex items-center gap-1 text-[11px] text-muted-foreground">
          {badge}
          {meta}
        </div>
      ) : null}
    </div>
  )
}

/** 系统提示条：居中的小胶囊（转派/关闭/合并等流程事件），两个会话流同一形态。 */
export function SystemPill({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex justify-center py-1', className)}>
      <span className="max-w-[85%] rounded-full bg-muted px-2.5 py-1 text-center text-[11px] text-muted-foreground [overflow-wrap:anywhere]">
        {children}
      </span>
    </div>
  )
}
