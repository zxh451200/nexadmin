import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'message',
    component: lazy(() => import('./MessagePage')),
    menu: { title: 'message.menu', icon: 'Bell', order: 20, group: 'content' },
  },
]
