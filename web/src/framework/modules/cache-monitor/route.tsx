import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/cache',
    component: lazy(() => import('./CacheMonitorPage')),
    perm: 'cacheMonitor:list',
    // 快照表 sys_monitor_snapshot 没有 tenant_id，实时值也来自本进程：读的是整个部署的量，
    // 后端整组挂了 core.RequirePlatformFeature()（modules/*monitor/module.go），页面级给解释页。
    platform: true,
    menu: { title: 'cacheMonitor.menu', icon: 'Database', order: 50, group: 'monitor' },
  },
]
