import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/config',
    component: lazy(() => import('./SysConfigPage')),
    perm: 'config:list',
    menu: { title: 'sysConfig.menu', icon: 'Settings', order: 70, group: 'system' },
  },
]
