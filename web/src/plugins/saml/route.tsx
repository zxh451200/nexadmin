import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/saml',
    component: lazy(() => import('./SamlPage')),
    perm: 'saml:list',
    menu: { title: 'plugin_saml.menu', icon: 'KeyRound', order: 115, group: 'system' },
  },
]
