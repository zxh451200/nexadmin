import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

// /sso/authorize 是无布局的全屏页，不走这里，见 router/AppRoutes.tsx
export const routes: ModuleRoute[] = [
  {
    path: 'system/sso-apps',
    component: lazy(() => import('./SsoAppsPage')),
    perm: 'sso:list',
    menu: { title: 'sso.menu', icon: 'AppWindow', order: 110, group: 'system' },
  },
  {
    path: 'portal',
    component: lazy(() => import('./PortalPage')),
    menu: { title: 'sso.portal.menu', icon: 'LayoutGrid', order: 15, group: 'dashboard' },
  },
]
