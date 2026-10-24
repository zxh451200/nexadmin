import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/menu',
    component: lazy(() => import('./MenuPage')),
    perm: 'menu:list',
    menu: { title: 'sysMenu.menu', icon: 'ListTree', order: 30, group: 'system' },
  },
]
