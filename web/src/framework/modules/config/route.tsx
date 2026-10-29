import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// 「系统参数」已并进「系统设置 → 高级 → 参数」：菜单不再单列，旧地址直接跳过去，
// 收藏夹和老文档里的链接不至于 404。
export const routes: ModuleRoute[] = [
  {
    path: 'system/config',
    component: () => <Navigate to="/system/settings?tab=params" replace />,
  },
]
