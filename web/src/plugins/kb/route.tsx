// 知识库是平台级功能（文档与切块都没有 tenant_id，与 IM 会话同一套归属）：
// platform: true 让非平台调用方进页面就看到解释页，而不是一串 403。
import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'kb/documents',
    component: lazy(() => import('./KbPage')),
    perm: 'kb:list',
    platform: true,
    menu: { title: 'plugin_kb.menu', icon: 'BookOpen', order: 70, group: 'system' },
  },
]
