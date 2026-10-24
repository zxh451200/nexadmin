import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/oper-log',
    component: lazy(() => import('./OperLogPage')),
    perm: 'operLog:list',
    menu: { title: 'operLog.menu', icon: 'ScrollText', order: 10, group: 'monitor' },
  },
]
