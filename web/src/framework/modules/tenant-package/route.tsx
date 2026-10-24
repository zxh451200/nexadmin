import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'tenant/package',
    component: lazy(() => import('./TenantPackagePage')),
    perm: 'tenantPkg:list',
    menu: { title: 'tenantPkg.menu', icon: 'Package', order: 20, group: 'tenant' },
  },
]
