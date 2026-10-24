import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/report',
    component: lazy(() => import('./ReportPage')),
    perm: 'report:view',
    menu: { title: 'plugin_report.menu', icon: 'ChartColumn', order: 70, group: 'monitor' },
  },
  {
    // 数据大屏展示页：从报表中心的「大屏」页签进入，不进菜单
    path: 'monitor/report-screen',
    component: lazy(() => import('./ScreenViewPage')),
    perm: 'report:view',
    hidden: true,
  },
]
