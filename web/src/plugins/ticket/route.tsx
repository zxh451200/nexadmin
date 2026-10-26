import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'ticket/tickets',
    component: lazy(() => import('./TicketPage')),
    perm: 'ticket:list',
    menu: { title: 'plugin_ticket.menu', icon: 'Inbox', order: 62, group: 'system' },
  },
]
