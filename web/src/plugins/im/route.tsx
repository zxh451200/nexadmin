// 会话/消息/附件/快捷回复/设置五张表都没有 tenant_id：任何持 im:list 的租户一次列表或一次导出
// 就把所有租户的聊天记录带走，所以后端整组挂了 core.RequirePlatformFeature()（plugins/im/handler.go）。
// platform: true 让前端在进页面前就给解释页，而不是发一串必然 403 的请求。
import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'im/conversations',
    component: lazy(() => import('./ImPage')),
    perm: 'im:list',
    platform: true,
    menu: { title: 'plugin_im.menu', icon: 'MessagesSquare', order: 61, group: 'system' },
  },
]
