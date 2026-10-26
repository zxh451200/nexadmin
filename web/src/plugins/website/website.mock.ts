import { defineMock, mockError } from '@/framework/request/mock'
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
  icp: '',
  footerNote: '框架区源码以 MIT 许可开源；业务与插件代码归各自作者。',
  marketEnabled: true,
  i18nJson: '',
  updatedAt: ago(60 * 24),
}

let navs: NavItem[] = [
  { id: id(1), label: '能力', href: '#features', sort: 10, enabled: true, i18nJson: '' },
  { id: id(2), label: '动态', href: '/news', sort: 20, enabled: true, i18nJson: '' },
  { id: id(3), label: '关于', href: '/p/about', sort: 30, enabled: true, i18nJson: '' },
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
    updatedAt: ago(60 * 26),
  },
]

let posts: SitePost[] = [
  {
    id: id(1), slug: 'hello-nexadmin', title: 'NexAdmin 上线：以源码交付的后台管理框架', published: true,
    coverUrl: '', summary: '后端 Gin + GORM、前端 React + shadcn/ui，生产构建为单一二进制。',
    content: '<p>十分钟上手：构建、启动、打开 <code>/setup</code> 向导四步完成初始化。</p>',
    publishedAt: ago(60 * 24 * 3), i18nJson: '', updatedAt: ago(60 * 24 * 3),
  },
  {
    id: id(2), slug: 'website-plugin', title: '官网插件发布：企业官网即插即用', published: true,
    coverUrl: '', summary: '匿名公开官网（服务端渲染、SEO 友好）+ 后台内容管理。',
    content: '<p>你正在浏览的页面，就是它的默认模板。</p>',
    publishedAt: ago(60 * 24), i18nJson: '', updatedAt: ago(60 * 24),
  },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/website/languages',
    handler: () => [
      { code: 'zh-CN', label: '简体中文', htmlLang: 'zh-CN', default: true },
      { code: 'en-US', label: 'English', htmlLang: 'en', default: false },
    ],
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
      const row: NavItem = { id: id(100 + navs.length), label: body.label, href: body.href, sort: body.sort ?? 0, enabled: !!body.enabled, i18nJson: body.i18nJson ?? '' }
      navs = [...navs, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/navs/:id',
    handler: ({ params, body }) => {
      const row = navs.find((x) => x.id === params.id) ?? mockError(404, 'not found')
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
        i18nJson: body.i18nJson ?? '', updatedAt: new Date().toISOString(),
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
        coverUrl: body.coverUrl ?? '', summary: body.summary ?? '', content: body.content ?? '',
        published: !!body.published, publishedAt: body.published ? now : null, i18nJson: body.i18nJson ?? '', updatedAt: now,
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
