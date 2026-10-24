import type { DashboardCardSize } from './types'

/** 手机单列；md 起为 4 列栅格，sm=1/4、md=1/2、lg=3/4、full=整行（类名写全，Tailwind 才扫得到） */
export const CARD_GRID = 'grid grid-cols-1 gap-3 md:grid-cols-4 md:gap-4'

export const CARD_SPAN: Record<DashboardCardSize, string> = {
  sm: 'md:col-span-1',
  md: 'md:col-span-2',
  lg: 'md:col-span-3',
  full: 'md:col-span-4',
}
