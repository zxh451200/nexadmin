import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/user',
    component: lazy(() => import('./UserPage')),
    perm: 'user:list',
    menu: { title: 'user.menu', icon: 'Users', order: 10, group: 'system' },
  },
]
