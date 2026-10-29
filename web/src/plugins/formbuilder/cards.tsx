import { lazy } from 'react'
import type { DashboardCard } from '@/framework/modules/dashboard/types'

/** 工作台卡片：我的待填表单数（需 formbuilder:fill） */
export const cards: DashboardCard[] = [
  {
    id: 'formbuilder-pending',
    title: 'plugin_formbuilder.card.pending',
    perm: 'formbuilder:fill',
    size: 'md',
    order: 220,
    component: lazy(() => import('./PendingFormsCard')),
  },
]
