import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { api, http } from '@/framework/request/http'
import { MENU_BADGES_KEY } from '@/framework/layout/menuBadge'
import { useLanguages as usePlatformLanguages } from '@/framework/i18n/useLanguages'
import type { EntitySchema, LocaleOption } from '@/framework/components/i18n'
import type { Page, PageQuery } from '@/framework/request/types'

/** 后端 entityViews 的键；新增内容类型时同步。 */
export type I18nEntity = 'setting' | 'nav' | 'feature' | 'page' | 'post' | 'product' | 'faq' | 'category' | 'product-category'

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

/** 办公地点（联系页地点卡 + 地图打点）：坐标经后台地图选点或手填。 */
export interface WebsiteOffice {
  name: string
  address: string
  lng: number
  lat: number
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
  /** 公开站主题 id（主题插件注册）；空 = 内置 default 主题。主题被卸载时官网自动回退 default */
  theme: string
  /** 官网默认语言（占根路径）：空 / auto = 跟随浏览器语言协商；具体 code = 该语言独占根路径。改完需重启 */
  defaultLang: string
  /** 展示币种（未选币种的访客看到的）：空 = 不启用换算，价格按商品各自币种直出 */
  defaultCurrency: string
  /** 汇率表 JSON 文本 {"USD":1,"EUR":0.92}：USD 恒为基准 1，键集合即头部可选币种 */
  currencyRates: string
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
  /** 产品在线购买开关：开启后有定价的产品详情页出「立即购买」，匿名访客下单走 pay 中台收款 */
  purchaseEnabled: boolean
  /** 办公地点列表（联系页地点卡 + 地图打点）；坐标经后台地图选点或手填 */
  offices: WebsiteOffice[]
  /** 新留言是否抄送一封到 contactEmail（SMTP 未配置时自动跳过） */
  messageNotifyEmail: boolean
  /** 留言是否必须先勾选「已阅读并同意隐私政策」 */
  contactConsent: boolean
  /** 被同意的政策地址：站内 /p/xxx 或完整 https:// 地址（开启时必填） */
  contactConsentUrl: string
  /** 成立年份：商详供应商卡的「实力档案」，0 = 不展示 */
  establishedYear: number
  /** 员工规模（演示 500+），空 = 不展示 */
  staffCount: string
  /** 年营收区间（如 US$50M - US$100M），空 = 不展示 */
  annualRevenue: string
  /** 主营市场（逗号/顿号分隔的自然语言），空 = 不展示 */
  mainMarkets: string
  /** 平均响应时长（如 ≤4h），空 = 不展示 */
  responseTime: string
  i18nJson: string
  updatedAt: string
}

export type SiteSettingsForm = Omit<SiteSettings, 'id' | 'updatedAt'>

export interface NavItem {
  id: string
  /** 上级导航 ID；"0" = 顶级（雪花 ID 用字符串避免丢精度） */
  parentId: string
  label: string
  /** 二级面板里跟在标题下的一句话说明；为空则该子项落在右侧链接列 */
  description: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface NavForm {
  parentId: string
  label: string
  description: string
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
  /** 非默认语言的栏目名译文；空 = 各语言回退原文 */
  i18nJson: string
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
  i18nJson: string
}

export interface TagForm {
  name: string
  /** 留空由后端按 name 派生；改名时回传原 slug，已分享的 /news?tag= 链接才不会断 */
  slug: string
}

export interface ContentQuery extends PageQuery {
  /** 逗号分隔多值：?status=published,draft */
  status?: string
  /** 语言码 CSV 多值：筛「缺任一所选语言译文」的条目（不是缺某个字段，那看列表里的 done/total 徽标） */
  missing?: string
  /** 新闻分类筛选（只对新闻列表生效），兼容旧单值 */
  categoryId?: string
  /** 分类 CSV 多值 */
  categoryIds?: string
}

/** 筛选草稿（页面 state）：枚举字段是多选数组，搜索时用 toContentQuery 序列化成 CSV */
export interface ContentDraft {
  keyword?: string
  status?: string[]
  missing?: string[]
  categoryIds?: string[]
}

export function toContentQuery(d: ContentDraft): ContentQuery {
  return {
    keyword: d.keyword,
    status: d.status?.length ? d.status.join(',') : undefined,
    missing: d.missing?.length ? d.missing.join(',') : undefined,
    categoryIds: d.categoryIds?.length ? d.categoryIds.join(',') : undefined,
  }
}

/** 商品动态属性的一条键值对（对标阿里的类目属性）。 */
export interface ProductSpec {
  label: string
  value: string
}

/** 产品状态机：draft（草稿）/ online（上架，公开站唯一可见）/ offline（下架）。 */
export type ProductStatus = 'draft' | 'online' | 'offline'

export interface SiteProduct {
  id: string
  slug: string
  title: string
  /** 搜索关键词（最多 3 个，空格分隔） */
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
  /** 运费金额（与价格同币种）；0 = 未报价 */
  freightAmount: number
  port: string
  /** 付款方式（如 "T/T, L/C"），详情页「运输」块展示 */
  paymentTerms: string
  /** 动态属性键值对的 JSON 文本 */
  specsJson: string
  /** 认证资质（JSON 数组文本，如 ["CE","UL 1973"]）：详情页渲染成认证徽章排 */
  certificationsJson?: string
  status: ProductStatus
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
  freightAmount: number
  port: string
  paymentTerms: string
  specs: ProductSpec[]
  /** 认证资质名称清单；undefined = 本次不改（保留原值），[] = 清空 */
  certifications?: string[]
  /** 所属类目的完整 ID 列表；空 = 清空类目 */
  categoryIds: string[]
  i18nJson: string
  publishedJson: string
  noindex: boolean
  author: string
}

/**
 * 产品类目：组织维度，支持最多 3 级树形层级。
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
  /** 非默认语言的类目名译文；空 = 各语言回退原文 */
  i18nJson: string
}

export interface ProductCategoryForm {
  parentId: string
  title: string
  slug: string
  sort: number
  i18nJson: string
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

/** 一键拉取汇率：只回填编辑器、不写库；管理员确认后随站点设置一起保存。 */
export interface CurrencyRatesFetchResult {
  rates: string
  updated: number
  source: string
}

export function useFetchCurrencyRates() {
  return useMutation({
    mutationFn: (rates: string) => api.post<CurrencyRatesFetchResult>(`${BASE}/currency-rates/fetch`, { rates }),
  })
}

/** 主题清单条目：default 恒在最前；hasDemo = 主题插件带演示内容可一键导入。 */
export interface ThemeInfo {
  id: string
  label: string
  version: string
  hasDemo: boolean
  active: boolean
}

export function useThemes() {
  return useQuery({
    queryKey: [...KEY, 'themes'],
    queryFn: () => api.get<{ theme: string; items: ThemeInfo[] }>(`${BASE}/themes`),
  })
}

/** 导入主题演示内容：insert-only（按 slug/href/标题判重），重复导入不产生重复行。 */
export interface DemoReport {
  settings: number
  navs: number
  features: number
  categories: number
  pages: number
  posts: number
  productCategories: number
  products: number
  skipped: number
}

export function useSeedThemeDemo() {
  return useMutation({
    mutationFn: (id: string) => api.post<DemoReport>(`${BASE}/themes/${encodeURIComponent(id)}/seed-demo`),
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

// ---- 常见问题（FAQ） ----

/** FAQ 列表项：问题必填、答案纯文本（公开页保留换行），sort 小的在前。 */
export interface FaqItem {
  id: string
  question: string
  answer: string
  sort: number
  enabled: boolean
  /** 非默认语言的问题/答案译文；空 = 回退原文（非默认语言不展示） */
  i18nJson: string
}

export interface FaqQuery extends PageQuery {
  /** "true" | "false"；省略 = 全部 */
  enabled?: string
}

/** FAQ 列表（分页 + 关键词/启用状态过滤 + 表头排序）。 */
export function useFaqs(query: FaqQuery) {
  return useQuery({
    queryKey: [...KEY, 'faqs', query],
    queryFn: () => api.get<Page<FaqItem>>(`${BASE}/faqs`, query),
    placeholderData: keepPreviousData,
  })
}

/** 创建 FAQ 导出任务：按当前筛选提交到导出中心；进度与下载见导出中心。 */
export function createFaqExportJob(query: Pick<FaqQuery, 'keyword' | 'enabled'>) {
  return api.post<{ id: string }>(
    `${BASE}/faqs/export-async`,
    null,
    { params: { keyword: query.keyword ?? '', enabled: query.enabled ?? '' } },
  )
}

/** 批量删除 FAQ。 */
export function useDeleteFaqs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/faqs`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'faqs'] }),
  })
}

export function useSaveFaq() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...data }: { id?: string; question: string; answer: string; sort: number; enabled: boolean; i18nJson?: string }) =>
      id ? api.put<FaqItem>(`${BASE}/faqs/${id}`, data) : api.post<FaqItem>(`${BASE}/faqs`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'faqs'] }),
  })
}

export function useDeleteFaq() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/faqs/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'faqs'] }),
  })
}

/** FAQ 导入源标识（通用导入中心，与 PRODUCT_IMPORT_SOURCE 同一注册机制）。 */
export const FAQ_IMPORT_SOURCE = 'website_faqs'

/** 拖拽排序：ids 按目标顺序排列，服务端写回 sort（=序号×10）。失败抛错，表格自行回滚。 */
export function useReorderFaqs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.put<null>(`${BASE}/faqs/reorder`, { ids }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'faqs'] }),
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
 * 创建新闻导出任务：按当前筛选提交到导出中心，立即返回任务 ID；
 * 进度与下载见导出中心，创建失败由 http 拦截器统一弹出原因。
 */
export function createPostExportJob(query: Pick<ContentQuery, 'keyword' | 'status' | 'categoryIds'>) {
  return api.post<{ id: string }>(
    `${BASE}/posts/export-async`,
    null,
    { params: { keyword: query.keyword ?? '', status: query.status ?? '', categoryIds: query.categoryIds ?? '' } },
  )
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

/** 询盘附件：下载走官网公开的 token 链接，后台直接复用（token 不透明且只在授权接口里出现）。 */
export interface MessageAttachment {
  token: string
  name: string
  mime: string
  size: number
}

export interface SiteMessage {
  id: string
  name: string
  email: string
  phone: string
  content: string
  ip: string
  ua: string
  status: MessageStatus
  /** 询盘来源商品 ID（0/空 = 从联系页提交的普通留言） */
  productId?: string
  /** 来源商品精简信息（后端按 productId 批量补齐，含草稿） */
  sourceProduct?: SuggestedProduct
  /** 买家是否勾选「同意与供应商共享名片信息」 */
  cardConsent?: boolean
  /** 超过 24 小时仍未处理（后端按状态与时间现算） */
  overdue?: boolean
  /** 询盘弹窗上传的附件；来自联系页的普通留言为空 */
  attachments?: MessageAttachment[]
  createdAt: string
  updatedAt: string
}

export interface MessageQuery {
  page: number
  pageSize: number
  keyword?: string
  /** 逗号分隔多值：?status=new,read；省略 = 全部状态 */
  status?: string
  /** 表头排序字段（后端白名单：status/created_at），空 = 默认 created_at DESC */
  sort?: string
  desc?: boolean
}

export function useMessages(query: MessageQuery) {
  return useQuery({
    queryKey: [...KEY, 'messages', query],
    queryFn: () => api.get<Page<SiteMessage>>(`${BASE}/messages`, query),
    placeholderData: keepPreviousData,
  })
}

/** 超时询盘的同品类推荐：只在后台打开详情时按需请求，不随列表一起拉。 */
export interface SuggestedProduct {
  id: string
  title: string
  slug: string
  currency: string
  priceMin: number
  priceMax: number
  moq: number
  coverUrl: string
  path: string
}

export function useMessageSuggestions(id: string | null) {
  return useQuery({
    queryKey: [...KEY, 'messages', 'suggest', id],
    queryFn: () => api.get<{ suggested: SuggestedProduct[] }>(`${BASE}/messages/suggest/${id}`),
    enabled: !!id,
    staleTime: 60_000,
  })
}

/** 未读留言数：route.tsx 把它注册成侧边栏徽标数据源（见 framework/layout/menuBadge.ts）。 */
export function fetchUnreadMessageCount(): Promise<number> {
  return api
    .get<{ count: number }>(`${BASE}/messages/unread-count`, undefined, { silent: true })
    .then((d) => d.count)
}

/**
 * 创建留言导出任务：按当前筛选提交到导出中心，立即返回任务 ID；
 * 进度与下载见导出中心，创建失败由 http 拦截器统一弹出原因。
 */
export function createMessageExportJob(query: Pick<MessageQuery, 'keyword' | 'status'>) {
  return api.post<{ id: string }>(
    `${BASE}/messages/export-async`,
    null,
    { params: { keyword: query.keyword ?? '', status: query.status ?? '' } },
  )
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

/** 创建商品导出任务（同样的筛选参数），立即返回任务 ID；进度与下载见导出中心 */
export function createProductExportJob(
  query: Pick<ContentQuery, 'keyword' | 'status' | 'categoryIds'> & { ids?: string[] },
) {
  return api.post<{ id: string }>(`${BASE}/products/export-async`, {
    keyword: query.keyword ?? '',
    status: query.status ?? '',
    categoryIds: query.categoryIds ?? '',
    ids: query.ids ?? [],
  })
}

/**
 * 产品在导入中心（plugins/imports）的来源标识，与后端 SourceProducts 同源；
 * 文件解析/模板/逐行报错/台账都由导入中心负责，这里只是它的一个前端调用方。
 */
export const PRODUCT_IMPORT_SOURCE = 'website_products'

/** 导入中心任务视图（与 plugins/imports/api.ts 的 ImportJob 同形，只取弹窗用到的字段）。 */
export interface ProductImportJob {
  id: string
  status: 'pending' | 'running' | 'done' | 'failed' | 'cancelled'
  totalRows: number
  okRows: number
  failRows: number
  /** 失败明细只随单条任务返回；行数少时同步完成直接带在结果里。 */
  errors?: { row: number; key: string; message: string }[]
}

/** 上传文件执行产品导入：行数少时同步完成，多时后台跑（用 fetchProductImportJob 轮询）。 */
export function useRunProductImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ file, source = PRODUCT_IMPORT_SOURCE }: { file: File; source?: string }) => {
      const fd = new FormData()
      fd.append('source', source)
      fd.append('file', file)
      return api.post<ProductImportJob>('/plugins/imports', fd)
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'products'] }),
  })
}

/** 轮询导入任务（silent：轮询失败不打断界面，下一轮再试）。 */
export function fetchProductImportJob(id: string) {
  return api.get<ProductImportJob>(`/plugins/imports/${id}`, undefined, { silent: true })
}

/** 下载产品导入模板（导入中心按来源生成表头与示例行）。 */
export async function fetchProductImportTemplate(source = PRODUCT_IMPORT_SOURCE): Promise<Blob> {
  const res = await http.get<unknown>(
    '/plugins/imports/template',
    { params: { source, format: 'csv' }, responseType: 'blob' },
  )
  // 真实后端返回文件（Blob）；mock 阶段返回 CSV 文本
  return res.data instanceof Blob ? res.data : new Blob([String(res.data)], { type: 'text/csv;charset=utf-8' })
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

// ---- 站点订单（官网商品页在线购买） ----

export interface SiteOrder {
  id: string
  orderNo: string
  productTitle: string
  qty: number
  currency: string
  amountMinor: number
  payCurrency: string
  payAmountMinor: number
  buyerEmail: string
  buyerPhone: string
  status: 'pending' | 'paid' | 'failed' | 'closed'
  createdAt: string
}

export interface SiteOrderQuery extends PageQuery {
  /** 逗号分隔多值：?status=pending,paid；省略 = 全部状态 */
  status?: string
}

/** 站点订单列表（管理端业务记录；资金状态在支付中台按业务单号 SO… 对账）。 */
export function useSiteOrders(query: SiteOrderQuery) {
  return useQuery({
    queryKey: [...KEY, 'site-orders', query],
    queryFn: () => api.get<Page<SiteOrder>>(`${BASE}/site-orders`, query),
    placeholderData: keepPreviousData,
  })
}

/** 创建订单导出任务：按当前筛选提交到导出中心；进度与下载见导出中心。 */
export function createSiteOrderExportJob(query: Pick<SiteOrderQuery, 'keyword' | 'status'>) {
  return api.post<{ id: string }>(
    `${BASE}/site-orders/export-async`,
    null,
    { params: { keyword: query.keyword ?? '', status: query.status ?? '' } },
  )
}

/** 批量删除订单（业务侧记录，不影响 pay 中台资金流水）。 */
export function useDeleteSiteOrders() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) => api.del<{ deleted: number }>(`${BASE}/site-orders`, { ids: ids.join(',') }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'site-orders'] }),
  })
}

// ---- 访问统计（第一方轻量：官网公开页浏览行的聚合） ----

/** 统计窗口天数（后端白名单：7/14/30） */
export type AnalyticsDays = 7 | 14 | 30

export interface AnalyticsPoint {
  day: string
  views: number
  sessions: number
  inquiries: number
}

export interface AnalyticsPathRow {
  path: string
  views: number
}

export interface AnalyticsRefRow {
  kind: 'direct' | 'search' | 'social' | 'external' | 'internal'
  host: string
  sessions: number
}

/** 设备画像 / 来源大类汇总的一行：key 是浏览器/系统/来源类目，sessions 去重会话 */
export interface AnalyticsDeviceRow {
  key: string
  sessions: number
}

export interface AnalyticsData {
  days: AnalyticsDays
  views: number
  sessions: number
  inquiries: number
  /** 近 5 分钟去重会话（近似实时在线） */
  realtime: number
  /** 跳出率 0-100：只有一次浏览的会话占比（口径对齐 Plausible） */
  bounceRate: number
  /** 平均访问时长（秒）：会话首末浏览间隔均值，单浏览会话不计入 */
  avgDuration: number
  trend: AnalyticsPoint[]
  topPaths: AnalyticsPathRow[]
  /** 入口页：每个会话的首个浏览页 */
  entryPages: AnalyticsPathRow[]
  referrers: AnalyticsRefRow[]
  /** 来源五大类的会话份额（与 referrers 明细互补，跨 host 会话不可加） */
  refKindStats: AnalyticsDeviceRow[]
  browsers: AnalyticsDeviceRow[]
  oses: AnalyticsDeviceRow[]
}

/** 访问统计总览；60s 轮询让「实时访客」名实相符（其余指标顺带刷新）。
 * 切换窗口（7/14/30 天）期间保留上一窗口数据原地显示（keepPreviousData）——
 * 否则 key 一变 data 变 undefined，整页 KPI 归零 + 图表拆成骨架屏再弹回，肉眼可见闪一下。 */
export function useAnalytics(days: AnalyticsDays) {
  return useQuery({
    queryKey: [...KEY, 'analytics', days],
    queryFn: () => api.get<AnalyticsData>(`${BASE}/analytics?days=${days}`),
    placeholderData: keepPreviousData,
    refetchInterval: 60_000,
  })
}
