import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [{
  path: 'monitor/assistant',
  component: lazy(() => import('./AssistantPage')),
  perm: 'assistant:use',
  menu: { title: 'plugin_assistant.menu', icon: 'MessageSquare', order: 80, group: 'monitor' },
}]
