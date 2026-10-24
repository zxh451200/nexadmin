import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

// 登录即可访问，无需权限码；管理员的“全部任务”用按钮权限 job:all
export const routes: ModuleRoute[] = [
  {
    path: 'jobs',
    component: lazy(() => import('./ExportCenterPage')),
    menu: { title: 'jobs.menu', icon: 'Download', order: 16, group: 'dashboard' },
  },
]
