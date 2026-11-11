// 会话/消息/附件/快捷回复/设置五张表都没有 tenant_id：任何持 im:list 的租户一次列表或一次导出
// 就把所有租户的聊天记录带走，所以后端整组挂了 core.RequirePlatformFeature()（plugins/im/handler.go）。
// platform: true 让前端在进页面前就给解释页，而不是发一串必然 403 的请求。
import { lazy } from 'react'
import { registerMenuBadge } from '@/framework/layout/menuBadge'
import type { ModuleRoute } from '@/framework/router/types'
import { fetchUnreadCount } from './api'

// 在线客服菜单徽标：进行中会话的访客未读消息数。Sidebar 只在菜单对当前用户可见时才轮询，
// 无 im:list 权限/非平台调用方不会发这条请求；失败由 menuBadge 归零兜底。
// 坐席读到新消息、关闭/删除会话后由 api.ts 手动失效刷新。
registerMenuBadge({ path: '/im/conversations', load: fetchUnreadCount })

export const routes: ModuleRoute[] = [
  {
    path: 'im/conversations',
    component: lazy(() => import('./ImPage')),
    perm: 'im:list',
    platform: true,
    menu: { title: 'plugin_im.menu', icon: 'MessagesSquare', order: 61, group: 'system' },
  },
]
