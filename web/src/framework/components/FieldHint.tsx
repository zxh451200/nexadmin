import { CircleAlert, CircleHelp } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/framework/ui/tooltip'

// 字段标签旁的悬浮说明，避免长文案铺满表单。
export function FieldHint({ text, icon = 'help' }: { text?: string; icon?: 'help' | 'alert' }) {
  if (!text) return null
  const Icon = icon === 'alert' ? CircleAlert : CircleHelp
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {/* 常见外壳是 <label>，button 属于 interactive content 不能嵌进去；用可聚焦 span，
            全文再补一份 sr-only：Tooltip 只有悬停/聚焦才展开，读屏要无条件拿到说明。 */}
        <span
          tabIndex={0}
          onClick={(e) => e.preventDefault()}
          className="relative ms-1 inline-flex shrink-0 cursor-help rounded-sm text-muted-foreground outline-none transition-colors before:absolute before:-inset-2.5 before:content-[''] hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Icon className={icon === 'alert' ? 'size-3.5 text-warning' : 'size-3.5'} aria-hidden="true" />
          <span className="sr-only">{text}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-normal">
        {text}
      </TooltipContent>
    </Tooltip>
  )
}
