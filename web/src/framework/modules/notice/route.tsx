import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'content/notice',
    component: lazy(() => import('./NoticePage')),
    perm: 'notice:list',
    menu: { title: 'notice.menu', icon: 'Megaphone', order: 50, group: 'system' },
  },
]
