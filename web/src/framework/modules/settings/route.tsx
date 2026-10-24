import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/settings',
    component: lazy(() => import('./SettingsPage')),
    perm: 'settings:list',
    menu: { title: 'settings.menu', icon: 'SlidersHorizontal', order: 80, group: 'system' },
  },
]
