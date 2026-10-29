import type { DashboardCardSize } from './types'

/** 手机单列；md 起为 4 列栅格，sm=1/4、md=1/2、lg=3/4、full=整行（类名写全，Tailwind 才扫得到） */
export const CARD_GRID = 'grid grid-cols-1 gap-3 md:grid-cols-4 md:gap-4'

/**
 * 已加载卡片才用的附加规则：
 * 「div>*」让卡片撑满所在行 —— 同一行等高，不会出现一高一矮的下沿参差；
 * 「div:empty」收掉组件返回 null 的卡片（例如插件未启用）—— 否则单元格还占着位置，行中间会留洞。
 * 骨架屏那组不能带：Skeleton 本身就是空 div，会被一起收掉。
 */
export const CARD_GRID_LOADED = `${CARD_GRID} [&>div>*]:h-full [&>div:empty]:hidden`

export const CARD_SPAN: Record<DashboardCardSize, string> = {
  sm: 'md:col-span-1',
  md: 'md:col-span-2',
  lg: 'md:col-span-3',
  full: 'md:col-span-4',
}
