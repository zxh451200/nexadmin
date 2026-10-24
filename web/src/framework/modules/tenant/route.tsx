import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'tenant/list',
    component: lazy(() => import('./TenantPage')),
    perm: 'tenant:list',
    menu: { title: 'tenant.menu', icon: 'Building2', order: 10, group: 'tenant' },
  },
]
