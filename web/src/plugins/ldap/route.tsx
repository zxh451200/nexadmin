import { lazy } from 'react'
import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// LDAP 配置已并进「系统设置 → 安全与登录」：侧栏不再单列，旧地址直接跳过去。
export const routes: ModuleRoute[] = [
  {
    path: 'ldap/config',
    component: () => <Navigate to="/system/settings?tab=ldap" replace />,
    perm: 'ldap:list',
    settings: {
      key: 'ldap',
      group: 'security',
      title: 'plugin_ldap.menu',
      order: 100,
      component: lazy(() => import('./LdapPage').then((m) => ({ default: m.LdapSection }))),
    },
  },
]
