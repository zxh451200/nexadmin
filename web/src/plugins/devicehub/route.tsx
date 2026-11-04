// 设备台账/测点/读数/指令/告警七张表都没有 tenant_id，后端整组挂了
// core.RequirePlatformFeature()（plugins/devicehub/handler.go）：非平台调用方进页面就看不到
// 别人的设备，也下不了别人的指令。platform: true 让前端在发请求之前先给解释页。
import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'devicehub/monitor',
    component: lazy(() => import('./DevicePage')),
    perm: 'devicehub:list',
    platform: true,
    menu: { title: 'plugin_devicehub.menu', icon: 'Gauge', order: 64, group: 'system' },
  },
]
