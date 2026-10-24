import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'dashboard',
    component: lazy(() => import('./DashboardPage')),
    menu: { title: 'dashboard.menu', icon: 'LayoutDashboard', order: 0, group: 'dashboard' },
  },
]
