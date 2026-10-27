import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'ai/models',
    component: lazy(() => import('./AiGatewayPage')),
    perm: 'aigateway:list',
    menu: { title: 'plugin_aigateway.menu', icon: 'BrainCircuit', order: 65, group: 'system' },
  },
]
