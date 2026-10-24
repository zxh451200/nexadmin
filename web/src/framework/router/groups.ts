import type { MenuGroupKey } from './types'

export interface MenuGroup {
  key: MenuGroupKey
  /** i18n key */
  title: string
  icon: string
  order: number
}

export const menuGroups: MenuGroup[] = [
  { key: 'dashboard', title: 'menu.group.dashboard', icon: 'LayoutDashboard', order: 10 },
  { key: 'system', title: 'menu.group.system', icon: 'Settings', order: 20 },
  { key: 'content', title: 'menu.group.content', icon: 'FileText', order: 30 },
  { key: 'monitor', title: 'menu.group.monitor', icon: 'Activity', order: 40 },
  { key: 'tenant', title: 'menu.group.tenant', icon: 'Building2', order: 50 },
  { key: 'mine', title: 'menu.group.mine', icon: 'User', order: 60 },
]
