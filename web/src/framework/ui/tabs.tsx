import * as React from 'react'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import { cn } from '@/lib/utils'

const Tabs = TabsPrimitive.Root

/**
 * 分段控件外观的唯一样式源：Tabs 与 components/Segmented、顶部标签条共用同一口径。
 * 手写切换控件不要另起一套圆角/底色，否则同一个产品里会出现两种「选中」。
 */
export const segmentedListClass =
  'inline-flex h-auto w-fit max-w-full items-center justify-start gap-0.5 overflow-x-auto overflow-y-hidden rounded-lg border bg-transparent p-0.5 text-muted-foreground'

export const segmentedItemClass =
  'inline-flex min-h-10 items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-3.5 py-1.5 text-sm font-medium transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-[state=active]:bg-primary data-[state=active]:text-primary-foreground data-[state=active]:shadow-sm md:min-h-8'

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List ref={ref} className={cn(segmentedListClass, className)} {...props} />
))
TabsList.displayName = TabsPrimitive.List.displayName

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      // 选中态与顶部标签条（framework/layout/TabsBar）同一口径：填满品牌色的胶囊 + 浅阴影，未选中是纯文字。
      // 两处必须一起改——只改一处就会出现「有的 tab 有样式、有的没有」。
      segmentedItemClass,
      className,
    )}
    {...props}
  />
))
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn('mt-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background', className)}
    {...props}
  />
))
TabsContent.displayName = TabsPrimitive.Content.displayName

export { Tabs, TabsList, TabsTrigger, TabsContent }
