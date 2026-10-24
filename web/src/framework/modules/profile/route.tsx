import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'profile',
    component: lazy(() => import('./ProfilePage')),
    hidden: true,
  },
]
