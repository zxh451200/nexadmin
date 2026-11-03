import { defineMock, mockError } from '@/framework/request/mock'
import type { EntitySchema } from '@/framework/components/i18n'
import type { FeatureItem, MessageStatus, NavItem, PostCategory, PostTag, SiteMessage, SitePage, SitePost, SiteSettings } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const id = (n: number) => String(8100000000000000000n + BigInt(n))

const settings: SiteSettings = {
  id: '1',
  siteName: 'NexAdmin',
  siteDesc: '以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui，生产构建为单一二进制。',
  logoUrl: '',
  faviconUrl: '',
  ogImageUrl: '',
  headCode: '',
  titleSuffix: '',
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
  messageNotifyEmail: true,
  contactConsent: false,
  contactConsentUrl: '',
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
    seoTitle: '', seoDesc: 'NexAdmin 是以源码交付的后台管理框架。',
    content: '<p>NexAdmin 是一个以源码交付的后台管理框架：后端 Gin + GORM，前端 React + shadcn/ui。</p>',
    i18nJson: '',
    publishedJson: '',
    ogImage: '', noindex: false,
    updatedAt: ago(60 * 26),
  },
]

let posts: SitePost[] = [
  {
    id: id(1), slug: 'hello-nexadmin', title: 'NexAdmin 上线：以源码交付的后台管理框架', published: true,
    coverUrl: '', summary: '后端 Gin + GORM、前端 React + shadcn/ui，生产构建为单一二进制。',
    seoDesc: 'NexAdmin：以源码交付的后台管理框架，Gin + GORM + React，生产环境单二进制部署。',
    seoTitle: '', seoKeywords: '后台框架,源码交付',
    // 新闻行内嵌的栏目/标签副本 postCount 恒为 0：后端 Preload 出来的也是 0，计数只在各自清单接口里回填
    categories: [{ id: id(701), title: '产品动态', slug: 'product', sort: 0, postCount: 0 }],
    tags: [{ id: id(901), name: '源码交付', slug: '源码交付', postCount: 0 }],
    content: '<p>十分钟上手：构建、启动、打开 <code>/setup</code> 向导四步完成初始化。</p>',
    publishedAt: ago(60 * 24 * 3),
    i18nJson: JSON.stringify({
      'en-US': {
        title: 'NexAdmin is live: an admin framework delivered as source',
        summary: 'Gin + GORM backend, React + shadcn/ui frontend, one binary in production.',
        content: '<p>Ten minutes to start: build, run, and finish the four-step wizard at <code>/setup</code>.</p>',
      },
    }),
    publishedJson: '', noindex: false,
    views: 128, pinned: true, author: '市场部',
    updatedAt: ago(60 * 24 * 3),
  },
  {
    id: id(2), slug: 'website-plugin', title: '官网插件发布：企业官网即插即用', published: true,
    coverUrl: '', summary: '匿名公开官网（服务端渲染、SEO 友好）+ 后台内容管理。',
    seoDesc: '官网插件：服务端渲染、SEO 友好的企业官网，后台可管设置、导航、页面与新闻。',
    seoTitle: '', seoKeywords: '',
    categories: [{ id: id(702), title: '插件发布', slug: 'plugins', sort: 0, postCount: 0 }, { id: id(701), title: '产品动态', slug: 'product', sort: 0, postCount: 0 }],
    tags: [{ id: id(901), name: '源码交付', slug: '源码交付', postCount: 0 }, { id: id(902), name: 'release', slug: 'release', postCount: 0 }],
    content: '<p>你正在浏览的页面，就是它的默认模板。</p>',
    publishedAt: ago(60 * 24), i18nJson: '', publishedJson: '', noindex: false,
    views: 64, pinned: false, author: '',
    updatedAt: ago(60 * 24),
  },
]

// 新新闻的号段同样要和种子的 401/402 分开：按条数让号，第三条会正好撞上种子的 402。
let nextPostId = 450

// 新闻分类（栏目）：mock 里只有两条种子；新建走 POST /categories（见下方路由）。
// 行上的 postCount 恒为 0，GET /categories 查询时按新闻真实算，与后端 gorm:"-" 同一口径。
let categories: PostCategory[] = [
  { id: id(701), title: '产品动态', slug: 'product', sort: 0, postCount: 0 },
  { id: id(702), title: '插件发布', slug: 'plugins', sort: 1, postCount: 0 },
]
// 新闻标签：名字是身份，slug 只是 URL 标识；同样由 GET 回填 postCount。
let tags: PostTag[] = [
  { id: id(901), name: '源码交付', slug: '源码交付', postCount: 0 },
  { id: id(902), name: 'release', slug: 'release', postCount: 0 },
]
// 与后端 service.go 的 SaveCategory 对齐：同一个 slug 规则，否则 mock 会存出一个打不开的 /news/
const slugReg = /^[a-z0-9][a-z0-9-]{0,63}$/
function checkCategory(title: string, slug: string) {
  if (!slugReg.test(slug)) mockError(400, 'slug 只能是小写字母、数字与连字符，长度 1-64')
  if (!title) mockError(400, '分类标题必填')
}
// 新分类的号段要和种子的 701/702 分开：按已有条数让号会让第一条新建正好撞上 702，
// 两行同 key 后新行沿用旧行的行内状态，看着像点一下多造了一条。
let nextCategoryId = 800
// 标签同理：种子占 901+，新建走 950 起的独立号段。
let nextTagId = 950

/** 与后端 taxonomy.go 的 asciiSlug 同口径：拉丁名压成小写连字符，非拉丁字符压成单个连字符。 */
function asciiSlug(s: string): string {
  let out = ''
  let prevDash = false
  for (const r of s.toLowerCase()) {
    if (/[a-z0-9]/.test(r)) {
      out += r
      prevDash = false
    } else if (!prevDash && out) {
      out += '-'
      prevDash = true
    }
  }
  return out.replace(/-$/, '')
}

/** tagSlugOf：压不出拉丁名的中文名直接用原名，浏览器百分号编码后照样能分享（同后端）。 */
const tagSlugOf = (name: string) => asciiSlug(name) || name

/** normalizeTagName：折叠空白 + 按字符限长，与后端同名函数一致（列宽 64）。 */
const normalizeTagName = (s: string) => s.trim().replace(/\s+/g, ' ').slice(0, 64)

/** 同名 slug 让号：foo → foo-2，否则「Foo Bar」与「foo-bar」会撞唯一索引（同后端）。 */
function uniqueTagSlug(base: string): string {
  let slug = base
  for (let n = 2; tags.some((t) => t.slug === slug); n++) slug = `${base}-${n}`
  return slug
}

/** 上限与文案都和后端一致（maxTagsPerPost / website.tag_too_many），且要在建标签之前挡住。 */
const maxTagsPerPost = 20
function checkTagLimit(names: unknown) {
  if (Array.isArray(names) && names.length > maxTagsPerPost) mockError(400, `一篇新闻最多 ${maxTagsPerPost} 个标签`)
}

/** 与后端 resolveTagsTx 一致：按名字（忽略大小写）找或建，顺序同输入、按名字去重。 */
function resolveTags(names: unknown[]): PostTag[] {
  const out: PostTag[] = []
  const seen = new Set<string>()
  for (const raw of names) {
    const name = normalizeTagName(String(raw ?? ''))
    if (!name) continue
    const key = name.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    let row = tags.find((t) => t.name.toLowerCase() === key)
    if (!row) {
      row = { id: id(nextTagId++), name, slug: uniqueTagSlug(tagSlugOf(name)), postCount: 0 }
      tags = [...tags, row]
    }
    out.push(row)
  }
  return out
}

/** postCount 是查询时算出来的：写死就和新闻种子对不上。 */
function decorate<T extends { id: string }>(rows: T[], pick: (p: SitePost) => T[] | undefined): T[] {
  const counts = new Map<string, number>()
  for (const p of posts) for (const r of pick(p) ?? []) counts.set(r.id, (counts.get(r.id) ?? 0) + 1)
  return rows.map((r) => ({ ...r, postCount: counts.get(r.id) ?? 0 }))
}

// 后台留言收件箱：三态各留几条，未读/已读/已处理的界面都能在 mock 模式里看到。
let messages: SiteMessage[] = [
  {
    id: id(501), name: '张先生', email: 'zhang@example.com', phone: '13800000000',
    content: '我们公司想用这套框架做内部后台，想确认二次开发的报价和交付周期，方便的话请回个电话。',
    ip: '203.0.113.24', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', status: 'new',
    createdAt: ago(35), updatedAt: ago(35),
  },
  {
    id: id(502), name: 'Nora', email: 'nora@studio.io', phone: '',
    content: 'Does the source delivery include the plugin SDK and the license server?',
    ip: '198.51.100.7', ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', status: 'new',
    createdAt: ago(180), updatedAt: ago(180),
  },
  {
    id: id(503), name: '李女士', email: 'li@vendor.cn', phone: '13900000000',
    content: '咨询一下多租户隔离的实现方式，我们有两个子公司要共用一套系统。',
    ip: '203.0.113.51', ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)', status: 'read',
    createdAt: ago(60 * 20), updatedAt: ago(60 * 19),
  },
  {
    id: id(504), name: '王工', email: 'wang@dev.team', phone: '',
    content: '官网插件的留言表能不能加自定义字段？',
    ip: '192.0.2.10', ua: 'Mozilla/5.0 (X11; Linux x86_64)', status: 'handled',
    createdAt: ago(60 * 40), updatedAt: ago(60 * 39),
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
    // 与后端默认一致：2MB、位图四类。真机上这条会跟着系统参数「上传限制」变。
    method: 'GET',
    url: '/plugins/website/media/limits',
    handler: () => ({
      maxMb: 2,
      maxBytes: 2 * 1024 * 1024,
      mimes: ['image/png', 'image/jpeg', 'image/webp', 'image/gif'],
      labels: ['PNG', 'JPEG', 'WebP', 'GIF'],
    }),
  },
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
    url: '/plugins/website/navs',
    handler: ({ query }) => {
      const roots = String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean)
      // 与后端同口径：删一个节点连同它的整棵子树
      const doomed = new Set(roots)
      let grew = true
      while (grew) {
        grew = false
        for (const n of navs) {
          if (!doomed.has(n.id) && doomed.has(n.parentId)) {
            doomed.add(n.id)
            grew = true
          }
        }
      }
      const before = navs.length
      navs = navs.filter((x) => !doomed.has(x.id))
      return { deleted: before - navs.length }
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
    url: '/plugins/website/features',
    handler: ({ query }) => {
      const ids = new Set(String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean))
      const before = features.length
      features = features.filter((x) => !ids.has(x.id))
      return { deleted: before - features.length }
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
        seoTitle: body.seoTitle ?? '', seoDesc: body.seoDesc ?? '', content: body.content ?? '', published: !!body.published,
        i18nJson: body.i18nJson ?? '', publishedJson: body.publishedJson ?? '',
        ogImage: body.ogImage ?? '', noindex: !!body.noindex, updatedAt: new Date().toISOString(),
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
    // 预览链接在 mock 下直接给页面地址：mock 站点没有发布闸门，点开就是内容
    method: 'GET',
    url: '/plugins/website/pages/:id/preview',
    handler: ({ params }) => {
      const row = pages.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      return { url: `/p/${row.slug}` }
    },
  },
  {
    method: 'POST',
    url: '/plugins/website/pages/:id/copy',
    handler: ({ params }) => {
      const src = pages.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      let slug = `${src.slug}-copy`
      for (let i = 2; pages.some((x) => x.slug === slug); i++) slug = `${src.slug}-copy-${i}`
      const row: SitePage = {
        id: id(300 + pages.length), slug, title: src.title, seoTitle: src.seoTitle, seoDesc: src.seoDesc, content: src.content,
        published: false, i18nJson: src.i18nJson, publishedJson: '',
        ogImage: src.ogImage, noindex: false, updatedAt: new Date().toISOString(),
      }
      pages = [...pages, row]
      return row
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/categories',
    handler: () => decorate([...categories].sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id)), (p) => p.categories),
  },
  {
    method: 'POST',
    url: '/plugins/website/categories',
    handler: ({ body }) => {
      const title = String(body.title ?? '').trim()
      const slug = String(body.slug ?? '').trim()
      checkCategory(title, slug)
      const row: PostCategory = { id: id(nextCategoryId++), title, slug, sort: Number(body.sort ?? 0), postCount: 0 }
      categories = [...categories, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/categories/:id',
    handler: ({ params, body }) => {
      const row = categories.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      const title = String(body.title ?? row.title).trim()
      const slug = String(body.slug ?? row.slug).trim()
      checkCategory(title, slug)
      row.title = title
      row.slug = slug
      row.sort = Number(body.sort ?? row.sort)
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/categories/:id',
    handler: ({ params }) => {
      categories = categories.filter((x) => x.id !== params.id)
      posts = posts.map((p) => ({ ...p, categories: (p.categories ?? []).filter((c) => c.id !== params.id) }))
      return { ok: true }
    },
  },
  {
    // 标签清单：后端按名字排（标签没有人工 sort），带每个标签的新闻数
    method: 'GET',
    url: '/plugins/website/tags',
    handler: () => decorate([...tags].sort((a, b) => a.name.localeCompare(b.name)), (p) => p.tags),
  },
  {
    method: 'POST',
    url: '/plugins/website/tags',
    handler: ({ body }) => {
      const name = normalizeTagName(String(body.name ?? ''))
      if (!name) mockError(400, '标签需要名称')
      const slug = String(body.slug ?? '').trim()
      if (slug) {
        if (!slugReg.test(slug)) mockError(400, 'slug 只能是小写字母、数字与连字符，长度 1-64')
        if (tags.some((t) => t.slug === slug)) mockError(400, '这个标识已被占用，换一个')
      }
      const row: PostTag = { id: id(nextTagId++), name, slug: slug || uniqueTagSlug(tagSlugOf(name)), postCount: 0 }
      tags = [...tags, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/tags/:id',
    handler: ({ body, params }) => {
      const row = tags.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      const name = normalizeTagName(String(body.name ?? row.name))
      if (!name) mockError(400, '标签需要名称')
      const slug = String(body.slug ?? '').trim()
      if (slug) {
        if (!slugReg.test(slug)) mockError(400, 'slug 只能是小写字母、数字与连字符，长度 1-64')
        if (tags.some((t) => t.slug === slug && t.id !== row.id)) mockError(400, '这个标识已被占用，换一个')
        row.slug = slug
      } else if (tagSlugOf(name) !== row.slug) {
        // 与后端同口径：slug 留空才按新名字重新派生，界面改名时会回传原 slug 保住链接
        row.slug = uniqueTagSlug(tagSlugOf(name))
      }
      row.name = name
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/tags/:id',
    handler: ({ params }) => {
      tags = tags.filter((x) => x.id !== params.id)
      posts = posts.map((p) => ({ ...p, tags: (p.tags ?? []).filter((t) => t.id !== params.id) }))
      return { ok: true }
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/posts',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = posts.filter((p) => !kw || p.title.toLowerCase().includes(kw) || p.slug.includes(kw))
      const scheduled = (p: SitePost) => p.published && !!p.publishedAt && new Date(p.publishedAt).getTime() > Date.now()
      if (query.status === 'published') rows = rows.filter((p) => p.published && !scheduled(p))
      if (query.status === 'scheduled') rows = rows.filter(scheduled)
      if (query.status === 'draft') rows = rows.filter((p) => !p.published)
      if (query.categoryId) rows = rows.filter((p) => (p.categories ?? []).some((c) => c.id === query.categoryId))
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/posts/export',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = posts.filter((p) => !kw || p.title.toLowerCase().includes(kw) || p.slug.includes(kw))
      if (query.status === 'published') rows = rows.filter((p) => p.published)
      if (query.status === 'draft') rows = rows.filter((p) => !p.published)
      return [...rows].reverse()
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/posts/status',
    handler: ({ body }) => {
      const ids = new Set<string>((body.ids ?? []).map(String))
      const status = String(body.status ?? '')
      if (!ids.size || !['published', 'draft'].includes(status)) return mockError(400, 'bad request')
      let updated = 0
      for (const row of posts) {
        if (ids.has(row.id) && row.published !== (status === 'published')) {
          row.published = status === 'published'
          if (row.published && !row.publishedAt) row.publishedAt = new Date().toISOString()
          row.updatedAt = new Date().toISOString()
          updated++
        }
      }
      return { updated }
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/posts',
    handler: ({ query }) => {
      const ids = new Set(String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean))
      const before = posts.length
      posts = posts.filter((p) => !ids.has(p.id))
      return { deleted: before - posts.length }
    },
  },
  {
    method: 'POST',
    url: '/plugins/website/posts',
    handler: ({ body }) => {
      checkTagLimit(body.tags)
      const now = new Date().toISOString()
      const catIds: string[] = Array.isArray(body.categoryIds) ? body.categoryIds.map(String) : []
      const row: SitePost = {
        id: id(nextPostId++), slug: body.slug, title: body.title,
        coverUrl: body.coverUrl ?? '', summary: body.summary ?? '', seoDesc: body.seoDesc ?? '',
        seoTitle: body.seoTitle ?? '', seoKeywords: body.seoKeywords ?? '',
        categories: categories.filter((c) => catIds.includes(c.id)),
        tags: resolveTags(Array.isArray(body.tags) ? body.tags : []),
        content: body.content ?? '',
        published: !!body.published, publishedAt: body.published ? now : null, i18nJson: body.i18nJson ?? '',
        publishedJson: body.publishedJson ?? '', noindex: !!body.noindex,
        views: 0, pinned: !!body.pinned, author: body.author ?? '',
        updatedAt: now,
      }
      posts = [...posts, row]
      return row
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/posts/:id/preview',
    handler: ({ params }) => {
      const row = posts.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      return { url: `/news/${row.slug}` }
    },
  },
  {
    method: 'POST',
    url: '/plugins/website/posts/:id/copy',
    handler: ({ params }) => {
      const src = posts.find((x) => x.id === params.id) ?? mockError(404, 'not found')
      let slug = `${src.slug}-copy`
      for (let i = 2; posts.some((x) => x.slug === slug); i++) slug = `${src.slug}-copy-${i}`
      const now = new Date().toISOString()
      const row: SitePost = {
        ...src, id: id(nextPostId++), slug, published: false, publishedAt: null,
        publishedJson: '', updatedAt: now,
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
      checkTagLimit(body.tags)
      const wasPublished = row.published
      // tags/categoryIds 是表单字段（标签名与栏目 ID），行上存的是展开后的对象，不能直接 assign
      const { tags: tagNames, categoryIds: catIds, ...rest } = body
      Object.assign(row, rest, { updatedAt: new Date().toISOString() })
      if (Array.isArray(tagNames)) row.tags = resolveTags(tagNames)
      if (Array.isArray(catIds)) {
        const ids = catIds.map(String)
        row.categories = categories.filter((c) => ids.includes(c.id))
      }
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
  {
    method: 'GET',
    url: '/plugins/website/messages',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = messages.filter((m) => !kw || m.name.toLowerCase().includes(kw) || m.email.includes(kw) || m.content.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((m) => m.status === query.status)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/website/messages/unread-count',
    handler: () => ({ count: messages.filter((m) => m.status === 'new').length }),
  },
  {
    method: 'GET',
    url: '/plugins/website/messages/export',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = messages.filter((m) => !kw || m.name.toLowerCase().includes(kw) || m.email.includes(kw) || m.content.toLowerCase().includes(kw))
      if (query.status) rows = rows.filter((m) => m.status === query.status)
      return [...rows].reverse()
    },
  },
  {
    method: 'PUT',
    url: '/plugins/website/messages/status',
    handler: ({ body }) => {
      const ids = new Set<string>((body.ids ?? []).map(String))
      const status = String(body.status ?? '') as MessageStatus
      if (!ids.size || !['new', 'read', 'handled'].includes(status)) return mockError(400, 'bad request')
      let updated = 0
      for (const row of messages) {
        if (ids.has(row.id)) {
          row.status = status
          row.updatedAt = new Date().toISOString()
          updated++
        }
      }
      return { updated }
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/website/messages',
    handler: ({ query }) => {
      const ids = new Set(String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean))
      const before = messages.length
      messages = messages.filter((m) => !ids.has(m.id))
      return { deleted: before - messages.length }
    },
  },
])
