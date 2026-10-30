import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'

export const routes: ModuleRoute[] = [
  {
    path: 'website/settings',
    component: lazy(() => import('./SettingsPage')),
    perm: 'website:setting',
    menu: { title: 'plugin_website.menu.settings', icon: 'Settings', order: 65, group: 'system' },
  },
  {
    path: 'website/nav',
    component: lazy(() => import('./NavPage')),
    perm: 'website:nav',
    menu: { title: 'plugin_website.menu.nav', icon: 'Link2', order: 66, group: 'system' },
  },
  {
    path: 'website/features',
    component: lazy(() => import('./FeaturePage')),
    perm: 'website:feature',
    menu: { title: 'plugin_website.menu.features', icon: 'LayoutGrid', order: 67, group: 'system' },
  },
  {
    path: 'website/pages',
    component: lazy(() => import('./PagePage')),
    perm: 'website:page',
    menu: { title: 'plugin_website.menu.pages', icon: 'FileText', order: 68, group: 'system' },
  },
  {
    path: 'website/news',
    component: lazy(() => import('./PostPage')),
    perm: 'website:post',
    menu: { title: 'plugin_website.menu.posts', icon: 'Newspaper', order: 69, group: 'system' },
  },
  {
    path: 'website/messages',
    component: lazy(() => import('./MessagePage')),
    perm: 'website:message',
    menu: { title: 'plugin_website.menu.messages', icon: 'Inbox', order: 70, group: 'system' },
  },
]
