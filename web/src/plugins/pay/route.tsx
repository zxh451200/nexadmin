import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'pay/orders',
    component: lazy(() => import('./PayPage')),
    perm: 'pay:list',
    menu: { title: 'plugin_pay.menu', icon: 'CreditCard', order: 60, group: 'system' },
  },
]
