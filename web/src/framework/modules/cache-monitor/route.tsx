import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/cache',
    component: lazy(() => import('./CacheMonitorPage')),
    perm: 'cacheMonitor:list',
    menu: { title: 'cacheMonitor.menu', icon: 'Database', order: 50, group: 'monitor' },
  },
]
