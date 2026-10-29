import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/map',
    component: lazy(() => import('./MapSettingsPage')),
    perm: 'maps:config',
    menu: { title: 'plugin_maps.menu', icon: 'MapPinned', order: 80, group: 'system' },
  },
]
