import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

// path 与 plugin.yaml 的菜单一致；perm 与后端路由的权限码一致。
export const routes: ModuleRoute[] = [
  {
    path: 'system/formbuilder',
    component: lazy(() => import('./FormbuilderPage')),
    perm: 'formbuilder:design',
    menu: { title: 'plugin_formbuilder.menu', icon: 'ClipboardList', order: 130, group: 'system' },
  },
  {
    path: 'system/formbuilder/fill',
    component: lazy(() => import('./FillPage')),
    perm: 'formbuilder:fill',
    menu: { title: 'plugin_formbuilder.fillMenu', icon: 'FilePen', order: 131, group: 'system' },
  },
  {
    // 设计器：从表单列表进入（?id=），不进菜单
    path: 'system/formbuilder/design',
    component: lazy(() => import('./DesignerPage')),
    perm: 'formbuilder:design',
    hidden: true,
  },
  {
    // 提交列表：从表单列表进入（?id=），不进菜单
    path: 'system/formbuilder/submissions',
    component: lazy(() => import('./SubmissionsPage')),
    perm: 'formbuilder:submission',
    hidden: true,
  },
]
