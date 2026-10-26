import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'ldap/config',
    component: lazy(() => import('./LdapPage')),
    perm: 'ldap:list',
    menu: { title: 'plugin_ldap.menu', icon: 'Server', order: 65, group: 'system' },
  },
]
