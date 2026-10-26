import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'im/conversations',
    component: lazy(() => import('./ImPage')),
    perm: 'im:list',
    menu: { title: 'plugin_im.menu', icon: 'MessagesSquare', order: 61, group: 'system' },
  },
]
