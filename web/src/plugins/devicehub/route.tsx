import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'devicehub/monitor',
    component: lazy(() => import('./DevicePage')),
    perm: 'devicehub:list',
    menu: { title: 'plugin_devicehub.menu', icon: 'Gauge', order: 64, group: 'system' },
  },
]
