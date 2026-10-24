import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/codegen',
    component: lazy(() => import('./CodegenPage')),
    perm: 'codegen:list',
    menu: { title: 'plugin_codegen.menu', icon: 'Code', order: 120, group: 'system' },
  },
]
