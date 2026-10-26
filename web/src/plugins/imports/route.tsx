import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'imports/jobs',
    component: lazy(() => import('./ImportsPage')),
    perm: 'imports:list',
    menu: { title: 'plugin_imports.menu', icon: 'FileUp', order: 64, group: 'system' },
  },
]
