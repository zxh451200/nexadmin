import type { ComponentType, LazyExoticComponent } from 'react'

export type MenuGroupKey = 'dashboard' | 'system' | 'content' | 'monitor' | 'tenant' | 'mine'

export type SettingsGroupKey = 'site' | 'security' | 'integration' | 'advanced'

/** 页面同时作为「系统设置」的一个分区出现时的声明。
 *  组件按分区样式渲染（不要自带 PageContainer / PageHeader）；路由本身仍保留，供旧地址重定向。 */
export interface SettingsSectionSpec {
  key: string // ?tab= 的取值
  group: SettingsGroupKey
  title: string // i18n key
  order?: number // 同组内排序；内置分区排在前面，未声明按 100
  component: LazyExoticComponent<ComponentType> | ComponentType
}

export interface ModuleRoute {
  path: string // 无前导斜杠，如 'system/user'
  component: LazyExoticComponent<ComponentType> | ComponentType
  perm?: string | string[] // 需要的权限点，如 'user:list'；数组默认「任一命中」即可进（合并页用）
  // 该页面所属插件 ID（由 route.tsx 所在目录推断，见 discover/merge）：插件未启用时守卫直接拦截，
  // 不再渲染页面（否则页面会发出一串必然 404 的插件接口请求）。
  plugin?: string
  hidden?: boolean // true 则不进菜单
  menu?: { title: string; icon?: string; order?: number; group?: MenuGroupKey } // title 为 i18n key；icon 为 lucide 图标名
  settings?: SettingsSectionSpec // 声明后该页同时出现在「系统设置」的分区导航里
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
