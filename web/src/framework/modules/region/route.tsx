import { lazy } from 'react'
import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    // 省市区数据是「系统设置 → 高级」里的一个分区，不单独占菜单；旧地址跳转到设置页。
    path: 'system/region',
    component: () => <Navigate to="/system/settings?tab=region" replace />,
    perm: 'region:sync',
    settings: {
      key: 'region',
      group: 'advanced',
      title: 'region.settingsTitle',
      order: 300,
      component: lazy(() => import('./RegionSection').then((m) => ({ default: m.RegionSection }))),
    },
  },
]
