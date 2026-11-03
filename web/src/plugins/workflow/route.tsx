import { lazy } from 'react'
import { registerMenuBadge } from '@/framework/layout/menuBadge'
import type { ModuleRoute } from '@/framework/router/types'
import { fetchTodoCount } from './api'

// 审批中心的待办徽标：Sidebar 只在菜单对当前用户可见时才轮询，
// 无 workflow:list 权限/插件停用的人不会发出这条请求；失败由 menuBadge 归零兜底。
registerMenuBadge({
  path: '/workflow/center',
  load: () => fetchTodoCount(),
})

export const routes: ModuleRoute[] = [
  {
    path: 'workflow/center',
    component: lazy(() => import('./WorkflowPage')),
    perm: 'workflow:list',
    menu: { title: 'plugin_workflow.menu', icon: 'GitBranch', order: 63, group: 'system' },
  },
]
