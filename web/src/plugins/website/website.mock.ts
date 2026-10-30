import { defineMock, mockError } from '@/framework/request/mock'
import type { EntitySchema } from '@/framework/components/i18n'
import type { FeatureItem, NavItem, SitePage, SitePost, SiteSettings } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const id = (n: number) => String(8100000000000000000n + BigInt(n))

const settings: SiteSettings = {
  id: '1',
  siteName: 'NexAdmin',
  siteDesc: '以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui，生产构建为单一二进制。',
  logoUrl: '',
  homeTitle: '为生产级后台系统\n而生的全栈框架',
  homeSubtitle: '多租户、RBAC、开放平台与插件体系，出厂即备。每一项能力都以源码交付——可读、可改、可升级。',
  homeCtaLabel: '开始使用',
  homeCtaHref: '#quickstart',
  seoKeywords: 'NexAdmin,后台管理框架,Gin,React,多租户,RBAC,插件,开源',
  contactEmail: '',
  contactPhone: '',
  qrCodeUrl: '',
  qrCodeCaption: '',
  socials: [],
  friendLinks: [],
  icp: '',
  footerNote: '框架区源码以 MIT 许可开源；业务与插件代码归各自作者。',
  marketEnabled: true,
  i18nJson: '',
  updatedAt: ago(60 * 24),
}

let navs: NavItem[] = [
  { id: id(1), parentId: '0', label: '能力', href: '#features', sort: 10, enabled: true, i18nJson: '' },
  { id: id(2), parentId: '0', label: '动态', href: '/news', sort: 20, enabled: true, i18nJson: '' },
  { id: id(3), parentId: '0', label: '关于', href: '/p/about', sort: 30, enabled: true, i18nJson: '' },
]

let features: FeatureItem[] = [
  { id: id(1), icon: 'building', title: '多租户', description: '共享库 + tenant_id 数据层隔离；额度与到期在登录入口即强制执行。', sort: 10, enabled: true, i18nJson: '' },
  { id: id(2), icon: 'shield', title: '权限与数据范围', description: '按钮级权限码；五档数据范围在服务端裁剪列表与写操作。', sort: 20, enabled: true, i18nJson: '' },
  { id: id(3), icon: 'key', title: '登录与 SSO', description: '密码、短信与多家 OAuth2；本站亦可作为 OIDC Provider。', sort: 30, enabled: true, i18nJson: '' },
  { id: id(4), icon: 'plug', title: '开放平台', description: '访问令牌、HMAC 应用签名、Webhook、OpenAPI 文档与 MCP 入口。', sort: 40, enabled: true, i18nJson: '' },
  { id: id(5), icon: 'file', title: '文件与导出', description: 'tus 断点续传，本地或 S3 兼容存储；导出走异步任务中心。', sort: 50, enabled: true, i18nJson: '' },
  { id: id(6), icon: 'chart', title: '监控可观测', description: '服务、缓存、接口与前端错误监控；/metrics 输出 Prometheus 文本。', sort: 60, enabled: true, i18nJson: '' },
  { id: id(7), icon: 'globe', title: '国际化与移动端', description: 'zh-CN、en-US 出厂可增语言；PWA 与移动端适配开箱即用。', sort: 70, enabled: true, i18nJson: '' },
  { id: id(8), icon: 'puzzle', title: '源码插件体系', description: '插件即源码包：拷文件、重新编译即安装；离线签名授权可售卖。', sort: 80, enabled: true, i18nJson: '' },
]

let pages: SitePage[] = [
  {
    id: id(1), slug: 'about', title: '关于 NexAdmin', published: true,
    seoDesc: 'NexAdmin 是以源码交付的后台管理框架。',
    content: '<p>NexAdmin 是一个以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui。</p>',
    i18nJson: '',
    publishedJson: '',
    updatedAt: ago(60 * 26),
  },
]

let posts: SitePost[] = [
  {
    id: id(1), slug: 'hello-nexadmin', title: 'NexAdmin 上线：以源码交付的后台管理框架', published: true,
    coverUrl: '', summary: '后端 Gin + GORM、前端 React + shadcn/ui，生产构建为单一二进制。',
    seoDesc: 'NexAdmin：以源码交付的后台管理框架，Gin + GORM + React，生产环境单二进制部署。',
    content: '<p>十分钟上手：构建、启动、打开 <code>/setup</code> 向导四步完成初始化。</p>',
    publishedAt: ago(60 * 24 * 3),
    i18nJson: JSON.stringify({
      'en-US': {
        title: 'NexAdmin is live: an admin framework delivered as source',
        summary: 'Gin + GORM backend, React + shadcn/ui frontend, one binary in production.',
        content: '<p>Ten minutes to start: build, run, and finish the four-step wizard at <code>/setup</code>.</p>',
      },
    }),
    publishedJson: '', updatedAt: ago(60 * 24 * 3),
  },
  {
    id: id(2), slug: 'website-plugin', title: '官网插件发布：企业官网即插即用', published: true,
    coverUrl: '', summary: '匿名公开官网（服务端渲染、SEO 友好）+ 后台内容管理。',
    seoDesc: '官网插件：服务端渲染、SEO 友好的企业官网，后台可管设置、导航、页面与新闻。',
    content: '<p>你正在浏览的页面，就是它的默认模板。</p>',
    publishedAt: ago(60 * 24), i18nJson: '', publishedJson: '', updatedAt: ago(60 * 24),
  },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

// 与 plugins/website/render.go 的 iconPaths 同步（mock 模式没有 Go 后端，只能本地带一份）。
const featureIcons = [
  { name: 'building', paths: '<rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M9 21v-4h6v4M9 7h2M13 7h2M9 11h2M13 11h2M9 15h2"/>' },
  { name: 'shield', paths: '<path d="M12 3l7 3v5c0 4.6-3 8.4-7 10-4-1.6-7-5.4-7-10V6z"/><path d="M9.5 12l2 2 3.5-4"/>' },
  { name: 'key', paths: '<circle cx="8" cy="14" r="4"/><path d="M11 11L20 2M17 5l3 3M14 8l2 2"/>' },
  { name: 'plug', paths: '<path d="M9 3v5M15 3v5M7 8h10v4a5 5 0 0 1-10 0zM12 17v4"/>' },
  { name: 'file', paths: '<path d="M13 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9z"/><path d="M13 3v6h6M9 14h6M9 17h4"/>' },
  { name: 'chart', paths: '<path d="M4 20h16M7 20v-6M12 20V8M17 20v-9"/>' },
  { name: 'globe', paths: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14 0 18M12 3c-3 3.5-3 14 0 18"/>' },
  { name: 'puzzle', paths: '<path d="M10 3a2 2 0 0 1 4 0v2h3a1 1 0 0 1 1 1v3h2a2 2 0 0 1 0 4h-2v5a1 1 0 0 1-1 1h-5v-2a2 2 0 0 0-4 0v2H4a1 1 0 0 1-1-1v-5h2a2 2 0 0 0 0-4H3V6a1 1 0 0 1 1-1h6z"/>' },
  { name: 'terminal', paths: '<path d="M4 4h16a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z"/><path d="M7 9l3 3-3 3M12 15h5"/>' },
  { name: 'zap', paths: '<path d="M13 2L4 14h6l-1 8 9-12h-6z"/>' },
  { name: 'layers', paths: '<path d="M12 3l9 5-9 5-9-5zM3 13l9 5 9-5"/>' },
  { name: 'box', paths: '<path d="M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12L4 7.5M12 12v9"/>' },
  { name: 'users', paths: '<circle cx="9" cy="8" r="3.5"/><path d="M3 20c.8-3.2 3-5 6-5s5.2 1.8 6 5M16 4.5a3.5 3.5 0 0 1 0 7M18 20c-.4-1.9-1.2-3.4-2.3-4.4"/>' },
  { name: 'bell', paths: '<path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 19a2 2 0 0 0 4 0"/>' },
  { name: 'lock', paths: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>' },
  { name: 'code', paths: '<path d="M8 6l-6 6 6 6M16 6l6 6-6 6M13 4l-2 16"/>' },
]

/**
 * 翻译表单字段元数据的 mock 镜像，必须与 plugins/website/schema.go 的 entityViews 一致。
 * 真接口由 Go 侧的 TestEntityViewsMatchWhitelist 把关；这里只让不起后端的 mock 流程能走通。
 */
const mockSchemas: Record<string, EntitySchema> = {
  setting: {
    entity: 'setting',
    publishable: false,
    translatable: [
      { key: 'siteDesc', labelKey: 'plugin_website.fields.siteDesc', widget: 'textarea', max: 300, rows: 2 },
      { key: 'homeTitle', labelKey: 'plugin_website.fields.homeTitle', widget: 'textarea', max: 200, rows: 2 },
      { key: 'homeSubtitle', labelKey: 'plugin_website.fields.homeSubtitle', widget: 'textarea', max: 500, rows: 3 },
      { key: 'homeCtaLabel', labelKey: 'plugin_website.fields.homeCtaLabel', widget: 'text', max: 64 },
      { key: 'seoKeywords', labelKey: 'plugin_website.fields.seoKeywords', widget: 'textarea', max: 300, rows: 2 },
      { key: 'footerNote', labelKey: 'plugin_website.fields.footerNote', widget: 'textarea', max: 300, rows: 2 },
    ],
  },
  nav: {
    entity: 'nav',
    publishable: false,
    translatable: [{ key: 'label', labelKey: 'plugin_website.fields.navLabel', widget: 'text', max: 64 }],
  },
  feature: {
    entity: 'feature',
    publishable: false,
    translatable: [
      { key: 'title', labelKey: 'plugin_website.fields.featureTitle', widget: 'text', max: 128 },
      { key: 'description', labelKey: 'plugin_website.fields.featureDesc', widget: 'textarea', max: 500, rows: 3 },
    ],
  },
  page: {
    entity: 'page',
    publishable: true,
    translatable: [
      { key: 'title', labelKey: 'plugin_website.fields.pageTitle', widget: 'text', max: 200 },
      { key: 'seoDesc', labelKey: 'plugin_website.fields.seoDesc', widget: 'text', max: 300 },
      { key: 'content', labelKey: 'plugin_website.fields.content', widget: 'rich' },
    ],
  },
  post: {
    entity: 'post',
    publishable: true,
    translatable: [
      { key: 'title', labelKey: 'plugin_website.fields.postTitle', widget: 'text', max: 200 },
      { key: 'summary', labelKey: 'plugin_website.fields.summary', widget: 'textarea', max: 500, rows: 2 },
      { key: 'content', labelKey: 'plugin_website.fields.content', widget: 'rich' },
    ],
  },
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/website/i18n/schema/:entity',
    handler: ({ params }) => mockSchemas[params.entity] ?? mockError(404, 'unknown entity'),
  },
  { method: 'GET', url: '/plugins/website/settings', handler: () => settings },
  {
    method: 'POST',
    url: '/plugins/website/media',
    handler: () => ({ url: '/website-assets/media/8100000000000000999', mime: 'image/png', size: 1024 }),
  },
  {
    method: 'PUT',
    url: '/plugins/website/settings',
    handler: ({ body }) => {
      Object.assign(settings, body, { updatedAt: new Date().toISOString() })
      return settings
    },
  },
  { method: 'GET', url: '/plugins/website/navs', handler: () => navs },
  {
    method: 'POST',
    url: '/plugins/website/navs',
    handler: ({ body }) => {
      const row: NavItem = { id: id(100 + navs.length), parentId: String(body.parentId ?? '0'), label: body.label, href: body.href, sort: body.sort ?? 0, enabled: !!body.enabled, i18nJson: body.i18nJson ?? '' }
      navs = [...navs, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/navs/reorder',
    handler: ({ body }) => {
      for (const item of body.items ?? []) {
        const row = navs.find((x) => x.id === item.id)
        if (row) {
          row.parentId = String(item.parentId ?? '0')
          row.sort = item.sort ?? row.sort
        }
      }
      return { ok: true }
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/navs/:id',
    handler: ({ params, body }) => {
      const row = navs.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      row.parentId = String(body.parentId ?? '0')
      row.label = body.label
      row.href = body.href
      row.sort = body.sort ?? 0
      row.enabled = !!body.enabled
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/navs/:id',
    handler: ({ params }) => {
      navs = navs.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  { method: 'GET', url: '/plugins/website/features', handler: () => features },
  { method: 'GET', url: '/plugins/website/feature-icons', handler: () => featureIcons },
  {
    method: 'POST',
    url: '/plugins/website/features',
    handler: ({ body }) => {
      const row: FeatureItem = {
        id: id(200 + features.length), icon: body.icon ?? 'building', title: body.title,
        description: body.description ?? '', sort: body.sort ?? 0, enabled: !!body.enabled, i18nJson: body.i18nJson ?? '',
      }
      features = [...features, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/features/:id',
    handler: ({ params, body }) => {
      const row = features.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      row.icon = body.icon
      row.title = body.title
      row.description = body.description
      row.sort = body.sort ?? 0
      row.enabled = !!body.enabled
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/features/:id',
    handler: ({ params }) => {
      features = features.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/pages',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = pages.filter((p) => !kw || p.title.toLowerCase().includes(kw) || p.slug.includes(kw))
      if (query.status === 'published') rows = rows.filter((p) => p.published)
      if (query.status === 'draft') rows = rows.filter((p) => !p.published)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/website/pages',
    handler: ({ body }) => {
      const row: SitePage = {
        id: id(300 + pages.length), slug: body.slug, title: body.title,
        seoDesc: body.seoDesc ?? '', content: body.content ?? '', published: !!body.published,
        i18nJson: body.i18nJson ?? '', publishedJson: body.publishedJson ?? '', updatedAt: new Date().toISOString(),
      }
      pages = [...pages, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/pages/:id',
    handler: ({ params, body }) => {
      const row = pages.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      Object.assign(row, body, { updatedAt: new Date().toISOString() })
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/pages/:id',
    handler: ({ params }) => {
      pages = pages.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/posts',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = posts.filter((p) => !kw || p.title.toLowerCase().includes(kw) || p.slug.includes(kw))
      if (query.status === 'published') rows = rows.filter((p) => p.published)
      if (query.status === 'draft') rows = rows.filter((p) => !p.published)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/website/posts',
    handler: ({ body }) => {
      const now = new Date().toISOString()
      const row: SitePost = {
        id: id(400 + posts.length), slug: body.slug, title: body.title,
        coverUrl: body.coverUrl ?? '', summary: body.summary ?? '', seoDesc: body.seoDesc ?? '', content: body.content ?? '',
        published: !!body.published, publishedAt: body.published ? now : null, i18nJson: body.i18nJson ?? '',
        publishedJson: body.publishedJson ?? '', updatedAt: now,
      }
      posts = [...posts, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/posts/:id',
    handler: ({ params, body }) => {
      const row = posts.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      const wasPublished = row.published
      Object.assign(row, body, { updatedAt: new Date().toISOString() })
      if (row.published && !wasPublished && !row.publishedAt) row.publishedAt = row.updatedAt
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/posts/:id',
    handler: ({ params }) => {
      posts = posts.filter((x) => x.id !== params.id)
      return { ok: true }
    },
  },
])
