import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'workflow/center',
    component: lazy(() => import('./WorkflowPage')),
    perm: 'workflow:list',
    menu: { title: 'plugin_workflow.menu', icon: 'GitBranch', order: 63, group: 'system' },
  },
]
