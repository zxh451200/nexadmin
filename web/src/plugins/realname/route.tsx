import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'realname',
    component: lazy(() => import('./RealnamePage')),
    perm: 'realname:list',
    menu: { title: 'plugin_realname.menu', icon: 'FingerprintPattern', order: 75 },
  },
]
