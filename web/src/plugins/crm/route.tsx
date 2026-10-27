import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'crm/customers',
    component: lazy(() => import('./CrmPage')),
    perm: 'crm:list',
    menu: { title: 'plugin_crm.menu', icon: 'Contact', order: 70, group: 'system' },
  },
]
