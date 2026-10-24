import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/plugin',
    component: lazy(() => import('./PluginPage')),
    perm: 'plugin:list',
    menu: { title: 'pluginMgr.menu', icon: 'Puzzle', order: 100, group: 'system' },
  },
]
