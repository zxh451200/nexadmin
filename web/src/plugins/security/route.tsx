import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/security',
    component: lazy(() => import('./SecurityPage')),
    perm: 'security:self',
    menu: { title: 'plugin_security.menu', icon: 'ShieldCheck', order: 110, group: 'system' },
  },
]
