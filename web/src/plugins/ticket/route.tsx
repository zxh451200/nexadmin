import { lazy } from 'react'
import { api } from '@/framework/request/http'
import { registerMenuBadge } from '@/framework/layout/menuBadge'
import type { ModuleRoute } from '@/framework/router/types'

// 工单中心菜单徽标：待我注意的未完结数（未认领 open + 认领给我的未完结）。
// Sidebar 只在菜单对当前用户可见时才轮询，无 ticket:list 权限的人不会发这条请求；
// 请求失败由 menuBadge 归零兜底。工单页里认领/解决/关闭/建单后会手动失效刷新。
registerMenuBadge({
  path: '/ticket/tickets',
  load: () =>
    api
      .get<{ count: number }>('/plugins/ticket/tickets/badge', undefined, { silent: true })
      .then((d) => d.count),
})

export const routes: ModuleRoute[] = [
  {
    path: 'ticket/tickets',
    component: lazy(() => import('./TicketPage')),
    perm: 'ticket:list',
    menu: { title: 'plugin_ticket.menu', icon: 'Inbox', order: 62, group: 'system' },
  },
]
