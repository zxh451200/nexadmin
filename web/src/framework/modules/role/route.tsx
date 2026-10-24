import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/role',
    component: lazy(() => import('./RolePage')),
    perm: 'role:list',
    menu: { title: 'role.menu', icon: 'ShieldCheck', order: 20, group: 'system' },
  },
]
