import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'monitor/server',
    component: lazy(() => import('./ServerMonitorPage')),
    perm: 'serverMonitor:list',
    // 快照表 sys_monitor_snapshot 没有 tenant_id，实时值也来自本进程：读的是整个部署的量，
    // 后端整组挂了 core.RequirePlatformFeature()（modules/*monitor/module.go），页面级给解释页。
    platform: true,
    menu: { title: 'serverMonitor.menu', icon: 'Cpu', order: 40, group: 'monitor' },
  },
]
