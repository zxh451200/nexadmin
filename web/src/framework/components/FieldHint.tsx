import { CircleHelp } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/framework/ui/tooltip'

/**
 * 字段标签旁的「?」说明：把成段的规格与后果文案收进 Tooltip。
 * 说明整段铺在字段下方时，它和字段标签同为 12px 灰字，一屏读下来分不出主次；
 * 长文案在英文下普遍 100–230 字符，折行两三行更甚。
 */
export function FieldHint({ text }: { text?: string }) {
  if (!text) return null
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {/* 常见外壳是 <label>，button 属于 interactive content 不能嵌进去；用可聚焦 span，
            全文再补一份 sr-only：Tooltip 只有悬停/聚焦才展开，读屏要无条件拿到说明。 */}
        <span
          tabIndex={0}
          onClick={(e) => e.preventDefault()}
          className="inline-flex shrink-0 cursor-help rounded-sm text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
        >
          <CircleHelp className="size-3.5" aria-hidden="true" />
          <span className="sr-only">{text}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-normal">
        {text}
      </TooltipContent>
    </Tooltip>
  )
}
