import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// 「岗位管理」已并进「部门管理」的第二个标签：岗位是三五条的查找表，与部门同属组织架构。
// 旧地址跳到那个标签，收藏夹和老文档里的链接不至于 404。
export const routes: ModuleRoute[] = [
  {
    path: 'system/post',
    component: () => <Navigate to="/system/dept?tab=post" replace />,
  },
]
