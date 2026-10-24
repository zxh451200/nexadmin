import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/dict',
    component: lazy(() => import('./DictPage')),
    perm: 'dict:list',
    menu: { title: 'dict.menu', icon: 'BookA', order: 60, group: 'system' },
  },
]
