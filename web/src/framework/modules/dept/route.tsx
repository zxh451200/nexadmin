import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/dept',
    component: lazy(() => import('./DeptPage')),
    perm: 'dept:list',
    menu: { title: 'dept.menu', icon: 'Network', order: 40, group: 'system' },
  },
]
