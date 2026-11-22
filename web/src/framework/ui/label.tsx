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
    /* label 文本的可复制（双击选中整个 label）由 framework/lib/label-copy 的全局监听统一保证：
       Radix 在 mousedown 的 detail>1（双击）上 preventDefault，原生双击选词被吞，按元素补偿
       已升级为全局方案（覆盖含各处裸 <label> 在内的所有 label），这里不再逐元素挂。 */
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
