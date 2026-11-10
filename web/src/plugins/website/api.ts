import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { api, http } from '@/framework/request/http'
import { MENU_BADGES_KEY } from '@/framework/layout/menuBadge'
import { useLanguages as usePlatformLanguages } from '@/framework/i18n/useLanguages'
import type { EntitySchema, LocaleOption } from '@/framework/components/i18n'
import type { Page, PageQuery } from '@/framework/request/types'

/** 后端 entityViews 的键；新增内容类型时同步。 */
export type I18nEntity = 'setting' | 'nav' | 'feature' | 'page' | 'post' | 'product'

/** 页脚社交图标可选平台；键集合与后端 socialPlatforms 一致。 */
export type SocialPlatform =
  | 'github'
  | 'x'
  | 'linkedin'
  | 'youtube'
  | 'instagram'
  | 'facebook'
  | 'discord'
  | 'telegram'
  | 'whatsapp'
  | 'reddit'
  | 'tiktok'
  | 'twitch'
  | 'mastodon'
  | 'wechat'
  | 'weibo'
  | 'bilibili'
  | 'rss'
  | 'mail'

export interface SocialLink {
  platform: SocialPlatform
  url: string
}

export interface FriendLink {
  name: string
  url: string
}

export interface SiteSettings {
  id: string
  siteName: string
  siteDesc: string
  logoUrl: string
  /** 浏览器标签图标；空 = 模板内置 favicon.svg */
  faviconUrl: string
  /** 站点默认分享图（og:image 兜底）；空 = 模板内置 og.png */
  ogImageUrl: string
  /** 自定义 <head> 代码（统计脚本等），原样注入；空 = 不注入 */
  headCode: string
  /** <title> 后缀，替换默认的「 - 站点名」；空 = 沿用内置组合 */
  titleSuffix: string
  /** 被设为首页的单页 slug；空 = 内置首页。页面被删或未发布时官网自动回退内置首页 */
  homeSlug: string
  homeTitle: string
  homeSubtitle: string
  homeCtaLabel: string
  homeCtaHref: string
  seoKeywords: string
  contactEmail: string
  contactPhone: string
  qrCodeUrl: string
  qrCodeCaption: string
  /** 页脚社交图标（后端 JSON 数组展开） */
  socials: SocialLink[]
  /** 页脚友情链接 */
  friendLinks: FriendLink[]
  icp: string
  footerNote: string
  marketEnabled: boolean
  /** 新留言是否抄送一封到 contactEmail（SMTP 未配置时自动跳过） */
  messageNotifyEmail: boolean
  /** 留言是否必须先勾选「已阅读并同意隐私政策」 */
  contactConsent: boolean
  /** 被同意的政策地址：站内 /p/xxx 或完整 https:// 地址（开启时必填） */
  contactConsentUrl: string
  i18nJson: string
  updatedAt: string
}

export type SiteSettingsForm = Omit<SiteSettings, 'id' | 'updatedAt'>

export interface NavItem {
  id: string
  /** 上级导航 ID；"0" = 顶级（雪花 ID 用字符串避免丢精度） */
  parentId: string
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface NavForm {
  parentId: string
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

/** 拖拽排序：一次提交同一层内的完整顺序。 */
export interface NavOrderItem {
  id: string
  parentId: string
  sort: number
}

export interface FeatureItem {
  id: string
  icon: string
  title: string
  description: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface FeatureForm {
  icon: string
  title: string
  description: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface SitePage {
  id: string
  slug: string
  title: string
  /** 独立 meta 标题；空 = 用「标题 - 站点名」 */
  seoTitle: string
  seoDesc: string
  content: string
  published: boolean
  i18nJson: string
  /** 非默认语言发布态；空 = 所有语言沿用 published */
  publishedJson: string
  /** 社交分享图（og:image）；空 = 站点默认分享图 */
  ogImage: string
  /** 关闭搜索引擎收录（<meta robots> 与 sitemap 同步） */
  noindex: boolean
  /** 管理端列表排序：越小越靠前；0 = 未设置（排在最后） */
  sort: number
  updatedAt: string
}

export interface PageForm {
  slug: string
  title: string
  seoTitle: string
  seoDesc: string
  content: string
  published: boolean
  i18nJson: string
  publishedJson: string
  ogImage: string
  noindex: boolean
  sort: number
}

export interface SitePost {
  id: string
  slug: string
  title: string
  coverUrl: string
  summary: string
  seoDesc: string
  /** 独立 meta 标题；空 = 用「标题 - 站点名」 */
  seoTitle: string
  seoKeywords: string
  content: string
  published: boolean
  publishedAt?: string | null
  /** 所属分类；保存时按完整列表整体替换 */
  categories?: PostCategory[]
  /** 标签；保存时按名字整体替换，后端可能不带这个字段（老数据） */
  tags?: PostTag[]
  i18nJson: string
  /** 非默认语言发布态；空 = 所有语言沿用 published */
  publishedJson: string
  /** 关闭搜索引擎收录（<meta robots> 与 sitemap 同步） */
  noindex: boolean
  /** 公开详情浏览量（只增不清） */
  views: number
  /** 置顶：公开列表置顶优先于发布时间 */
  pinned: boolean
  /** 展示名（不关联账号） */
  author: string
  updatedAt: string
}

export interface PostForm {
  slug: string
  title: string
  coverUrl: string
  summary: string
  seoTitle: string
  seoDesc: string
  seoKeywords: string
  content: string
  published: boolean
  /** 只在发布时生效；设为未来时间即定时发布，到点前公开端不展示 */
  publishedAt?: string
  /** 所属分类的完整 ID 列表；空 = 清空分类 */
  categoryIds: string[]
  /** 标签名的完整列表；空 = 清空，没出现过的名字后端当场建标签，上限 20 */
  tags: string[]
  i18nJson: string
  publishedJson: string
  noindex: boolean
  /** 展示名；置顶：公开列表置顶优先 */
  author: string
  pinned: boolean
}

/** 新闻分类，即编辑部的栏目：后台单独维护、集合有限，公开侧按 /news?category=<slug> 导航。 */
export interface PostCategory {
  id: string
  title: string
  slug: string
  sort: number
  /** 栏目下的新闻数：界面按它排栏目，删栏目前先让人看见影响面 */
  postCount: number
}

/** 新闻标签：编辑器里打字即长的自由主题词，名字是身份（展示字段是 name，不是 title）。 */
export interface PostTag {
  id: string
  name: string
  slug: string
  /** 语义同 PostCategory.postCount */
  postCount: number
}

export interface CategoryForm {
  title: string
  slug: string
  sort: number
}

export interface TagForm {
  name: string
  /** 留空由后端按 name 派生；改名时回传原 slug，已分享的 /news?tag= 链接才不会断 */
  slug: string
}

export interface ContentQuery extends PageQuery {
  status?: string
  /** 语言码：筛「该语言完全没有译文」的条目（不是缺某个字段，那看列表里的 done/total 徽标） */
  missing?: string
  /** 新闻分类筛选（只对新闻列表生效） */
  categoryId?: string
}

/** 商品动态属性的一条键值对（对标阿里的类目属性）。 */
export interface ProductSpec {
  label: string
  value: string
}

/**
 * 产品状态机，对齐阿里国际站：本地一期只用 draft / online / offline；
 * pending / rejected 留给二期同步——商品提交到阿里审核后回写「审核中 / 审核不通过」。
 */
export type ProductStatus = 'draft' | 'online' | 'offline' | 'pending' | 'rejected'

/** 同步状态：none=从未同步，pending=已提交待确认，synced=已发布到阿里，failed=上次失败。 */
export type ProductSyncStatus = 'none' | 'pending' | 'synced' | 'failed'

export interface SiteProduct {
  id: string
  slug: string
  title: string
  /** 搜索关键词（对标阿里的 1-3 个关键词，空格分隔） */
  keywords: string
  summary: string
  /** 独立 meta 标题；空 = 用「标题 - 站点名」 */
  seoTitle: string
  seoDesc: string
  seoKeywords: string
  content: string
  noindex: boolean
  /** 主图 */
  coverUrl: string
  /** 图集 URL 数组的 JSON 文本（后端 text 列，前端自己解析） */
  imagesJson: string
  videoUrl: string
  /** 价格区间；0 = 未定价（只展示询盘） */
  priceMin: number
  priceMax: number
  currency: string
  /** 计量单位：piece / set… */
  priceUnit: string
  /** 最小起订量 */
  moq: number
  /** 发货天数 */
  leadTimeDays: number
  port: string
  /** 付款方式（如 "T/T, L/C"），详情页「交易信息」块展示 */
  paymentTerms: string
  /** 动态属性键值对的 JSON 文本 */
  specsJson: string
  status: ProductStatus
  /** 阿里同步预留（一期只读展示） */
  alibabaProductId: string
  syncStatus: ProductSyncStatus
  syncedAt?: string | null
  /** 所属类目；保存时按完整列表整体替换 */
  categories?: ProductCategoryNode[]
  i18nJson: string
  /** 非默认语言发布态；空 = 所有语言沿用 status（online 即发布） */
  publishedJson: string
  views: number
  author: string
  updatedAt: string
}

export interface ProductForm {
  slug: string
  title: string
  keywords: string
  summary: string
  seoTitle: string
  seoDesc: string
  seoKeywords: string
  content: string
  /** 表单里只出 draft / online；offline 由列表的上下架动作写入 */
  status: 'draft' | 'online'
  coverUrl: string
  images: string[]
  videoUrl: string
  priceMin: number
  priceMax: number
  currency: string
  priceUnit: string
  moq: number
  leadTimeDays: number
  port: string
  paymentTerms: string
  specs: ProductSpec[]
  /** 所属类目的完整 ID 列表；空 = 清空类目 */
  categoryIds: string[]
  i18nJson: string
  publishedJson: string
  noindex: boolean
  author: string
}

/**
 * 产品类目：对标阿里的「商品分组」，但支持最多 3 级树形层级。
 * 后端给平铺清单，前端自己组树（与导航同款）。
 */
export interface ProductCategoryNode {
  id: string
  parentId: string
  title: string
  slug: string
  sort: number
  /** 类目（不含子类目）下的商品数：删类目前先让人看见影响面 */
  productCount: number
}

export interface ProductCategoryForm {
  parentId: string
  title: string
  slug: string
  sort: number
}

/** 拖拽排序：一次提交整棵树的完整顺序（与导航同款）。 */
export interface ProductCategoryOrderItem {
  id: string
  parentId: string
  sort: number
}

/** 特性卡片内置图标：名称 + SVG 内部标记。路径以后端 iconPaths 为准，前端不另存一份。 */
export interface FeatureIcon {
  name: string
  paths: string
}

const BASE = '/plugins/website'
const KEY = ['plugin_website'] as const

/** 图标集随插件发布变化极少，按 Infinity 缓存，切页面回来不闪空列表。 */
export function useFeatureIcons() {
  return useQuery({
    queryKey: [...KEY, 'feature-icons'],
    queryFn: () => api.get<FeatureIcon[]>(`${BASE}/feature-icons`),
    staleTime: Infinity,
  })
}

/** 翻译表单的字段元数据：白名单与长度只在 Go 里有一份，界面据此渲染。 */
export function useI18nSchema(entity: I18nEntity) {
  return useQuery({
    queryKey: [...KEY, 'i18n-schema', entity],
    queryFn: () => api.get<EntitySchema>(`${BASE}/i18n/schema/${entity}`),
    staleTime: Infinity,
  })
}

/**
 * 批量机翻：同一批必须同一种格式（Format 是请求级的），所以纯文本与富文本分两批。
 * 只回译文、不落库——写库仍走各自的 save 接口，由后端统一清洗。
 */
export async function translateContent(
  entity: I18nEntity, texts: string[], from: string, to: string, format: 'text' | 'html',
): Promise<string[]> {
  const res = await api.post<{ texts: string[] }>(`${BASE}/i18n/translate/${entity}`, { texts, from, to, format })
  return res.texts ?? []
}

/**
 * 内容语言就是平台语言清单（sys_language，见 modules/system/language）：
 * 官网不再单独维护一份，否则「后台有哪些语言」和「官网有哪些语言」会各自跑偏。
 * label 取 name，整成框架 locale 表单需要的最小形状。
 */
export function useLanguages() {
  const { languages, isLoading } = usePlatformLanguages()
  // 必须 memo：LocaleFormProvider 的 context 以 locales 为依赖，
  // 每次渲染都新建数组会让整棵表单子树无谓重渲染。
  const data = useMemo<LocaleOption[]>(
    () => languages.map((l) => ({ code: l.code, label: l.name, default: l.default, prefix: l.prefix })),
    [languages],
  )
  return { data, isLoading }
}

export function useSettings() {
  return useQuery({
    queryKey: [...KEY, 'settings'],
    queryFn: () => api.get<SiteSettings>(`${BASE}/settings`),
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: SiteSettingsForm) => api.put<SiteSettings>(`${BASE}/settings`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'settings'] }),
  })
}

/** 图片上传的生效限制：大小随系统参数「上传限制」/config.yaml 变化，类型是后端持有的位图白名单。 */
export interface MediaLimits {
  maxMb: number
  maxBytes: number
  /** 文件选择器的 accept 值，如 image/png */
  mimes: string[]
  /** 给人看的类型名，如 PNG */
  labels: string[]
}

/** 官网管理页的生效上限：图片上传 + 页脚两个列表条数 + 导航层级 + 单篇标签数。全部由后端说一次。 */
export interface WebsiteLimits {
  media: MediaLimits
  socialMax: number
  friendMax: number
  navMaxDepth: number
  postTagsMax: number
}

export function useWebsiteLimits() {
  return useQuery({
    queryKey: [...KEY, 'limits'],
    queryFn: () => api.get<WebsiteLimits>(`${BASE}/limits`),
  })
}

/**
 * 出厂值：与服务端 plugins/website/nav.go 的 navMaxDepth、taxonomy.go 的 maxTagsPerPost 同值，
 * 只在 /limits 还没到货的瞬间兜底（界面少一档不会造成错误数据，服务端始终会拒收超限）。
 */
export const FALLBACK_NAV_MAX_DEPTH = 3
export const FALLBACK_POST_TAGS_MAX = 20

/** 导航层级上限：与后端校验同源，改服务端不用再来这里同步一次。 */
export function useNavMaxDepth(): number {
  const { data } = useWebsiteLimits()
  return data?.navMaxDepth || FALLBACK_NAV_MAX_DEPTH
}

/** 单篇新闻的标签上限：同上，与后端 maxTagsPerPost 同源。 */
export function usePostTagsMax(): number {
  const { data } = useWebsiteLimits()
  return data?.postTagsMax || FALLBACK_POST_TAGS_MAX
}

/** 上传 logo / 二维码 / favicon 小图；大小与类型上限见 useWebsiteLimits 的 media 字段，超限由请求层 toast 服务端文案。 */
export function uploadMedia(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  return http
    .post<{ url: string; mime: string; size: number }>(`${BASE}/media`, fd)
    .then((r) => r.data)
}

export function useNavs() {
  return useQuery({
    queryKey: [...KEY, 'navs'],
    queryFn: () => api.get<NavItem[]>(`${BASE}/navs`),
  })
}

export function useSaveNav() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: NavForm }) =>
      id ? api.put<NavItem>(`${BASE}/navs/${id}`, data) : api.post<NavItem>(`${BASE}/navs`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

/** 拖拽排序 / 改上级：后端在一个事务里批量写 sort 与 parentId。 */
export function useReorderNavs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (items: NavOrderItem[]) => api.put<null>(`${BASE}/navs/reorder`, { items }),
    // 失败时 TreeTable 会回滚乐观数据并 toast，这里只保证最终与库一致
    onSettled: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

export function useDeleteNav() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/navs/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

export function useDeleteNavs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/navs`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

export function useFeatures() {
  return useQuery({
    queryKey: [...KEY, 'features'],
    queryFn: () => api.get<FeatureItem[]>(`${BASE}/features`),
  })
}

export function useSaveFeature() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: FeatureForm }) =>
      id ? api.put<FeatureItem>(`${BASE}/features/${id}`, data) : api.post<FeatureItem>(`${BASE}/features`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'features'] }),
  })
}

export function useDeleteFeature() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/features/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'features'] }),
  })
}

export function useDeleteFeatures() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/features`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'features'] }),
  })
}

export function usePages(query: ContentQuery) {
  return useQuery({
    queryKey: [...KEY, 'pages', query],
    queryFn: () => api.get<Page<SitePage>>(`${BASE}/pages`, query),
    placeholderData: keepPreviousData,
  })
}

export function useSavePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: PageForm }) =>
      id ? api.put<SitePage>(`${BASE}/pages/${id}`, data) : api.post<SitePage>(`${BASE}/pages`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

export function useDeletePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/pages/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

/**
 * 签发草稿预览链接（24h 内有效）：/p/:slug?preview=…。
 * 未发布的页面公开站是 404，这条是发布前看真实版式的唯一入口；silent：错误由页面 toast。
 */
export function fetchPagePreviewURL(id: string): Promise<string> {
  return api.get<{ url: string }>(`${BASE}/pages/${id}/preview`, undefined, { silent: true }).then((d) => d.url)
}

/** 复制页面为草稿：内容与译文带走，发布态归零，slug 由后端自动去重为 <slug>-copy。 */
export function useCopyPage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<SitePage>(`${BASE}/pages/${id}/copy`, {}),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

/** 批量发布/下线：单条操作就是长度为 1 的 ids。updated 是服务端实际改动条数。 */
export function useSetPagesStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ ids, status }: { ids: string[]; status: 'published' | 'draft' }) =>
      api.put<{ updated: number }>(`${BASE}/pages/status`, { ids, status }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

/** 批量删除：deleted 是服务端实际删除条数（并发下有人先删了几条，界面据实刷新）。 */
export function useDeletePages() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/pages`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

export function usePosts(query: ContentQuery) {
  return useQuery({
    queryKey: [...KEY, 'posts', query],
    queryFn: () => api.get<Page<SitePost>>(`${BASE}/posts`, query),
    placeholderData: keepPreviousData,
  })
}

export function useSavePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: PostForm }) =>
      id ? api.put<SitePost>(`${BASE}/posts/${id}`, data) : api.post<SitePost>(`${BASE}/posts`, data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
      invalidateTaxonomies(qc)
    },
  })
}

export function useDeletePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/posts/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
      invalidateTaxonomies(qc)
    },
  })
}

/** 新闻增删改都会挪动栏目/标签下的新闻数，两份清单要跟着刷。 */
function invalidateTaxonomies(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: [...KEY, 'categories'] })
  void qc.invalidateQueries({ queryKey: [...KEY, 'tags'] })
}

/** 新闻分类：新闻表单与列表筛选共用一份。 */
export function useCategories() {
  return useQuery({
    queryKey: [...KEY, 'categories'],
    queryFn: () => api.get<PostCategory[]>(`${BASE}/categories`),
  })
}

export function useSaveCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: CategoryForm }) =>
      id ? api.put<PostCategory>(`${BASE}/categories/${id}`, data) : api.post<PostCategory>(`${BASE}/categories`, data),
    // 新闻列表的分类徽标带的是行内副本，改名后不刷列表就会显示旧名
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'categories'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
    },
  })
}

export function useDeleteCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/categories/${id}`),
    // 删除会连带解除新闻上的关联，列表同样要刷
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'categories'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
    },
  })
}

/** 新闻标签：编辑器里的自动补全与标签管理共用一份。 */
export function useTags() {
  return useQuery({
    queryKey: [...KEY, 'tags'],
    queryFn: () => api.get<PostTag[]>(`${BASE}/tags`),
  })
}

export function useSaveTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: TagForm }) =>
      id ? api.put<PostTag>(`${BASE}/tags/${id}`, data) : api.post<PostTag>(`${BASE}/tags`, data),
    // 改名会让标签清单与新闻上的标签一起变
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'tags'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
    },
  })
}

export function useDeleteTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/tags/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'tags'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
    },
  })
}

/**
 * 批量发布/下线/删除都会改列表内容：成功后统一刷新闻列表。
 * updated/deleted 是服务端实际改动条数，并发下有人先删了几条时界面据实刷新。
 */
export function useSetPostStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ ids, status }: { ids: string[]; status: 'published' | 'draft' }) =>
      api.put<{ updated: number }>(`${BASE}/posts/status`, { ids, status }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] }),
  })
}

export function useDeletePosts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/posts`, { ids: ids.join(',') }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
      invalidateTaxonomies(qc)
    },
  })
}

/**
 * 导出当前筛选下的全部新闻（不带分页，上限见后端 exportMax），CSV 由前端生成；
 * silent：错误由页面捕获后 toast，不走全局报错。
 */
export function fetchPostsForExport(query: Pick<ContentQuery, 'keyword' | 'status' | 'categoryId'>): Promise<SitePost[]> {
  return api.get<SitePost[]>(`${BASE}/posts/export`, { ...query }, { silent: true })
}

/** 签发草稿预览链接（24h 有效）：返回 /news/:slug?preview=<token>，新开标签页打开。 */
export function fetchPostPreview(id: string): Promise<string> {
  return api.get<{ url: string }>(`${BASE}/posts/${id}/preview`, undefined, { silent: true }).then((d) => d.url)
}

/** 复制为草稿：内容/译文/分类带走，发布态归零，slug 自动去重。 */
export function useCopyPost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<SitePost>(`${BASE}/posts/${id}/copy`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
      invalidateTaxonomies(qc)
    },
  })
}

/** 与后端 service.go 的 maxPostVersions 同值：界面提示保留额度用。 */
export const MAX_POST_VERSIONS = 50

/** 版本历史展示行：后端列表接口不回传正文（Post 的 content 是 mediumtext，不进列表响应）。 */
export interface PostVersionRow {
  version: number
  title: string
  summary: string
  createdBy: string
  createdAt: string
}

/** 单个历史版本详情：正文只在这里回传；words 是剥掉 HTML 后的字符数，界面按「标题/摘要 + 字数」预览，不把整篇 HTML 上屏。 */
export interface PostVersionDetail extends PostVersionRow {
  coverUrl: string
  content: string
  words: number
  published: boolean
  publishedAt?: string | null
}

/** 新闻版本历史（不含正文），新→旧，最多 50 条；弹窗打开时才拉。 */
export function usePostVersions(postId?: string) {
  return useQuery({
    queryKey: [...KEY, 'post-versions', postId],
    queryFn: () => api.get<PostVersionRow[]>(`${BASE}/posts/${postId}/versions`),
    enabled: !!postId,
  })
}

/** 取单个历史版本详情（含正文与字数）；silent：错误由弹窗 toast。 */
export function fetchPostVersion(postId: string, version: number): Promise<PostVersionDetail> {
  return api.get<PostVersionDetail>(`${BASE}/posts/${postId}/versions/${version}`, undefined, { silent: true })
}

/** 回滚到历史版本：快照走一次正常保存，当前内容先归档、版本继续递增，回滚可再回滚。 */
export function useRollbackPost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      api.post<SitePost>(`${BASE}/posts/${v.id}/versions/${v.version}/rollback`, {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] })
      // 回滚会新增一条版本行：弹窗里的历史列表要跟着刷（v{旧最大+1} 就是被顶掉的当前内容）。
      void qc.invalidateQueries({ queryKey: [...KEY, 'post-versions'] })
      invalidateTaxonomies(qc)
    },
  })
}

/** 官网「在线留言」：匿名投稿，后台按状态跟进（只读不改正文）。 */
export type MessageStatus = 'new' | 'read' | 'handled'

/** 列表筛选与批量操作共用的状态顺序；与后端 MsgNew/MsgRead/MsgHandled 同源。 */
export const MESSAGE_STATUSES: MessageStatus[] = ['new', 'read', 'handled']

export interface SiteMessage {
  id: string
  name: string
  email: string
  phone: string
  content: string
  ip: string
  ua: string
  status: MessageStatus
  createdAt: string
  updatedAt: string
}

export interface MessageQuery {
  page: number
  pageSize: number
  keyword?: string
  /** 省略 = 全部状态 */
  status?: MessageStatus
}

export function useMessages(query: MessageQuery) {
  return useQuery({
    queryKey: [...KEY, 'messages', query],
    queryFn: () => api.get<Page<SiteMessage>>(`${BASE}/messages`, query),
    placeholderData: keepPreviousData,
  })
}

/** 未读留言数：route.tsx 把它注册成侧边栏徽标数据源（见 framework/layout/menuBadge.ts）。 */
export function fetchUnreadMessageCount(): Promise<number> {
  return api
    .get<{ count: number }>(`${BASE}/messages/unread-count`, undefined, { silent: true })
    .then((d) => d.count)
}

/**
 * 导出当前筛选下的全部留言（不带分页，上限见后端 exportMax），CSV 由前端生成；
 * silent：错误由页面捕获后 toast，不走全局报错。
 */
export function fetchMessagesForExport(query: Pick<MessageQuery, 'keyword' | 'status'>): Promise<SiteMessage[]> {
  return api.get<SiteMessage[]>(`${BASE}/messages/export`, { keyword: query.keyword, status: query.status }, { silent: true })
}

/** 批量改状态 / 删除都会影响未读数：把侧边栏徽标也刷一遍。 */
function useInvalidateMessages() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: [...KEY, 'messages'] })
    void qc.invalidateQueries({ queryKey: MENU_BADGES_KEY })
  }
}

/** 批量改状态：单条操作就是长度为 1 的 ids。updated 是服务端实际改动条数。 */
export function useSetMessageStatus() {
  const invalidate = useInvalidateMessages()
  return useMutation({
    mutationFn: ({ ids, status }: { ids: string[]; status: MessageStatus }) =>
      api.put<{ updated: number }>(`${BASE}/messages/status`, { ids, status }),
    onSuccess: invalidate,
  })
}

export function useDeleteMessages() {
  const invalidate = useInvalidateMessages()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/messages`, { ids: ids.join(',') }),
    onSuccess: invalidate,
  })
}

// ---- 产品中心 ----

/** 产品增删改都会挪动类目下的商品数，两份清单要跟着刷。 */
function invalidateProductTaxonomies(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: [...KEY, 'product-categories'] })
}

export function useProducts(query: ContentQuery) {
  return useQuery({
    queryKey: [...KEY, 'products', query],
    queryFn: () => api.get<Page<SiteProduct>>(`${BASE}/products`, query),
    placeholderData: keepPreviousData,
  })
}

export function useSaveProduct() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: ProductForm }) =>
      id ? api.put<SiteProduct>(`${BASE}/products/${id}`, data) : api.post<SiteProduct>(`${BASE}/products`, data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
      invalidateProductTaxonomies(qc)
    },
  })
}

export function useDeleteProduct() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/products/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
      invalidateProductTaxonomies(qc)
    },
  })
}

/** 批量上架/下线：单条操作就是长度为 1 的 ids。updated 是服务端实际改动条数。 */
export function useSetProductStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ ids, status }: { ids: string[]; status: 'online' | 'offline' }) =>
      api.put<{ updated: number }>(`${BASE}/products/status`, { ids, status }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'products'] }),
  })
}

export function useDeleteProducts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/products`, { ids: ids.join(',') }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
      invalidateProductTaxonomies(qc)
    },
  })
}

/** 签发草稿预览链接（24h 有效）：返回 /products/:slug?preview=<token>，新开标签页打开。 */
export function fetchProductPreview(id: string): Promise<string> {
  return api.get<{ url: string }>(`${BASE}/products/${id}/preview`, undefined, { silent: true }).then((d) => d.url)
}

/** 复制为草稿（对标阿里的「发布类似」）：内容/译文/类目带走，状态归草稿、同步信息清零。 */
export function useCopyProduct() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<SiteProduct>(`${BASE}/products/${id}/copy`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
      invalidateProductTaxonomies(qc)
    },
  })
}

/** 产品类目：产品表单与类目管理页共用一份平铺清单（前端组树）。 */
export function useProductCategories() {
  return useQuery({
    queryKey: [...KEY, 'product-categories'],
    queryFn: () => api.get<ProductCategoryNode[]>(`${BASE}/product-categories`),
  })
}

export function useSaveProductCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: ProductCategoryForm }) =>
      id
        ? api.put<ProductCategoryNode>(`${BASE}/product-categories/${id}`, data)
        : api.post<ProductCategoryNode>(`${BASE}/product-categories`, data),
    // 商品类目改名/移动后，产品列表的类目徽标带的是行内副本，两份都要刷
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'product-categories'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
    },
  })
}

export function useDeleteProductCategory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/product-categories/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: [...KEY, 'product-categories'] })
      void qc.invalidateQueries({ queryKey: [...KEY, 'products'] })
    },
  })
}

/** 拖拽排序 / 改上级：后端在一个事务里批量写 sort 与 parentId。 */
export function useReorderProductCategories() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (items: ProductCategoryOrderItem[]) => api.put<null>(`${BASE}/product-categories/reorder`, { items }),
    // 失败时 TreeTable 会回滚乐观数据并 toast，这里只保证最终与库一致
    onSettled: () => void qc.invalidateQueries({ queryKey: [...KEY, 'product-categories'] }),
  })
}
