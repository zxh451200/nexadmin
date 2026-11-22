// 官网 12 张表（内容 + 访客留言）都没有 tenant_id，代表的是整个部署主体的对外窗口，
// 后端整组挂了 core.RequirePlatformFeature()（plugins/website/plugin.go）；
// platform: true 让非平台调用方进页面就看到解释页，不是一串 403。
import { lazy } from 'react'
import { api } from '@/framework/request/http'
import { registerMenuBadge } from '@/framework/layout/menuBadge'
import type { ModuleRoute } from '@/framework/router/types'

// 留言收件箱的未读徽标：Sidebar 只在「留言管理」对当前用户可见时才轮询，
// 所以无权限/插件停用的人不会发出这条请求；请求失败由 menuBadge 归零兜底。
registerMenuBadge({
  path: '/website/messages',
  load: () =>
    api
      .get<{ count: number }>('/plugins/website/messages/unread-count', undefined, { silent: true })
      .then((d) => d.count),
})

export const routes: ModuleRoute[] = [
  {
    path: 'website/settings',
    component: lazy(() => import('./SettingsPage')),
    perm: 'website:setting',
    platform: true,
    menu: { title: 'plugin_website.menu.settings', icon: 'Settings', order: 65, group: 'system' },
  },
  {
    path: 'website/nav',
    component: lazy(() => import('./NavPage')),
    perm: 'website:nav',
    platform: true,
    menu: { title: 'plugin_website.menu.nav', icon: 'Link2', order: 66, group: 'system' },
  },
  {
    path: 'website/features',
    component: lazy(() => import('./FeaturePage')),
    perm: 'website:feature',
    platform: true,
    menu: { title: 'plugin_website.menu.features', icon: 'LayoutGrid', order: 67, group: 'system' },
  },
  {
    path: 'website/pages',
    component: lazy(() => import('./PagePage')),
    perm: 'website:page',
    platform: true,
    menu: { title: 'plugin_website.menu.pages', icon: 'FileText', order: 68, group: 'system' },
  },
  {
    path: 'website/news',
    component: lazy(() => import('./PostPage')),
    perm: 'website:post',
    platform: true,
    menu: { title: 'plugin_website.menu.posts', icon: 'Newspaper', order: 69, group: 'system' },
  },
  {
    // 留言收件箱：plugin.yaml 声明了菜单、MessagePage 也实现了，但 routes 漏了这条导致
    // 菜单/直链都不可达（框架按 path 匹配前端路由，sys_menu.component 不解析）。
    path: 'website/messages',
    component: lazy(() => import('./MessagePage')),
    perm: 'website:message',
    platform: true,
    menu: { title: 'plugin_website.menu.messages', icon: 'Inbox', order: 70, group: 'system' },
  },
  {
    path: 'website/products',
    component: lazy(() => import('./ProductPage')),
    perm: 'website:product',
    platform: true,
    menu: { title: 'plugin_website.menu.products', icon: 'Package', order: 71, group: 'system' },
  },
  {
    path: 'website/product-categories',
    component: lazy(() => import('./ProductCategoryPage')),
    perm: 'website:product-category',
    platform: true,
    menu: { title: 'plugin_website.menu.productCategories', icon: 'FolderTree', order: 72, group: 'system' },
  },
  {
    path: 'website/faq',
    component: lazy(() => import('./FaqPage')),
    perm: 'website:faq',
    platform: true,
    menu: { title: 'plugin_website.menu.faq', icon: 'CircleHelp', order: 73, group: 'system' },
  },
  {
    path: 'website/orders',
    component: lazy(() => import('./SiteOrdersPage')),
    perm: 'website:order',
    platform: true,
    menu: { title: 'plugin_website.menu.orders', icon: 'ReceiptText', order: 74, group: 'system' },
  },
  {
    path: 'website/analytics',
    component: lazy(() => import('./AnalyticsPage')),
    perm: 'website:analytics',
    platform: true,
    menu: { title: 'plugin_website.menu.analytics', icon: 'ChartLine', order: 75, group: 'system' },
  },
]
