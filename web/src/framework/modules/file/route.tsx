import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'content/file',
    component: lazy(() => import('./FilePage')),
    perm: 'file:list',
    menu: { title: 'fileMgr.menu', icon: 'FolderOpen', order: 70, group: 'system' },
  },
]
