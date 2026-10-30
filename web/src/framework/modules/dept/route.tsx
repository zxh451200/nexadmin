import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/dept',
    component: lazy(() => import('./DeptPage')),
    // 岗位已并成本页第二个标签：只有 post:list 的角色也要进得来，否则旧地址 /system/post 跳过来直接 403
    perm: ['dept:list', 'post:list'],
    menu: { title: 'dept.menu', icon: 'Network', order: 40, group: 'system' },
  },
]
