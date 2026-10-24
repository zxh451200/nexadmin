import type { ComponentType, LazyExoticComponent } from 'react'

/** 工作台卡片尺寸：桌面（md 起）栅格占 1/4、1/2、3/4、整行；手机一律整行 */
export type DashboardCardSize = 'sm' | 'md' | 'lg' | 'full'

/** 卡片声明：任意模块/插件/业务页目录下的 cards.tsx 导出 `cards: DashboardCard[]` 即被工作台自动发现 */
export interface DashboardCard {
  /** 全局唯一，建议「模块名-含义」；最长 64，只允许 [a-z0-9-_.]，且以字母或数字开头 */
  id: string
  /** i18n key，编辑布局时也用它显示卡片名 */
  title: string
  /** 权限码；不填则所有登录用户可见 */
  perm?: string
  size: DashboardCardSize
  /** 默认顺序，小的在前；不填排在有值的后面 */
  order?: number
  /** 建议用 React.lazy 懒加载；卡片自己拉数据 */
  component: ComponentType | LazyExoticComponent<ComponentType>
  /** 新卡片初始是否隐藏（用户可在编辑布局里打开） */
  defaultHidden?: boolean
}

/** 后端保存的布局：order 为卡片顺序，hidden 为隐藏的卡片 id */
export interface SavedLayout {
  order: string[]
  hidden: string[]
}
