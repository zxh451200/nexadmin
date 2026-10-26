import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'mine/totp',
    component: lazy(() => import('./TotpPage')),
    perm: 'totp:manage',
    menu: { title: 'plugin_totp.menu', icon: 'ShieldCheck', order: 1, group: 'mine' },
  },
]
