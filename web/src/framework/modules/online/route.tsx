import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/online',
    component: lazy(() => import('./OnlinePage')),
    perm: 'online:list',
    menu: { title: 'online.menu', icon: 'MonitorSmartphone', order: 30, group: 'monitor' },
  },
]
