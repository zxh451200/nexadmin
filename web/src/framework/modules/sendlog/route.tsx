import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/send-log',
    component: lazy(() => import('./SendLogPage')),
    perm: 'sendlog:list',
    menu: { title: 'sendlog.menu', icon: 'Send', order: 70, group: 'monitor' },
  },
]
