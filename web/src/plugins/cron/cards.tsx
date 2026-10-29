import { lazy } from 'react'
import type { DashboardCard } from '@/framework/modules/dashboard/types'

/** 工作台卡片：任务总数 / 启用数 / 近 24 小时失败数（需 cron:list） */
export const cards: DashboardCard[] = [
  {
    id: 'cron-overview',
    title: 'plugin_cron.card.overview',
    perm: 'cron:list',
    size: 'md',
    order: 200,
    component: lazy(() => import('./CronOverviewCard')),
  },
]
