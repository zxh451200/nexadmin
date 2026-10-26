import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'system/backup',
    component: lazy(() => import('./BackupPage')),
    perm: 'backup:list',
    menu: { title: 'backup.menu', icon: 'DatabaseBackup', order: 140, group: 'system' },
  },
]
