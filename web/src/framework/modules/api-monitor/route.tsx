import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/api',
    component: lazy(() => import('./ApiMonitorPage')),
    perm: 'apiMonitor:list',
    menu: { title: 'apiMonitor.menu', icon: 'Gauge', order: 60, group: 'monitor' },
  },
]
