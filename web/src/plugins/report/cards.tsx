import { lazy } from 'react'
import type { DashboardCard } from '@/framework/modules/dashboard/types'

/** 工作台卡片：报表数 / 可见大屏数（需 report:view） */
export const cards: DashboardCard[] = [
  {
    id: 'report-overview',
    title: 'plugin_report.card.overview',
    perm: 'report:view',
    size: 'md',
    order: 210,
    component: lazy(() => import('./ReportOverviewCard')),
  },
]
