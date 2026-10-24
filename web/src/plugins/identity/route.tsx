import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'
import { registerLoginContinuation } from '@/framework/auth/useAuth'
import { resumeCASLogin } from './api'

registerLoginContinuation('identity-cas', resumeCASLogin)

export const routes: ModuleRoute[] = [
  {
    path: 'system/identity',
    component: lazy(() => import('./IdentityPage')),
    perm: 'identity:list',
    menu: { title: 'plugin_identity.menu', icon: 'KeyRound', order: 110, group: 'system' },
  },
]
