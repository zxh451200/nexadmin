import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/openapi',
    component: lazy(() => import('./OpenApiPage')),
    perm: 'openapi:list',
    menu: { title: 'openapi.menu', icon: 'Webhook', order: 120, group: 'system' },
  },
]
