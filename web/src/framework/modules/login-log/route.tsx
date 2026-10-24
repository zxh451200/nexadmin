import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/login-log',
    component: lazy(() => import('./LoginLogPage')),
    perm: 'loginLog:list',
    menu: { title: 'loginLog.menu', icon: 'LogIn', order: 20, group: 'monitor' },
  },
]
