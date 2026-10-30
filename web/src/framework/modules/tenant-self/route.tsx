import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'tenant/self',
    component: lazy(() => import('./TenantSelfPage')),
    perm: 'tenantSelf:view',
    menu: { title: 'tenantSelf.menu', icon: 'Building2', order: 30, group: 'tenant' },
  },
]
