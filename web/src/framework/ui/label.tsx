import * as React from 'react'
import * as LabelPrimitive from '@radix-ui/react-label'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

// 行高不压成 leading-none：text-sm 自带的 1.4 行高让 label 文本上下各有 ~3px 自然余量，
// 与 SharedFieldShell/LocalizedField 的 min-h-7 居中标签行同一呼吸感——否则 FormItem 系的 label
// 紧贴上一行，站内两种外壳混排时间距明显不齐。
const labelVariants = cva('text-sm font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70')

const Label = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root> &
    VariantProps<typeof labelVariants> & {
      /** 必填项：在文字后补一个红色星号 */
      required?: boolean
    }
>(({ className, children, required, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={cn(labelVariants(), className)}
    {...props}
    /* Radix 在 mousedown 的 detail>1（双击）上 preventDefault——本意是防双击误触关联控件，顺带把浏览器
       原生的「双击选词」也禁了，label 上的字段名没法复制。这里不碰 mousedown（误触保护仍在），
       双击时手动选中 label 全部文本，把复制还回来。 */
    onDoubleClick={(e) => {
      const sel = window.getSelection()
      if (!sel) return
      const range = document.createRange()
      range.selectNodeContents(e.currentTarget)
      sel.removeAllRanges()
      sel.addRange(range)
    }}
  >
    {children}
    {required ? (
      <span aria-hidden="true" className="ms-0.5 text-destructive">
        *
      </span>
    ) : null}
  </LabelPrimitive.Root>
))
Label.displayName = LabelPrimitive.Root.displayName

export { Label }
