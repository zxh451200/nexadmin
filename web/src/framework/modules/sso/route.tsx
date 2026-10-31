import { lazy } from 'react'
import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// /sso/authorize 是无布局的全屏页，不走这里，见 router/AppRoutes.tsx
export const routes: ModuleRoute[] = [
  {
    // 单点登录应用已并进「系统设置 → 安全与登录」：侧栏不再单列，旧地址直接跳过去。
    path: 'system/sso-apps',
    component: () => <Navigate to="/system/settings?tab=sso" replace />,
    perm: 'sso:list',
    settings: {
      key: 'sso',
      group: 'security',
      title: 'sso.settingsTitle',
      order: 200,
      component: lazy(() => import('./SsoAppsPage').then((m) => ({ default: m.SsoSection }))),
    },
  },
  {
    path: 'portal',
    component: lazy(() => import('./PortalPage')),
    menu: { title: 'sso.portal.menu', icon: 'LayoutGrid', order: 15, group: 'dashboard' },
  },
]
