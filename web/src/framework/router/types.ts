import type { ComponentType, LazyExoticComponent } from 'react'

export type MenuGroupKey = 'dashboard' | 'system' | 'content' | 'monitor' | 'tenant' | 'mine'

export interface ModuleRoute {
  path: string // 无前导斜杠，如 'system/user'
  component: LazyExoticComponent<ComponentType> | ComponentType
  perm?: string // 需要的权限点，如 'user:list'
  hidden?: boolean // true 则不进菜单
  menu?: { title: string; icon?: string; order?: number; group?: MenuGroupKey } // title 为 i18n key；icon 为 lucide 图标名
  // true = 沉浸式全屏页：有鉴权但无布局壳（侧边栏/顶栏不渲染），数据大屏、三维场景等用；
  // 页面仍可出现在菜单里，适合搭配页面内自实现的全屏 API 使用。
  fullscreen?: boolean
}

export interface MenuNode {
  key: string
  /** i18n key */
  title: string
  icon?: string
  /** 带前导斜杠的完整路径；分组节点没有 */
  path?: string
  children?: MenuNode[]
}
