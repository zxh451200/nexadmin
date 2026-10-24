import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/cron',
    component: lazy(() => import('./CronPage')),
    perm: 'cron:list',
    menu: { title: 'plugin_cron.menu', icon: 'Timer', order: 100, group: 'system' },
  },
]
