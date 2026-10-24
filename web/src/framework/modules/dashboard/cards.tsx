import { lazy } from 'react'
import type { DashboardCard } from './types'

/** 工作台内置卡片；业务/插件卡片见 docs/二次开发指南.md「往工作台加卡片」 */
export const cards: DashboardCard[] = [
  { id: 'dashboard-stats', title: 'dashboard.card.stats', size: 'full', order: 10, component: lazy(() => import('./cards/StatsCard')) },
  { id: 'dashboard-login-trend', title: 'dashboard.loginTrend', size: 'md', order: 20, component: lazy(() => import('./cards/LoginTrendCard')) },
  { id: 'dashboard-request-trend', title: 'dashboard.requestTrend', size: 'md', order: 30, component: lazy(() => import('./cards/RequestTrendCard')) },
  { id: 'dashboard-oper-ratio', title: 'dashboard.operRatio', size: 'md', order: 40, component: lazy(() => import('./cards/OperRatioCard')) },
  { id: 'dashboard-todos', title: 'dashboard.todos', size: 'md', order: 50, component: lazy(() => import('./cards/TodosCard')) },
  { id: 'dashboard-quick-links', title: 'dashboard.quickLinks', size: 'md', order: 60, component: lazy(() => import('./cards/QuickLinksCard')) },
  { id: 'dashboard-recent-notices', title: 'dashboard.recentNotices', size: 'md', order: 70, component: lazy(() => import('./cards/RecentNoticesCard')) },
]
