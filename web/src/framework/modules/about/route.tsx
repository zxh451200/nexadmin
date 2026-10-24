import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'about',
    component: lazy(() => import('./AboutPage')),
    perm: 'about:view',
    menu: { title: 'about.menu', icon: 'Info', order: 65 },
  },
]
