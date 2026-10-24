import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/post',
    component: lazy(() => import('./PostPage')),
    perm: 'post:list',
    menu: { title: 'post.menu', icon: 'BriefcaseBusiness', order: 50, group: 'system' },
  },
]
