import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'payment/orders',
    component: lazy(() => import('./PaymentPage')),
    perm: 'payment:list',
    menu: { title: 'plugin_payment.menu', icon: 'CreditCard', order: 60, group: 'system' },
  },
]
