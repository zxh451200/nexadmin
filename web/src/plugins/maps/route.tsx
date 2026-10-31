import { lazy } from 'react'
import { Navigate } from 'react-router-dom'
import type { ModuleRoute } from '@/framework/router/types'

// 地图服务已并进「系统设置 → 存储与集成」：侧栏不再单列，旧地址直接跳过去。
export const routes: ModuleRoute[] = [
  {
    path: 'system/map',
    component: () => <Navigate to="/system/settings?tab=maps" replace />,
    perm: 'maps:config',
    settings: {
      key: 'maps',
      group: 'integration',
      title: 'plugin_maps.menu',
      order: 100,
      component: lazy(() => import('./MapSettingsPage').then((m) => ({ default: m.MapSection }))),
    },
  },
]
