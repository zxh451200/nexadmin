import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'content/media',
    component: lazy(() => import('./MediaOptimizePage')),
    perm: 'media:optimize',
    menu: { title: 'plugin_media.menu', icon: 'ImageDown', order: 31, group: 'content' },
  },
]
