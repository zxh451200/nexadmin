import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/audit',
    component: lazy(() => import('./AuditPage')),
    perm: 'audit:list',
    menu: { title: 'plugin_audit.menu', icon: 'ScrollText', order: 110, group: 'system' },
  },
]
