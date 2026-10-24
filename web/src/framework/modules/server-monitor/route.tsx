import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/server',
    component: lazy(() => import('./ServerMonitorPage')),
    perm: 'serverMonitor:list',
    menu: { title: 'serverMonitor.menu', icon: 'Cpu', order: 40, group: 'monitor' },
  },
]
