import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [{
  path: 'system/scim',
  component: lazy(() => import('./ScimPage')),
  perm: 'scim:list',
  menu: { title: 'plugin_scim.menu', icon: 'UsersRound', order: 120, group: 'system' },
}]
