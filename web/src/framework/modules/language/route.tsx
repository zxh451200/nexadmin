import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// 「语言管理」已并进「系统设置 → 高级 → 语言」：菜单不再单列，旧地址直接跳过去。
export const routes: ModuleRoute[] = [
  {
    path: 'system/language',
    component: () => <Navigate to="/system/settings?tab=language" replace />,
  },
]
