import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/recycle',
    component: lazy(() => import('./RecyclePage')),
    perm: 'recycle:list',
    menu: { title: 'recycle.menu', icon: 'Trash2', order: 90, group: 'system' },
  },
]
